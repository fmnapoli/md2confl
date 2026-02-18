// Copyright 2026 md2confl contributors
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadPullConfig_Valid(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, ".confl2md.yml")
	content := `
url: https://site.atlassian.net
space: DEVOPS
email: user@example.com
pages:
  - page-id: "12345"
    output-dir: ./docs
    recursive: true
    depth: 5
  - title: "Architecture Overview"
    output-dir: ./arch
`
	if err := os.WriteFile(cfgPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := loadPullConfig(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.URL != "https://site.atlassian.net" {
		t.Errorf("expected URL, got %s", cfg.URL)
	}
	if cfg.Space != "DEVOPS" {
		t.Errorf("expected DEVOPS, got %s", cfg.Space)
	}
	if len(cfg.Pages) != 2 {
		t.Fatalf("expected 2 pages, got %d", len(cfg.Pages))
	}
	if cfg.Pages[0].PageID != "12345" {
		t.Errorf("expected page-id 12345, got %s", cfg.Pages[0].PageID)
	}
	if cfg.Pages[0].Depth != 5 {
		t.Errorf("expected depth 5, got %d", cfg.Pages[0].Depth)
	}
	if !cfg.Pages[0].Recursive {
		t.Error("expected recursive true")
	}
	if cfg.Pages[1].Title != "Architecture Overview" {
		t.Errorf("expected title, got %s", cfg.Pages[1].Title)
	}
	// Depth default
	if cfg.Pages[1].Depth != 10 {
		t.Errorf("expected default depth 10, got %d", cfg.Pages[1].Depth)
	}
	// Output dir resolved relative to config dir
	if !filepath.IsAbs(cfg.Pages[0].OutputDir) {
		t.Errorf("expected absolute output-dir, got %s", cfg.Pages[0].OutputDir)
	}
}

func TestLoadPullConfig_MissingFields(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, ".confl2md.yml")
	content := `
url: https://site.atlassian.net
pages:
  - recursive: true
`
	if err := os.WriteFile(cfgPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := loadPullConfig(cfgPath)
	if err == nil {
		t.Fatal("expected error for missing page-id/title")
	}
}

func TestLoadPullConfig_MutuallyExclusive(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, ".confl2md.yml")
	content := `
url: https://site.atlassian.net
space: DEVOPS
pages:
  - page-id: "12345"
    title: "Some Title"
`
	if err := os.WriteFile(cfgPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := loadPullConfig(cfgPath)
	if err == nil {
		t.Fatal("expected error for mutually exclusive page-id/title")
	}
}

func TestLoadPullConfig_TokenRejected(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, ".confl2md.yml")
	content := `
url: https://site.atlassian.net
token: secret123
pages:
  - page-id: "12345"
`
	if err := os.WriteFile(cfgPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := loadPullConfig(cfgPath)
	if err == nil {
		t.Fatal("expected error for token in config")
	}
}

func TestLoadPullConfig_DepthBounds(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, ".confl2md.yml")
	content := `
url: https://site.atlassian.net
pages:
  - page-id: "12345"
    depth: 200
`
	if err := os.WriteFile(cfgPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := loadPullConfig(cfgPath)
	if err == nil {
		t.Fatal("expected error for depth > 100")
	}
}

func TestLoadPullConfig_TitleRequiresSpace(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, ".confl2md.yml")
	content := `
url: https://site.atlassian.net
pages:
  - title: "My Page"
`
	if err := os.WriteFile(cfgPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := loadPullConfig(cfgPath)
	if err == nil {
		t.Fatal("expected error when title is set without space")
	}
}
