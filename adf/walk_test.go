// Copyright 2026 md2confl contributors
// SPDX-License-Identifier: Apache-2.0

package adf

import "testing"

func TestIsLocalPath(t *testing.T) {
	tests := []struct {
		path  string
		local bool
	}{
		{"./img/photo.png", true},
		{"img/photo.png", true},
		{"../assets/logo.svg", true},
		{"https://example.com/logo.png", false},
		{"http://example.com/logo.png", false},
		{"//cdn.example.com/logo.png", false},
		{"data:image/svg+xml;base64,PHN2Zy...", false},
		{"data:image/png;base64,abc123", false},
	}
	for _, tt := range tests {
		if got := IsLocalPath(tt.path); got != tt.local {
			t.Errorf("IsLocalPath(%q) = %v, want %v", tt.path, got, tt.local)
		}
	}
}

func TestFindLocalImages(t *testing.T) {
	doc := &Document{
		Version: 1,
		Type:    "doc",
		Content: []Node{
			{
				Type:  "mediaSingle",
				Attrs: map[string]any{"layout": "center"},
				Content: []Node{
					{Type: "media", Attrs: map[string]any{"type": "external", "url": "./img/local.png"}},
				},
			},
			{
				Type:  "mediaSingle",
				Attrs: map[string]any{"layout": "center"},
				Content: []Node{
					{Type: "media", Attrs: map[string]any{"type": "external", "url": "https://example.com/remote.png"}},
				},
			},
		},
	}

	images := FindLocalImages(doc)
	if len(images) != 1 {
		t.Fatalf("expected 1 local image, got %d", len(images))
	}
	if images[0] != "./img/local.png" {
		t.Errorf("expected ./img/local.png, got %s", images[0])
	}
}

func TestPatchLocalImages(t *testing.T) {
	doc := &Document{
		Version: 1,
		Type:    "doc",
		Content: []Node{
			{
				Type: "mediaSingle",
				Content: []Node{
					{Type: "media", Attrs: map[string]any{"type": "external", "url": "./img/photo.png"}},
				},
			},
		},
	}

	PatchLocalImages(doc, map[string]string{"./img/photo.png": "file-uuid-999"}, "12345")

	media := doc.Content[0].Content[0]
	if media.Attrs["type"] != "file" {
		t.Errorf("expected type 'file', got %v", media.Attrs["type"])
	}
	if media.Attrs["id"] != "file-uuid-999" {
		t.Errorf("expected id 'file-uuid-999', got %v", media.Attrs["id"])
	}
	if media.Attrs["collection"] != "contentId-12345" {
		t.Errorf("expected collection 'contentId-12345', got %v", media.Attrs["collection"])
	}
	if _, exists := media.Attrs["url"]; exists {
		t.Error("expected url to be removed")
	}
}

func TestPatchDocLinks(t *testing.T) {
	linkMap := map[string]string{
		"/docs/instalacao.md": "https://site.atlassian.net/wiki/spaces/DEV/pages/123/Instalacao",
		"/docs/ci-cd.md":     "https://site.atlassian.net/wiki/spaces/DEV/pages/456/CI-CD",
	}

	tests := []struct {
		name       string
		href       string
		baseDir    string
		expectURL  string
		patchCount int
	}{
		{
			name:       "relative link resolved",
			href:       "instalacao.md",
			baseDir:    "/docs",
			expectURL:  "https://site.atlassian.net/wiki/spaces/DEV/pages/123/Instalacao",
			patchCount: 1,
		},
		{
			name:       "relative link with fragment",
			href:       "instalacao.md#secao",
			baseDir:    "/docs",
			expectURL:  "https://site.atlassian.net/wiki/spaces/DEV/pages/123/Instalacao#secao",
			patchCount: 1,
		},
		{
			name:       "absolute URL ignored",
			href:       "https://example.com/page",
			baseDir:    "/docs",
			expectURL:  "https://example.com/page",
			patchCount: 0,
		},
		{
			name:       "link to file not in config ignored",
			href:       "unknown.md",
			baseDir:    "/docs",
			expectURL:  "unknown.md",
			patchCount: 0,
		},
		{
			name:       "fragment-only link ignored",
			href:       "#section",
			baseDir:    "/docs",
			expectURL:  "#section",
			patchCount: 0,
		},
		{
			name:       "subdirectory relative link",
			href:       "../docs/ci-cd.md",
			baseDir:    "/other",
			expectURL:  "https://site.atlassian.net/wiki/spaces/DEV/pages/456/CI-CD",
			patchCount: 1,
		},
		{
			name:       "fragment preserved on different doc",
			href:       "ci-cd.md#pipeline",
			baseDir:    "/docs",
			expectURL:  "https://site.atlassian.net/wiki/spaces/DEV/pages/456/CI-CD#pipeline",
			patchCount: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			doc := &Document{
				Version: 1,
				Type:    "doc",
				Content: []Node{
					{
						Type: "paragraph",
						Content: []Node{
							{
								Type: "text",
								Text: "Click here",
								Marks: []Mark{
									{Type: "link", Attrs: map[string]any{"href": tt.href}},
								},
							},
						},
					},
				},
			}

			got := PatchDocLinks(doc, tt.baseDir, linkMap, "", "")
			if got != tt.patchCount {
				t.Errorf("PatchDocLinks returned %d, want %d", got, tt.patchCount)
			}

			href := doc.Content[0].Content[0].Marks[0].Attrs["href"].(string)
			if href != tt.expectURL {
				t.Errorf("href = %q, want %q", href, tt.expectURL)
			}
		})
	}
}

func TestPatchDocLinks_MultipleLinks(t *testing.T) {
	linkMap := map[string]string{
		"/docs/a.md": "https://site.atlassian.net/wiki/pages/1/A",
		"/docs/b.md": "https://site.atlassian.net/wiki/pages/2/B",
	}

	doc := &Document{
		Version: 1,
		Type:    "doc",
		Content: []Node{
			{
				Type: "paragraph",
				Content: []Node{
					{
						Type: "text",
						Text: "Link A",
						Marks: []Mark{
							{Type: "link", Attrs: map[string]any{"href": "a.md"}},
						},
					},
					{Type: "text", Text: " and "},
					{
						Type: "text",
						Text: "Link B",
						Marks: []Mark{
							{Type: "link", Attrs: map[string]any{"href": "b.md"}},
						},
					},
				},
			},
		},
	}

	count := PatchDocLinks(doc, "/docs", linkMap, "", "")
	if count != 2 {
		t.Errorf("expected PatchDocLinks to return 2, got %d", count)
	}

	hrefA := doc.Content[0].Content[0].Marks[0].Attrs["href"].(string)
	if hrefA != "https://site.atlassian.net/wiki/pages/1/A" {
		t.Errorf("link A href = %q, want Confluence URL", hrefA)
	}
	hrefB := doc.Content[0].Content[2].Marks[0].Attrs["href"].(string)
	if hrefB != "https://site.atlassian.net/wiki/pages/2/B" {
		t.Errorf("link B href = %q, want Confluence URL", hrefB)
	}
}

func TestPatchDocLinks_NestedContent(t *testing.T) {
	linkMap := map[string]string{
		"/docs/target.md": "https://site.atlassian.net/wiki/pages/99/Target",
	}

	doc := &Document{
		Version: 1,
		Type:    "doc",
		Content: []Node{
			{
				Type: "bulletList",
				Content: []Node{
					{
						Type: "listItem",
						Content: []Node{
							{
								Type: "paragraph",
								Content: []Node{
									{
										Type: "text",
										Text: "Nested link",
										Marks: []Mark{
											{Type: "link", Attrs: map[string]any{"href": "target.md"}},
										},
									},
								},
							},
						},
					},
				},
			},
		},
	}

	count := PatchDocLinks(doc, "/docs", linkMap, "", "")
	if count != 1 {
		t.Errorf("expected PatchDocLinks to return 1, got %d", count)
	}

	href := doc.Content[0].Content[0].Content[0].Content[0].Marks[0].Attrs["href"].(string)
	if href != "https://site.atlassian.net/wiki/pages/99/Target" {
		t.Errorf("nested href = %q, want Confluence URL", href)
	}
}

func TestPatchDocLinks_RepoURL(t *testing.T) {
	linkMap := map[string]string{
		"/repo/docs/setup.md": "https://site.atlassian.net/wiki/pages/1/Setup",
	}

	tests := []struct {
		name       string
		href       string
		baseDir    string
		repoURL    string
		repoRoot   string
		expectURL  string
		patchCount int
	}{
		{
			name:       "non-md link resolved to repo URL",
			href:       "LICENSE",
			baseDir:    "/repo",
			repoURL:    "https://github.com/user/repo/blob/main/",
			repoRoot:   "/repo",
			expectURL:  "https://github.com/user/repo/blob/main/LICENSE",
			patchCount: 1,
		},
		{
			name:       "non-md link from subdirectory",
			href:       "../LICENSE",
			baseDir:    "/repo/docs",
			repoURL:    "https://github.com/user/repo/blob/main/",
			repoRoot:   "/repo",
			expectURL:  "https://github.com/user/repo/blob/main/LICENSE",
			patchCount: 1,
		},
		{
			name:       "published md link resolved via linkMap (not repo URL)",
			href:       "setup.md",
			baseDir:    "/repo/docs",
			repoURL:    "https://github.com/user/repo/blob/main/",
			repoRoot:   "/repo",
			expectURL:  "https://site.atlassian.net/wiki/pages/1/Setup",
			patchCount: 1,
		},
		{
			name:       "link outside repo root not resolved",
			href:       "../../outside",
			baseDir:    "/repo",
			repoURL:    "https://github.com/user/repo/blob/main/",
			repoRoot:   "/repo",
			expectURL:  "../../outside",
			patchCount: 0,
		},
		{
			name:       "non-md link with fragment",
			href:       "Makefile#target",
			baseDir:    "/repo",
			repoURL:    "https://github.com/user/repo/blob/main/",
			repoRoot:   "/repo",
			expectURL:  "https://github.com/user/repo/blob/main/Makefile#target",
			patchCount: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			doc := &Document{
				Version: 1,
				Type:    "doc",
				Content: []Node{
					{
						Type: "paragraph",
						Content: []Node{
							{
								Type: "text",
								Text: "Click here",
								Marks: []Mark{
									{Type: "link", Attrs: map[string]any{"href": tt.href}},
								},
							},
						},
					},
				},
			}

			got := PatchDocLinks(doc, tt.baseDir, linkMap, tt.repoURL, tt.repoRoot)
			if got != tt.patchCount {
				t.Errorf("PatchDocLinks returned %d, want %d", got, tt.patchCount)
			}

			href := doc.Content[0].Content[0].Marks[0].Attrs["href"].(string)
			if href != tt.expectURL {
				t.Errorf("href = %q, want %q", href, tt.expectURL)
			}
		})
	}
}

func TestPatchDocLinks_NoRepoURL(t *testing.T) {
	linkMap := map[string]string{}
	doc := &Document{
		Version: 1,
		Type:    "doc",
		Content: []Node{
			{
				Type: "paragraph",
				Content: []Node{
					{
						Type: "text",
						Text: "License",
						Marks: []Mark{
							{Type: "link", Attrs: map[string]any{"href": "LICENSE"}},
						},
					},
				},
			},
		},
	}

	count := PatchDocLinks(doc, "/repo", linkMap, "", "")
	if count != 0 {
		t.Errorf("expected 0 patches without repoURL, got %d", count)
	}

	href := doc.Content[0].Content[0].Marks[0].Attrs["href"].(string)
	if href != "LICENSE" {
		t.Errorf("href should be unchanged, got %q", href)
	}
}

func TestCountResolvableLinks(t *testing.T) {
	linkMap := map[string]string{
		"/docs/target.md": "https://site.atlassian.net/wiki/pages/1/Target",
		"/docs/other.md":  "https://site.atlassian.net/wiki/pages/2/Other",
	}

	doc := &Document{
		Version: 1,
		Type:    "doc",
		Content: []Node{
			{
				Type: "paragraph",
				Content: []Node{
					{
						Type: "text",
						Text: "Link 1",
						Marks: []Mark{
							{Type: "link", Attrs: map[string]any{"href": "target.md"}},
						},
					},
					{
						Type: "text",
						Text: "Link 2",
						Marks: []Mark{
							{Type: "link", Attrs: map[string]any{"href": "other.md#section"}},
						},
					},
					{
						Type: "text",
						Text: "External",
						Marks: []Mark{
							{Type: "link", Attrs: map[string]any{"href": "https://example.com"}},
						},
					},
					{
						Type: "text",
						Text: "Unknown",
						Marks: []Mark{
							{Type: "link", Attrs: map[string]any{"href": "unknown.md"}},
						},
					},
				},
			},
		},
	}

	count := CountResolvableLinks(doc, "/docs", linkMap, "", "")
	if count != 2 {
		t.Errorf("expected 2 resolvable links, got %d", count)
	}

	// Verify the ADF was not modified (read-only)
	href := doc.Content[0].Content[0].Marks[0].Attrs["href"].(string)
	if href != "target.md" {
		t.Errorf("expected original href 'target.md', got %q (ADF was modified!)", href)
	}
}

func TestCountResolvableLinks_WithRepoURL(t *testing.T) {
	linkMap := map[string]string{
		"/repo/docs/target.md": "https://site.atlassian.net/wiki/pages/1/Target",
	}

	doc := &Document{
		Version: 1,
		Type:    "doc",
		Content: []Node{
			{
				Type: "paragraph",
				Content: []Node{
					{
						Type: "text",
						Text: "Link to published",
						Marks: []Mark{
							{Type: "link", Attrs: map[string]any{"href": "docs/target.md"}},
						},
					},
					{
						Type: "text",
						Text: "Link to LICENSE",
						Marks: []Mark{
							{Type: "link", Attrs: map[string]any{"href": "LICENSE"}},
						},
					},
					{
						Type: "text",
						Text: "External",
						Marks: []Mark{
							{Type: "link", Attrs: map[string]any{"href": "https://example.com"}},
						},
					},
					{
						Type: "text",
						Text: "Outside repo",
						Marks: []Mark{
							{Type: "link", Attrs: map[string]any{"href": "../../outside"}},
						},
					},
				},
			},
		},
	}

	count := CountResolvableLinks(doc, "/repo", linkMap, "https://github.com/user/repo/blob/main/", "/repo")
	if count != 2 {
		t.Errorf("expected 2 resolvable links (1 linkMap + 1 repoURL), got %d", count)
	}
}

func TestIsUnchanged(t *testing.T) {
	tests := []struct {
		name     string
		existing string
		newADF   string
		want     bool
	}{
		{
			name:     "same content different formatting",
			existing: `{"type":"doc","version":1,"content":[]}`,
			newADF:   "{\n  \"type\": \"doc\",\n  \"version\": 1,\n  \"content\": []\n}",
			want:     true,
		},
		{
			name:     "different content",
			existing: `{"type":"doc","version":1,"content":[{"type":"paragraph"}]}`,
			newADF:   `{"type":"doc","version":1,"content":[]}`,
			want:     false,
		},
		{
			name:     "empty existing",
			existing: "",
			newADF:   `{"type":"doc"}`,
			want:     false,
		},
		{
			name:     "invalid JSON in existing",
			existing: `{not valid json`,
			newADF:   `{"type":"doc"}`,
			want:     false,
		},
		{
			name:     "invalid JSON in new",
			existing: `{"type":"doc"}`,
			newADF:   `{not valid json`,
			want:     false,
		},
		{
			name:     "both identical",
			existing: `{"type":"doc","version":1}`,
			newADF:   `{"type":"doc","version":1}`,
			want:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IsUnchanged(tt.existing, tt.newADF)
			if got != tt.want {
				t.Errorf("IsUnchanged() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestIsUnchanged_EmptyArrayVsNull(t *testing.T) {
	existing := `{"type":"doc","version":1,"content":[{"type":"paragraph","content":null}]}`
	newADF := `{"type":"doc","version":1,"content":[{"type":"paragraph","content":[]}]}`
	if !IsUnchanged(existing, newADF) {
		t.Error("expected [] and null to be treated as equivalent")
	}
}

func TestRepoURLNormalization(t *testing.T) {
	linkMap := map[string]string{}
	doc := &Document{
		Version: 1,
		Type:    "doc",
		Content: []Node{
			{
				Type: "paragraph",
				Content: []Node{
					{
						Type: "text",
						Text: "License",
						Marks: []Mark{
							{Type: "link", Attrs: map[string]any{"href": "LICENSE"}},
						},
					},
				},
			},
		},
	}

	count := PatchDocLinks(doc, "/repo", linkMap, "https://github.com/user/repo/blob/main/", "/repo")
	if count != 1 {
		t.Fatalf("expected 1 patched link, got %d", count)
	}
	href := doc.Content[0].Content[0].Marks[0].Attrs["href"].(string)
	if href != "https://github.com/user/repo/blob/main/LICENSE" {
		t.Errorf("expected correct URL, got %q", href)
	}
}
