// Copyright 2026 md2confl contributors
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// PullConfig represents the .confl2md.yml configuration file.
type PullConfig struct {
	URL   string           `yaml:"url"`
	Space string           `yaml:"space"`
	Email string           `yaml:"email"`
	Pages []PullPageConfig `yaml:"pages"`
}

// PullPageConfig represents a single page entry in the pull config file.
type PullPageConfig struct {
	PageID          string `yaml:"page-id"`
	Title           string `yaml:"title"`
	OutputDir       string `yaml:"output-dir"`
	Recursive       bool   `yaml:"recursive"`
	Depth           int    `yaml:"depth"`
	SkipAttachments bool   `yaml:"skip-attachments"`
}

// loadPullConfig reads and parses a .confl2md.yml file.
func loadPullConfig(path string) (*PullConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading config %q: %w", path, err)
	}

	// Reject token in config for security
	var raw map[string]any
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("parsing config %q: %w", path, err)
	}
	if _, ok := raw["token"]; ok {
		return nil, fmt.Errorf("config %q must not contain 'token' — use CONFLUENCE_TOKEN env var or --token flag", path)
	}

	var cfg PullConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parsing config %q: %w", path, err)
	}

	if err := validatePullConfig(&cfg); err != nil {
		return nil, fmt.Errorf("config %q: %w", path, err)
	}

	// Resolve relative output-dir paths against config dir
	configDir := filepath.Dir(path)
	for i, page := range cfg.Pages {
		if page.OutputDir != "" && !filepath.IsAbs(page.OutputDir) {
			cfg.Pages[i].OutputDir = filepath.Join(configDir, page.OutputDir)
		}
		if page.Depth == 0 {
			cfg.Pages[i].Depth = 10
		}
	}

	return &cfg, nil
}

// validatePullConfig checks config invariants.
func validatePullConfig(cfg *PullConfig) error {
	for i, page := range cfg.Pages {
		if page.PageID == "" && page.Title == "" {
			return fmt.Errorf("pages[%d]: must specify either 'page-id' or 'title'", i)
		}
		if page.PageID != "" && page.Title != "" {
			return fmt.Errorf("pages[%d]: 'page-id' and 'title' are mutually exclusive", i)
		}
		if page.Title != "" && cfg.Space == "" {
			return fmt.Errorf("pages[%d]: 'title' requires 'space' to be set", i)
		}
		if page.Depth < 0 || page.Depth > 100 {
			return fmt.Errorf("pages[%d]: 'depth' must be between 1 and 100", i)
		}
	}
	return nil
}

// findPullConfig looks for .confl2md.yml in the current working directory.
func findPullConfig() string {
	candidates := []string{".confl2md.yml", ".confl2md.yaml"}
	for _, name := range candidates {
		if info, err := os.Stat(name); err == nil && !info.IsDir() {
			abs, err := filepath.Abs(name)
			if err != nil {
				return name
			}
			return abs
		}
	}
	return ""
}
