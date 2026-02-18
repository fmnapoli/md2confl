<!-- confluence-page-id: 99999 -->
# Round-Trip Fidelity Test

This document tests that Markdown published to Confluence and pulled back preserves semantic content.

## Inline Formatting

Text with **bold**, *italic*, `code inline`, ~~strikethrough~~ and [a link](https://example.com).

Combinations: ***bold and italic***, **bold with `code`**, ~~strike with **bold**~~.

## Headings

### Heading 3

#### Heading 4

##### Heading 5

###### Heading 6

## Lists

### Bullet List

- First item
- Second item with **formatting**
- Third item
  - Nested sub-item
  - Another sub-item

### Ordered List

1. Step one
2. Step two
3. Step three

Ordered list with custom start:

5. Item five
6. Item six
7. Item seven

### Task List

- [ ] Pending task
- [x] Completed task
- [ ] Task with **bold** and `code`

## Blockquote

> A simple quote with **bold** and *italic*.

## GitHub Alerts

> [!NOTE]
> Useful information the user should know.

> [!TIP]
> Optional advice to help the user succeed.

> [!IMPORTANT]
> Crucial information for the user to succeed.

> [!WARNING]
> Critical content requiring immediate attention.

> [!CAUTION]
> Potential negative consequences of an action.

## Code Blocks

Inline: use `go test ./...` to run tests.

Fenced with language:

```go
func main() {
    fmt.Println("Hello, Confluence!")
}
```

Fenced without language:

```
plain text code block
no syntax highlighting
```

## Tables

| Feature | Status | Notes |
|---------|--------|-------|
| Headings | Supported | H1 to H6 |
| Task lists | Supported | Checkboxes |
| Alerts | Supported | 5 panel types |

## Emoji

Emoji inline: :wave: Hello! :+1: All good :rocket:

## Superscript

Formulas: x^2^ + y^3^ = z^n^

References: 1^st^, 2^nd^, 3^rd^

## Expand

<details><summary>Click to expand</summary>
Hidden content with **formatting** and `code`.
</details>

## Separators

Content above.

---

Content below.
