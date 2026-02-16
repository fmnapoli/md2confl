// Copyright 2026 md2confl contributors
// SPDX-License-Identifier: Apache-2.0

// Package parser converts Markdown source into an ADF [adf.Document].
// It uses goldmark to parse Markdown (with GFM extensions) into an AST, then
// walks the AST with a stack-based visitor to build the corresponding ADF
// node tree. The single entry point is [ConvertToADF].
package parser

import (
	"regexp"
	"strconv"
	"strings"

	"github.com/fmnapoli/md2confl/adf"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	east "github.com/yuin/goldmark/extension/ast"
	"github.com/yuin/goldmark/text"

	emoji "github.com/yuin/goldmark-emoji"
	emojast "github.com/yuin/goldmark-emoji/ast"
)

// alertPattern matches GitHub-style alert markers like [!NOTE].
var alertPattern = regexp.MustCompile(`^\[!(NOTE|TIP|IMPORTANT|WARNING|CAUTION)\]$`)

// alertPanelMap maps GitHub alert types to ADF panel types.
var alertPanelMap = map[string]string{
	"NOTE":      "info",
	"TIP":       "success",
	"IMPORTANT": "note",
	"WARNING":   "warning",
	"CAUTION":   "error",
}

// detailsOpenRe matches the opening <details> tag (with optional attributes).
var detailsOpenRe = regexp.MustCompile(`(?i)^<details[^>]*>`)

// summaryRe extracts the content of a <summary>...</summary> tag.
var summaryRe = regexp.MustCompile(`(?is)<summary>(.*?)</summary>`)

// ConvertToADF converts Markdown source bytes to an ADF Document.
func ConvertToADF(source []byte) (*adf.Document, error) {
	md := goldmark.New(goldmark.WithExtensions(
		extension.GFM,
		emoji.Emoji,
		&superscriptExtension{},
	))
	reader := text.NewReader(source)
	tree := md.Parser().Parse(reader)

	doc := adf.NewDocument()
	w := &walker{
		source: source,
		stack:  [][]adf.Node{{}},
	}

	if err := ast.Walk(tree, w.walk); err != nil {
		return nil, err
	}

	doc.Content = w.stack[0]
	return doc, nil
}

type walker struct {
	source         []byte
	stack          [][]adf.Node // stack of content slices for nesting
	localIDCounter int          // monotonic counter for taskList/taskItem localId
	taskState      *string      // set by TaskCheckBox, consumed by ListItem
	alertType      string       // set by Blockquote entering, consumed on exit
}

func (w *walker) push() {
	w.stack = append(w.stack, []adf.Node{})
}

func (w *walker) pop() []adf.Node {
	n := len(w.stack)
	content := w.stack[n-1]
	w.stack = w.stack[:n-1]
	return content
}

func (w *walker) append(node adf.Node) {
	n := len(w.stack)
	w.stack[n-1] = append(w.stack[n-1], node)
}

// appendWithHoist wraps content in a node of blockType, but hoists any
// block-level children (mediaSingle) out so they become siblings instead of
// being nested inside an inline container like paragraph.
func (w *walker) appendWithHoist(blockType string, content []adf.Node) {
	var inlineRun []adf.Node
	for _, child := range content {
		if child.Type == "mediaSingle" {
			if len(inlineRun) > 0 {
				w.append(adf.Node{Type: blockType, Content: inlineRun})
				inlineRun = nil
			}
			w.append(child)
		} else {
			inlineRun = append(inlineRun, child)
		}
	}
	if len(inlineRun) > 0 {
		w.append(adf.Node{Type: blockType, Content: inlineRun})
	}
}

func (w *walker) nextLocalID() string {
	w.localIDCounter++
	return strconv.Itoa(w.localIDCounter)
}

func (w *walker) walk(node ast.Node, entering bool) (ast.WalkStatus, error) {
	switch n := node.(type) {
	case *ast.Document:
		// root — no action needed

	case *ast.Heading:
		if entering {
			w.push()
		} else {
			content := w.pop()
			w.append(adf.Node{
				Type:    "heading",
				Attrs:   map[string]any{"level": n.Level},
				Content: content,
			})
		}

	case *ast.Paragraph:
		if entering {
			w.push()
		} else {
			content := w.pop()
			if len(content) > 0 {
				w.appendWithHoist("paragraph", content)
			}
		}

	case *ast.TextBlock:
		if entering {
			w.push()
		} else {
			content := w.pop()
			if len(content) > 0 {
				w.appendWithHoist("paragraph", content)
			}
		}

	// Phase 1: Task lists — detect taskItem children and emit taskList.
	case *ast.List:
		if entering {
			w.push()
		} else {
			content := w.pop()
			isTaskList := len(content) > 0
			for _, item := range content {
				if item.Type != "taskItem" {
					isTaskList = false
					break
				}
			}
			if isTaskList {
				w.append(adf.Node{
					Type:    "taskList",
					Attrs:   map[string]any{"localId": w.nextLocalID()},
					Content: content,
				})
			} else {
				nodeType := "bulletList"
				var attrs map[string]any
				if n.IsOrdered() {
					nodeType = "orderedList"
					if n.Start != 1 {
						attrs = map[string]any{"order": n.Start}
					}
				}
				// Convert stray taskItem nodes to listItem (mixed lists)
				for i := range content {
					if content[i].Type == "taskItem" {
						content[i].Type = "listItem"
						content[i].Attrs = nil
						// Wrap inline content in a paragraph for listItem
						content[i].Content = []adf.Node{{Type: "paragraph", Content: content[i].Content}}
					}
				}
				w.append(adf.Node{
					Type:    nodeType,
					Attrs:   attrs,
					Content: content,
				})
			}
		}

	// Phase 1: Task lists — emit taskItem when a checkbox was seen.
	case *ast.ListItem:
		if entering {
			w.push()
		} else {
			content := w.pop()
			if w.taskState != nil {
				// ADF taskItem expects inline content directly — unwrap
				// the paragraph that goldmark wraps around list item text.
				inline := content
				if len(content) == 1 && content[0].Type == "paragraph" {
					inline = content[0].Content
				}
				w.append(adf.Node{
					Type: "taskItem",
					Attrs: map[string]any{
						"localId": w.nextLocalID(),
						"state":   *w.taskState,
					},
					Content: inline,
				})
				w.taskState = nil
			} else {
				w.append(adf.Node{
					Type:    "listItem",
					Content: content,
				})
			}
		}

	case *ast.FencedCodeBlock:
		if entering {
			lang := string(n.Language(w.source))
			var code []byte
			lines := n.Lines()
			for i := 0; i < lines.Len(); i++ {
				line := lines.At(i)
				code = append(code, line.Value(w.source)...)
			}
			// Trim trailing newline
			codeStr := string(code)
			if len(codeStr) > 0 && codeStr[len(codeStr)-1] == '\n' {
				codeStr = codeStr[:len(codeStr)-1]
			}

			var attrs map[string]any
			if lang != "" {
				attrs = map[string]any{"language": lang}
			}

			w.append(adf.Node{
				Type:  "codeBlock",
				Attrs: attrs,
				Content: []adf.Node{
					{Type: "text", Text: codeStr},
				},
			})
		}
		return ast.WalkSkipChildren, nil

	case *ast.CodeBlock:
		if entering {
			var code []byte
			lines := n.Lines()
			for i := 0; i < lines.Len(); i++ {
				line := lines.At(i)
				code = append(code, line.Value(w.source)...)
			}
			codeStr := string(code)
			if len(codeStr) > 0 && codeStr[len(codeStr)-1] == '\n' {
				codeStr = codeStr[:len(codeStr)-1]
			}
			w.append(adf.Node{
				Type: "codeBlock",
				Content: []adf.Node{
					{Type: "text", Text: codeStr},
				},
			})
		}
		return ast.WalkSkipChildren, nil

	// Phase 2: GitHub Alerts — detect [!TYPE] in blockquotes and emit panel.
	case *ast.Blockquote:
		if entering {
			w.alertType = detectAlertType(n, w.source)
			w.push()
		} else {
			content := w.pop()
			if w.alertType != "" {
				// ADF panels don't support codeBlock children — fall back to blockquote.
				hasCodeBlock := false
				for _, child := range content {
					if child.Type == "codeBlock" {
						hasCodeBlock = true
						break
					}
				}
				if !hasCodeBlock {
					content = removeAlertMarker(content)
					w.append(adf.Node{
						Type:    "panel",
						Attrs:   map[string]any{"panelType": alertPanelMap[w.alertType]},
						Content: content,
					})
					w.alertType = ""
					break
				}
				w.alertType = ""
			}
			w.append(adf.Node{
				Type:    "blockquote",
				Content: content,
			})
		}

	case *ast.ThematicBreak:
		if entering {
			w.append(adf.Node{Type: "rule"})
		}

	// Table nodes (GFM extension)
	case *east.Table:
		if entering {
			w.push()
		} else {
			content := w.pop()
			w.append(adf.Node{
				Type:    "table",
				Content: content,
			})
		}

	case *east.TableHeader:
		if entering {
			w.push()
		} else {
			content := w.pop()
			w.append(adf.Node{
				Type:    "tableRow",
				Content: content,
			})
		}

	case *east.TableRow:
		if entering {
			w.push()
		} else {
			content := w.pop()
			w.append(adf.Node{
				Type:    "tableRow",
				Content: content,
			})
		}

	case *east.TableCell:
		if entering {
			w.push()
		} else {
			children := w.pop()
			// Wrap inline content in a paragraph
			cellContent := []adf.Node{}
			if len(children) > 0 {
				cellContent = []adf.Node{
					{Type: "paragraph", Content: children},
				}
			}
			cellType := "tableCell"
			if n.Parent().Kind() == east.KindTableHeader {
				cellType = "tableHeader"
			}
			w.append(adf.Node{
				Type:    cellType,
				Content: cellContent,
			})
		}

	// Phase 1: Task lists — capture checkbox state for the enclosing ListItem.
	case *east.TaskCheckBox:
		if entering {
			state := "TODO"
			if n.IsChecked {
				state = "DONE"
			}
			w.taskState = &state
		}

	// Inline nodes
	case *ast.Text:
		if entering {
			text := string(n.Value(w.source))
			if text != "" {
				w.append(adf.Node{Type: "text", Text: text})
			}
			if n.SoftLineBreak() {
				w.append(adf.Node{Type: "text", Text: " "})
			}
			if n.HardLineBreak() {
				w.append(adf.Node{Type: "hardBreak"})
			}
		}

	case *ast.String:
		if entering {
			text := string(n.Value)
			if text != "" {
				w.append(adf.Node{Type: "text", Text: text})
			}
		}

	case *ast.Emphasis:
		if entering {
			w.push()
		} else {
			content := w.pop()
			markType := "em"
			if n.Level == 2 {
				markType = "strong"
			}
			for i := range content {
				content[i].Marks = append(content[i].Marks, adf.Mark{Type: markType})
			}
			for _, c := range content {
				w.append(c)
			}
		}

	case *ast.CodeSpan:
		if entering {
			w.push()
		} else {
			content := w.pop()
			for i := range content {
				content[i].Marks = append(content[i].Marks, adf.Mark{Type: "code"})
			}
			for _, c := range content {
				w.append(c)
			}
		}

	case *ast.Link:
		if entering {
			w.push()
		} else {
			content := w.pop()
			mark := adf.Mark{
				Type: "link",
				Attrs: map[string]any{
					"href": string(n.Destination),
				},
			}
			if title := string(n.Title); title != "" {
				mark.Attrs["title"] = title
			}
			for i := range content {
				// Only text nodes support link marks in ADF
				if content[i].Type == "text" || content[i].Type == "mediaInline" {
					content[i].Marks = append(content[i].Marks, mark)
				}
			}
			for _, c := range content {
				w.append(c)
			}
		}

	case *ast.AutoLink:
		if entering {
			url := string(n.URL(w.source))
			label := string(n.Label(w.source))
			w.append(adf.Node{
				Type: "text",
				Text: label,
				Marks: []adf.Mark{
					{Type: "link", Attrs: map[string]any{"href": url}},
				},
			})
		}

	case *ast.Image:
		if entering {
			if _, isLink := n.Parent().(*ast.Link); isLink {
				url := string(n.Destination)
				attrs := map[string]any{
					"type": "external",
					"url":  url,
				}
				if alt := imageAltText(n, w.source); alt != "" {
					attrs["alt"] = alt
				}
				w.append(adf.Node{
					Type:  "mediaInline",
					Attrs: attrs,
				})
			} else {
				url := string(n.Destination)
				mediaAttrs := map[string]any{"type": "external", "url": url}
				if alt := imageAltText(n, w.source); alt != "" {
					mediaAttrs["alt"] = alt
				}
				w.append(adf.Node{
					Type:  "mediaSingle",
					Attrs: map[string]any{"layout": "center"},
					Content: []adf.Node{
						{Type: "media", Attrs: mediaAttrs},
					},
				})
			}
		}
		return ast.WalkSkipChildren, nil

	case *east.Strikethrough:
		if entering {
			w.push()
		} else {
			content := w.pop()
			for i := range content {
				content[i].Marks = append(content[i].Marks, adf.Mark{Type: "strike"})
			}
			for _, c := range content {
				w.append(c)
			}
		}

	// Phase 3: Emoji shortcodes → ADF emoji node.
	case *emojast.Emoji:
		if entering {
			shortName := ":" + string(n.ShortName) + ":"
			w.append(adf.Node{
				Type: "emoji",
				Attrs: map[string]any{
					"shortName": shortName,
					"text":      string(n.Value.Unicode),
				},
			})
		}

	// Phase 5: Superscript — emit subsup mark with type=sup.
	case *SuperscriptNode:
		if entering {
			w.push()
		} else {
			content := w.pop()
			for i := range content {
				content[i].Marks = append(content[i].Marks, adf.Mark{
					Type:  "subsup",
					Attrs: map[string]any{"type": "sup"},
				})
			}
			for _, c := range content {
				w.append(c)
			}
		}

	// Phase 4: Expand/Collapse — parse <details> HTML blocks.
	case *ast.HTMLBlock:
		if entering {
			html := htmlBlockContent(n, w.source)
			if title, body, ok := parseDetailsBlock(html); ok {
				var content []adf.Node
				if body != "" {
					if bodyDoc, err := ConvertToADF([]byte(body)); err == nil {
						content = bodyDoc.Content
					}
				}
				attrs := map[string]any{}
				if title != "" {
					attrs["title"] = title
				}
				w.append(adf.Node{
					Type:    "expand",
					Attrs:   attrs,
					Content: content,
				})
				return ast.WalkSkipChildren, nil
			}
		}
		return ast.WalkSkipChildren, nil

	case *ast.RawHTML:
		// Skip inline HTML
		return ast.WalkSkipChildren, nil
	}

	return ast.WalkContinue, nil
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
	// Accumulate leading text nodes until the full marker is found.
	var buf strings.Builder
	removeCount := 0
	for i, node := range inner {
		if node.Type != "text" {
			break
		}
		buf.WriteString(node.Text)
		removeCount = i + 1
		if alertPattern.MatchString(strings.TrimSpace(buf.String())) {
			// Also strip the trailing soft-line-break space.
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

	// Extract summary.
	if m := summaryRe.FindStringSubmatch(html); m != nil {
		title = strings.TrimSpace(m[1])
	}

	// Extract body: everything after </summary> (or after <details...>) and
	// before </details>.
	bodyStart := 0
	if idx := strings.Index(strings.ToLower(html), "</summary>"); idx >= 0 {
		bodyStart = idx + len("</summary>")
	} else {
		// No summary — body starts after the opening tag.
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
