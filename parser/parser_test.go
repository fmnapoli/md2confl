// Copyright 2026 md2confl contributors
// SPDX-License-Identifier: Apache-2.0

package parser

import (
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var update = flag.Bool("update", false, "update golden files")

func TestConvertToADF(t *testing.T) {
	entries, err := os.ReadDir("testdata")
	if err != nil {
		t.Fatal(err)
	}

	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), ".md") {
			continue
		}

		name := strings.TrimSuffix(entry.Name(), ".md")
		t.Run(name, func(t *testing.T) {
			inputPath := filepath.Join("testdata", entry.Name())
			goldenPath := filepath.Join("testdata", name+".json")

			input, err := os.ReadFile(inputPath)
			if err != nil {
				t.Fatal(err)
			}

			doc, err := ConvertToADF(input)
			if err != nil {
				t.Fatalf("ConvertToADF failed: %v", err)
			}

			got, err := json.MarshalIndent(doc, "", "  ")
			if err != nil {
				t.Fatalf("MarshalIndent failed: %v", err)
			}
			got = append(got, '\n')

			if *update {
				if err := os.WriteFile(goldenPath, got, 0644); err != nil {
					t.Fatal(err)
				}
				t.Log("updated golden file:", goldenPath)
				return
			}

			expected, err := os.ReadFile(goldenPath)
			if err != nil {
				t.Fatalf("golden file not found: %s (run with -update to create)", goldenPath)
			}

			if string(got) != string(expected) {
				t.Errorf("output mismatch for %s.\nGot:\n%s\nWant:\n%s", name, string(got), string(expected))
			}
		})
	}
}
