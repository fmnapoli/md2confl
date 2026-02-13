// Copyright 2026 md2confl contributors
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/fmnapoli/md2confl/adf"
)

func TestExtractPageID(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		expect string
	}{
		{"with marker", "<!-- confluence-page-id: 12345 -->\n# Title", "12345"},
		{"no marker", "# Title\nContent", ""},
		{"marker at end", "# Title\n<!-- confluence-page-id: 99999 -->", "99999"},
		{"extra spaces", "<!--  confluence-page-id:  67890  -->", "67890"},
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

func TestDeriveTitle(t *testing.T) {
	app := &appEnv{}

	// From H1
	title := app.deriveTitle("test.md", []byte("# My Document\nContent"))
	if title != "My Document" {
		t.Errorf("expected 'My Document', got %q", title)
	}

	// From filename
	title = app.deriveTitle("/path/to/setup.md", []byte("No heading here"))
	if title != "setup" {
		t.Errorf("expected 'setup', got %q", title)
	}

	// From flag
	app.title = "Custom Title"
	title = app.deriveTitle("test.md", []byte("# Ignored"))
	if title != "Custom Title" {
		t.Errorf("expected 'Custom Title', got %q", title)
	}
}

func TestIsLocalPath(t *testing.T) {
	tests := []struct {
		path  string
		local bool
	}{
		{"./img/photo.png", true},
		{"img/photo.png", true},
		{"../assets/logo.svg", true},
		{"https://example.com/logo.png", false},
		{"http://example.com/logo.png", false},
		{"//cdn.example.com/logo.png", false},
	}
	for _, tt := range tests {
		if got := isLocalPath(tt.path); got != tt.local {
			t.Errorf("isLocalPath(%q) = %v, want %v", tt.path, got, tt.local)
		}
	}
}

func TestFindLocalImages(t *testing.T) {
	doc := &adf.Document{
		Version: 1,
		Type:    "doc",
		Content: []adf.Node{
			{
				Type:  "mediaSingle",
				Attrs: map[string]any{"layout": "center"},
				Content: []adf.Node{
					{Type: "media", Attrs: map[string]any{"type": "external", "url": "./img/local.png"}},
				},
			},
			{
				Type:  "mediaSingle",
				Attrs: map[string]any{"layout": "center"},
				Content: []adf.Node{
					{Type: "media", Attrs: map[string]any{"type": "external", "url": "https://example.com/remote.png"}},
				},
			},
		},
	}

	images := findLocalImages(doc)
	if len(images) != 1 {
		t.Fatalf("expected 1 local image, got %d", len(images))
	}
	if images[0] != "./img/local.png" {
		t.Errorf("expected ./img/local.png, got %s", images[0])
	}
}

func TestPatchLocalImages(t *testing.T) {
	doc := &adf.Document{
		Version: 1,
		Type:    "doc",
		Content: []adf.Node{
			{
				Type: "mediaSingle",
				Content: []adf.Node{
					{Type: "media", Attrs: map[string]any{"type": "external", "url": "./img/photo.png"}},
				},
			},
		},
	}

	patchLocalImages(doc, map[string]string{"./img/photo.png": "att999"})

	media := doc.Content[0].Content[0]
	if media.Attrs["type"] != "file" {
		t.Errorf("expected type 'file', got %v", media.Attrs["type"])
	}
	if media.Attrs["id"] != "att999" {
		t.Errorf("expected id 'att999', got %v", media.Attrs["id"])
	}
	if _, exists := media.Attrs["url"]; exists {
		t.Error("expected url to be removed")
	}
}

func TestRun_NoArgs(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := Run(nil, "test", &stdout, &stderr)
	if code != 1 {
		t.Errorf("expected exit code 1, got %d", code)
	}
}

func TestRun_Version(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := Run([]string{"--version"}, "1.2.3", &stdout, &stderr)
	if code != 0 {
		t.Errorf("expected exit code 0, got %d", code)
	}
	if !strings.Contains(stdout.String(), "1.2.3") {
		t.Errorf("expected version in output, got %q", stdout.String())
	}
}

func TestRun_Help(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := Run([]string{"--help"}, "test", &stdout, &stderr)
	if code != 0 {
		t.Errorf("expected exit code 0, got %d", code)
	}
	if !strings.Contains(stderr.String(), "md2confl") {
		t.Errorf("expected help text, got %q", stderr.String())
	}
}

func TestRun_InputNotFound(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := Run([]string{"--input", "/nonexistent/file.md"}, "test", &stdout, &stderr)
	if code != 1 {
		t.Errorf("expected exit code 1, got %d", code)
	}
}

func TestRun_InputOutput(t *testing.T) {
	dir := t.TempDir()
	inputFile := filepath.Join(dir, "test.md")
	outputFile := filepath.Join(dir, "test.json")

	if err := os.WriteFile(inputFile, []byte("# Hello\n\nWorld.\n"), 0644); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	code := Run([]string{"--input", inputFile, "--output", outputFile}, "test", &stdout, &stderr)
	if code != 0 {
		t.Errorf("expected exit code 0, got %d; stderr: %s", code, stderr.String())
	}

	data, err := os.ReadFile(outputFile)
	if err != nil {
		t.Fatal(err)
	}

	var doc map[string]any
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("invalid JSON output: %v", err)
	}

	if doc["type"] != "doc" {
		t.Errorf("expected type 'doc', got %v", doc["type"])
	}
	if doc["version"] != float64(1) {
		t.Errorf("expected version 1, got %v", doc["version"])
	}
}

func TestRun_DryRun(t *testing.T) {
	dir := t.TempDir()
	inputFile := filepath.Join(dir, "test.md")
	if err := os.WriteFile(inputFile, []byte("# Hello\n"), 0644); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	code := Run([]string{"--input", inputFile, "--dry-run"}, "test", &stdout, &stderr)
	if code != 0 {
		t.Errorf("expected exit code 0, got %d; stderr: %s", code, stderr.String())
	}

	var doc map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &doc); err != nil {
		t.Fatalf("dry-run output is not valid JSON: %v\nOutput: %s", err, stdout.String())
	}
}

func TestRun_DryRunWithPublishFlags(t *testing.T) {
	dir := t.TempDir()
	inputFile := filepath.Join(dir, "test.md")
	if err := os.WriteFile(inputFile, []byte("# Hello\n"), 0644); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	code := Run([]string{
		"--input", inputFile,
		"--dry-run",
		"--publish",
		"--url", "https://test.atlassian.net",
		"--space", "TEST",
		"--email", "test@example.com",
		"--token", "fake-token",
		"--title", "My Page",
	}, "test", &stdout, &stderr)
	if code != 0 {
		t.Errorf("expected exit code 0, got %d; stderr: %s", code, stderr.String())
	}

	if !strings.Contains(stderr.String(), "Dry-run: would publish") {
		t.Errorf("expected simulation message, got %q", stderr.String())
	}
	if !strings.Contains(stderr.String(), "My Page") {
		t.Errorf("expected title in simulation, got %q", stderr.String())
	}
}

func TestRun_MutuallyExclusiveFlags(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{
			name: "output and publish",
			args: []string{"--input", "f.md", "--output", "o.json", "--publish",
				"--url", "https://t.atlassian.net", "--space", "S", "--email", "e@e.com", "--token", "t"},
		},
		{
			name: "output and dry-run",
			args: []string{"--input", "f.md", "--output", "o.json", "--dry-run"},
		},
		{
			name: "write-marker without publish",
			args: []string{"--input", "f.md", "--write-marker"},
		},
		{
			name: "force without publish",
			args: []string{"--input", "f.md", "--force"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			code := Run(tt.args, "test", &stdout, &stderr)
			if code != 1 {
				t.Errorf("expected exit code 1, got %d", code)
			}
		})
	}
}

func TestRun_PublishMissingRequiredFlags(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{
			name: "missing url",
			args: []string{"--input", "f.md", "--publish", "--space", "S", "--email", "e@e.com", "--token", "t"},
		},
		{
			name: "missing space",
			args: []string{"--input", "f.md", "--publish", "--url", "https://t.atlassian.net", "--email", "e@e.com", "--token", "t"},
		},
		{
			name: "missing email",
			args: []string{"--input", "f.md", "--publish", "--url", "https://t.atlassian.net", "--space", "S", "--token", "t"},
		},
		{
			name: "missing token",
			args: []string{"--input", "f.md", "--publish", "--url", "https://t.atlassian.net", "--space", "S", "--email", "e@e.com"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Clear env vars to avoid interference
			t.Setenv("CONFLUENCE_URL", "")
			t.Setenv("CONFLUENCE_EMAIL", "")
			t.Setenv("CONFLUENCE_TOKEN", "")

			var stdout, stderr bytes.Buffer
			code := Run(tt.args, "test", &stdout, &stderr)
			if code != 1 {
				t.Errorf("expected exit code 1, got %d", code)
			}
		})
	}
}

func TestBuildDirTree(t *testing.T) {
	dir := t.TempDir()

	os.WriteFile(filepath.Join(dir, "README.md"), []byte("# Root\n"), 0644)
	os.WriteFile(filepath.Join(dir, "setup.md"), []byte("# Setup\n"), 0644)
	os.MkdirAll(filepath.Join(dir, "guides"), 0755)
	os.WriteFile(filepath.Join(dir, "guides", "README.md"), []byte("# Guides\n"), 0644)
	os.WriteFile(filepath.Join(dir, "guides", "intro.md"), []byte("# Introduction\n"), 0644)

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

func TestRun_DirDryRun(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "doc1.md"), []byte("# Doc 1\n\nHello\n"), 0644)
	os.WriteFile(filepath.Join(dir, "doc2.md"), []byte("# Doc 2\n\nWorld\n"), 0644)

	var stdout, stderr bytes.Buffer
	code := Run([]string{"--input", dir, "--dry-run"}, "test", &stdout, &stderr)
	if code != 0 {
		t.Errorf("expected exit code 0, got %d; stderr: %s", code, stderr.String())
	}

	output := stdout.String()
	if !strings.Contains(output, `"type": "doc"`) {
		t.Errorf("expected ADF output, got %q", output)
	}
}
