// Copyright 2026 md2confl contributors
// SPDX-License-Identifier: Apache-2.0

// Package adf defines the Atlassian Document Format (ADF) data structures.
// ADF is a JSON-based document format used by Confluence Cloud to represent
// rich-text content. The core types — [Document], [Node], and [Mark] — model
// the full ADF tree: a Document contains block-level Nodes (paragraphs,
// headings, tables, …), which in turn contain inline Nodes carrying Marks
// (bold, italic, links, …).
package adf

// Document is the top-level ADF envelope.
type Document struct {
	Version int    `json:"version"`
	Type    string `json:"type"`
	Content []Node `json:"content"`
}

// NewDocument creates a new ADF document with version 1.
func NewDocument() *Document {
	return &Document{
		Version: 1,
		Type:    "doc",
		Content: []Node{},
	}
}

// Node is a generic ADF node that supports all block and inline types.
type Node struct {
	Type    string         `json:"type"`
	Attrs   map[string]any `json:"attrs,omitempty"`
	Content []Node         `json:"content,omitempty"`
	Marks   []Mark         `json:"marks,omitempty"`
	Text    string         `json:"text,omitempty"`
}

// Mark represents inline formatting (strong, em, code, link, etc.).
type Mark struct {
	Type  string         `json:"type"`
	Attrs map[string]any `json:"attrs,omitempty"`
}
