// Copyright 2026 md2confl contributors
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadConfig_Full(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, ".md2confl.yml")
	content := `url: https://site.atlassian.net
space: DEVOPS
email: user@example.com
parent-id: "12345"
force: true
write-marker: true

documents:
  - input: docs/architecture.md
    title: "Architecture Overview"
  - input: docs/runbook.md
    title: "Operations Runbook"
    space: OPS
    parent-id: "67890"
  - input: docs/spec.md
    output: dist/spec.json
`
	if err := os.WriteFile(cfgPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := loadConfig(cfgPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.URL != "https://site.atlassian.net" {
		t.Errorf("expected url 'https://site.atlassian.net', got %q", cfg.URL)
	}
	if cfg.Space != "DEVOPS" {
		t.Errorf("expected space 'DEVOPS', got %q", cfg.Space)
	}
	if cfg.Email != "user@example.com" {
		t.Errorf("expected email 'user@example.com', got %q", cfg.Email)
	}
	if cfg.ParentID != "12345" {
		t.Errorf("expected parent-id '12345', got %q", cfg.ParentID)
	}
	if cfg.Force == nil || !*cfg.Force {
		t.Error("expected force true")
	}
	if cfg.WriteMarker == nil || !*cfg.WriteMarker {
		t.Error("expected write-marker true")
	}
	if len(cfg.Documents) != 3 {
		t.Fatalf("expected 3 documents, got %d", len(cfg.Documents))
	}

	// Check paths are resolved relative to config dir
	if cfg.Documents[0].Input != filepath.Join(dir, "docs/architecture.md") {
		t.Errorf("expected resolved path, got %q", cfg.Documents[0].Input)
	}
	if cfg.Documents[0].Title != "Architecture Overview" {
		t.Errorf("expected title 'Architecture Overview', got %q", cfg.Documents[0].Title)
	}

	// Check document overrides
	if cfg.Documents[1].Space != "OPS" {
		t.Errorf("expected space 'OPS', got %q", cfg.Documents[1].Space)
	}
	if cfg.Documents[1].ParentID != "67890" {
		t.Errorf("expected parent-id '67890', got %q", cfg.Documents[1].ParentID)
	}

	// Check output mode (convert)
	if cfg.Documents[2].Output != filepath.Join(dir, "dist/spec.json") {
		t.Errorf("expected resolved output path, got %q", cfg.Documents[2].Output)
	}
}

func TestLoadConfig_Minimal(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, ".md2confl.yml")
	content := `url: https://site.atlassian.net
documents:
  - input: doc.md
`
	if err := os.WriteFile(cfgPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := loadConfig(cfgPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.URL != "https://site.atlassian.net" {
		t.Errorf("expected url, got %q", cfg.URL)
	}
	if len(cfg.Documents) != 1 {
		t.Fatalf("expected 1 document, got %d", len(cfg.Documents))
	}
}

func TestLoadConfig_GlobalsOnly(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, ".md2confl.yml")
	content := `url: https://site.atlassian.net
space: DEVOPS
email: user@example.com
`
	if err := os.WriteFile(cfgPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := loadConfig(cfgPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.URL != "https://site.atlassian.net" {
		t.Errorf("expected url, got %q", cfg.URL)
	}
	if len(cfg.Documents) != 0 {
		t.Errorf("expected 0 documents, got %d", len(cfg.Documents))
	}
}

func TestLoadConfig_TokenRejected(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, ".md2confl.yml")
	content := `url: https://site.atlassian.net
token: secret-token
documents:
  - input: doc.md
`
	if err := os.WriteFile(cfgPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := loadConfig(cfgPath)
	if err == nil {
		t.Fatal("expected error for token in config")
	}
	if want := "must not contain 'token'"; !contains(err.Error(), want) {
		t.Errorf("expected error containing %q, got %q", want, err.Error())
	}
}

func TestLoadConfig_EmptyInput(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, ".md2confl.yml")
	content := `url: https://site.atlassian.net
documents:
  - title: "No input"
`
	if err := os.WriteFile(cfgPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := loadConfig(cfgPath)
	if err == nil {
		t.Fatal("expected error for empty input")
	}
	if want := "missing required 'input' field"; !contains(err.Error(), want) {
		t.Errorf("expected error containing %q, got %q", want, err.Error())
	}
}

func TestLoadConfig_BoolPointers(t *testing.T) {
	dir := t.TempDir()

	tests := []struct {
		name      string
		yaml      string
		forceNil  bool
		forceVal  bool
		markerNil bool
		markerVal bool
	}{
		{
			name:      "force true",
			yaml:      "force: true\n",
			forceVal:  true,
			markerNil: true,
		},
		{
			name:      "force false",
			yaml:      "force: false\n",
			forceVal:  false,
			markerNil: true,
		},
		{
			name:     "omitted",
			yaml:     "url: https://example.com\n",
			forceNil: true, markerNil: true,
		},
		{
			name:      "write-marker true",
			yaml:      "write-marker: true\n",
			forceNil:  true,
			markerVal: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfgPath := filepath.Join(dir, tt.name+".yml")
			if err := os.WriteFile(cfgPath, []byte(tt.yaml), 0644); err != nil {
				t.Fatal(err)
			}
			cfg, err := loadConfig(cfgPath)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tt.forceNil && cfg.Force != nil {
				t.Errorf("expected force nil, got %v", *cfg.Force)
			}
			if !tt.forceNil && (cfg.Force == nil || *cfg.Force != tt.forceVal) {
				t.Errorf("expected force=%v", tt.forceVal)
			}
			if tt.markerNil && cfg.WriteMarker != nil {
				t.Errorf("expected write-marker nil, got %v", *cfg.WriteMarker)
			}
			if !tt.markerNil && (cfg.WriteMarker == nil || *cfg.WriteMarker != tt.markerVal) {
				t.Errorf("expected write-marker=%v", tt.markerVal)
			}
		})
	}
}

func TestLoadConfig_OutputMode(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, ".md2confl.yml")
	content := `documents:
  - input: doc.md
    output: out.json
  - input: other.md
`
	if err := os.WriteFile(cfgPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := loadConfig(cfgPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.Documents[0].Output == "" {
		t.Error("expected output field set")
	}
	if cfg.Documents[1].Output != "" {
		t.Errorf("expected empty output, got %q", cfg.Documents[1].Output)
	}
}

func TestLoadConfig_FileNotFound(t *testing.T) {
	_, err := loadConfig("/nonexistent/.md2confl.yml")
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestFindConfig_Found(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, ".md2confl.yml")
	if err := os.WriteFile(cfgPath, []byte("url: https://example.com\n"), 0644); err != nil {
		t.Fatal(err)
	}

	// Change to temp dir
	origDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(origDir) })

	found := findConfig()
	if found == "" {
		t.Fatal("expected to find config")
	}
}

func TestFindConfig_NotFound(t *testing.T) {
	dir := t.TempDir()

	origDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(origDir) })

	found := findConfig()
	if found != "" {
		t.Errorf("expected empty string, got %q", found)
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && searchString(s, substr)
}

func searchString(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
