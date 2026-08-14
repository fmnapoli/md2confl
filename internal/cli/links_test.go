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

func TestPatchStorageLinks(t *testing.T) {
	base := filepath.Join(string(filepath.Separator), "repo")
	linkMap := map[string]string{
		filepath.Join(base, "docs", "guide.md"): "https://wiki/display/X/Guide",
	}
	repoURL := "https://github.com/acme/repo/blob/main/"

	tests := []struct {
		name  string
		html  string
		want  string
		count int
	}{
		{
			name:  "plain relative link",
			html:  `<p><a href="docs/guide.md">Guide</a></p>`,
			want:  `<p><a href="https://wiki/display/X/Guide">Guide</a></p>`,
			count: 1,
		},
		{
			name:  "dot slash prefix",
			html:  `<p><a href="./docs/guide.md">Guide</a></p>`,
			want:  `<p><a href="https://wiki/display/X/Guide">Guide</a></p>`,
			count: 1,
		},
		{
			name:  "fragment is preserved",
			html:  `<p><a href="docs/guide.md#setup">Setup</a></p>`,
			want:  `<p><a href="https://wiki/display/X/Guide#setup">Setup</a></p>`,
			count: 1,
		},
		{
			name:  "unpublished target falls back to repo URL",
			html:  `<p><a href="Makefile">build</a></p>`,
			want:  `<p><a href="https://github.com/acme/repo/blob/main/Makefile">build</a></p>`,
			count: 1,
		},
		{
			name:  "absolute URL untouched",
			html:  `<p><a href="https://example.com/x.md">ext</a></p>`,
			want:  `<p><a href="https://example.com/x.md">ext</a></p>`,
			count: 0,
		},
		{
			name:  "internal anchor untouched",
			html:  `<p><a href="#secao">section</a></p>`,
			want:  `<p><a href="#secao">section</a></p>`,
			count: 0,
		},
		{
			name:  "macro attribute ac:href untouched",
			html:  `<ac:link ac:href="docs/guide.md"/>`,
			want:  `<ac:link ac:href="docs/guide.md"/>`,
			count: 0,
		},
		{
			name:  "several links in one document",
			html:  `<a href="docs/guide.md">a</a><a href="./docs/guide.md#x">b</a>`,
			want:  `<a href="https://wiki/display/X/Guide">a</a><a href="https://wiki/display/X/Guide#x">b</a>`,
			count: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, count := patchStorageLinks(tt.html, base, linkMap, repoURL, base)
			if count != tt.count {
				t.Errorf("count = %d, want %d", count, tt.count)
			}
			if got != tt.want {
				t.Errorf("got  %s\nwant %s", got, tt.want)
			}
		})
	}
}

func TestServerLinkMap_FallsBackToViewpage(t *testing.T) {
	app := &appEnv{
		url: "https://tdn.example.com/",
		docResults: map[string]*docPublishResult{
			"/repo/a.md": {pageID: "1", pageURL: "https://tdn.example.com/display/X/A"},
			"/repo/b.md": {pageID: "2"},
			"/repo/c.md": {},
		},
	}

	linkMap := app.serverLinkMap()

	if got, want := linkMap["/repo/a.md"], "https://tdn.example.com/display/X/A"; got != want {
		t.Errorf("a.md = %q, want %q", got, want)
	}
	if got, want := linkMap["/repo/b.md"], "https://tdn.example.com/pages/viewpage.action?pageId=2"; got != want {
		t.Errorf("b.md = %q, want %q", got, want)
	}
	if _, ok := linkMap["/repo/c.md"]; ok {
		t.Error("documents without page ID must stay out of the link map")
	}
}
