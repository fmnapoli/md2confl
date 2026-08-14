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
	"strings"
	"sync"
	"testing"

	"github.com/fmnapoli/md2confl/parser"
)

// fakeServerPage is an in-memory Confluence Server/DC page.
type fakeServerPage struct {
	ID      string
	Title   string
	Body    string
	Version int
	Parent  string
}

// fakeConfluenceServer emulates the subset of the Confluence Server/DC REST
// API v1 used by ServerClient: find-by-title, create, get and update page.
type fakeConfluenceServer struct {
	mu      sync.Mutex
	pages   map[string]*fakeServerPage
	counter int
	baseURL string

	// searchHandler, when set, replaces the find-by-title endpoint. Tests use
	// it to emulate a WAF/proxy blocking GET /rest/api/content with 403.
	searchHandler http.HandlerFunc
}

func newFakeConfluenceServer(t *testing.T) (*httptest.Server, *fakeConfluenceServer) {
	t.Helper()
	f := &fakeConfluenceServer{pages: make(map[string]*fakeServerPage)}
	ts := httptest.NewServer(http.HandlerFunc(f.handle))
	f.baseURL = ts.URL
	t.Cleanup(ts.Close)
	return ts, f
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

func (f *fakeConfluenceServer) encode(w http.ResponseWriter, p *fakeServerPage) {
	_ = json.NewEncoder(w).Encode(map[string]any{
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
	})
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
				"_links":    map[string]any{"webui": "/display/TEST/" + p.ID, "base": f.baseURL},
			})
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"results": results})

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
			ID:      fmt.Sprintf("page-%d", f.counter),
			Title:   req.Title,
			Body:    req.Body.Storage.Value,
			Version: 1,
		}
		if len(req.Ancestors) > 0 {
			p.Parent = req.Ancestors[0].ID
		}
		f.pages[p.ID] = p
		cp := *p
		f.mu.Unlock()

		f.encode(w, &cp)

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
			p.Body = req.Body.Storage.Value
			p.Version = req.Version.Number
		}
		cp := *p
		f.mu.Unlock()
		f.encode(w, &cp)

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
