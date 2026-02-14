// Copyright 2026 md2confl contributors
// SPDX-License-Identifier: Apache-2.0

package mermaid

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
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

// writeFakeMMDC creates a fake "mmdc" executable shell script in the given
// directory with the provided body, and returns the directory path.
func writeFakeMMDC(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	script := filepath.Join(dir, "mmdc")
	content := "#!/bin/sh\n" + body
	if err := os.WriteFile(script, []byte(content), 0755); err != nil {
		t.Fatalf("writing fake mmdc: %v", err)
	}
	return dir
}

func TestEnsureAvailable_Found(t *testing.T) {
	dir := writeFakeMMDC(t, "exit 0\n")
	t.Setenv("PATH", dir)

	if err := EnsureAvailable(); err != nil {
		t.Fatalf("expected EnsureAvailable() to succeed, got: %v", err)
	}
}

func TestRender_Timeout(t *testing.T) {
	dir := writeFakeMMDC(t, "sleep 5\n")
	t.Setenv("PATH", dir+":/usr/bin:/bin")

	outDir := t.TempDir()
	r := &Renderer{OutputDir: outDir}

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	_, err := r.Render(ctx, []byte("graph TD;\n    A-->B;"))
	if err == nil {
		t.Fatal("expected error due to timeout, got nil")
	}
	if !strings.Contains(err.Error(), "timed out") {
		t.Errorf("expected error to contain %q, got: %v", "timed out", err)
	}
}

func TestRender_Failure(t *testing.T) {
	dir := writeFakeMMDC(t, "echo 'error: bad diagram' >&2\nexit 1\n")
	t.Setenv("PATH", dir+":/usr/bin:/bin")

	outDir := t.TempDir()
	r := &Renderer{OutputDir: outDir}

	_, err := r.Render(context.Background(), []byte("graph TD;\n    A-->B;"))
	if err == nil {
		t.Fatal("expected error from failed mmdc, got nil")
	}
	if !strings.Contains(err.Error(), "mmdc failed") {
		t.Errorf("expected error to contain %q, got: %v", "mmdc failed", err)
	}
}
