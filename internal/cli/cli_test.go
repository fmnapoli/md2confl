// Copyright 2026 md2confl contributors
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestDeriveTitle(t *testing.T) {
	// From H1
	title := deriveTitle("", "test.md", []byte("# My Document\nContent"))
	if title != "My Document" {
		t.Errorf("expected 'My Document', got %q", title)
	}

	// From filename
	title = deriveTitle("", "/path/to/setup.md", []byte("No heading here"))
	if title != "setup" {
		t.Errorf("expected 'setup', got %q", title)
	}

	// From flag
	title = deriveTitle("Custom Title", "test.md", []byte("# Ignored"))
	if title != "Custom Title" {
		t.Errorf("expected 'Custom Title', got %q", title)
	}
}

func TestDeriveTitleFromSource(t *testing.T) {
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
			got := titleFromSource([]byte(tt.source), tt.fallback)
			if got != tt.want {
				t.Errorf("expected %q, got %q", tt.want, got)
			}
		})
	}
}

func TestDeriveTitleFromSource_CodeBlock(t *testing.T) {
	tests := []struct {
		name     string
		source   string
		fallback string
		want     string
	}{
		{
			"h1 in code block ignored",
			"```bash\n# This is a comment\necho hello\n```\n# Real Title",
			"fallback",
			"Real Title",
		},
		{
			"h1 before code block",
			"# My Title\n\n```bash\n# comment\n```",
			"fallback",
			"My Title",
		},
		{
			"only h1 in code block",
			"```\n# Not a title\n```",
			"fallback",
			"fallback",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := titleFromSource([]byte(tt.source), tt.fallback)
			if got != tt.want {
				t.Errorf("expected %q, got %q", tt.want, got)
			}
		})
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

func TestRun_ConcurrencyValidation(t *testing.T) {
	dir := t.TempDir()
	inputFile := filepath.Join(dir, "test.md")
	if err := os.WriteFile(inputFile, []byte("# Hello\n"), 0644); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name     string
		args     []string
		wantCode int
	}{
		{
			name:     "concurrency 0 fails",
			args:     []string{"--input", inputFile, "--concurrency", "0"},
			wantCode: 1,
		},
		{
			name:     "concurrency 17 fails",
			args:     []string{"--input", inputFile, "--concurrency", "17"},
			wantCode: 1,
		},
		{
			name:     "concurrency 4 succeeds",
			args:     []string{"--input", inputFile, "--concurrency", "4"},
			wantCode: 0,
		},
		{
			name:     "concurrency 1 succeeds",
			args:     []string{"--input", inputFile, "--concurrency", "1"},
			wantCode: 0,
		},
		{
			name:     "concurrency 16 succeeds",
			args:     []string{"--input", inputFile, "--concurrency", "16"},
			wantCode: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			code := Run(tt.args, "test", &stdout, &stderr)
			if code != tt.wantCode {
				t.Errorf("expected exit code %d, got %d; stderr: %s", tt.wantCode, code, stderr.String())
			}
		})
	}
}

func TestRun_VerboseFlag(t *testing.T) {
	dir := t.TempDir()

	// Use a multi-doc config with inter-document links so that
	// the dry-run path produces logger.Info output we can observe.
	cfgPath := writeConfigAndDocs(t, dir, `url: https://site.atlassian.net
space: DEVOPS
email: user@example.com
documents:
  - input: doc1.md
  - input: doc2.md
`, map[string]string{
		"doc1.md": "# Doc 1\n\nSee [doc2](doc2.md) for details.\n",
		"doc2.md": "# Doc 2\n\nContent here.\n",
	})

	t.Setenv("CONFLUENCE_TOKEN", "fake-token")

	var stdout, stderr bytes.Buffer
	code := Run([]string{"--config", cfgPath, "--dry-run", "--verbose"}, "test", &stdout, &stderr)
	if code != 0 {
		t.Errorf("expected exit code 0, got %d; stderr: %s", code, stderr.String())
	}

	// With --verbose, the logger is set to DEBUG level, so all INFO+ logs appear.
	// The dry-run multi-doc path emits INFO-level "would resolve" messages.
	stderrStr := stderr.String()
	if !strings.Contains(stderrStr, "level=INFO") {
		t.Errorf("expected INFO-level log output with --verbose, got %q", stderrStr)
	}
}

func TestAddWarning(t *testing.T) {
	app := &appEnv{}
	w := make([]string, 0)
	app.warnings = &w
	app.warningsMu = &sync.Mutex{}

	app.addWarning("first warning")
	app.addWarning("second warning")
	app.addWarning("third warning")

	if len(*app.warnings) != 3 {
		t.Fatalf("expected 3 warnings, got %d", len(*app.warnings))
	}
	if (*app.warnings)[0] != "first warning" {
		t.Errorf("expected 'first warning', got %q", (*app.warnings)[0])
	}
	if (*app.warnings)[1] != "second warning" {
		t.Errorf("expected 'second warning', got %q", (*app.warnings)[1])
	}
	if (*app.warnings)[2] != "third warning" {
		t.Errorf("expected 'third warning', got %q", (*app.warnings)[2])
	}
}

func TestAddWarning_ThreadSafe(t *testing.T) {
	app := &appEnv{}
	w := make([]string, 0)
	app.warnings = &w
	app.warningsMu = &sync.Mutex{}

	var wg sync.WaitGroup
	for i := range 100 {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			app.addWarning(fmt.Sprintf("warning %d", n))
		}(i)
	}
	wg.Wait()

	if len(*app.warnings) != 100 {
		t.Errorf("expected 100 warnings, got %d", len(*app.warnings))
	}
}

func TestPrintWarningSummary(t *testing.T) {
	tests := []struct {
		name      string
		warnings  []string
		wantOut   string
		wantEmpty bool
	}{
		{
			name:     "with warnings",
			warnings: []string{"image not found", "upload failed"},
			wantOut:  "2 warning(s):\n  - image not found\n  - upload failed\n",
		},
		{
			name:      "no warnings",
			warnings:  []string{},
			wantEmpty: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var stderr bytes.Buffer
			app := &appEnv{stderr: &stderr}
			w := make([]string, len(tt.warnings))
			copy(w, tt.warnings)
			app.warnings = &w
			app.warningsMu = &sync.Mutex{}

			app.printWarningSummary()

			output := stderr.String()
			if tt.wantEmpty {
				if output != "" {
					t.Errorf("expected no output for empty warnings, got %q", output)
				}
			} else {
				if !strings.Contains(output, tt.wantOut) {
					t.Errorf("expected output containing %q, got %q", tt.wantOut, output)
				}
			}
		})
	}
}

func TestPrintWarningSummary_NilWarnings(t *testing.T) {
	var stderr bytes.Buffer
	app := &appEnv{stderr: &stderr, warnings: nil}

	// Should not panic when warnings is nil
	app.printWarningSummary()

	if stderr.String() != "" {
		t.Errorf("expected no output for nil warnings, got %q", stderr.String())
	}
}
