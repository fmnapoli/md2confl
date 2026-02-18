// Copyright 2026 md2confl contributors
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"testing"

	"github.com/fmnapoli/md2confl/adf"
)

func TestFindMermaidBlocks(t *testing.T) {
	doc := &adf.Document{
		Version: 1,
		Type:    "doc",
		Content: []adf.Node{
			{
				Type:  "codeBlock",
				Attrs: map[string]any{"language": "mermaid"},
				Content: []adf.Node{
					{Type: "text", Text: "graph TD;\n    A-->B;"},
				},
			},
			{
				Type:  "codeBlock",
				Attrs: map[string]any{"language": "go"},
				Content: []adf.Node{
					{Type: "text", Text: "fmt.Println(\"hello\")"},
				},
			},
			{
				Type: "paragraph",
				Content: []adf.Node{
					{Type: "text", Text: "Some text"},
				},
			},
			{
				Type:  "codeBlock",
				Attrs: map[string]any{"language": "mermaid"},
				Content: []adf.Node{
					{Type: "text", Text: "sequenceDiagram\n    Alice->>Bob: Hello"},
				},
			},
			{
				Type:    "codeBlock",
				Content: []adf.Node{{Type: "text", Text: "plain code"}},
			},
		},
	}

	blocks := findMermaidBlocks(doc)
	if len(blocks) != 2 {
		t.Fatalf("expected 2 mermaid blocks, got %d", len(blocks))
	}
	if blocks[0].source != "graph TD;\n    A-->B;" {
		t.Errorf("unexpected source[0]: %q", blocks[0].source)
	}
	if blocks[1].source != "sequenceDiagram\n    Alice->>Bob: Hello" {
		t.Errorf("unexpected source[1]: %q", blocks[1].source)
	}
}

func TestFindMermaidBlocks_NoMermaid(t *testing.T) {
	doc := &adf.Document{
		Version: 1,
		Type:    "doc",
		Content: []adf.Node{
			{
				Type:  "codeBlock",
				Attrs: map[string]any{"language": "go"},
				Content: []adf.Node{
					{Type: "text", Text: "package main"},
				},
			},
			{
				Type: "paragraph",
				Content: []adf.Node{
					{Type: "text", Text: "No mermaid here"},
				},
			},
		},
	}

	blocks := findMermaidBlocks(doc)
	if len(blocks) != 0 {
		t.Errorf("expected 0 mermaid blocks, got %d", len(blocks))
	}
}

func TestPatchMermaidBlock(t *testing.T) {
	doc := &adf.Document{
		Version: 1,
		Type:    "doc",
		Content: []adf.Node{
			{
				Type:  "codeBlock",
				Attrs: map[string]any{"language": "mermaid"},
				Content: []adf.Node{
					{Type: "text", Text: "graph TD;\n    A-->B;"},
				},
			},
		},
	}

	blocks := findMermaidBlocks(doc)
	if len(blocks) != 1 {
		t.Fatalf("expected 1 block, got %d", len(blocks))
	}

	patchMermaidBlock(blocks[0], "/tmp/mermaid-abc123.svg")

	node := doc.Content[0]
	if node.Type != "mediaSingle" {
		t.Fatalf("expected mediaSingle, got %q", node.Type)
	}
	if node.Attrs["layout"] != "wide" {
		t.Errorf("expected layout 'wide', got %v", node.Attrs["layout"])
	}
	if len(node.Content) != 1 {
		t.Fatalf("expected 1 child, got %d", len(node.Content))
	}

	media := node.Content[0]
	if media.Type != "media" {
		t.Errorf("expected media, got %q", media.Type)
	}
	if media.Attrs["type"] != "external" {
		t.Errorf("expected type 'external', got %v", media.Attrs["type"])
	}
	if media.Attrs["url"] != "/tmp/mermaid-abc123.svg" {
		t.Errorf("expected url '/tmp/mermaid-abc123.svg', got %v", media.Attrs["url"])
	}
}
