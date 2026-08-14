// Copyright 2026 md2confl contributors
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"

	"github.com/fmnapoli/md2confl/parser"
)

// fakeServerPage is an in-memory Confluence Server/DC page.
type fakeServerPage struct {
	ID         string
	Title      string
	Body       string
	Version    int
	Parent     string
	Properties map[string]fakeProperty
}

// fakeProperty is a content property stored against a page.
type fakeProperty struct {
	Value   json.RawMessage
	Version int
}

// fakeConfluenceServer emulates the subset of the Confluence Server/DC REST
// API v1 used by ServerClient: find-by-title, create, get and update page, and
// content properties.
type fakeConfluenceServer struct {
	mu         sync.Mutex
	pages      map[string]*fakeServerPage
	counter    int
	macroCount int
	baseURL    string

	// searchHandler, when set, replaces the find-by-title endpoint. Tests use
	// it to emulate a WAF/proxy blocking GET /rest/api/content with 403.
	searchHandler http.HandlerFunc

	// dropProperties makes the server accept a content property write and
	// keep nothing, emulating an instance that discards tool metadata — the
	// failure mode that an HTML comment in the body hit on the real TDN.
	dropProperties bool

	// failPropertySets rejects creating or updating a content property while
	// still allowing it to be deleted, emulating a run that updates the body
	// and then loses the metadata write (5xx, a blocked endpoint, or the
	// process dying in between).
	failPropertySets bool

	// failPropertyDeletes rejects removing a content property, which is what
	// invalidates the previous digest before the body is rewritten.
	failPropertyDeletes bool

	// failPropertyGets makes the next N reads of a content property fail with
	// 403, emulating a one-off error on the read-back that confirms the digest
	// was persisted. 403 is the one status the client does not retry, so the
	// failure reaches the caller instead of being absorbed.
	failPropertyGets int
}

func newFakeConfluenceServer(t *testing.T) (*httptest.Server, *fakeConfluenceServer) {
	t.Helper()
	f := &fakeConfluenceServer{pages: make(map[string]*fakeServerPage)}
	ts := httptest.NewServer(http.HandlerFunc(f.handle))
	f.baseURL = ts.URL
	t.Cleanup(ts.Close)
	return ts, f
}

// macroIDRegex encontra a abertura de uma macro sem ac:macro-id.
var macroIDRegex = regexp.MustCompile(`<ac:structured-macro ac:name="([^"]*)"`)

// htmlCommentRegex encontra um comentário HTML.
var htmlCommentRegex = regexp.MustCompile(`(?s)<!--.*?-->`)

// sanitizeStorage reproduz o que o Confluence Server faz com o Storage Format
// que recebe, medido contra o TDN em ago/2026 numa página publicada por esta
// ferramenta: comentários HTML somem, macros ganham ac:schema-version e um
// ac:macro-id gerado no servidor, e caracteres não-ASCII viram entidades.
//
// Sem isso o fake devolve exatamente o que recebeu, e qualquer mecanismo de
// idempotência baseado em comparar corpos — ou em esconder um marcador dentro
// do corpo — passa no teste e falha em produção.
func (f *fakeConfluenceServer) sanitizeStorage(body string) string {
	body = htmlCommentRegex.ReplaceAllString(body, "")
	body = macroIDRegex.ReplaceAllStringFunc(body, func(m string) string {
		sub := macroIDRegex.FindStringSubmatch(m)
		f.macroCount++
		return fmt.Sprintf(`<ac:structured-macro ac:name=%q ac:schema-version="1" ac:macro-id="fake-%d"`,
			sub[1], f.macroCount)
	})
	var sb strings.Builder
	for _, r := range body {
		if r > 127 {
			fmt.Fprintf(&sb, "&#%d;", r)
			continue
		}
		sb.WriteRune(r)
	}
	return sb.String()
}

// pageByTitle returns the stored page with the given title, or nil.
func (f *fakeConfluenceServer) pageByTitle(title string) *fakeServerPage {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, p := range f.pages {
		if p.Title == title {
			cp := *p
			return &cp
		}
	}
	return nil
}

func (f *fakeConfluenceServer) encode(w http.ResponseWriter, p *fakeServerPage, expand string) {
	payload := map[string]any{
		"id":    p.ID,
		"type":  "page",
		"title": p.Title,
		"body": map[string]any{
			"storage": map[string]any{"value": p.Body, "representation": "storage"},
		},
		"version":   map[string]any{"number": p.Version},
		"ancestors": []map[string]any{{"id": p.Parent}},
		"_links": map[string]any{
			"webui": "/display/TEST/" + p.ID,
			"base":  f.baseURL,
		},
	}
	// expand=metadata.properties.<key>, como na API v1.
	if key, ok := propertyExpandKey(expand); ok {
		props := map[string]any{}
		if prop, found := p.Properties[key]; found {
			props[key] = map[string]any{
				"key":     key,
				"value":   prop.Value,
				"version": map[string]any{"number": prop.Version},
			}
		}
		payload["metadata"] = map[string]any{"properties": props}
	}
	_ = json.NewEncoder(w).Encode(payload)
}

// propertyExpandKey extrai a chave pedida em expand=metadata.properties.<key>.
func propertyExpandKey(expand string) (string, bool) {
	for _, part := range strings.Split(expand, ",") {
		if key, found := strings.CutPrefix(part, "metadata.properties."); found {
			return key, true
		}
	}
	return "", false
}

// handleProperty serves /rest/api/content/{id}/property/{key}.
func (f *fakeConfluenceServer) handleProperty(w http.ResponseWriter, r *http.Request, pageID, key string) {
	f.mu.Lock()
	defer f.mu.Unlock()

	p, ok := f.pages[pageID]
	if !ok {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}

	switch r.Method {
	case http.MethodGet:
		if f.failPropertyGets > 0 {
			f.failPropertyGets--
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		prop, found := p.Properties[key]
		if !found {
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"statusCode":404,"message":"No content property found"}`))
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"key": key, "value": prop.Value, "version": map[string]any{"number": prop.Version},
		})

	case http.MethodPost, http.MethodPut:
		if f.failPropertySets {
			http.Error(w, "boom", http.StatusInternalServerError)
			return
		}
		_, exists := p.Properties[key]
		// O Confluence real rejeita POST em chave existente: criar e atualizar
		// são verbos diferentes. Aceitar como upsert esconderia o caso em que a
		// ferramenta escreve sem antes invalidar o valor anterior.
		if r.Method == http.MethodPost && exists {
			w.WriteHeader(http.StatusConflict)
			_, _ = w.Write([]byte(`{"statusCode":409,"message":"Property already exists"}`))
			return
		}
		body, _ := io.ReadAll(r.Body)
		var req struct {
			Value   json.RawMessage `json:"value"`
			Version struct {
				Number int `json:"number"`
			} `json:"version"`
		}
		_ = json.Unmarshal(body, &req)
		version := req.Version.Number
		if version == 0 {
			version = 1
		}
		// A instância aceita a escrita e não guarda nada.
		if !f.dropProperties {
			if p.Properties == nil {
				p.Properties = map[string]fakeProperty{}
			}
			p.Properties[key] = fakeProperty{Value: req.Value, Version: version}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"key": key, "value": req.Value, "version": map[string]any{"number": version},
		})

	case http.MethodDelete:
		if f.failPropertyDeletes {
			http.Error(w, "boom", http.StatusInternalServerError)
			return
		}
		if _, exists := p.Properties[key]; !exists {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		delete(p.Properties, key)
		w.WriteHeader(http.StatusNoContent)

	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (f *fakeConfluenceServer) handle(w http.ResponseWriter, r *http.Request) {
	switch {
	// Comala Workflows approval — not configured.
	case r.Method == http.MethodPatch && strings.Contains(r.URL.Path, "/approvals/approve"):
		http.Error(w, "no workflow", http.StatusNotFound)

	// FindByTitle
	case r.Method == http.MethodGet && r.URL.Path == "/rest/api/content":
		if f.searchHandler != nil {
			f.searchHandler(w, r)
			return
		}
		title := r.URL.Query().Get("title")
		results := []map[string]any{}
		if p := f.pageByTitle(title); p != nil {
			results = append(results, map[string]any{
				"id":    p.ID,
				"title": p.Title,
				"body": map[string]any{
					"storage": map[string]any{"value": p.Body},
				},
				"version":   map[string]any{"number": p.Version},
				"ancestors": []map[string]any{{"id": p.Parent}},
				// Como na API real, o _links do resultado traz só o caminho:
				// o "base" fica no envelope da busca.
				"_links": map[string]any{"webui": "/display/TEST/" + p.ID},
			})
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"results": results,
			"_links":  map[string]any{"base": f.baseURL},
		})

	// CreatePage
	case r.Method == http.MethodPost && r.URL.Path == "/rest/api/content":
		body, _ := io.ReadAll(r.Body)
		var req struct {
			Title string `json:"title"`
			Body  struct {
				Storage struct {
					Value string `json:"value"`
				} `json:"storage"`
			} `json:"body"`
			Ancestors []struct {
				ID string `json:"id"`
			} `json:"ancestors"`
		}
		_ = json.Unmarshal(body, &req)

		f.mu.Lock()
		f.counter++
		p := &fakeServerPage{
			ID:      fmt.Sprintf("%d", 1050000000+f.counter),
			Title:   req.Title,
			Body:    f.sanitizeStorage(req.Body.Storage.Value),
			Version: 1,
		}
		if len(req.Ancestors) > 0 {
			p.Parent = req.Ancestors[0].ID
		}
		f.pages[p.ID] = p
		cp := *p
		f.mu.Unlock()

		f.encode(w, &cp, "")

	// Content properties
	case strings.HasPrefix(r.URL.Path, "/rest/api/content/") && strings.Contains(r.URL.Path, "/property/"):
		rest := strings.TrimPrefix(r.URL.Path, "/rest/api/content/")
		id, key, _ := strings.Cut(rest, "/property/")
		f.handleProperty(w, r, id, key)

	// GetPage / UpdatePage
	case strings.HasPrefix(r.URL.Path, "/rest/api/content/"):
		id := strings.TrimPrefix(r.URL.Path, "/rest/api/content/")
		f.mu.Lock()
		p, ok := f.pages[id]
		if !ok {
			f.mu.Unlock()
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		if r.Method == http.MethodPut {
			body, _ := io.ReadAll(r.Body)
			var req struct {
				Title string `json:"title"`
				Body  struct {
					Storage struct {
						Value string `json:"value"`
					} `json:"storage"`
				} `json:"body"`
				Version struct {
					Number int `json:"number"`
				} `json:"version"`
			}
			_ = json.Unmarshal(body, &req)
			p.Title = req.Title
			p.Body = f.sanitizeStorage(req.Body.Storage.Value)
			p.Version = req.Version.Number
		}
		cp := *p
		f.mu.Unlock()
		f.encode(w, &cp, r.URL.Query().Get("expand"))

	default:
		http.Error(w, "not found", http.StatusNotFound)
	}
}

// consumerDocs mirrors the layout of the tcloud-worker repository: a root
// README.md published as a single-file document plus a docs/tdn/ directory
// published as a page tree, with relative Markdown links between them.
func consumerDocs() map[string]string {
	return map[string]string{
		"README.md": "# TCloud Worker\n\n" +
			"See [Arquitetura do TCloud Worker](docs/tdn/architecture/architecture.md).\n\n" +
			"See [Configuration](./docs/tdn/guides/configuration.md).\n\n" +
			"See [Providers](docs/tdn/guides/configuration.md#providers).\n\n" +
			"See [the chart](charts/tcloud-worker/values.yaml).\n",
		"docs/tdn/README.md":                    "# TDN Docs\n\nIndex.\n",
		"docs/tdn/architecture/README.md":       "# Architecture\n\nIndex.\n",
		"docs/tdn/architecture/architecture.md": "# Arquitetura do TCloud Worker\n\nBack to [root](../../../README.md).\n",
		"docs/tdn/guides/README.md":             "# Guides\n\nIndex.\n",
		"docs/tdn/guides/configuration.md":      "# Configuration\n\nContent.\n",
	}
}

func serverConfig(baseURL string) string {
	return `url: ` + baseURL + `
space: TEST
email: user@example.com
server: true
force: true
write-marker: false
repo-url: https://github.com/acme/tcloud-worker/blob/main/
documents:
  - input: README.md
    title: "TCloud Worker"

  - input: docs/tdn/
    parent-id: "root-parent"
`
}

// TestResolveInterDocLinksServer_ConfigMode reproduces the production scenario:
// server mode with two config documents (a single file plus a directory).
// Relative Markdown links from the root README must be rewritten to Confluence
// page URLs instead of being published as raw relative paths.
func TestResolveInterDocLinksServer_ConfigMode(t *testing.T) {
	dir := t.TempDir()
	ts, fake := newFakeConfluenceServer(t)

	cfgPath := writeConfigAndDocs(t, dir, serverConfig(ts.URL), consumerDocs())
	// findRepoRoot() needs a .git directory to anchor the repo-url fallback.
	if err := os.MkdirAll(filepath.Join(dir, ".git"), 0755); err != nil {
		t.Fatal(err)
	}

	t.Chdir(dir)
	t.Setenv("CONFLUENCE_TOKEN", "fake-token")

	var stdout, stderr strings.Builder
	if code := Run([]string{"--config", cfgPath}, "test", &stdout, &stderr); code != 0 {
		t.Fatalf("expected exit code 0, got %d; stderr: %s", code, stderr.String())
	}

	readme := fake.pageByTitle("TCloud Worker")
	if readme == nil {
		t.Fatal("README page was not published")
	}
	arch := fake.pageByTitle("Arquitetura do TCloud Worker")
	if arch == nil {
		t.Fatal("architecture page was not published")
	}
	conf := fake.pageByTitle("Configuration")
	if conf == nil {
		t.Fatal("configuration page was not published")
	}

	archURL := ts.URL + "/display/TEST/" + arch.ID
	confURL := ts.URL + "/display/TEST/" + conf.ID

	for _, want := range []string{
		`href="` + archURL + `"`,
		// "./" prefixed link.
		`href="` + confURL + `"`,
		// Link carrying a fragment.
		`href="` + confURL + `#providers"`,
		// Non-Markdown target: falls back to the repository URL.
		`href="https://github.com/acme/tcloud-worker/blob/main/charts/tcloud-worker/values.yaml"`,
	} {
		if !strings.Contains(readme.Body, want) {
			t.Errorf("README page missing %s\nbody: %s", want, readme.Body)
		}
	}

	if strings.Contains(readme.Body, `href="docs/`) || strings.Contains(readme.Body, `href="./docs/`) {
		t.Errorf("README page still contains raw relative links\nbody: %s", readme.Body)
	}

	// The reverse direction (child doc → root README) must resolve too.
	readmeURL := ts.URL + "/display/TEST/" + readme.ID
	if !strings.Contains(arch.Body, `href="`+readmeURL+`"`) {
		t.Errorf("architecture page missing link back to README (%s)\nbody: %s", readmeURL, arch.Body)
	}
}

// TestResolveInterDocLinksServer_UnchangedSingleFile covers the production
// regression: a single-file document whose page already holds the exact
// unresolved HTML (published by an older release) is skipped as unchanged.
// The skip must still register the document so the second pass can resolve
// its links — otherwise the page stays broken forever, because its content
// never changes and it is skipped on every subsequent run.
func TestResolveInterDocLinksServer_UnchangedSingleFile(t *testing.T) {
	dir := t.TempDir()
	ts, fake := newFakeConfluenceServer(t)

	docs := consumerDocs()
	// Marker makes the single-file path take the pageID branch, like in production.
	docs["README.md"] = "<!-- confluence-page-id: 1032133041 -->\n" + docs["README.md"]

	cfgPath := writeConfigAndDocs(t, dir, serverConfig(ts.URL), docs)

	// Seed the page with the raw, unresolved HTML that an older md2confl
	// release would have published.
	rawHTML, err := parser.ConvertToStorageFormat([]byte(docs["README.md"]))
	if err != nil {
		t.Fatal(err)
	}
	fake.mu.Lock()
	fake.pages["1032133041"] = &fakeServerPage{
		ID: "1032133041", Title: "TCloud Worker", Body: rawHTML, Version: 7,
	}
	fake.mu.Unlock()

	t.Chdir(dir)
	t.Setenv("CONFLUENCE_TOKEN", "fake-token")

	var stdout, stderr strings.Builder
	if code := Run([]string{"--config", cfgPath}, "test", &stdout, &stderr); code != 0 {
		t.Fatalf("expected exit code 0, got %d; stderr: %s", code, stderr.String())
	}

	if !strings.Contains(stderr.String(), "Skipped (unchanged)") {
		t.Fatalf("expected the README to be skipped as unchanged; stderr: %s", stderr.String())
	}

	readme := fake.pageByTitle("TCloud Worker")
	if readme == nil {
		t.Fatal("README page disappeared")
	}
	arch := fake.pageByTitle("Arquitetura do TCloud Worker")
	if arch == nil {
		t.Fatal("architecture page was not published")
	}

	archURL := ts.URL + "/display/TEST/" + arch.ID
	if !strings.Contains(readme.Body, `href="`+archURL+`"`) {
		t.Errorf("skipped README page kept unresolved links, expected %s\nbody: %s", archURL, readme.Body)
	}
}
