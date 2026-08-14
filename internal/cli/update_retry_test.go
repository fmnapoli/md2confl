// Copyright 2026 md2confl contributors
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/fmnapoli/md2confl/confluence"
)

// fakeCloudPage serves the two Cloud (API v2) endpoints updatePageWithRetry
// needs: read the page and write it back.
type fakeCloudPage struct {
	body         string
	version      int
	updates      atomic.Int32
	conflictOnce atomic.Bool
}

func (f *fakeCloudPage) server(t *testing.T) *httptest.Server {
	t.Helper()
	// TLS porque o client Cloud recusa base URL sem HTTPS.
	ts := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id": "1", "title": "T",
				"body":    map[string]any{"atlas_doc_format": map[string]any{"value": f.body}},
				"version": map[string]any{"number": f.version},
				"_links":  map[string]any{"webui": "/x", "base": "https://site"},
			})
			return
		}
		if f.conflictOnce.CompareAndSwap(true, false) {
			w.WriteHeader(http.StatusConflict)
			_, _ = fmt.Fprint(w, `{"errors":[{"status":409,"title":"version conflict"}]}`)
			return
		}
		var req struct {
			Body struct {
				Value string `json:"value"`
			} `json:"body"`
			Version struct {
				Number int `json:"number"`
			} `json:"version"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		f.body = req.Body.Value
		f.version = req.Version.Number
		f.updates.Add(1)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id": "1", "title": "T",
			"version": map[string]any{"number": f.version},
			"_links":  map[string]any{"webui": "/x", "base": "https://site"},
		})
	}))
	t.Cleanup(ts.Close)
	return ts
}

func cloudClient(t *testing.T, ts *httptest.Server) *confluence.Client {
	t.Helper()
	client, err := confluence.NewClient(confluence.Config{
		BaseURL: ts.URL, SpaceKey: "DEV", Email: "user@example.com", Token: "t",
	})
	if err != nil {
		t.Fatal(err)
	}
	client.SetHTTPClient(ts.Client())
	return client
}

// TestUpdatePageWithRetry_SkipsUnchangedBody cobre o caminho Cloud/ADF do
// segundo pass: ele roda em toda publicação, então reescrever um corpo idêntico
// somaria uma versão de página por execução.
func TestUpdatePageWithRetry_SkipsUnchangedBody(t *testing.T) {
	const adfJSON = `{"type":"doc","version":1,"content":[{"type":"paragraph"}]}`
	// Mesmo documento com espaçamento diferente: a comparação é do ADF
	// normalizado, não dos bytes.
	page := &fakeCloudPage{body: `{"type":"doc","version":1,"content":[{"type":"paragraph"}]}`, version: 7}
	ts := page.server(t)

	if err := updatePageWithRetry(cloudClient(t, ts), "1", "T", adfJSON); err != nil {
		t.Fatal(err)
	}
	if got := page.updates.Load(); got != 0 {
		t.Errorf("an unchanged body must not be rewritten, got %d update(s)", got)
	}
	if page.version != 7 {
		t.Errorf("page version = %d, want it untouched at 7", page.version)
	}
}

// TestUpdatePageWithRetry_WritesChangedBody é o contrapeso: pular um corpo que
// mudou seria perder a resolução de links.
func TestUpdatePageWithRetry_WritesChangedBody(t *testing.T) {
	page := &fakeCloudPage{body: `{"type":"doc","version":1,"content":[]}`, version: 7}
	ts := page.server(t)

	const adfJSON = `{"type":"doc","version":1,"content":[{"type":"paragraph"}]}`
	if err := updatePageWithRetry(cloudClient(t, ts), "1", "T", adfJSON); err != nil {
		t.Fatal(err)
	}
	if got := page.updates.Load(); got != 1 {
		t.Errorf("expected exactly one update, got %d", got)
	}
	if !strings.Contains(page.body, "paragraph") {
		t.Errorf("the new body was not published: %s", page.body)
	}
	if page.version != 8 {
		t.Errorf("page version = %d, want 8", page.version)
	}
}

// TestUpdatePageWithRetry_RetriesOnVersionConflict garante que a checagem de
// corpo inalterado não atropelou o retry por conflito de versão (409).
func TestUpdatePageWithRetry_RetriesOnVersionConflict(t *testing.T) {
	page := &fakeCloudPage{body: `{"type":"doc","version":1,"content":[]}`, version: 7}
	page.conflictOnce.Store(true)
	ts := page.server(t)

	const adfJSON = `{"type":"doc","version":1,"content":[{"type":"paragraph"}]}`
	if err := updatePageWithRetry(cloudClient(t, ts), "1", "T", adfJSON); err != nil {
		t.Fatalf("the conflict should have been retried, got %v", err)
	}
	if got := page.updates.Load(); got != 1 {
		t.Errorf("expected the retry to land exactly one update, got %d", got)
	}
}
