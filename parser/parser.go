// Copyright 2026 md2confl contributors
// SPDX-License-Identifier: Apache-2.0

package parser

import (
	"github.com/fabrizio/md2confl/adf"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	east "github.com/yuin/goldmark/extension/ast"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/text"
)

// ConvertToADF converts Markdown source bytes to an ADF Document.
func ConvertToADF(source []byte) (*adf.Document, error) {
	md := goldmark.New(goldmark.WithExtensions(extension.GFM))
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
	source []byte
	stack  [][]adf.Node // stack of content slices for nesting
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
		// Skip paragraphs inside list items that are the first child — goldmark wraps
		// list item content in paragraphs, and we handle this in listItem
		if entering {
			w.push()
		} else {
			content := w.pop()
			if len(content) > 0 {
				w.append(adf.Node{
					Type:    "paragraph",
					Content: content,
				})
			}
		}

	case *ast.TextBlock:
		if entering {
			w.push()
		} else {
			content := w.pop()
			if len(content) > 0 {
				w.append(adf.Node{
					Type:    "paragraph",
					Content: content,
				})
			}
		}

	case *ast.List:
		if entering {
			w.push()
		} else {
			content := w.pop()
			nodeType := "bulletList"
			var attrs map[string]any
			if n.IsOrdered() {
				nodeType = "orderedList"
				if n.Start != 1 {
					attrs = map[string]any{"order": n.Start}
				}
			}
			w.append(adf.Node{
				Type:    nodeType,
				Attrs:   attrs,
				Content: content,
			})
		}

	case *ast.ListItem:
		if entering {
			w.push()
		} else {
			content := w.pop()
			w.append(adf.Node{
				Type:    "listItem",
				Content: content,
			})
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

	case *ast.Blockquote:
		if entering {
			w.push()
		} else {
			content := w.pop()
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
				content[i].Marks = append(content[i].Marks, mark)
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
			url := string(n.Destination)
			mediaType := "external"
			attrs := map[string]any{
				"type": mediaType,
				"url":  url,
			}
			w.append(adf.Node{
				Type:  "mediaSingle",
				Attrs: map[string]any{"layout": "center"},
				Content: []adf.Node{
					{Type: "media", Attrs: attrs},
				},
			})
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

	case *ast.HTMLBlock:
		// Skip HTML blocks — output warning if needed
		return ast.WalkSkipChildren, nil

	case *ast.RawHTML:
		// Skip inline HTML
		return ast.WalkSkipChildren, nil
	}

	return ast.WalkContinue, nil
}
