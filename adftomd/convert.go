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
	// Called for media and mediaInline nodes with external URLs.
	// If nil, URLs are emitted as-is.
	ImageRewriter func(url string) string

	// FileIDResolver maps a Confluence media file ID (UUID) to a local path.
	// Called for media nodes with type:"file" (attachment-based images).
	// If nil, file-type media are skipped.
	FileIDResolver func(fileID string) string
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
		fmt.Fprintf(&c.buf, "<!-- unsupported: %s -->\n\n", node.Type)
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
	start := 1
	if o, ok := node.Attrs["order"]; ok {
		switch v := o.(type) {
		case float64:
			start = int(v)
		case int:
			start = v
		}
	}
	for i, item := range node.Content {
		if item.Type != "listItem" {
			continue
		}
		prefix := fmt.Sprintf("%s%d. ", strings.Repeat("  ", depth), start+i)
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
			fmt.Fprintf(&c.buf, "![%s](%s)\n\n", alt, url)
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
	c.buf.WriteString("</summary>\n")
	c.buf.WriteString(body)
	c.buf.WriteString("\n</details>\n\n")
}

// Inline rendering
//
// renderInlines uses a mark-coalescing state machine: it tracks which
// formatting marks (strong, em, strike, link, subsup) are currently open
// and only emits delimiters when marks change between adjacent text nodes.
// This prevents premature closing of marks that span multiple ADF text nodes,
// e.g. **bold with `code` inside** instead of **bold with **`code`.

func (c *converter) renderInlines(nodes []adf.Node) {
	// markStack tracks the order in which marks were opened,
	// so they can be closed in correct (reverse) order.
	type openMark struct {
		kind  string // "strong", "em", "strike", "sup", "link"
		delim string // closing delimiter
	}
	var markStack []openMark

	isOpen := func(kind string) bool {
		for _, m := range markStack {
			if m.kind == kind {
				return true
			}
		}
		return false
	}

	// closeMarks closes a set of marks in correct stack order, without
	// reopening marks that are also being closed. Marks not in the close
	// set that sit above a closing mark are temporarily closed and reopened.
	closeMarks := func(toClose map[string]bool) {
		if len(toClose) == 0 {
			return
		}
		// Find the deepest stack index among marks to close.
		deepest := len(markStack)
		for i, m := range markStack {
			if toClose[m.kind] {
				if i < deepest {
					deepest = i
				}
				break // stack is ordered, first match is deepest
			}
		}
		if deepest >= len(markStack) {
			return
		}
		// Pop from top down to deepest, collecting marks to reopen.
		var reopen []openMark
		for len(markStack) > deepest {
			top := markStack[len(markStack)-1]
			markStack = markStack[:len(markStack)-1]
			c.buf.WriteString(top.delim)
			if !toClose[top.kind] {
				reopen = append(reopen, top)
			}
		}
		// Reopen marks that were only temporarily closed (reverse order).
		for i := len(reopen) - 1; i >= 0; i-- {
			m := reopen[i]
			c.buf.WriteString(openDelim(m.kind))
			markStack = append(markStack, m)
		}
	}

	closeAll := func() {
		for len(markStack) > 0 {
			top := markStack[len(markStack)-1]
			markStack = markStack[:len(markStack)-1]
			c.buf.WriteString(top.delim)
		}
	}

	openLinkHref := func() string {
		for _, m := range markStack {
			if m.kind == "link" {
				// Extract href from the closing delimiter ](href)
				return m.delim[2 : len(m.delim)-1]
			}
		}
		return ""
	}

	for _, node := range nodes {
		if node.Type != "text" {
			closeAll()
			c.renderNonTextInline(node)
			continue
		}

		marks := extractMarks(node)

		// Code mark is special: rendered as inline backticks, not tracked
		// on the mark stack. Confluence drops other marks (e.g. strong)
		// from code nodes, so we treat code nodes as transparent to the
		// mark stack — don't close/open surrounding marks.
		if marks.code && !marks.strong && !marks.em && !marks.strike && !marks.sup && marks.link == "" {
			c.buf.WriteString("`")
			c.buf.WriteString(node.Text)
			c.buf.WriteString("`")
			continue
		}

		// Collect all marks that need to close in this transition.
		toClose := make(map[string]bool)
		if isOpen("sup") && !marks.sup {
			toClose["sup"] = true
		}
		if isOpen("strike") && !marks.strike {
			toClose["strike"] = true
		}
		if isOpen("em") && !marks.em {
			toClose["em"] = true
		}
		if isOpen("strong") && !marks.strong {
			toClose["strong"] = true
		}
		if curLink := openLinkHref(); curLink != "" && marks.link != curLink {
			toClose["link"] = true
		}
		closeMarks(toClose)

		// Open marks that are newly active (outermost first: link, strong, em, strike, sup)
		if marks.link != "" && !isOpen("link") {
			c.buf.WriteString("[")
			markStack = append(markStack, openMark{"link", "](" + marks.link + ")"})
		}
		if marks.strong && !isOpen("strong") {
			c.buf.WriteString("**")
			markStack = append(markStack, openMark{"strong", "**"})
		}
		if marks.em && !isOpen("em") {
			c.buf.WriteString("*")
			markStack = append(markStack, openMark{"em", "*"})
		}
		if marks.strike && !isOpen("strike") {
			c.buf.WriteString("~~")
			markStack = append(markStack, openMark{"strike", "~~"})
		}
		if marks.sup && !isOpen("sup") {
			c.buf.WriteString("^")
			markStack = append(markStack, openMark{"sup", "^"})
		}

		// Emit text, wrapping with code backticks if applicable
		if marks.code {
			c.buf.WriteString("`")
			c.buf.WriteString(node.Text)
			c.buf.WriteString("`")
		} else {
			c.buf.WriteString(node.Text)
		}
	}

	closeAll()
}

func openDelim(kind string) string {
	switch kind {
	case "strong":
		return "**"
	case "em":
		return "*"
	case "strike":
		return "~~"
	case "sup":
		return "^"
	case "link":
		return "["
	}
	return ""
}

// inlineMarks holds the parsed marks of a text node.
type inlineMarks struct {
	code, strong, em, strike, sup bool
	link                          string
}

func extractMarks(node adf.Node) inlineMarks {
	var m inlineMarks
	for _, mark := range node.Marks {
		switch mark.Type {
		case "code":
			m.code = true
		case "strong":
			m.strong = true
		case "em":
			m.em = true
		case "strike":
			m.strike = true
		case "link":
			if href, ok := mark.Attrs["href"]; ok {
				if s, ok := href.(string); ok {
					m.link = s
				}
			}
		case "subsup":
			if t, ok := mark.Attrs["type"]; ok {
				if s, ok := t.(string); ok && s == "sup" {
					m.sup = true
				}
			}
		}
	}
	return m
}

func (c *converter) renderNonTextInline(node adf.Node) {
	switch node.Type {
	case "hardBreak":
		c.buf.WriteString("\\\n")
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
		fmt.Fprintf(&c.buf, "![%s](%s)", alt, url)
	case "inlineExtension":
		c.renderInlineExtension(node)
	}
}

func (c *converter) renderInlineExtension(node adf.Node) {
	// Handle Confluence inline-external-image extensions (e.g. badges)
	if params, ok := node.Attrs["parameters"]; ok {
		if pm, ok := params.(map[string]interface{}); ok {
			src, _ := pm["src"].(string)
			alt, _ := pm["alt"].(string)
			if src != "" {
				fmt.Fprintf(&c.buf, "![%s](%s)", alt, src)
				return
			}
		}
	}
}

func (c *converter) mediaURL(node adf.Node) string {
	// Check for file-type media (Confluence attachments with UUID)
	if mediaType, ok := node.Attrs["type"]; ok {
		if s, ok := mediaType.(string); ok && s == "file" {
			if id, ok := node.Attrs["id"]; ok {
				if fileID, ok := id.(string); ok && c.opts.FileIDResolver != nil {
					return c.opts.FileIDResolver(fileID)
				}
			}
			return "" // file-type media without resolver
		}
	}

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
