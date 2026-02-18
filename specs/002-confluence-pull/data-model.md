# Data Model: Confluence-to-Markdown Pull

**Branch**: `002-confluence-pull` | **Date**: 2026-02-18

## Entities

### PullConfig (`.confl2md.yml`)

```yaml
url: string            # Confluence base URL (required)
space: string           # Space key (required for title-based lookup)
email: string           # Atlassian account email
pages:                  # List of pages to pull
  - page-id: string     # Confluence page ID (mutually exclusive with title)
    title: string        # Page title (requires space)
    output-dir: string   # Destination directory (default: ".")
    recursive: bool      # Pull child pages (default: false)
    depth: int           # Max recursion depth (default: 10)
    skip-attachments: bool  # Skip attachment download (default: false)
```

**Validation rules**:
- `url` must start with `https://`
- Each page entry must have either `page-id` or `title` (not both)
- `title` requires `space` to be set (at top level or per-page)
- `depth` must be 1-100 (default 10)
- `token` field rejected (same security rule as `.md2confl.yml`)

### PullResult

```go
type PullResult struct {
    PageID   string `json:"pageId"`
    Title    string `json:"title"`
    FilePath string `json:"filePath"`
    Action   string `json:"action"`   // "written", "skipped" (dry-run)
    Children int    `json:"children"` // count of child pages pulled (recursive only)
}
```

### PageNode (internal tree structure for recursive pull)

```go
type pageNode struct {
    id       string
    title    string
    adfBody  string        // raw ADF JSON from API
    children []*pageNode   // child pages (populated by recursive fetch)
}
```

**State transitions**: None — pull is a stateless read operation.

### ADF-to-Markdown Converter (adftomd package)

```go
// Convert transforms an ADF Document into Markdown bytes.
func Convert(doc *adf.Document) []byte

// ConvertWithOptions transforms an ADF Document with configuration.
func ConvertWithOptions(doc *adf.Document, opts Options) []byte

type Options struct {
    // ImageRewriter rewrites image URLs (e.g., Confluence attachment → local path).
    // If nil, URLs are left as-is.
    ImageRewriter func(url string) string
}
```

**Design notes**:
- Returns `[]byte` (not error) — conversion is best-effort, unsupported nodes produce comments
- `ImageRewriter` callback allows the CLI layer to rewrite attachment URLs to local paths without the converter knowing about Confluence

## Relationships

```
PullConfig (`.confl2md.yml`)
  └── pages[] ──→ Confluence API ──→ pageNode tree
                                        │
                                        ▼
                                   ADF Document ──→ adftomd.Convert() ──→ Markdown bytes
                                        │                                      │
                                        ▼                                      ▼
                                   Attachments ──→ download ──→ attachments/   local .md files
```

## API Client Extensions

### New methods on `confluence.Client`

```go
// GetChildren returns all child pages of a given page (handles pagination).
func (c *Client) GetChildren(pageID string) ([]childPage, error)

type childPage struct {
    ID    string
    Title string
}

// GetAttachments returns all attachments for a page (handles pagination).
func (c *Client) GetAttachments(pageID string) ([]attachment, error)

type attachment struct {
    ID           string
    Title        string // filename
    MediaType    string
    DownloadLink string // relative URL
}

// DownloadAttachment downloads attachment content by its download link.
func (c *Client) DownloadAttachment(downloadLink string) ([]byte, error)
```

## Filename Sanitization

```go
// sanitizeFilename converts a page title to a valid, filesystem-safe filename.
// Rules:
// 1. Replace characters /\:*?"<>| with hyphen
// 2. Collapse consecutive hyphens
// 3. Lowercase
// 4. Trim hyphens from edges
// 5. Truncate to 200 characters (preserving .md extension)
// 6. If empty after sanitization, use "untitled"
func sanitizeFilename(title string) string
```
