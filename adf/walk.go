// Copyright 2026 md2confl contributors
// SPDX-License-Identifier: Apache-2.0

package adf

import (
	"encoding/json"
	"path/filepath"
	"strings"
)

// IsLocalPath returns true if the given path is a local file reference (not a URL).
func IsLocalPath(path string) bool {
	return !strings.HasPrefix(path, "http://") &&
		!strings.HasPrefix(path, "https://") &&
		!strings.HasPrefix(path, "//") &&
		!strings.HasPrefix(path, "data:")
}

// FindLocalImages walks the ADF tree and collects local image URLs.
func FindLocalImages(doc *Document) []string {
	var images []string
	for _, node := range doc.Content {
		collectLocalImages(&node, &images)
	}
	return images
}

func collectLocalImages(node *Node, images *[]string) {
	if node.Type == "media" {
		if t, ok := node.Attrs["type"].(string); ok && t == "external" {
			if u, ok := node.Attrs["url"].(string); ok && IsLocalPath(u) {
				*images = append(*images, u)
			}
		}
	}
	for i := range node.Content {
		collectLocalImages(&node.Content[i], images)
	}
}

// PatchLocalImages replaces external media nodes for local images with file attachment references.
func PatchLocalImages(doc *Document, attachmentMap map[string]string, pageID string) {
	for i := range doc.Content {
		patchImageNode(&doc.Content[i], attachmentMap, pageID)
	}
}

func patchImageNode(node *Node, attachmentMap map[string]string, pageID string) {
	if node.Type == "media" {
		if t, ok := node.Attrs["type"].(string); ok && t == "external" {
			if u, ok := node.Attrs["url"].(string); ok {
				if fileID, found := attachmentMap[u]; found {
					node.Attrs["type"] = "file"
					node.Attrs["id"] = fileID
					node.Attrs["collection"] = "contentId-" + pageID
					delete(node.Attrs, "url")
				}
			}
		}
	}
	for i := range node.Content {
		patchImageNode(&node.Content[i], attachmentMap, pageID)
	}
}

// PatchDocLinks walks the ADF tree and replaces relative Markdown link hrefs
// with Confluence page URLs using the provided linkMap. Links not found in
// linkMap are resolved to the repository URL if repoURL and repoRoot are set.
// Returns the number of links patched.
func PatchDocLinks(doc *Document, baseDir string, linkMap map[string]string, repoURL, repoRoot string) int {
	count := 0
	for i := range doc.Content {
		count += patchNodeLinks(&doc.Content[i], baseDir, linkMap, repoURL, repoRoot)
	}
	return count
}

func patchNodeLinks(node *Node, baseDir string, linkMap map[string]string, repoURL, repoRoot string) int {
	count := 0

	for i := range node.Marks {
		if node.Marks[i].Type != "link" {
			continue
		}
		href, ok := node.Marks[i].Attrs["href"].(string)
		if !ok || href == "" {
			continue
		}
		if strings.HasPrefix(href, "http://") || strings.HasPrefix(href, "https://") || strings.HasPrefix(href, "//") {
			continue
		}

		cleanHref := href
		fragment := ""
		if idx := strings.Index(cleanHref, "#"); idx >= 0 {
			fragment = cleanHref[idx:]
			cleanHref = cleanHref[:idx]
		}
		if cleanHref == "" {
			continue
		}

		resolved := filepath.Join(baseDir, cleanHref)
		absResolved, err := filepath.Abs(resolved)
		if err != nil {
			continue
		}

		if pageURL, found := linkMap[absResolved]; found {
			node.Marks[i].Attrs["href"] = pageURL + fragment
			count++
		} else if repoURL != "" && repoRoot != "" {
			relPath, err := filepath.Rel(repoRoot, absResolved)
			if err == nil && !strings.HasPrefix(relPath, "..") {
				node.Marks[i].Attrs["href"] = repoURL + relPath + fragment
				count++
			}
		}
	}

	for i := range node.Content {
		count += patchNodeLinks(&node.Content[i], baseDir, linkMap, repoURL, repoRoot)
	}

	return count
}

// CountResolvableLinks counts how many relative links in the ADF document
// could be resolved against the linkMap or repo URL, without modifying the tree.
func CountResolvableLinks(doc *Document, baseDir string, linkMap map[string]string, repoURL, repoRoot string) int {
	count := 0
	for i := range doc.Content {
		count += countNodeResolvableLinks(&doc.Content[i], baseDir, linkMap, repoURL, repoRoot)
	}
	return count
}

func countNodeResolvableLinks(node *Node, baseDir string, linkMap map[string]string, repoURL, repoRoot string) int {
	count := 0

	for _, mark := range node.Marks {
		if mark.Type != "link" {
			continue
		}
		href, ok := mark.Attrs["href"].(string)
		if !ok || href == "" {
			continue
		}
		if strings.HasPrefix(href, "http://") || strings.HasPrefix(href, "https://") || strings.HasPrefix(href, "//") {
			continue
		}

		cleanHref := href
		if idx := strings.Index(cleanHref, "#"); idx >= 0 {
			cleanHref = cleanHref[:idx]
		}
		if cleanHref == "" {
			continue
		}

		resolved := filepath.Join(baseDir, cleanHref)
		absResolved, err := filepath.Abs(resolved)
		if err != nil {
			continue
		}

		if _, found := linkMap[absResolved]; found {
			count++
		} else if repoURL != "" && repoRoot != "" {
			relPath, err := filepath.Rel(repoRoot, absResolved)
			if err == nil && !strings.HasPrefix(relPath, "..") {
				count++
			}
		}
	}

	for i := range node.Content {
		count += countNodeResolvableLinks(&node.Content[i], baseDir, linkMap, repoURL, repoRoot)
	}

	return count
}

// IsUnchanged compares two ADF JSON strings for equivalence.
// It normalizes by unmarshaling and re-marshaling to ignore formatting differences.
func IsUnchanged(existing, newADF string) bool {
	if existing == "" {
		return false
	}
	var existingDoc, newDoc any
	if err := json.Unmarshal([]byte(existing), &existingDoc); err != nil {
		return false
	}
	if err := json.Unmarshal([]byte(newADF), &newDoc); err != nil {
		return false
	}
	normalizeJSON(existingDoc)
	normalizeJSON(newDoc)
	a, _ := json.Marshal(existingDoc)
	b, _ := json.Marshal(newDoc)
	return string(a) == string(b)
}

// normalizeJSON recursively normalizes a decoded JSON value so that
// null and empty arrays/objects are treated equivalently.
func normalizeJSON(v any) {
	switch val := v.(type) {
	case map[string]any:
		for k, child := range val {
			switch c := child.(type) {
			case []any:
				if len(c) == 0 {
					val[k] = nil
				} else {
					normalizeJSON(c)
				}
			case map[string]any:
				normalizeJSON(c)
			}
		}
	case []any:
		for i, child := range val {
			switch c := child.(type) {
			case []any:
				if len(c) == 0 {
					val[i] = nil
				} else {
					normalizeJSON(c)
				}
			case map[string]any:
				normalizeJSON(c)
			}
		}
	}
}
