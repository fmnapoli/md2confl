// Copyright 2026 md2confl contributors
// SPDX-License-Identifier: Apache-2.0

package parser

import (
	"strings"

	"github.com/fmnapoli/md2confl/adf"
	"github.com/yuin/goldmark/ast"
)

// detectAlertType inspects the first paragraph of a blockquote for a GitHub
// alert marker like [!NOTE]. It reads the raw source of the first line
// because goldmark splits [!NOTE] across multiple AST text nodes.
func detectAlertType(bq *ast.Blockquote, source []byte) string {
	first := bq.FirstChild()
	if first == nil {
		return ""
	}
	p, ok := first.(*ast.Paragraph)
	if !ok {
		return ""
	}
	lines := p.Lines()
	if lines.Len() == 0 {
		return ""
	}
	seg := lines.At(0)
	firstLine := strings.TrimSpace(string(seg.Value(source)))
	m := alertPattern.FindStringSubmatch(firstLine)
	if m == nil {
		return ""
	}
	return m[1]
}

// removeAlertMarker strips the [!TYPE] marker text nodes from the first
// paragraph of the collected ADF content. Goldmark may split [!NOTE] across
// multiple text nodes (e.g. "[", "!NOTE", "]"), so we accumulate text until
// the marker pattern matches. If the first paragraph ends up empty after
// removal, the entire paragraph is dropped.
func removeAlertMarker(content []adf.Node) []adf.Node {
	if len(content) == 0 || content[0].Type != "paragraph" || len(content[0].Content) == 0 {
		return content
	}
	inner := content[0].Content
	var buf strings.Builder
	removeCount := 0
	for i, node := range inner {
		if node.Type != "text" {
			break
		}
		buf.WriteString(node.Text)
		removeCount = i + 1
		if alertPattern.MatchString(strings.TrimSpace(buf.String())) {
			if removeCount < len(inner) && strings.TrimSpace(inner[removeCount].Text) == "" {
				removeCount++
			}
			inner = inner[removeCount:]
			if len(inner) == 0 {
				return content[1:]
			}
			content[0].Content = inner
			return content
		}
	}
	return content
}

// htmlBlockContent extracts the raw text of an ast.HTMLBlock.
func htmlBlockContent(n *ast.HTMLBlock, source []byte) string {
	var buf []byte
	lines := n.Lines()
	for i := 0; i < lines.Len(); i++ {
		line := lines.At(i)
		buf = append(buf, line.Value(source)...)
	}
	if n.HasClosure() {
		buf = append(buf, n.ClosureLine.Value(source)...)
	}
	return string(buf)
}

// parseDetailsBlock checks if an HTML string is a <details> block and extracts
// the summary title and body content. Returns ("", "", false) when the HTML is
// not a complete <details>…</details> block.
func parseDetailsBlock(html string) (title, body string, ok bool) {
	html = strings.TrimSpace(html)
	if !detailsOpenRe.MatchString(html) {
		return "", "", false
	}
	if !strings.Contains(strings.ToLower(html), "</details>") {
		return "", "", false
	}

	if m := summaryRe.FindStringSubmatch(html); m != nil {
		title = strings.TrimSpace(m[1])
	}

	bodyStart := 0
	if idx := strings.Index(strings.ToLower(html), "</summary>"); idx >= 0 {
		bodyStart = idx + len("</summary>")
	} else {
		loc := detailsOpenRe.FindStringIndex(html)
		if loc != nil {
			bodyStart = loc[1]
		}
	}
	bodyEnd := strings.LastIndex(strings.ToLower(html), "</details>")
	if bodyEnd > bodyStart {
		body = strings.TrimSpace(html[bodyStart:bodyEnd])
	}
	return title, body, true
}

// imageAltText extracts the alt text from an ast.Image's text children.
func imageAltText(n *ast.Image, source []byte) string {
	var buf strings.Builder
	for child := n.FirstChild(); child != nil; child = child.NextSibling() {
		if t, ok := child.(*ast.Text); ok {
			buf.Write(t.Segment.Value(source))
		}
	}
	return buf.String()
}
