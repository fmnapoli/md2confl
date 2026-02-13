// Copyright 2026 md2confl contributors
// SPDX-License-Identifier: Apache-2.0

package parser

import (
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"
)

// KindSuperscript is the NodeKind for superscript nodes.
var KindSuperscript = ast.NewNodeKind("Superscript")

// SuperscriptNode is an inline AST node for ^text^ syntax.
type SuperscriptNode struct {
	ast.BaseInline
}

// Kind implements ast.Node.
func (*SuperscriptNode) Kind() ast.NodeKind { return KindSuperscript }

// Dump implements ast.Node.
func (*SuperscriptNode) Dump(source []byte, level int) {
	ast.DumpHelper(nil, source, level, nil, nil)
}

// superscriptParser is a goldmark inline parser for ^text^.
type superscriptParser struct{}

func (*superscriptParser) Trigger() []byte { return []byte{'^'} }

func (*superscriptParser) Parse(_ ast.Node, block text.Reader, _ parser.Context) ast.Node {
	line, segment := block.PeekLine()
	if len(line) == 0 || line[0] != '^' {
		return nil
	}
	// Find closing ^, disallow spaces and newlines inside.
	for i := 1; i < len(line); i++ {
		if line[i] == '^' {
			if i == 1 {
				return nil // empty ^^
			}
			node := &SuperscriptNode{}
			node.AppendChild(node, ast.NewTextSegment(
				text.NewSegment(segment.Start+1, segment.Start+i),
			))
			block.Advance(i + 1)
			return node
		}
		if line[i] == ' ' || line[i] == '\n' {
			return nil
		}
	}
	return nil
}

// superscriptExtension is a goldmark extension for ^superscript^ syntax.
type superscriptExtension struct{}

func (e *superscriptExtension) Extend(m goldmark.Markdown) {
	m.Parser().AddOptions(
		parser.WithInlineParsers(
			util.Prioritized(&superscriptParser{}, 500),
		),
	)
}
