// Copyright 2026 md2confl contributors
// SPDX-License-Identifier: Apache-2.0

package parser

import (
	"strings"
	"testing"
)

func TestConvertToStorageFormat(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		contains []string
		absent   []string
	}{
		{
			name:  "headings",
			input: "# Title\n\n## Subtitle\n",
			contains: []string{
				"<h1>Title</h1>",
				"<h2>Subtitle</h2>",
			},
		},
		{
			name:  "bold and italic",
			input: "Some **bold** and *italic* text.\n",
			contains: []string{
				"<strong>bold</strong>",
				"<em>italic</em>",
			},
		},
		{
			name:  "fenced code block with language",
			input: "```go\nfmt.Println(\"hello\")\n```\n",
			contains: []string{
				`ac:name="code"`,
				`ac:name="language">go</ac:parameter>`,
				"<![CDATA[",
				`fmt.Println("hello")`,
			},
			absent: []string{
				"<pre><code",
			},
		},
		{
			name:  "fenced code block without language",
			input: "```\nsome code\n```\n",
			contains: []string{
				`ac:name="code"`,
				"<![CDATA[",
				"some code",
			},
		},
		{
			name:  "mermaid block preserved as code",
			input: "```mermaid\ngraph TD\n  A --> B\n```\n",
			contains: []string{
				`language-mermaid`,
				"graph TD",
			},
			absent: []string{
				`ac:name="code"`,
			},
		},
		{
			name:  "table with confluence classes",
			input: "| A | B |\n|---|---|\n| 1 | 2 |\n",
			contains: []string{
				"confluenceTable",
				"confluenceTh",
				"confluenceTd",
			},
		},
		{
			name:  "github alert note",
			input: "> [!NOTE]\n> This is important.\n",
			contains: []string{
				`ac:name="info"`,
				"ac:rich-text-body",
				"This is important.",
			},
		},
		{
			name:  "github alert warning",
			input: "> [!WARNING]\n> Be careful.\n",
			contains: []string{
				`ac:name="warning"`,
			},
		},
		{
			name:  "links preserved",
			input: "[Click here](https://example.com)\n",
			contains: []string{
				`href="https://example.com"`,
				"Click here",
			},
		},
		{
			name:  "strikethrough",
			input: "~~deleted~~\n",
			contains: []string{
				"<del>deleted</del>",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := ConvertToStorageFormat([]byte(tt.input))
			if err != nil {
				t.Fatalf("ConvertToStorageFormat() error = %v", err)
			}

			for _, want := range tt.contains {
				if !strings.Contains(result, want) {
					t.Errorf("result should contain %q\ngot: %s", want, result)
				}
			}

			for _, notWant := range tt.absent {
				if strings.Contains(result, notWant) {
					t.Errorf("result should NOT contain %q\ngot: %s", notWant, result)
				}
			}
		})
	}
}

func TestConvertToStorageFormat_RealReadme(t *testing.T) {
	// Testa com um markdown mais completo
	md := `# Project

## Overview

Some **bold** text with [link](https://example.com).

### Features

- Item 1
- Item 2
- Item 3

` + "```bash\nmake build\n```" + `

| Column | Value |
|--------|-------|
| A      | 1     |

> [!NOTE]
> Remember this.
`
	result, err := ConvertToStorageFormat([]byte(md))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	checks := []string{
		"<h1>Project</h1>",
		"<h2>Overview</h2>",
		"<strong>bold</strong>",
		`ac:name="code"`,
		`ac:name="language">bash`,
		"make build",
		"confluenceTable",
		`ac:name="info"`,
		"Remember this",
	}

	for _, want := range checks {
		if !strings.Contains(result, want) {
			t.Errorf("missing %q in output", want)
		}
	}
}
