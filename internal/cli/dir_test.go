// Copyright 2026 md2confl contributors
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"bytes"
	"encoding/json"
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

	"github.com/fmnapoli/md2confl/adf"
	"github.com/fmnapoli/md2confl/confluence"
)

func TestBuildDirTree(t *testing.T) {
	dir := t.TempDir()

	for _, f := range []struct {
		path    string
		content string
	}{
		{filepath.Join(dir, "README.md"), "# Root\n"},
		{filepath.Join(dir, "setup.md"), "# Setup\n"},
		{filepath.Join(dir, "guides", "README.md"), "# Guides\n"},
		{filepath.Join(dir, "guides", "intro.md"), "# Introduction\n"},
	} {
		if err := os.MkdirAll(filepath.Dir(f.path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(f.path, []byte(f.content), 0644); err != nil {
			t.Fatal(err)
		}
	}

	tree, err := buildDirTree(dir)
	if err != nil {
		t.Fatal(err)
	}

	if tree.Readme == nil {
		t.Fatal("expected README.md")
	}
	if len(tree.Files) != 1 {
		t.Errorf("expected 1 file, got %d", len(tree.Files))
	}
	if len(tree.Children) != 1 {
		t.Fatalf("expected 1 child dir, got %d", len(tree.Children))
	}
	child := tree.Children[0]
	if child.Name != "guides" {
		t.Errorf("expected child name 'guides', got %q", child.Name)
	}
	if child.Readme == nil {
		t.Error("expected guides/README.md")
	}
	if len(child.Files) != 1 {
		t.Errorf("expected 1 file in guides/, got %d", len(child.Files))
	}
}

func TestBuildDirTree_SkipsEmptyDirs(t *testing.T) {
	dir := t.TempDir()

	assetsDir := filepath.Join(dir, "assets")
	if err := os.MkdirAll(assetsDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(assetsDir, "demo.svg"), []byte("<svg/>"), 0644); err != nil {
		t.Fatal(err)
	}

	guidesDir := filepath.Join(dir, "guides")
	if err := os.MkdirAll(guidesDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(guidesDir, "intro.md"), []byte("# Intro\n"), 0644); err != nil {
		t.Fatal(err)
	}

	nestedEmpty := filepath.Join(dir, "empty", "deep")
	if err := os.MkdirAll(nestedEmpty, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(nestedEmpty, "data.json"), []byte("{}"), 0644); err != nil {
		t.Fatal(err)
	}

	tree, err := buildDirTree(dir)
	if err != nil {
		t.Fatal(err)
	}

	if len(tree.Children) != 1 {
		t.Fatalf("expected 1 child dir, got %d: %v", len(tree.Children), dirNames(tree.Children))
	}
	if tree.Children[0].Name != "guides" {
		t.Errorf("expected child 'guides', got %q", tree.Children[0].Name)
	}
}

func dirNames(dirs []DirEntry) []string {
	names := make([]string, len(dirs))
	for i, d := range dirs {
		names[i] = d.Name
	}
	return names
}

func TestBuildDirTree_NotADir(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "file.txt")
	if err := os.WriteFile(f, []byte("not a dir"), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := buildDirTree(f)
	if err == nil {
		t.Fatal("expected error for non-directory")
	}
}

func TestBuildDirTree_SkipsHiddenDirs(t *testing.T) {
	dir := t.TempDir()

	for _, d := range []string{".git", ".github", ".vscode", "_templates", "node_modules", "vendor"} {
		subdir := filepath.Join(dir, d)
		if err := os.MkdirAll(subdir, 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(subdir, "README.md"), []byte("# Hidden\n"), 0644); err != nil {
			t.Fatal(err)
		}
	}

	visibleDir := filepath.Join(dir, "docs")
	if err := os.MkdirAll(visibleDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(visibleDir, "intro.md"), []byte("# Intro\n"), 0644); err != nil {
		t.Fatal(err)
	}

	tree, err := buildDirTree(dir)
	if err != nil {
		t.Fatal(err)
	}

	if len(tree.Children) != 1 {
		t.Fatalf("expected 1 visible child dir, got %d: %v", len(tree.Children), dirNames(tree.Children))
	}
	if tree.Children[0].Name != "docs" {
		t.Errorf("expected child 'docs', got %q", tree.Children[0].Name)
	}
}

func TestPublishDirTree_MermaidRendering(t *testing.T) {
	dir := t.TempDir()

	readme := "# Root\n\n```mermaid\ngraph TD;\n    A-->B;\n```\n"
	child := "# Child\n\n```mermaid\nsequenceDiagram\n    Alice->>Bob: Hello\n```\n"

	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte(readme), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "child.md"), []byte(child), 0644); err != nil {
		t.Fatal(err)
	}

	var sentADFs []string
	var mu sync.Mutex
	pageCounter := 0
	baseURL := "https://test.atlassian.net/wiki"

	ts := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "GET" && strings.Contains(r.URL.Path, "/spaces") {
			json.NewEncoder(w).Encode(map[string]any{
				"results": []map[string]any{
					{"id": "space-1", "key": "TEST"},
				},
			})
			return
		}

		if r.Method == "POST" && strings.Contains(r.URL.Path, "/pages") {
			body, _ := io.ReadAll(r.Body)

			var req map[string]any
			_ = json.Unmarshal(body, &req)
			if bodyField, ok := req["body"].(map[string]any); ok {
				if value, ok := bodyField["value"].(string); ok {
					mu.Lock()
					sentADFs = append(sentADFs, value)
					mu.Unlock()
				}
			}

			mu.Lock()
			pageCounter++
			pageID := fmt.Sprintf("page-%d", pageCounter)
			mu.Unlock()

			json.NewEncoder(w).Encode(map[string]any{
				"id":    pageID,
				"title": "Test Page",
				"version": map[string]any{
					"number": 1,
				},
				"spaceId": "space-1",
				"_links": map[string]any{
					"webui": "/spaces/TEST/pages/" + pageID,
					"base":  baseURL,
				},
			})
			return
		}

		if r.Method == "GET" && strings.Contains(r.URL.Path, "/pages/") {
			json.NewEncoder(w).Encode(map[string]any{
				"id":    "page-1",
				"title": "Test Page",
				"version": map[string]any{
					"number": 1,
				},
				"_links": map[string]any{
					"webui": "/spaces/TEST/pages/page-1",
					"base":  baseURL,
				},
			})
			return
		}

		if r.Method == "PUT" && strings.Contains(r.URL.Path, "/pages/") {
			json.NewEncoder(w).Encode(map[string]any{
				"id":    "page-1",
				"title": "Test Page",
				"version": map[string]any{
					"number": 2,
				},
				"_links": map[string]any{
					"webui": "/spaces/TEST/pages/page-1",
					"base":  baseURL,
				},
			})
			return
		}

		if r.Method == "POST" && strings.Contains(r.URL.Path, "/child/attachment") {
			json.NewEncoder(w).Encode(map[string]any{
				"results": []map[string]any{
					{"id": "att-1", "title": "mermaid.svg", "extensions": map[string]any{"fileId": "file-1"}},
				},
			})
			return
		}

		http.Error(w, "not found", 404)
	}))
	defer ts.Close()

	client, err := confluence.NewClient(confluence.Config{
		BaseURL:  ts.URL,
		SpaceKey: "TEST",
		Email:    "test@example.com",
		Token:    "test-token",
	})
	if err != nil {
		t.Fatal(err)
	}
	client.SetHTTPClient(ts.Client())

	var stdout, stderr bytes.Buffer
	warnings := make([]string, 0)
	app := &appEnv{
		publish:      true,
		force:        false,
		stdout:       &stdout,
		stderr:       &stderr,
		outputMu:     &sync.Mutex{},
		warnings:     &warnings,
		warningsMu:   &sync.Mutex{},
		docResults:   make(map[string]*docPublishResult),
		docResultsMu: &sync.Mutex{},
	}
	app.logger = slog.New(slog.NewTextHandler(&stderr, &slog.HandlerOptions{Level: slog.LevelDebug}))

	tree, err := buildDirTree(dir)
	if err != nil {
		t.Fatal(err)
	}

	if err := app.publishDirTree(client, "space-1", "", tree, true); err != nil {
		t.Fatalf("publishDirTree failed: %v", err)
	}

	if len(sentADFs) != 2 {
		t.Fatalf("expected 2 ADF payloads sent, got %d", len(sentADFs))
	}

	for i, adfStr := range sentADFs {
		var doc adf.Document
		if err := json.Unmarshal([]byte(adfStr), &doc); err != nil {
			t.Fatalf("invalid ADF in payload %d: %v", i, err)
		}

		hasMermaidCode := false
		hasMediaSingle := false
		for _, node := range doc.Content {
			if node.Type == "codeBlock" {
				if lang, ok := node.Attrs["language"].(string); ok && lang == "mermaid" {
					hasMermaidCode = true
				}
			}
			if node.Type == "mediaSingle" {
				hasMediaSingle = true
			}
		}

		if !hasMermaidCode && !hasMediaSingle {
			t.Errorf("payload %d: expected either mermaid codeBlock or mediaSingle, got neither", i)
		}
	}
}
