// Copyright 2026 md2confl contributors
// SPDX-License-Identifier: Apache-2.0

// Package adftomd converts Atlassian Document Format (ADF) documents to
// Markdown. It supports all ADF node types produced by the md2confl parser
// and common Confluence-native nodes. Unsupported node types produce HTML
// comment placeholders.
package adftomd

import (
	"fmt"
	"strings"

	"github.com/fmnapoli/md2confl/adf"
)

// Options configures the ADF-to-Markdown conversion.
type Options struct {
	// ImageRewriter rewrites image URLs before emitting Markdown.
	// Called for media and mediaInline nodes.
	// If nil, URLs are emitted as-is.
	ImageRewriter func(url string) string
}

// Convert transforms an ADF Document into Markdown bytes.
// Unsupported node types produce HTML comment placeholders.
// Never returns an error — conversion is best-effort.
func Convert(doc *adf.Document) []byte {
	return ConvertWithOptions(doc, Options{})
}

// ConvertWithOptions transforms an ADF Document with configuration.
func ConvertWithOptions(doc *adf.Document, opts Options) []byte {
	if doc == nil {
		return nil
	}
	c := &converter{opts: opts}
	c.renderBlocks(doc.Content, 0)
	return []byte(strings.TrimRight(c.buf.String(), "\n") + "\n")
}

type converter struct {
	opts Options
	buf  strings.Builder
}

func (c *converter) renderBlocks(nodes []adf.Node, depth int) {
	for i, node := range nodes {
		c.renderBlock(node, depth)
		_ = i
	}
}

func (c *converter) renderBlock(node adf.Node, depth int) {
	switch node.Type {
	case "heading":
		c.renderHeading(node)
	case "paragraph":
		c.renderParagraph(node, depth)
	case "codeBlock":
		c.renderCodeBlock(node)
	case "blockquote":
		c.renderBlockquote(node)
	case "panel":
		c.renderPanel(node)
	case "bulletList":
		c.renderBulletList(node, depth)
	case "orderedList":
		c.renderOrderedList(node, depth)
	case "taskList":
		c.renderTaskList(node, depth)
	case "table":
		c.renderTable(node)
	case "rule":
		c.buf.WriteString("---\n\n")
	case "mediaSingle":
		c.renderMediaSingle(node)
	case "expand":
		c.renderExpand(node)
	default:
		c.buf.WriteString(fmt.Sprintf("<!-- unsupported: %s -->\n\n", node.Type))
	}
}

func (c *converter) renderHeading(node adf.Node) {
	level := 1
	if l, ok := node.Attrs["level"]; ok {
		switch v := l.(type) {
		case float64:
			level = int(v)
		case int:
			level = v
		}
	}
	c.buf.WriteString(strings.Repeat("#", level))
	c.buf.WriteString(" ")
	c.renderInlines(node.Content)
	c.buf.WriteString("\n\n")
}

func (c *converter) renderParagraph(node adf.Node, depth int) {
	c.renderInlines(node.Content)
	c.buf.WriteString("\n\n")
}

func (c *converter) renderCodeBlock(node adf.Node) {
	lang := ""
	if l, ok := node.Attrs["language"]; ok {
		if s, ok := l.(string); ok {
			lang = s
		}
	}
	c.buf.WriteString("```")
	c.buf.WriteString(lang)
	c.buf.WriteString("\n")
	for _, child := range node.Content {
		if child.Type == "text" {
			c.buf.WriteString(child.Text)
		}
	}
	c.buf.WriteString("\n```\n\n")
}

func (c *converter) renderBlockquote(node adf.Node) {
	// Render children into a temporary buffer, then prefix each line with "> "
	inner := &converter{opts: c.opts}
	inner.renderBlocks(node.Content, 0)
	text := strings.TrimRight(inner.buf.String(), "\n")
	for _, line := range strings.Split(text, "\n") {
		c.buf.WriteString("> ")
		c.buf.WriteString(line)
		c.buf.WriteString("\n")
	}
	c.buf.WriteString("\n")
}

var panelTypeMap = map[string]string{
	"info":    "[!NOTE]",
	"success": "[!TIP]",
	"note":    "[!IMPORTANT]",
	"warning": "[!WARNING]",
	"error":   "[!CAUTION]",
}

func (c *converter) renderPanel(node adf.Node) {
	panelType := "info"
	if pt, ok := node.Attrs["panelType"]; ok {
		if s, ok := pt.(string); ok {
			panelType = s
		}
	}
	alert, ok := panelTypeMap[panelType]
	if !ok {
		alert = "[!NOTE]"
	}

	inner := &converter{opts: c.opts}
	inner.renderBlocks(node.Content, 0)
	text := strings.TrimRight(inner.buf.String(), "\n")

	c.buf.WriteString("> ")
	c.buf.WriteString(alert)
	c.buf.WriteString("\n")
	for _, line := range strings.Split(text, "\n") {
		c.buf.WriteString("> ")
		c.buf.WriteString(line)
		c.buf.WriteString("\n")
	}
	c.buf.WriteString("\n")
}

func (c *converter) renderBulletList(node adf.Node, depth int) {
	for _, item := range node.Content {
		if item.Type != "listItem" {
			continue
		}
		prefix := strings.Repeat("  ", depth) + "- "
		c.renderListItem(item, prefix, depth)
	}
	if depth == 0 {
		c.buf.WriteString("\n")
	}
}

func (c *converter) renderOrderedList(node adf.Node, depth int) {
	for i, item := range node.Content {
		if item.Type != "listItem" {
			continue
		}
		prefix := fmt.Sprintf("%s%d. ", strings.Repeat("  ", depth), i+1)
		c.renderListItem(item, prefix, depth)
	}
	if depth == 0 {
		c.buf.WriteString("\n")
	}
}

func (c *converter) renderTaskList(node adf.Node, depth int) {
	for _, item := range node.Content {
		if item.Type != "taskItem" {
			continue
		}
		checked := false
		if state, ok := item.Attrs["state"]; ok {
			if s, ok := state.(string); ok && s == "DONE" {
				checked = true
			}
		}
		prefix := strings.Repeat("  ", depth) + "- [ ] "
		if checked {
			prefix = strings.Repeat("  ", depth) + "- [x] "
		}
		c.buf.WriteString(prefix)
		c.renderInlines(item.Content)
		c.buf.WriteString("\n")
	}
	if depth == 0 {
		c.buf.WriteString("\n")
	}
}

func (c *converter) renderListItem(item adf.Node, prefix string, depth int) {
	for i, child := range item.Content {
		if i == 0 && child.Type == "paragraph" {
			c.buf.WriteString(prefix)
			c.renderInlines(child.Content)
			c.buf.WriteString("\n")
		} else {
			// Nested list
			switch child.Type {
			case "bulletList":
				c.renderBulletList(child, depth+1)
			case "orderedList":
				c.renderOrderedList(child, depth+1)
			case "taskList":
				c.renderTaskList(child, depth+1)
			default:
				c.buf.WriteString(prefix)
				c.renderInlines(child.Content)
				c.buf.WriteString("\n")
			}
		}
	}
}

func (c *converter) renderTable(node adf.Node) {
	if len(node.Content) == 0 {
		return
	}

	// Collect rows
	var rows [][]string
	for _, row := range node.Content {
		if row.Type != "tableRow" {
			continue
		}
		var cells []string
		for _, cell := range row.Content {
			inner := &converter{opts: c.opts}
			// Render cell content inline (no block separators)
			for _, p := range cell.Content {
				if p.Type == "paragraph" {
					inner.renderInlines(p.Content)
				}
			}
			cells = append(cells, strings.TrimSpace(inner.buf.String()))
		}
		rows = append(rows, cells)
	}

	if len(rows) == 0 {
		return
	}

	// Calculate column widths
	colCount := 0
	for _, row := range rows {
		if len(row) > colCount {
			colCount = len(row)
		}
	}

	// Write header row
	c.writeTableRow(rows[0], colCount)

	// Write separator
	sep := make([]string, colCount)
	for i := range sep {
		sep[i] = "---"
	}
	c.writeTableRow(sep, colCount)

	// Write data rows
	for _, row := range rows[1:] {
		c.writeTableRow(row, colCount)
	}
	c.buf.WriteString("\n")
}

func (c *converter) writeTableRow(cells []string, colCount int) {
	c.buf.WriteString("|")
	for i := 0; i < colCount; i++ {
		c.buf.WriteString(" ")
		if i < len(cells) {
			c.buf.WriteString(cells[i])
		}
		c.buf.WriteString(" |")
	}
	c.buf.WriteString("\n")
}

func (c *converter) renderMediaSingle(node adf.Node) {
	for _, child := range node.Content {
		if child.Type == "media" {
			url := c.mediaURL(child)
			alt := ""
			if a, ok := child.Attrs["alt"]; ok {
				if s, ok := a.(string); ok {
					alt = s
				}
			}
			c.buf.WriteString(fmt.Sprintf("![%s](%s)\n\n", alt, url))
			return
		}
	}
}

func (c *converter) renderExpand(node adf.Node) {
	title := ""
	if t, ok := node.Attrs["title"]; ok {
		if s, ok := t.(string); ok {
			title = s
		}
	}

	inner := &converter{opts: c.opts}
	inner.renderBlocks(node.Content, 0)
	body := strings.TrimRight(inner.buf.String(), "\n")

	c.buf.WriteString("<details><summary>")
	c.buf.WriteString(title)
	c.buf.WriteString("</summary>\n\n")
	c.buf.WriteString(body)
	c.buf.WriteString("\n\n</details>\n\n")
}

// Inline rendering

func (c *converter) renderInlines(nodes []adf.Node) {
	for _, node := range nodes {
		c.renderInline(node)
	}
}

func (c *converter) renderInline(node adf.Node) {
	switch node.Type {
	case "text":
		c.renderText(node)
	case "hardBreak":
		c.buf.WriteString("\n")
	case "emoji":
		if sn, ok := node.Attrs["shortName"]; ok {
			if s, ok := sn.(string); ok {
				c.buf.WriteString(s)
			}
		}
	case "mediaInline":
		url := c.mediaURL(node)
		alt := ""
		if a, ok := node.Attrs["alt"]; ok {
			if s, ok := a.(string); ok {
				alt = s
			}
		}
		c.buf.WriteString(fmt.Sprintf("![%s](%s)", alt, url))
	default:
		// Inline unsupported nodes are silently ignored
	}
}

func (c *converter) renderText(node adf.Node) {
	text := node.Text
	if len(node.Marks) == 0 {
		c.buf.WriteString(text)
		return
	}

	// Apply marks following stacking order:
	// code (innermost) → strong/em/strike → link (outermost) → subsup
	var hasCode, hasStrong, hasEm, hasStrike bool
	var linkHref string
	var subSupType string

	for _, mark := range node.Marks {
		switch mark.Type {
		case "code":
			hasCode = true
		case "strong":
			hasStrong = true
		case "em":
			hasEm = true
		case "strike":
			hasStrike = true
		case "link":
			if href, ok := mark.Attrs["href"]; ok {
				if s, ok := href.(string); ok {
					linkHref = s
				}
			}
		case "subsup":
			if t, ok := mark.Attrs["type"]; ok {
				if s, ok := t.(string); ok {
					subSupType = s
				}
			}
		}
	}

	if hasCode {
		text = "`" + text + "`"
	} else {
		if hasStrong {
			text = "**" + text + "**"
		}
		if hasEm {
			text = "*" + text + "*"
		}
		if hasStrike {
			text = "~~" + text + "~~"
		}
	}

	if linkHref != "" {
		text = "[" + text + "](" + linkHref + ")"
	}

	if subSupType == "sup" {
		text = "^" + text + "^"
	}

	c.buf.WriteString(text)
}

func (c *converter) mediaURL(node adf.Node) string {
	url := ""
	if u, ok := node.Attrs["url"]; ok {
		if s, ok := u.(string); ok {
			url = s
		}
	}
	if c.opts.ImageRewriter != nil && url != "" {
		url = c.opts.ImageRewriter(url)
	}
	return url
}
