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

func TestConvertToADF_ImageAltText(t *testing.T) {
	input := []byte("![My Alt Text](image.png)\n")
	doc, err := ConvertToADF(input)
	if err != nil {
		t.Fatal(err)
	}

	// Find the mediaSingle > media node
	if len(doc.Content) == 0 {
		t.Fatal("expected at least one node")
	}
	mediaSingle := doc.Content[0]
	if mediaSingle.Type != "mediaSingle" {
		t.Fatalf("expected mediaSingle, got %q", mediaSingle.Type)
	}
	if len(mediaSingle.Content) == 0 {
		t.Fatal("mediaSingle has no children")
	}
	media := mediaSingle.Content[0]
	if media.Type != "media" {
		t.Fatalf("expected media, got %q", media.Type)
	}
	alt, ok := media.Attrs["alt"].(string)
	if !ok || alt != "My Alt Text" {
		t.Errorf("expected alt 'My Alt Text', got %v", media.Attrs["alt"])
	}
}

func TestConvertToADF_MixedTaskList(t *testing.T) {
	// A list with both checkbox and non-checkbox items
	input := []byte("- [ ] Task item\n- Regular item\n")
	doc, err := ConvertToADF(input)
	if err != nil {
		t.Fatal(err)
	}

	if len(doc.Content) == 0 {
		t.Fatal("expected at least one node")
	}
	list := doc.Content[0]
	// Should be bulletList, not taskList (mixed)
	if list.Type != "bulletList" {
		t.Fatalf("expected bulletList for mixed list, got %q", list.Type)
	}
	// All children should be listItem (not taskItem)
	for i, item := range list.Content {
		if item.Type != "listItem" {
			t.Errorf("child %d: expected listItem, got %q", i, item.Type)
		}
	}
}

func TestConvertToADF_LinkWithEmoji(t *testing.T) {
	// A link wrapping an emoji — only text nodes should get link marks
	input := []byte("[Hello :wave:](https://example.com)\n")
	doc, err := ConvertToADF(input)
	if err != nil {
		t.Fatal(err)
	}

	if len(doc.Content) == 0 {
		t.Fatal("expected at least one node")
	}
	para := doc.Content[0]
	if para.Type != "paragraph" {
		t.Fatalf("expected paragraph, got %q", para.Type)
	}
	for _, child := range para.Content {
		if child.Type == "emoji" {
			if len(child.Marks) > 0 {
				t.Error("emoji node should not have link marks")
			}
		}
		if child.Type == "text" {
			hasLink := false
			for _, mark := range child.Marks {
				if mark.Type == "link" {
					hasLink = true
				}
			}
			if !hasLink {
				t.Errorf("text node %q should have link mark", child.Text)
			}
		}
	}
}
