// Copyright 2026 md2confl contributors
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/fmnapoli/md2confl/adf"
	"github.com/fmnapoli/md2confl/confluence"
)

func TestExtractPageID(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		expect string
	}{
		{"with marker", "<!-- confluence-page-id: 12345 -->\n# Title", "12345"},
		{"no marker", "# Title\nContent", ""},
		{"marker at end ignored", "# Title\n<!-- confluence-page-id: 99999 -->", ""},
		{"extra spaces", "<!--  confluence-page-id:  67890  -->", "67890"},
		{"marker in code block ignored", "```\n<!-- confluence-page-id: 11111 -->\n```", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractPageID([]byte(tt.input))
			if got != tt.expect {
				t.Errorf("expected %q, got %q", tt.expect, got)
			}
		})
	}
}

func TestApiError_Error(t *testing.T) {
	err := &apiError{message: "test error", hint: "try again", exitCode: 2}
	if err.Error() != "test error" {
		t.Errorf("expected 'test error', got %q", err.Error())
	}
}

func TestWrapConfluenceError_APIError(t *testing.T) {
	app := &appEnv{}
	original := &confluence.APIError{
		Category:   confluence.ErrCategoryAuth,
		StatusCode: 401,
		Message:    "authentication failed",
		Hint:       "check token",
	}

	err := app.wrapConfluenceError(original)
	var ae *apiError
	if !errors.As(err, &ae) {
		t.Fatalf("expected *apiError, got %T", err)
	}
	if ae.exitCode != 2 {
		t.Errorf("expected exit code 2, got %d", ae.exitCode)
	}
	if ae.hint != "check token" {
		t.Errorf("expected hint 'check token', got %q", ae.hint)
	}
}

func TestWrapConfluenceError_GenericError(t *testing.T) {
	app := &appEnv{}
	err := app.wrapConfluenceError(fmt.Errorf("network timeout"))

	var ae *apiError
	if !errors.As(err, &ae) {
		t.Fatalf("expected *apiError, got %T", err)
	}
	if ae.exitCode != 2 {
		t.Errorf("expected exit code 2, got %d", ae.exitCode)
	}
	if ae.message != "network timeout" {
		t.Errorf("expected message 'network timeout', got %q", ae.message)
	}
}

func TestWritePageIDMarker_NewMarker(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.md")
	source := []byte("# Title\n\nContent\n")
	if err := os.WriteFile(path, source, 0644); err != nil {
		t.Fatal(err)
	}

	app := &appEnv{}
	if err := app.writePageIDMarker(path, source, "12345"); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(data), "<!-- confluence-page-id: 12345 -->") {
		t.Errorf("expected marker at top, got %q", string(data[:60]))
	}
}

func TestWritePageIDMarker_ReplaceExisting(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.md")
	source := []byte("<!-- confluence-page-id: 99999 -->\n# Title\n")
	if err := os.WriteFile(path, source, 0644); err != nil {
		t.Fatal(err)
	}

	app := &appEnv{}
	if err := app.writePageIDMarker(path, source, "12345"); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "12345") {
		t.Errorf("expected updated marker, got %q", string(data))
	}
	if strings.Contains(string(data), "99999") {
		t.Errorf("old marker should be replaced")
	}
}

func TestWritePageIDMarker_PreservesPermissions(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.md")
	source := []byte("# Title\n\nContent\n")
	if err := os.WriteFile(path, source, 0600); err != nil {
		t.Fatal(err)
	}

	app := &appEnv{}
	if err := app.writePageIDMarker(path, source, "12345"); err != nil {
		t.Fatal(err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0600 {
		t.Errorf("expected permissions 0600, got %o", info.Mode().Perm())
	}
}

func TestWritePageIDMarker_OnlyReplacesFirstLine(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.md")
	source := []byte("<!-- confluence-page-id: 99999 -->\n# Title\n\n```\n<!-- confluence-page-id: 99999 -->\n```\n")
	if err := os.WriteFile(path, source, 0644); err != nil {
		t.Fatal(err)
	}

	app := &appEnv{}
	if err := app.writePageIDMarker(path, source, "12345"); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	content := string(data)
	if !strings.HasPrefix(content, "<!-- confluence-page-id: 12345 -->") {
		t.Errorf("expected first line to be updated, got %q", content[:60])
	}
	if !strings.Contains(content, "<!-- confluence-page-id: 99999 -->") {
		t.Error("code block marker should not have been replaced")
	}
}

func newTestApp(ts *httptest.Server, opts ...func(*appEnv)) (*appEnv, *confluence.Client) {
	client, _ := confluence.NewClient(confluence.Config{
		BaseURL:  ts.URL,
		SpaceKey: "TEST",
		Email:    "test@test.com",
		Token:    "token",
	})
	client.SetHTTPClient(ts.Client())

	var stderr bytes.Buffer
	warnings := make([]string, 0)
	app := &appEnv{
		space:      "TEST",
		force:      true,
		stdout:     &bytes.Buffer{},
		stderr:     &stderr,
		outputMu:   &sync.Mutex{},
		warnings:   &warnings,
		warningsMu: &sync.Mutex{},
		logger:     slog.New(slog.NewTextHandler(&stderr, &slog.HandlerOptions{Level: slog.LevelDebug})),
	}
	for _, opt := range opts {
		opt(app)
	}
	return app, client
}

func TestPublishOrSkip_MovesPageWhenParentDiffers(t *testing.T) {
	moved := false
	ts := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == "GET" && strings.Contains(r.URL.Path, "/pages/111"):
			json.NewEncoder(w).Encode(map[string]any{
				"id":       "111",
				"parentId": "old-parent",
				"title":    "Test Page",
				"version":  map[string]any{"number": 3},
				"body":     map[string]any{"atlas_doc_format": map[string]any{"value": `{"changed":true}`}},
				"_links":   map[string]any{"webui": "/pages/111", "base": "https://test.atlassian.net/wiki"},
			})
		case r.Method == "PUT" && strings.Contains(r.URL.Path, "/move/append/new-parent"):
			moved = true
			json.NewEncoder(w).Encode(map[string]any{"pageId": "111"})
		case r.Method == "PUT" && strings.Contains(r.URL.Path, "/pages/111"):
			json.NewEncoder(w).Encode(map[string]any{
				"id":      "111",
				"title":   "Test Page",
				"version": map[string]any{"number": 4},
				"_links":  map[string]any{"webui": "/pages/111", "base": "https://test.atlassian.net/wiki"},
			})
		default:
			http.Error(w, "not found", 404)
		}
	}))
	defer ts.Close()

	app, client := newTestApp(ts)
	result, err := app.publishOrSkip(publishInput{
		client:   client,
		spaceID:  "space-1",
		parentID: "new-parent",
		title:    "Test Page",
		pageID:   "111",
		adfStr:   `{"new":"content"}`,
		force:    true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Action != "updated" {
		t.Errorf("expected action 'updated', got %s", result.Action)
	}
	if !moved {
		t.Error("expected MovePage to be called")
	}
}

func TestPublishOrSkip_SkipsMoveWhenParentCorrect(t *testing.T) {
	moved := false
	ts := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == "GET" && strings.Contains(r.URL.Path, "/pages/111"):
			json.NewEncoder(w).Encode(map[string]any{
				"id":       "111",
				"parentId": "correct-parent",
				"title":    "Test Page",
				"version":  map[string]any{"number": 3},
				"body":     map[string]any{"atlas_doc_format": map[string]any{"value": `{"same":"content"}`}},
				"_links":   map[string]any{"webui": "/pages/111", "base": "https://test.atlassian.net/wiki"},
			})
		case r.Method == "PUT" && strings.Contains(r.URL.Path, "/move/"):
			moved = true
			json.NewEncoder(w).Encode(map[string]any{"pageId": "111"})
		default:
			http.Error(w, "not found", 404)
		}
	}))
	defer ts.Close()

	app, client := newTestApp(ts)
	result, err := app.publishOrSkip(publishInput{
		client:   client,
		spaceID:  "space-1",
		parentID: "correct-parent",
		title:    "Test Page",
		pageID:   "111",
		adfStr:   `{"same":"content"}`,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Action != "skipped" {
		t.Errorf("expected action 'skipped', got %s", result.Action)
	}
	if moved {
		t.Error("expected MovePage NOT to be called when parent is correct")
	}
}

func TestPublishOrSkip_DryRunLogsMove(t *testing.T) {
	ts := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == "GET" && strings.Contains(r.URL.Path, "/pages/111"):
			json.NewEncoder(w).Encode(map[string]any{
				"id":       "111",
				"parentId": "old-parent",
				"title":    "Test Page",
				"version":  map[string]any{"number": 1},
				"body":     map[string]any{"atlas_doc_format": map[string]any{"value": `{"same":"content"}`}},
				"_links":   map[string]any{"webui": "/pages/111", "base": "https://test.atlassian.net/wiki"},
			})
		case r.Method == "PUT" && strings.Contains(r.URL.Path, "/move/"):
			t.Error("MovePage should NOT be called in dry-run mode")
		default:
			http.Error(w, "not found", 404)
		}
	}))
	defer ts.Close()

	app, client := newTestApp(ts, func(a *appEnv) { a.dryRun = true })
	result, err := app.publishOrSkip(publishInput{
		client:   client,
		spaceID:  "space-1",
		parentID: "new-parent",
		title:    "Test Page",
		pageID:   "111",
		adfStr:   `{"same":"content"}`,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Action != "skipped" {
		t.Errorf("expected action 'skipped', got %s", result.Action)
	}

	stderr := app.stderr.(*bytes.Buffer).String()
	if !strings.Contains(stderr, "Would move page") {
		t.Errorf("expected dry-run log message 'Would move page', got:\n%s", stderr)
	}
}

func TestPublishOrSkip_ForcePathMovesPage(t *testing.T) {
	moved := false
	ts := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == "GET" && strings.Contains(r.URL.Path, "/pages") && r.URL.Query().Get("title") != "":
			json.NewEncoder(w).Encode(map[string]any{
				"results": []map[string]any{
					{"id": "555", "parentId": "wrong-parent", "title": "Found Page", "version": map[string]any{"number": 2}},
				},
			})
		case r.Method == "GET" && strings.Contains(r.URL.Path, "/pages/555"):
			json.NewEncoder(w).Encode(map[string]any{
				"id":       "555",
				"parentId": "wrong-parent",
				"title":    "Found Page",
				"version":  map[string]any{"number": 2},
				"body":     map[string]any{"atlas_doc_format": map[string]any{"value": `{"old":"content"}`}},
				"_links":   map[string]any{"webui": "/pages/555", "base": "https://test.atlassian.net/wiki"},
			})
		case r.Method == "PUT" && strings.Contains(r.URL.Path, "/move/append/right-parent"):
			moved = true
			json.NewEncoder(w).Encode(map[string]any{"pageId": "555"})
		case r.Method == "PUT" && strings.Contains(r.URL.Path, "/pages/555"):
			json.NewEncoder(w).Encode(map[string]any{
				"id":      "555",
				"title":   "Found Page",
				"version": map[string]any{"number": 3},
				"_links":  map[string]any{"webui": "/pages/555", "base": "https://test.atlassian.net/wiki"},
			})
		default:
			http.Error(w, "not found", 404)
		}
	}))
	defer ts.Close()

	app, client := newTestApp(ts)
	result, err := app.publishOrSkip(publishInput{
		client:   client,
		spaceID:  "space-1",
		parentID: "right-parent",
		title:    "Found Page",
		adfStr:   `{"new":"content"}`,
		force:    true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Action != "updated" {
		t.Errorf("expected action 'updated', got %s", result.Action)
	}
	if !moved {
		t.Error("expected MovePage to be called in force path")
	}
}

func TestUploadAndPatchImages_LogsErrors(t *testing.T) {
	dir := t.TempDir()
	imgPath := filepath.Join(dir, "photo.png")
	if err := os.WriteFile(imgPath, []byte("fake-png"), 0644); err != nil {
		t.Fatal(err)
	}

	doc := &adf.Document{
		Version: 1,
		Type:    "doc",
		Content: []adf.Node{
			{
				Type:  "mediaSingle",
				Attrs: map[string]any{"layout": "center"},
				Content: []adf.Node{
					{Type: "media", Attrs: map[string]any{"type": "external", "url": "photo.png"}},
				},
			},
		},
	}

	ts := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" && strings.Contains(r.URL.Path, "/child/attachment") {
			json.NewEncoder(w).Encode(map[string]any{
				"results": []map[string]any{
					{"id": "att-1", "title": "photo.png", "extensions": map[string]any{"fileId": "file-1"}},
				},
			})
			return
		}
		if r.Method == "GET" && strings.Contains(r.URL.Path, "/pages/") {
			http.Error(w, "internal error", 500)
			return
		}
		http.Error(w, "not found", 404)
	}))
	defer ts.Close()

	client, err := confluence.NewClient(confluence.Config{
		BaseURL:  ts.URL,
		SpaceKey: "TEST",
		Email:    "test@test.com",
		Token:    "token",
	})
	if err != nil {
		t.Fatal(err)
	}
	client.SetHTTPClient(ts.Client())

	var stderr bytes.Buffer
	warnings := make([]string, 0)
	app := &appEnv{
		stdout:     &bytes.Buffer{},
		stderr:     &stderr,
		outputMu:   &sync.Mutex{},
		warnings:   &warnings,
		warningsMu: &sync.Mutex{},
		logger:     slog.New(slog.NewTextHandler(&stderr, &slog.HandlerOptions{Level: slog.LevelDebug})),
	}

	result := &confluence.PublishResult{
		PageID:  "page-1",
		Title:   "Test",
		PageURL: "https://test.atlassian.net/wiki/pages/page-1",
	}

	err = app.uploadAndPatchImages(client, doc, result, dir)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	if len(warnings) == 0 {
		t.Error("expected warnings to be logged for GetPage failure")
	}
}
