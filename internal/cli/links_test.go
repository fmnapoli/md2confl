// Copyright 2026 md2confl contributors
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/fmnapoli/md2confl/adf"
)

func TestResolveInterDocLinks(t *testing.T) {
	dir := t.TempDir()
	doc1Path := filepath.Join(dir, "doc1.md")
	doc2Path := filepath.Join(dir, "doc2.md")

	doc1ADF := &adf.Document{
		Version: 1,
		Type:    "doc",
		Content: []adf.Node{
			{
				Type: "paragraph",
				Content: []adf.Node{
					{
						Type: "text",
						Text: "See doc2",
						Marks: []adf.Mark{
							{Type: "link", Attrs: map[string]any{"href": "doc2.md"}},
						},
					},
				},
			},
		},
	}
	doc2ADF := &adf.Document{
		Version: 1,
		Type:    "doc",
		Content: []adf.Node{
			{Type: "paragraph", Content: []adf.Node{{Type: "text", Text: "Doc 2 content"}}},
		},
	}

	docResults := map[string]*docPublishResult{
		doc1Path: {
			pageID:   "page-1",
			pageURL:  "https://site.atlassian.net/wiki/spaces/DEV/pages/1/Doc1",
			title:    "Doc 1",
			finalADF: doc1ADF,
		},
		doc2Path: {
			pageID:   "page-2",
			pageURL:  "https://site.atlassian.net/wiki/spaces/DEV/pages/2/Doc2",
			title:    "Doc 2",
			finalADF: doc2ADF,
		},
	}

	linkMap := make(map[string]string, len(docResults))
	for absPath, res := range docResults {
		linkMap[absPath] = res.pageURL
	}

	baseDir := filepath.Dir(doc1Path)
	count := adf.PatchDocLinks(doc1ADF, baseDir, linkMap, "", "")
	if count != 1 {
		t.Fatalf("expected 1 link patched in doc1, got %d", count)
	}

	href := doc1ADF.Content[0].Content[0].Marks[0].Attrs["href"].(string)
	if href != "https://site.atlassian.net/wiki/spaces/DEV/pages/2/Doc2" {
		t.Errorf("expected doc2 URL, got %q", href)
	}

	count = adf.PatchDocLinks(doc2ADF, baseDir, linkMap, "", "")
	if count != 0 {
		t.Errorf("doc2 should not have inter-doc links, got %d", count)
	}
}

func TestDryRunLinkPreview(t *testing.T) {
	dir := t.TempDir()

	cfgPath := writeConfigAndDocs(t, dir, `url: https://site.atlassian.net
space: DEVOPS
email: user@example.com
documents:
  - input: doc1.md
  - input: doc2.md
`, map[string]string{
		"doc1.md": "# Doc 1\n\nSee [doc2](doc2.md) for more.\n",
		"doc2.md": "# Doc 2\n\nContent here.\n",
	})

	t.Setenv("CONFLUENCE_TOKEN", "fake-token")

	var stdout, stderr strings.Builder
	code := Run([]string{"--config", cfgPath, "--dry-run"}, "test", &stdout, &stderr)
	if code != 0 {
		t.Errorf("expected exit code 0, got %d; stderr: %s", code, stderr.String())
	}

	if !strings.Contains(stderr.String(), "would resolve") {
		t.Errorf("expected dry-run link preview message, got %q", stderr.String())
	}
	if !strings.Contains(stderr.String(), "count=1") {
		t.Errorf("expected link count in preview, got %q", stderr.String())
	}
}
