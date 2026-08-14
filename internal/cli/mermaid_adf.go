// Copyright 2026 md2confl contributors
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"context"
	"fmt"
	"os"
	"strings"

	"golang.org/x/sync/errgroup"

	"github.com/fmnapoli/md2confl/adf"
	"github.com/fmnapoli/md2confl/mermaid"
)

// mermaidBlock holds information about a mermaid codeBlock in the ADF tree.
type mermaidBlock struct {
	index  int         // position in parent's Content slice
	parent *[]adf.Node // pointer to parent's Content slice
	source string      // the mermaid diagram source text
}

// findMermaidBlocks walks the ADF document and returns all codeBlocks with language "mermaid".
func findMermaidBlocks(doc *adf.Document) []mermaidBlock {
	var blocks []mermaidBlock
	collectMermaidBlocks(&doc.Content, &blocks)
	return blocks
}

func collectMermaidBlocks(nodes *[]adf.Node, blocks *[]mermaidBlock) {
	for i := range *nodes {
		node := &(*nodes)[i]
		if node.Type == "codeBlock" {
			if lang, ok := node.Attrs["language"].(string); ok && lang == "mermaid" {
				source := extractCodeBlockText(node)
				*blocks = append(*blocks, mermaidBlock{
					index:  i,
					parent: nodes,
					source: source,
				})
			}
		}
		if len(node.Content) > 0 {
			collectMermaidBlocks(&node.Content, blocks)
		}
	}
}

func extractCodeBlockText(node *adf.Node) string {
	var sb strings.Builder
	for _, child := range node.Content {
		if child.Type == "text" {
			sb.WriteString(child.Text)
		}
	}
	return sb.String()
}

// patchMermaidBlock replaces a mermaid codeBlock with a mediaSingle > media node
// pointing to the local SVG file path.
func patchMermaidBlock(block mermaidBlock, svgPath string) {
	(*block.parent)[block.index] = adf.Node{
		Type:  "mediaSingle",
		Attrs: map[string]any{"layout": "wide"},
		Content: []adf.Node{
			{
				Type: "media",
				Attrs: map[string]any{
					"type": "external",
					"url":  svgPath,
				},
			},
		},
	}
}

// renderMermaidBlocks renders all mermaid codeBlocks in the ADF document to SVG
// and patches them in-place. Returns true if any blocks were rendered.
// If mmdc is not available, blocks are left as codeBlocks (no error).
func renderMermaidBlocks(doc *adf.Document) (bool, error) {
	blocks := findMermaidBlocks(doc)
	if len(blocks) == 0 {
		return false, nil
	}

	if err := mermaid.EnsureAvailable(); err != nil {
		// mmdc not installed — skip rendering silently
		return false, nil
	}

	tempDir, err := os.MkdirTemp("", "md2confl-mermaid-*")
	if err != nil {
		return false, fmt.Errorf("creating temp dir for mermaid: %w", err)
	}
	// Note: caller is responsible for cleanup via the tempDir embedded in SVG paths,
	// but we rely on OS cleanup of /tmp. The files need to survive until upload.

	renderer := &mermaid.Renderer{OutputDir: tempDir}

	// Render all blocks in parallel (limit 2 — mmdc is heavy/Chromium)
	type renderResult struct {
		index   int
		svgPath string
	}
	results := make([]renderResult, len(blocks))
	g, ctx := errgroup.WithContext(context.Background())
	g.SetLimit(2)
	for i, block := range blocks {
		g.Go(func() error {
			svgPath, err := renderer.Render(ctx, []byte(block.source))
			if err != nil {
				return fmt.Errorf("rendering mermaid diagram: %w", err)
			}
			results[i] = renderResult{index: i, svgPath: svgPath}
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		return false, err
	}

	// Patch sequentially after all renders complete
	for i, block := range blocks {
		patchMermaidBlock(block, results[i].svgPath)
	}

	return true, nil
}

// countMermaidSVGs counts how many media nodes reference SVG files from mermaid rendering.
func countMermaidSVGs(doc *adf.Document) int {
	count := 0
	for _, node := range doc.Content {
		if node.Type == "mediaSingle" {
			for _, child := range node.Content {
				if child.Type == "media" {
					if url, ok := child.Attrs["url"].(string); ok && strings.Contains(url, "mermaid-") {
						count++
					}
				}
			}
		}
	}
	return count
}
