// Copyright 2026 md2confl contributors
// SPDX-License-Identifier: Apache-2.0

package adftomd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/fmnapoli/md2confl/adf"
)

func TestGoldenFiles(t *testing.T) {
	entries, err := filepath.Glob("testdata/*.adf.json")
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) == 0 {
		t.Fatal("no golden files found in testdata/")
	}

	for _, adfPath := range entries {
		name := strings.TrimSuffix(filepath.Base(adfPath), ".adf.json")
		t.Run(name, func(t *testing.T) {
			// Read ADF input
			adfData, err := os.ReadFile(adfPath)
			if err != nil {
				t.Fatal(err)
			}

			var doc adf.Document
			if err := json.Unmarshal(adfData, &doc); err != nil {
				t.Fatalf("parsing ADF: %v", err)
			}

			// Convert
			got := Convert(&doc)

			// Read expected Markdown
			mdPath := strings.TrimSuffix(adfPath, ".adf.json") + ".md"
			expected, err := os.ReadFile(mdPath)
			if err != nil {
				t.Fatalf("reading expected file %s: %v", mdPath, err)
			}

			if string(got) != string(expected) {
				t.Errorf("mismatch for %s:\n--- got ---\n%s\n--- want ---\n%s", name, got, expected)
			}
		})
	}
}

func TestConvert_Nil(t *testing.T) {
	result := Convert(nil)
	if result != nil {
		t.Errorf("expected nil for nil doc, got %q", result)
	}
}

func TestConvertWithOptions_ImageRewriter(t *testing.T) {
	doc := &adf.Document{
		Version: 1,
		Type:    "doc",
		Content: []adf.Node{
			{
				Type: "mediaSingle",
				Content: []adf.Node{
					{
						Type:  "media",
						Attrs: map[string]any{"url": "https://confluence/att/img.png", "alt": "photo"},
					},
				},
			},
		},
	}

	opts := Options{
		ImageRewriter: func(url string) string {
			return "attachments/img.png"
		},
	}

	got := string(ConvertWithOptions(doc, opts))
	if !strings.Contains(got, "![photo](attachments/img.png)") {
		t.Errorf("expected rewritten URL, got:\n%s", got)
	}
}
