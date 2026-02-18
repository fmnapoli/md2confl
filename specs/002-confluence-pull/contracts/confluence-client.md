# Contract: Confluence Client Extensions

## New Methods

### GetChildren

Fetches all child pages of a given page, handling cursor-based pagination.

```go
// GetChildren returns all direct child pages of a page.
// Paginates automatically until all children are fetched.
func (c *Client) GetChildren(pageID string) ([]ChildPage, error)

type ChildPage struct {
    ID    string
    Title string
}
```

**API**: `GET /wiki/api/v2/pages/{id}/children?limit=50`

**Pagination**: Follow `_links.next` until absent.

**Response mapping**:
```json
{
  "results": [
    {"id": "111", "title": "Child A"},
    {"id": "222", "title": "Child B"}
  ],
  "_links": {
    "next": "/wiki/api/v2/pages/123/children?cursor=abc&limit=50"
  }
}
```

---

### GetAttachments

Fetches all attachments for a page, handling cursor-based pagination.

```go
// GetAttachments returns all attachments for a page.
func (c *Client) GetAttachments(pageID string) ([]Attachment, error)

type Attachment struct {
    ID           string
    Title        string // filename
    MediaType    string // MIME type
    DownloadLink string // relative URL
}
```

**API**: `GET /wiki/api/v2/pages/{id}/attachments?limit=50`

**Pagination**: Same cursor pattern as GetChildren.

---

### DownloadAttachment

Downloads attachment binary content.

```go
// DownloadAttachment downloads an attachment by its relative download link.
// Returns the raw bytes of the file content.
func (c *Client) DownloadAttachment(downloadLink string) ([]byte, error)
```

**URL construction**: `{BaseURL}{downloadLink}`

**Notes**:
- Uses same auth headers as other API calls
- May receive 302 redirect — `http.Client` follows redirects automatically
- Returns `ErrCategoryNotFound` if attachment no longer exists
