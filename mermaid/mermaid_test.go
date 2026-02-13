// Copyright 2026 md2confl contributors
// SPDX-License-Identifier: Apache-2.0

package mermaid

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestEnsureAvailable_NotFound(t *testing.T) {
	// Override PATH to ensure mmdc is not found.
	t.Setenv("PATH", t.TempDir())

	err := EnsureAvailable()
	if err == nil {
		t.Fatal("expected error when mmdc is not in PATH")
	}
	if got := err.Error(); got == "" {
		t.Error("expected non-empty error message")
	}
}

func TestRender_Integration(t *testing.T) {
	if _, err := exec.LookPath("mmdc"); err != nil {
		t.Skip("mmdc not installed; skipping integration test")
	}

	dir := t.TempDir()
	r := &Renderer{OutputDir: dir}

	source := []byte("graph TD;\n    A-->B;\n    A-->C;")
	svgPath, err := r.Render(context.Background(), source)
	if err != nil {
		t.Fatalf("Render() error: %v", err)
	}

	// Verify SVG file exists and has content.
	info, err := os.Stat(svgPath)
	if err != nil {
		t.Fatalf("SVG file not found: %v", err)
	}
	if info.Size() == 0 {
		t.Error("SVG file is empty")
	}

	// Verify file is in the output directory.
	if filepath.Dir(svgPath) != dir {
		t.Errorf("SVG path %q not in output dir %q", svgPath, dir)
	}
}

func TestRender_Idempotent(t *testing.T) {
	if _, err := exec.LookPath("mmdc"); err != nil {
		t.Skip("mmdc not installed; skipping integration test")
	}

	dir := t.TempDir()
	r := &Renderer{OutputDir: dir}

	source := []byte("graph LR;\n    X-->Y;")
	path1, err := r.Render(context.Background(), source)
	if err != nil {
		t.Fatalf("first Render() error: %v", err)
	}

	path2, err := r.Render(context.Background(), source)
	if err != nil {
		t.Fatalf("second Render() error: %v", err)
	}

	if path1 != path2 {
		t.Errorf("expected same path, got %q and %q", path1, path2)
	}
}

func TestRender_DifferentSourcesDifferentPaths(t *testing.T) {
	if _, err := exec.LookPath("mmdc"); err != nil {
		t.Skip("mmdc not installed; skipping integration test")
	}

	dir := t.TempDir()
	r := &Renderer{OutputDir: dir}

	path1, err := r.Render(context.Background(), []byte("graph TD;\n    A-->B;"))
	if err != nil {
		t.Fatalf("Render() error: %v", err)
	}

	path2, err := r.Render(context.Background(), []byte("graph TD;\n    C-->D;"))
	if err != nil {
		t.Fatalf("Render() error: %v", err)
	}

	if path1 == path2 {
		t.Error("expected different paths for different sources")
	}
}
