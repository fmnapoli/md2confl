// Copyright 2026 md2confl contributors
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
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

	patchLocalImages(doc, map[string]string{"./img/photo.png": "file-uuid-999"}, "12345")

	media := doc.Content[0].Content[0]
	if media.Attrs["type"] != "file" {
		t.Errorf("expected type 'file', got %v", media.Attrs["type"])
	}
	if media.Attrs["id"] != "file-uuid-999" {
		t.Errorf("expected id 'file-uuid-999', got %v", media.Attrs["id"])
	}
	if media.Attrs["collection"] != "contentId-12345" {
		t.Errorf("expected collection 'contentId-12345', got %v", media.Attrs["collection"])
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

func TestFindMermaidBlocks(t *testing.T) {
	doc := &adf.Document{
		Version: 1,
		Type:    "doc",
		Content: []adf.Node{
			{
				Type:  "codeBlock",
				Attrs: map[string]any{"language": "mermaid"},
				Content: []adf.Node{
					{Type: "text", Text: "graph TD;\n    A-->B;"},
				},
			},
			{
				Type:  "codeBlock",
				Attrs: map[string]any{"language": "go"},
				Content: []adf.Node{
					{Type: "text", Text: "fmt.Println(\"hello\")"},
				},
			},
			{
				Type: "paragraph",
				Content: []adf.Node{
					{Type: "text", Text: "Some text"},
				},
			},
			{
				Type:  "codeBlock",
				Attrs: map[string]any{"language": "mermaid"},
				Content: []adf.Node{
					{Type: "text", Text: "sequenceDiagram\n    Alice->>Bob: Hello"},
				},
			},
			{
				Type:    "codeBlock",
				Content: []adf.Node{{Type: "text", Text: "plain code"}},
			},
		},
	}

	blocks := findMermaidBlocks(doc)
	if len(blocks) != 2 {
		t.Fatalf("expected 2 mermaid blocks, got %d", len(blocks))
	}
	if blocks[0].source != "graph TD;\n    A-->B;" {
		t.Errorf("unexpected source[0]: %q", blocks[0].source)
	}
	if blocks[1].source != "sequenceDiagram\n    Alice->>Bob: Hello" {
		t.Errorf("unexpected source[1]: %q", blocks[1].source)
	}
}

func TestFindMermaidBlocks_NoMermaid(t *testing.T) {
	doc := &adf.Document{
		Version: 1,
		Type:    "doc",
		Content: []adf.Node{
			{
				Type:  "codeBlock",
				Attrs: map[string]any{"language": "go"},
				Content: []adf.Node{
					{Type: "text", Text: "package main"},
				},
			},
			{
				Type: "paragraph",
				Content: []adf.Node{
					{Type: "text", Text: "No mermaid here"},
				},
			},
		},
	}

	blocks := findMermaidBlocks(doc)
	if len(blocks) != 0 {
		t.Errorf("expected 0 mermaid blocks, got %d", len(blocks))
	}
}

func TestPatchMermaidBlock(t *testing.T) {
	doc := &adf.Document{
		Version: 1,
		Type:    "doc",
		Content: []adf.Node{
			{
				Type:  "codeBlock",
				Attrs: map[string]any{"language": "mermaid"},
				Content: []adf.Node{
					{Type: "text", Text: "graph TD;\n    A-->B;"},
				},
			},
		},
	}

	blocks := findMermaidBlocks(doc)
	if len(blocks) != 1 {
		t.Fatalf("expected 1 block, got %d", len(blocks))
	}

	patchMermaidBlock(blocks[0], "/tmp/mermaid-abc123.svg")

	node := doc.Content[0]
	if node.Type != "mediaSingle" {
		t.Fatalf("expected mediaSingle, got %q", node.Type)
	}
	if node.Attrs["layout"] != "wide" {
		t.Errorf("expected layout 'wide', got %v", node.Attrs["layout"])
	}
	if len(node.Content) != 1 {
		t.Fatalf("expected 1 child, got %d", len(node.Content))
	}

	media := node.Content[0]
	if media.Type != "media" {
		t.Errorf("expected media, got %q", media.Type)
	}
	if media.Attrs["type"] != "external" {
		t.Errorf("expected type 'external', got %v", media.Attrs["type"])
	}
	if media.Attrs["url"] != "/tmp/mermaid-abc123.svg" {
		t.Errorf("expected url '/tmp/mermaid-abc123.svg', got %v", media.Attrs["url"])
	}
}

func TestRun_DirDryRun(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "doc1.md"), []byte("# Doc 1\n\nHello\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "doc2.md"), []byte("# Doc 2\n\nWorld\n"), 0644); err != nil {
		t.Fatal(err)
	}

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

func TestDeriveTitleFromSource(t *testing.T) {
	app := &appEnv{}

	tests := []struct {
		name     string
		source   string
		fallback string
		want     string
	}{
		{"h1 heading", "# My Title\nContent", "fallback", "My Title"},
		{"no heading", "Just content\nNo heading", "fallback", "fallback"},
		{"h2 ignored", "## Not H1\nContent", "fallback", "fallback"},
		{"marker before h1", "<!-- confluence-page-id: 123 -->\n# Real Title", "fallback", "Real Title"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := app.deriveTitleFromSource([]byte(tt.source), tt.fallback)
			if got != tt.want {
				t.Errorf("expected %q, got %q", tt.want, got)
			}
		})
	}
}

func TestRun_InputOutputJSON(t *testing.T) {
	dir := t.TempDir()
	inputFile := filepath.Join(dir, "test.md")
	outputFile := filepath.Join(dir, "test.json")
	if err := os.WriteFile(inputFile, []byte("# Hello\n\nWorld.\n"), 0644); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	code := Run([]string{"--input", inputFile, "--output", outputFile, "--json"}, "test", &stdout, &stderr)
	if code != 0 {
		t.Errorf("expected exit code 0, got %d; stderr: %s", code, stderr.String())
	}

	var result Result
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatalf("invalid JSON output: %v\nOutput: %s", err, stdout.String())
	}
	if result.Status != "success" {
		t.Errorf("expected status success, got %q", result.Status)
	}
}

func TestRun_ErrorJSON(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := Run([]string{"--input", "/nonexistent/file.md", "--json"}, "test", &stdout, &stderr)
	if code != 1 {
		t.Errorf("expected exit code 1, got %d", code)
	}

	var result ErrorResult
	if err := json.Unmarshal(stderr.Bytes(), &result); err != nil {
		t.Fatalf("invalid JSON error output: %v\nOutput: %s", err, stderr.String())
	}
	if result.Status != "error" {
		t.Errorf("expected status error, got %q", result.Status)
	}
}

func TestRun_DirWithSubdirs(t *testing.T) {
	dir := t.TempDir()
	for _, f := range []struct {
		path    string
		content string
	}{
		{filepath.Join(dir, "README.md"), "# Root\n\nRoot content\n"},
		{filepath.Join(dir, "setup.md"), "# Setup\n\nSetup content\n"},
		{filepath.Join(dir, "sub", "README.md"), "# Sub\n\nSub content\n"},
	} {
		if err := os.MkdirAll(filepath.Dir(f.path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(f.path, []byte(f.content), 0644); err != nil {
			t.Fatal(err)
		}
	}

	var stdout, stderr bytes.Buffer
	code := Run([]string{"--input", dir}, "test", &stdout, &stderr)
	if code != 0 {
		t.Errorf("expected exit code 0, got %d; stderr: %s", code, stderr.String())
	}

	output := stdout.String()
	// Should output 3 ADF documents (README, setup, sub/README)
	count := strings.Count(output, `"type": "doc"`)
	if count != 3 {
		t.Errorf("expected 3 ADF documents, got %d", count)
	}
}

func TestRun_DefaultStdout(t *testing.T) {
	dir := t.TempDir()
	inputFile := filepath.Join(dir, "test.md")
	if err := os.WriteFile(inputFile, []byte("# Hello\n"), 0644); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	code := Run([]string{"--input", inputFile}, "test", &stdout, &stderr)
	if code != 0 {
		t.Errorf("expected exit code 0, got %d; stderr: %s", code, stderr.String())
	}

	var doc map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &doc); err != nil {
		t.Fatalf("default output is not valid JSON: %v", err)
	}
}

func TestApiError_Error(t *testing.T) {
	err := &apiError{message: "test error", hint: "try again", exitCode: 2}
	if err.Error() != "test error" {
		t.Errorf("expected 'test error', got %q", err.Error())
	}
}

func TestRun_DryRunWithPublishFlagsJSON(t *testing.T) {
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
		"--parent-id", "999",
		"--json",
	}, "test", &stdout, &stderr)
	if code != 0 {
		t.Errorf("expected exit code 0, got %d; stderr: %s", code, stderr.String())
	}
	if !strings.Contains(stderr.String(), "Parent: 999") {
		t.Errorf("expected parent in simulation, got %q", stderr.String())
	}
}

func TestRun_MissingInput(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := Run([]string{"--publish"}, "test", &stdout, &stderr)
	if code != 1 {
		t.Errorf("expected exit code 1, got %d", code)
	}
}

func TestRun_EnvVarCredentials(t *testing.T) {
	dir := t.TempDir()
	inputFile := filepath.Join(dir, "test.md")
	if err := os.WriteFile(inputFile, []byte("# Hello\n"), 0644); err != nil {
		t.Fatal(err)
	}

	t.Setenv("CONFLUENCE_URL", "https://env.atlassian.net")
	t.Setenv("CONFLUENCE_EMAIL", "env@example.com")
	t.Setenv("CONFLUENCE_TOKEN", "env-token")

	var stdout, stderr bytes.Buffer
	code := Run([]string{
		"--input", inputFile,
		"--dry-run",
		"--publish",
		"--space", "TEST",
	}, "test", &stdout, &stderr)
	if code != 0 {
		t.Errorf("expected exit code 0, got %d; stderr: %s", code, stderr.String())
	}
}

func TestRun_PublishNonHTTPS(t *testing.T) {
	dir := t.TempDir()
	inputFile := filepath.Join(dir, "test.md")
	if err := os.WriteFile(inputFile, []byte("# Hello\n"), 0644); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	code := Run([]string{
		"--input", inputFile,
		"--publish",
		"--url", "http://insecure.example.com",
		"--space", "TEST",
		"--email", "test@example.com",
		"--token", "fake",
	}, "test", &stdout, &stderr)
	if code != 1 {
		t.Errorf("expected exit code 1, got %d", code)
	}
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

// --- Config integration tests ---

func writeConfigAndDocs(t *testing.T, dir, configContent string, docs map[string]string) string {
	t.Helper()
	cfgPath := filepath.Join(dir, ".md2confl.yml")
	if err := os.WriteFile(cfgPath, []byte(configContent), 0644); err != nil {
		t.Fatal(err)
	}
	for path, content := range docs {
		fullPath := filepath.Join(dir, path)
		if err := os.MkdirAll(filepath.Dir(fullPath), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(fullPath, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}
	return cfgPath
}

func TestRun_ConfigGlobalsDryRun(t *testing.T) {
	dir := t.TempDir()
	inputFile := filepath.Join(dir, "doc.md")
	if err := os.WriteFile(inputFile, []byte("# Hello\n\nContent\n"), 0644); err != nil {
		t.Fatal(err)
	}

	cfgPath := writeConfigAndDocs(t, dir, `url: https://site.atlassian.net
space: DEVOPS
email: user@example.com
documents:
  - input: doc.md
    title: "My Doc"
`, nil)

	t.Setenv("CONFLUENCE_TOKEN", "fake-token")

	var stdout, stderr bytes.Buffer
	code := Run([]string{"--config", cfgPath, "--dry-run"}, "test", &stdout, &stderr)
	if code != 0 {
		t.Errorf("expected exit code 0, got %d; stderr: %s", code, stderr.String())
	}

	// Dry-run with publish flags should show simulation
	if !strings.Contains(stderr.String(), "Dry-run: would publish") {
		t.Errorf("expected dry-run simulation message, got %q", stderr.String())
	}
}

func TestRun_ConfigFlagOverride(t *testing.T) {
	dir := t.TempDir()
	inputFile := filepath.Join(dir, "doc.md")
	if err := os.WriteFile(inputFile, []byte("# Hello\n"), 0644); err != nil {
		t.Fatal(err)
	}

	cfgPath := writeConfigAndDocs(t, dir, `url: https://site.atlassian.net
space: ORIGINAL
email: user@example.com
documents:
  - input: doc.md
`, nil)

	t.Setenv("CONFLUENCE_TOKEN", "fake-token")

	var stdout, stderr bytes.Buffer
	code := Run([]string{"--config", cfgPath, "--space", "OVERRIDE", "--dry-run"}, "test", &stdout, &stderr)
	if code != 0 {
		t.Errorf("expected exit code 0, got %d; stderr: %s", code, stderr.String())
	}

	// Space should be overridden by flag
	if !strings.Contains(stderr.String(), "Space: OVERRIDE") {
		t.Errorf("expected space OVERRIDE in output, got %q", stderr.String())
	}
}

func TestRun_ConfigOverridesEnvVar(t *testing.T) {
	dir := t.TempDir()
	inputFile := filepath.Join(dir, "doc.md")
	if err := os.WriteFile(inputFile, []byte("# Hello\n"), 0644); err != nil {
		t.Fatal(err)
	}

	cfgPath := writeConfigAndDocs(t, dir, `url: https://config.atlassian.net
space: CONFIG
email: config@example.com
documents:
  - input: doc.md
`, nil)

	t.Setenv("CONFLUENCE_URL", "https://env.atlassian.net")
	t.Setenv("CONFLUENCE_EMAIL", "env@example.com")
	t.Setenv("CONFLUENCE_TOKEN", "fake-token")

	var stdout, stderr bytes.Buffer
	code := Run([]string{"--config", cfgPath, "--dry-run"}, "test", &stdout, &stderr)
	if code != 0 {
		t.Errorf("expected exit code 0, got %d; stderr: %s", code, stderr.String())
	}

	// Config URL should override env var
	if !strings.Contains(stderr.String(), "URL: https://config.atlassian.net") {
		t.Errorf("expected config URL to override env var, got %q", stderr.String())
	}
}

func TestRun_ConfigDocumentOverride(t *testing.T) {
	dir := t.TempDir()
	inputFile := filepath.Join(dir, "doc.md")
	if err := os.WriteFile(inputFile, []byte("# Hello\n"), 0644); err != nil {
		t.Fatal(err)
	}

	cfgPath := writeConfigAndDocs(t, dir, `url: https://site.atlassian.net
space: GLOBAL
email: user@example.com
documents:
  - input: doc.md
    space: DOCSPACE
`, nil)

	t.Setenv("CONFLUENCE_TOKEN", "fake-token")

	var stdout, stderr bytes.Buffer
	code := Run([]string{"--config", cfgPath, "--dry-run"}, "test", &stdout, &stderr)
	if code != 0 {
		t.Errorf("expected exit code 0, got %d; stderr: %s", code, stderr.String())
	}

	// Document-level space should override global
	if !strings.Contains(stderr.String(), "Space: DOCSPACE") {
		t.Errorf("expected document space override, got %q", stderr.String())
	}
}

func TestRun_ConfigMultiDocument(t *testing.T) {
	dir := t.TempDir()

	cfgPath := writeConfigAndDocs(t, dir, `url: https://site.atlassian.net
space: DEVOPS
email: user@example.com
documents:
  - input: doc1.md
  - input: doc2.md
`, map[string]string{
		"doc1.md": "# Doc 1\n\nFirst\n",
		"doc2.md": "# Doc 2\n\nSecond\n",
	})

	t.Setenv("CONFLUENCE_TOKEN", "fake-token")

	var stdout, stderr bytes.Buffer
	code := Run([]string{"--config", cfgPath, "--dry-run"}, "test", &stdout, &stderr)
	if code != 0 {
		t.Errorf("expected exit code 0, got %d; stderr: %s", code, stderr.String())
	}

	// Should have 2 ADF documents in output
	output := stdout.String()
	count := strings.Count(output, `"type": "doc"`)
	if count != 2 {
		t.Errorf("expected 2 ADF documents, got %d", count)
	}
}

func TestRun_ConfigInputFilter(t *testing.T) {
	dir := t.TempDir()

	cfgPath := writeConfigAndDocs(t, dir, `url: https://site.atlassian.net
space: DEVOPS
email: user@example.com
documents:
  - input: doc1.md
  - input: doc2.md
`, map[string]string{
		"doc1.md": "# Doc 1\n\nFirst\n",
		"doc2.md": "# Doc 2\n\nSecond\n",
	})

	t.Setenv("CONFLUENCE_TOKEN", "fake-token")

	var stdout, stderr bytes.Buffer
	code := Run([]string{"--config", cfgPath, "--input", filepath.Join(dir, "doc1.md"), "--dry-run"}, "test", &stdout, &stderr)
	if code != 0 {
		t.Errorf("expected exit code 0, got %d; stderr: %s", code, stderr.String())
	}

	// Should have only 1 ADF document
	output := stdout.String()
	count := strings.Count(output, `"type": "doc"`)
	if count != 1 {
		t.Errorf("expected 1 ADF document, got %d", count)
	}
}

func TestRun_ConfigConvertMode(t *testing.T) {
	dir := t.TempDir()
	outputFile := filepath.Join(dir, "out.json")

	cfgPath := writeConfigAndDocs(t, dir, fmt.Sprintf(`documents:
  - input: doc.md
    output: %s
`, outputFile), map[string]string{
		"doc.md": "# Hello\n\nWorld\n",
	})

	var stdout, stderr bytes.Buffer
	code := Run([]string{"--config", cfgPath}, "test", &stdout, &stderr)
	if code != 0 {
		t.Errorf("expected exit code 0, got %d; stderr: %s", code, stderr.String())
	}

	// Output file should exist with valid JSON
	data, err := os.ReadFile(outputFile)
	if err != nil {
		t.Fatalf("output file not created: %v", err)
	}

	var doc map[string]any
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("invalid JSON output: %v", err)
	}
	if doc["type"] != "doc" {
		t.Errorf("expected type 'doc', got %v", doc["type"])
	}
}

func TestRun_ConfigNoInputNoDocuments(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, ".md2confl.yml")
	if err := os.WriteFile(cfgPath, []byte("url: https://example.com\n"), 0644); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	code := Run([]string{"--config", cfgPath}, "test", &stdout, &stderr)
	if code != 1 {
		t.Errorf("expected exit code 1, got %d", code)
	}
}

func TestRun_ConfigAutoDiscovery(t *testing.T) {
	dir := t.TempDir()

	writeConfigAndDocs(t, dir, `documents:
  - input: doc.md
`, map[string]string{
		"doc.md": "# Auto\n\nDiscovery\n",
	})

	// Change to temp dir for auto-discovery
	origDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(origDir) })

	var stdout, stderr bytes.Buffer
	code := Run([]string{"--dry-run"}, "test", &stdout, &stderr)
	if code != 0 {
		t.Errorf("expected exit code 0, got %d; stderr: %s", code, stderr.String())
	}

	if !strings.Contains(stderr.String(), "Using config:") {
		t.Errorf("expected auto-discovery message, got %q", stderr.String())
	}
}

func TestRun_NoConfigBackwardCompat(t *testing.T) {
	dir := t.TempDir()
	inputFile := filepath.Join(dir, "test.md")
	if err := os.WriteFile(inputFile, []byte("# Hello\n"), 0644); err != nil {
		t.Fatal(err)
	}

	// Change to a dir without config to avoid auto-discovery
	origDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(origDir) })

	var stdout, stderr bytes.Buffer
	code := Run([]string{"--input", inputFile, "--dry-run"}, "test", &stdout, &stderr)
	if code != 0 {
		t.Errorf("expected exit code 0, got %d; stderr: %s", code, stderr.String())
	}

	var doc map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &doc); err != nil {
		t.Fatalf("default output is not valid JSON: %v", err)
	}
}
