// Copyright 2026 md2confl contributors
// SPDX-License-Identifier: Apache-2.0

package storagetomd

import (
	"strings"
	"testing"
)

func TestConvert(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		contains []string
	}{
		{
			name:  "headings",
			input: "<h1>Title</h1><h2>Sub</h2>",
			contains: []string{
				"# Title",
				"## Sub",
			},
		},
		{
			name:  "paragraph with bold and italic",
			input: "<p>Some <strong>bold</strong> and <em>italic</em> text.</p>",
			contains: []string{
				"**bold**",
				"*italic*",
			},
		},
		{
			name:  "code macro",
			input: `<ac:structured-macro ac:name="code"><ac:parameter ac:name="language">go</ac:parameter><ac:plain-text-body><![CDATA[fmt.Println("hello")]]></ac:plain-text-body></ac:structured-macro>`,
			contains: []string{
				"```go",
				`fmt.Println("hello")`,
				"```",
			},
		},
		{
			name:  "info panel",
			input: `<ac:structured-macro ac:name="info"><ac:rich-text-body><p>Important note.</p></ac:rich-text-body></ac:structured-macro>`,
			contains: []string{
				"[!NOTE]",
				"Important note.",
			},
		},
		{
			name:  "warning panel",
			input: `<ac:structured-macro ac:name="warning"><ac:rich-text-body><p>Be careful!</p></ac:rich-text-body></ac:structured-macro>`,
			contains: []string{
				"[!WARNING]",
				"Be careful!",
			},
		},
		{
			name:  "link",
			input: `<p>Click <a href="https://example.com">here</a>.</p>`,
			contains: []string{
				"[here](https://example.com)",
			},
		},
		{
			name:  "unordered list",
			input: "<ul><li>A</li><li>B</li></ul>",
			contains: []string{
				"- A",
				"- B",
			},
		},
		{
			name:  "ordered list",
			input: "<ol><li>First</li><li>Second</li></ol>",
			contains: []string{
				"1. First",
				"2. Second",
			},
		},
		{
			name:  "table",
			input: "<table><tr><th>A</th><th>B</th></tr><tr><td>1</td><td>2</td></tr></table>",
			contains: []string{
				"| A",
				"| B",
				"|---|",
				"| 1",
				"| 2",
			},
		},
		{
			name:  "ac:image with attachment",
			input: `<ac:image><ri:attachment ri:filename="diagram.png" /></ac:image>`,
			contains: []string{
				"![diagram.png](attachments/diagram.png)",
			},
		},
		{
			name:  "ac:link with anchor",
			input: `<ac:link ac:anchor="section-one"><ac:plain-text-link-body><![CDATA[Go to section]]></ac:plain-text-link-body></ac:link>`,
			contains: []string{
				"[Go to section](#section-one)",
			},
		},
		{
			name:  "strikethrough",
			input: "<p><del>removed</del></p>",
			contains: []string{
				"~~removed~~",
			},
		},
		{
			name:  "inline code",
			input: "<p>Use <code>kubectl</code> command.</p>",
			contains: []string{
				"`kubectl`",
			},
		},
		{
			name:  "blockquote",
			input: "<blockquote><p>Quoted text.</p></blockquote>",
			contains: []string{
				"> Quoted text.",
			},
		},
		{
			name:  "horizontal rule",
			input: "<hr />",
			contains: []string{
				"---",
			},
		},
		{
			name:  "status macro",
			input: `<ac:structured-macro ac:name="status"><ac:parameter ac:name="title">DONE</ac:parameter></ac:structured-macro>`,
			contains: []string{
				"**DONE**",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := Convert(tt.input)
			if err != nil {
				t.Fatalf("Convert() error = %v", err)
			}
			for _, want := range tt.contains {
				if !strings.Contains(result, want) {
					t.Errorf("result should contain %q\ngot:\n%s", want, result)
				}
			}
		})
	}
}
