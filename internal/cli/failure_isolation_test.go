// Copyright 2026 md2confl contributors
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/fmnapoli/md2confl/confluence"
)

// blockSearch makes the fake server reject find-by-title with the HTML 403 the
// TDN proxy returns, while access by page ID keeps working.
func blockSearch(fake *fakeConfluenceServer) {
	fake.searchHandler = func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte("<!DOCTYPE html><html><body>403 Forbidden</body></html>"))
	}
}

// seedPage registers an already-published page in the fake server, so that a
// document carrying the matching confluence-page-id marker is updated by ID.
func seedPage(fake *fakeConfluenceServer, id, title string) {
	fake.mu.Lock()
	defer fake.mu.Unlock()
	fake.pages[id] = &fakeServerPage{ID: id, Title: title, Body: "<p>stale</p>", Version: 1}
}

// TestRun_ConfigMode_IsolatesBlockedDocument covers the production incident: the
// proxy blocks the title search, so documents without a confluence-page-id
// marker cannot be published. A single blocked document must not abort the run
// — the other documents are published, the second pass still resolves the links
// between them, and the process still exits non-zero so the CI step can see it.
func TestRun_ConfigMode_IsolatesBlockedDocument(t *testing.T) {
	dir := t.TempDir()
	ts, fake := newFakeConfluenceServer(t)
	blockSearch(fake)

	seedPage(fake, "900", "Alpha")
	seedPage(fake, "901", "Beta")

	docs := map[string]string{
		"alpha.md": "<!-- confluence-page-id: 900 -->\n# Alpha\n\nSee [Beta](beta.md).\n",
		"beta.md":  "<!-- confluence-page-id: 901 -->\n# Beta\n\nContent.\n",
		// No marker: publishing it needs the blocked title search.
		"gamma.md": "# Gamma\n\nContent.\n",
	}
	cfg := `url: ` + ts.URL + `
space: TEST
email: user@example.com
server: true
force: true
repo-url: https://github.com/acme/repo/blob/main/
documents:
  - input: alpha.md
  - input: beta.md
  - input: gamma.md
`
	cfgPath := writeConfigAndDocs(t, dir, cfg, docs)

	t.Chdir(dir)
	t.Setenv("CONFLUENCE_TOKEN", "fake-token")

	var stdout, stderr strings.Builder
	code := Run([]string{"--config", cfgPath}, "test", &stdout, &stderr)
	if code == 0 {
		t.Fatalf("expected a non-zero exit code, got 0; stderr: %s", stderr.String())
	}
	if code != 2 {
		t.Errorf("expected exit code 2 (API error), got %d", code)
	}

	// The two healthy documents went through...
	alpha := fake.pageByTitle("Alpha")
	beta := fake.pageByTitle("Beta")
	if alpha == nil || beta == nil {
		t.Fatalf("expected Alpha and Beta to be published, got alpha=%v beta=%v", alpha, beta)
	}
	if !strings.Contains(alpha.Body, "Alpha") || alpha.Version < 2 {
		t.Errorf("Alpha was not updated (version %d): %s", alpha.Version, alpha.Body)
	}

	// ...including the second pass that resolves the links between them, which
	// only runs after every document is published.
	betaURL := ts.URL + "/display/TEST/901"
	if !strings.Contains(alpha.Body, `href="`+betaURL+`"`) {
		t.Errorf("inter-document links were not resolved, expected %s\nbody: %s", betaURL, alpha.Body)
	}

	// The blocked document must not be published as a new page: a 403 search is
	// ambiguous, and creating a page would duplicate it on every run.
	if p := fake.pageByTitle("Gamma"); p != nil {
		t.Errorf("blocked document was published anyway as page %s", p.ID)
	}

	summary := stderr.String()
	for _, want := range []string{"1 document(s) failed", "gamma.md", "403", "confluence-page-id"} {
		if !strings.Contains(summary, want) {
			t.Errorf("failure report missing %q\nstderr: %s", want, summary)
		}
	}
}

// TestRun_ServerDirTree_IsolatesBlockedFile covers the same failure inside a
// directory document: one file in the middle of the tree cannot be published,
// and the remaining files, the sibling subdirectories and the link resolution
// must all still happen.
func TestRun_ServerDirTree_IsolatesBlockedFile(t *testing.T) {
	dir := t.TempDir()
	ts, fake := newFakeConfluenceServer(t)
	blockSearch(fake)

	seedPage(fake, "910", "Docs")
	seedPage(fake, "911", "Two")
	seedPage(fake, "912", "Sub")
	seedPage(fake, "913", "Three")

	docs := map[string]string{
		"docs/README.md": "<!-- confluence-page-id: 910 -->\n# Docs\n\nIndex.\n",
		// No marker, and sorted before two.md: the walk must not stop here.
		"docs/one.md":        "# One\n\nContent.\n",
		"docs/two.md":        "<!-- confluence-page-id: 911 -->\n# Two\n\nSee [Three](sub/three.md).\n",
		"docs/sub/README.md": "<!-- confluence-page-id: 912 -->\n# Sub\n\nIndex.\n",
		"docs/sub/three.md":  "<!-- confluence-page-id: 913 -->\n# Three\n\nContent.\n",
	}
	cfg := `url: ` + ts.URL + `
space: TEST
email: user@example.com
server: true
force: true
repo-url: https://github.com/acme/repo/blob/main/
documents:
  - input: docs/
`
	cfgPath := writeConfigAndDocs(t, dir, cfg, docs)

	t.Chdir(dir)
	t.Setenv("CONFLUENCE_TOKEN", "fake-token")

	var stdout, stderr strings.Builder
	if code := Run([]string{"--config", cfgPath}, "test", &stdout, &stderr); code != 2 {
		t.Fatalf("expected exit code 2, got %d; stderr: %s", code, stderr.String())
	}

	// Files after the blocked one, and the whole sibling subdirectory, are
	// still published.
	for _, title := range []string{"Docs", "Two", "Sub", "Three"} {
		p := fake.pageByTitle(title)
		if p == nil {
			t.Fatalf("page %q was not published", title)
		}
		if p.Version < 2 {
			t.Errorf("page %q was not updated (version %d)", title, p.Version)
		}
	}
	if p := fake.pageByTitle("One"); p != nil {
		t.Errorf("blocked file was published anyway as page %s", p.ID)
	}

	two := fake.pageByTitle("Two")
	threeURL := ts.URL + "/display/TEST/913"
	if !strings.Contains(two.Body, `href="`+threeURL+`"`) {
		t.Errorf("inter-document links were not resolved, expected %s\nbody: %s", threeURL, two.Body)
	}

	if !strings.Contains(stderr.String(), "one.md") {
		t.Errorf("failure report does not mention the blocked file\nstderr: %s", stderr.String())
	}
}

// TestPublishDirTree_ADF_IsolatesFailedFile mirrors the directory isolation on
// the Cloud/ADF path, which walks the tree in publishDirTree.
func TestPublishDirTree_ADF_IsolatesFailedFile(t *testing.T) {
	dir := t.TempDir()
	docs := map[string]string{
		"README.md":     "<!-- confluence-page-id: 100 -->\n# Root\n\nIndex.\n",
		"one.md":        "# One\n\nContent.\n", // no marker → blocked title search
		"two.md":        "<!-- confluence-page-id: 102 -->\n# Two\n\nContent.\n",
		"sub/README.md": "<!-- confluence-page-id: 103 -->\n# Sub\n\nIndex.\n",
	}
	for path, content := range docs {
		full := filepath.Join(dir, path)
		if err := os.MkdirAll(filepath.Dir(full), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}

	var mu sync.Mutex
	updated := map[string]bool{}
	created := 0

	ts := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := strings.TrimPrefix(r.URL.Path, "/wiki/api/v2/pages/")
		switch {
		// FindByTitle — blocked by the proxy.
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/pages"):
			w.Header().Set("Content-Type", "text/html")
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte("<!DOCTYPE html><html><body>403</body></html>"))

		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/pages"):
			mu.Lock()
			created++
			mu.Unlock()
			_, _ = io.WriteString(w, `{"id":"new","title":"New","version":{"number":1}}`)

		case r.Method == http.MethodPut:
			mu.Lock()
			updated[id] = true
			mu.Unlock()
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id": id, "title": "Page", "version": map[string]any{"number": 2},
			})

		case r.Method == http.MethodGet:
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id": id, "title": "Page", "version": map[string]any{"number": 1},
				"body": map[string]any{"value": `{"type":"doc"}`},
			})

		default:
			http.Error(w, "not found", http.StatusNotFound)
		}
	}))
	defer ts.Close()

	client, err := confluence.NewClient(confluence.Config{
		BaseURL: ts.URL, SpaceKey: "TEST", Email: "e@example.com", Token: "t",
	})
	if err != nil {
		t.Fatal(err)
	}
	client.SetHTTPClient(ts.Client())

	var stdout, stderr bytes.Buffer
	warnings := make([]string, 0)
	failures := make([]docFailure, 0)
	app := &appEnv{
		publish:      true,
		force:        true,
		space:        "TEST",
		stdout:       &stdout,
		stderr:       &stderr,
		outputMu:     &sync.Mutex{},
		warnings:     &warnings,
		warningsMu:   &sync.Mutex{},
		failures:     &failures,
		failuresMu:   &sync.Mutex{},
		docResults:   make(map[string]*docPublishResult),
		docResultsMu: &sync.Mutex{},
	}
	app.logger = slog.New(slog.NewTextHandler(&stderr, &slog.HandlerOptions{Level: slog.LevelDebug}))

	tree, err := buildDirTree(dir)
	if err != nil {
		t.Fatal(err)
	}

	if err := app.publishDirTree(client, "space-1", "", tree, true); err != nil {
		t.Fatalf("publishDirTree must not abort on a per-document failure: %v", err)
	}

	for _, id := range []string{"100", "102", "103"} {
		if !updated[id] {
			t.Errorf("page %s was not published", id)
		}
	}
	if created != 0 {
		t.Errorf("expected no page to be created after a blocked search, got %d", created)
	}

	got := app.docFailures()
	if len(got) != 1 {
		t.Fatalf("expected 1 recorded failure, got %d: %+v", len(got), got)
	}
	if filepath.Base(got[0].path) != "one.md" {
		t.Errorf("expected the failure to point at one.md, got %q", got[0].path)
	}
	if app.failureError() == nil {
		t.Error("expected failureError to report the collected failure")
	}
}

// TestFailureError_ExitCode pins the aggregation: no failures means no error,
// and the aggregated exit code is the highest one among the failures so that an
// API failure is not downgraded to a usage error.
func TestFailureError_ExitCode(t *testing.T) {
	tests := []struct {
		name     string
		failures []docFailure
		wantErr  bool
		wantCode int
	}{
		{name: "no failures", failures: nil},
		{
			name:     "usage error only",
			failures: []docFailure{{path: "a.md", err: &apiError{message: "bad input", exitCode: 1}}},
			wantErr:  true, wantCode: 1,
		},
		{
			name: "api error wins",
			failures: []docFailure{
				{path: "a.md", err: &apiError{message: "bad input", exitCode: 1}},
				{path: "b.md", err: &apiError{message: "blocked", exitCode: 2}},
			},
			wantErr: true, wantCode: 2,
		},
		{
			name:     "plain error",
			failures: []docFailure{{path: "a.md", err: fmt.Errorf("boom")}},
			wantErr:  true, wantCode: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var stderr bytes.Buffer
			failures := append([]docFailure(nil), tt.failures...)
			app := &appEnv{stderr: &stderr, failures: &failures, failuresMu: &sync.Mutex{}}

			err := app.failureError()
			if !tt.wantErr {
				if err != nil {
					t.Fatalf("expected no error, got %v", err)
				}
				app.printFailureSummary()
				if stderr.Len() != 0 {
					t.Errorf("expected no summary, got %q", stderr.String())
				}
				return
			}
			if err == nil {
				t.Fatal("expected an error")
			}
			var apiErr *apiError
			if !errors.As(err, &apiErr) {
				t.Fatalf("expected *apiError, got %T", err)
			}
			if apiErr.exitCode != tt.wantCode {
				t.Errorf("expected exit code %d, got %d", tt.wantCode, apiErr.exitCode)
			}
			if !strings.Contains(apiErr.message, fmt.Sprintf("%d document(s)", len(tt.failures))) {
				t.Errorf("message does not report the failure count: %q", apiErr.message)
			}

			app.printFailureSummary()
			for _, f := range tt.failures {
				if !strings.Contains(stderr.String(), f.path) {
					t.Errorf("summary missing %q: %s", f.path, stderr.String())
				}
			}
		})
	}
}
