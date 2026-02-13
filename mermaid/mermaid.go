// Copyright 2026 md2confl contributors
// SPDX-License-Identifier: Apache-2.0

// Package mermaid renders Mermaid diagrams to SVG using the mmdc CLI.
package mermaid

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// EnsureAvailable checks that mmdc (mermaid-cli) is available on the PATH.
func EnsureAvailable() error {
	_, err := exec.LookPath("mmdc")
	if err != nil {
		return fmt.Errorf("mmdc not found in PATH; install @mermaid-js/mermaid-cli or use the md2confl Docker image")
	}
	return nil
}

// Renderer renders Mermaid diagram source to SVG files using mmdc.
type Renderer struct {
	OutputDir string // directory where SVG files are written
}

// Render takes Mermaid source, renders it to SVG, and returns the SVG file path.
// The output filename is deterministic: mermaid-<sha256[:12]>.svg.
func (r *Renderer) Render(ctx context.Context, source []byte) (string, error) {
	hash := fmt.Sprintf("%x", sha256.Sum256(source))[:12]
	svgPath := filepath.Join(r.OutputDir, fmt.Sprintf("mermaid-%s.svg", hash))

	// If already rendered (idempotent), return existing file.
	if _, err := os.Stat(svgPath); err == nil {
		return svgPath, nil
	}

	// Write source to a temporary .mmd file.
	inputPath := filepath.Join(r.OutputDir, fmt.Sprintf("mermaid-%s.mmd", hash))
	if err := os.WriteFile(inputPath, source, 0644); err != nil {
		return "", fmt.Errorf("writing mermaid source: %w", err)
	}
	defer os.Remove(inputPath)

	// Write puppeteer config for --no-sandbox (required in Docker/CI).
	puppeteerCfg := filepath.Join(r.OutputDir, "puppeteer-config.json")
	if _, err := os.Stat(puppeteerCfg); os.IsNotExist(err) {
		cfg, _ := json.Marshal(map[string]any{
			"args": []string{"--no-sandbox", "--disable-setuid-sandbox"},
		})
		if err := os.WriteFile(puppeteerCfg, cfg, 0644); err != nil {
			return "", fmt.Errorf("writing puppeteer config: %w", err)
		}
	}

	cmd := exec.CommandContext(ctx, "mmdc",
		"-i", inputPath,
		"-o", svgPath,
		"-e", "svg",
		"-p", puppeteerCfg,
	)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("mmdc failed: %w\nOutput: %s", err, output)
	}

	return svgPath, nil
}
