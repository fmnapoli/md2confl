// Copyright 2026 md2confl contributors
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"bytes"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"
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
