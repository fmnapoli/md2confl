// Copyright 2026 md2confl contributors
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"testing"
)

func TestSanitizeFilename(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"simple", "Hello World", "hello-world"},
		{"special chars", `File/Name:With*Bad?"Chars`, "file-name-with-bad-chars"},
		{"consecutive hyphens", "a---b---c", "a-b-c"},
		{"empty title", "", "untitled"},
		{"only special chars", "***???", "untitled"},
		{"trim hyphens", "---hello---", "hello"},
		{"unicode", "Seção de Arquitetura", "seção-de-arquitetura"},
		{"tabs and spaces", "hello\tworld\nnew", "hello-world-new"},
		{"pipe and angle", "A | B <C> D", "a-b-c-d"},
		{"backslash", `path\to\page`, "path-to-page"},
		{"long title", string(make([]byte, 300)), "untitled"}, // 300 null bytes → all control chars → empty
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := sanitizeFilename(tt.input)
			if got != tt.expected {
				t.Errorf("sanitizeFilename(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}

func TestSanitizeFilename_LongTitle(t *testing.T) {
	// 210 character title
	long := ""
	for i := 0; i < 210; i++ {
		long += "a"
	}
	result := sanitizeFilename(long)
	if len(result) > 200 {
		t.Errorf("expected max 200 chars, got %d", len(result))
	}
}
