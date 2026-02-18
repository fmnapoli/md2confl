# Contract: adftomd Package

## Public API

```go
package adftomd

import "github.com/fmnapoli/md2confl/adf"

// Convert transforms an ADF Document into Markdown bytes.
// Unsupported node types produce HTML comment placeholders.
// Never returns an error — conversion is best-effort.
func Convert(doc *adf.Document) []byte

// ConvertWithOptions transforms an ADF Document with configuration.
func ConvertWithOptions(doc *adf.Document, opts Options) []byte

// Options configures the ADF-to-Markdown conversion.
type Options struct {
    // ImageRewriter rewrites image URLs before emitting Markdown.
    // Called for media and mediaInline nodes.
    // If nil, URLs are emitted as-is.
    ImageRewriter func(url string) string
}
```

## Conversion Rules

### Block Nodes

| ADF Input | Markdown Output |
|-----------|----------------|
| `{type: "heading", attrs: {level: N}, content: [...]}` | `{"#" * N} {content}\n\n` |
| `{type: "paragraph", content: [...]}` | `{content}\n\n` |
| `{type: "codeBlock", attrs: {language: "go"}, content: [{text: "..."}]}` | `` ```go\n...\n``` `` |
| `{type: "codeBlock", content: [{text: "..."}]}` | `` ```\n...\n``` `` |
| `{type: "blockquote", content: [...]}` | `> {content}` (per line) |
| `{type: "panel", attrs: {panelType: "info"}, content: [...]}` | `> [!NOTE]\n> {content}` |
| `{type: "bulletList", content: [listItems...]}` | `- {item}\n` |
| `{type: "orderedList", content: [listItems...]}` | `1. {item}\n` |
| `{type: "taskList", content: [taskItems...]}` | `- [ ] {item}\n` or `- [x] {item}\n` |
| `{type: "table", ...}` | GFM table with `\|` separators |
| `{type: "rule"}` | `---\n\n` |
| `{type: "mediaSingle", content: [{type: "media", attrs: {url: "..."}}]}` | `![alt](url)\n\n` |
| `{type: "expand", attrs: {title: "..."}, content: [...]}` | `<details><summary>title</summary>\n\n...\n\n</details>` |

### Panel Type Mapping (reverse of parser)

| ADF panelType | GitHub Alert |
|--------------|-------------|
| `info` | `[!NOTE]` |
| `success` | `[!TIP]` |
| `note` | `[!IMPORTANT]` |
| `warning` | `[!WARNING]` |
| `error` | `[!CAUTION]` |

### Inline Nodes & Marks

| ADF Input | Markdown Output |
|-----------|----------------|
| `{type: "text", text: "hello"}` | `hello` |
| `{type: "text", text: "bold", marks: [{type: "strong"}]}` | `**bold**` |
| `{type: "text", text: "italic", marks: [{type: "em"}]}` | `*italic*` |
| `{type: "text", text: "struck", marks: [{type: "strike"}]}` | `~~struck~~` |
| `{type: "text", text: "code", marks: [{type: "code"}]}` | `` `code` `` |
| `{type: "text", text: "link", marks: [{type: "link", attrs: {href: "url"}}]}` | `[link](url)` |
| `{type: "text", text: "sup", marks: [{type: "subsup", attrs: {type: "sup"}}]}` | `^sup^` |
| `{type: "hardBreak"}` | `\n` |
| `{type: "emoji", attrs: {shortName: ":smile:"}}` | `:smile:` |
| `{type: "mediaInline", attrs: {url: "..."}}` | `![alt](url)` |

### Mark Stacking Order

When multiple marks are present, apply them inside-out:
1. `code` (innermost — no nesting inside code)
2. `strong`, `em`, `strike` (in any order)
3. `link` (outermost — wraps formatted text)
4. `subsup` (applied to the final result)

### Unsupported Nodes

Any node type not listed above produces:
```markdown
<!-- unsupported: nodeType -->
```

A warning is emitted to stderr listing all unsupported node types encountered.

## Golden-File Test Convention

Test files live in `adftomd/testdata/`:
```
adftomd/testdata/
├── heading.adf.json     → heading.md
├── paragraph.adf.json   → paragraph.md
├── code-block.adf.json  → code-block.md
├── table.adf.json       → table.md
├── panel-note.adf.json  → panel-note.md
├── task-list.adf.json   → task-list.md
├── mixed-marks.adf.json → mixed-marks.md
├── expand.adf.json      → expand.md
├── unsupported.adf.json → unsupported.md
└── full-page.adf.json   → full-page.md
```

Each pair: input ADF JSON + expected Markdown output. Tests compare `Convert()` output against the `.md` golden file.
