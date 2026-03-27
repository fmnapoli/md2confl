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
	"sync"
	"time"
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
	OutputDir      string // directory where SVG files are written
	puppeteerOnce  sync.Once
	puppeteerError error
}

// Timeout is the maximum duration for a single mermaid render.
// Defaults to 60 seconds if zero.
const DefaultRenderTimeout = 60 * time.Second

// Render takes Mermaid source, renders it to SVG, and returns the SVG file path.
// The output filename is deterministic: mermaid-<sha256[:12]>.svg.
// A 60-second timeout is applied to the mmdc subprocess.
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

	// Write puppeteer config atomically (safe for parallel renders).
	puppeteerCfg := filepath.Join(r.OutputDir, "puppeteer-config.json")
	r.puppeteerOnce.Do(func() {
		cfg, _ := json.Marshal(map[string]any{
			"args": []string{"--no-sandbox", "--disable-setuid-sandbox", "--disable-dev-shm-usage", "--disable-gpu"},
		})
		r.puppeteerError = os.WriteFile(puppeteerCfg, cfg, 0644)
	})
	if r.puppeteerError != nil {
		return "", fmt.Errorf("writing puppeteer config: %w", r.puppeteerError)
	}

	// Apply timeout to the subprocess
	renderCtx, cancel := context.WithTimeout(ctx, DefaultRenderTimeout)
	defer cancel()

	cmd := exec.CommandContext(renderCtx, "mmdc",
		"-i", inputPath,
		"-o", svgPath,
		"-e", "svg",
		"-p", puppeteerCfg,
	)
	output, err := cmd.CombinedOutput()
	if err != nil {
		if renderCtx.Err() == context.DeadlineExceeded {
			return "", fmt.Errorf("mermaid rendering timed out after %s", DefaultRenderTimeout)
		}
		return "", fmt.Errorf("mmdc failed: %w\nOutput: %s", err, output)
	}

	return svgPath, nil
}
