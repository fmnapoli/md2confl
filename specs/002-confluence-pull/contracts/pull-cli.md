# Contract: Pull CLI Interface

## Subcommand

```
md2confl pull [flags]
```

## Flags

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--page-id` | string | | Confluence page ID to pull |
| `--title` | string | | Page title to search (requires `--space`) |
| `--space` | string | | Confluence space key |
| `--recursive` | bool | false | Pull child pages recursively |
| `--depth` | int | 10 | Max recursion depth (1-100) |
| `--output-dir` | string | "." | Destination directory |
| `--skip-attachments` | bool | false | Skip downloading image attachments |
| `--dry-run` | bool | false | Preview without writing files |
| `--json` | bool | false | JSON output format |
| `--url` | string | | Confluence base URL |
| `--email` | string | | Atlassian account email |
| `--token` | string | | Atlassian API token |
| `--config` | string | | Config file path (default: auto-detect `.confl2md.yml`) |
| `--verbose` | bool | false | Debug logging to stderr |
| `--version` | bool | false | Show version |

## Mutually Exclusive

- `--page-id` and `--title` (exactly one required, unless using config)
- `--title` requires `--space`

## Exit Codes

| Code | Meaning |
|------|---------|
| 0 | Success |
| 1 | User error (bad flags, missing config) |
| 2 | API error (auth failure, page not found, network) |

## Text Output (default)

```
Pulled: "Page Title" → docs/page-title.md
Pulled: "Child Page" → docs/child-page.md
Downloaded: 2 attachments → docs/attachments/

2 page(s) pulled successfully.
```

## JSON Output (`--json`)

### Success

```json
{
  "status": "success",
  "pages": [
    {
      "pageId": "12345",
      "title": "Page Title",
      "filePath": "docs/page-title.md",
      "action": "written",
      "children": 0
    }
  ],
  "attachments": 2,
  "warnings": []
}
```

### Error

```json
{
  "status": "error",
  "code": 2,
  "message": "page not found: 99999",
  "hint": "verify the page ID exists and you have read access"
}
```

## Dry-Run Output

```
Would write: docs/page-title.md (Page Title, 1234 bytes)
Would write: docs/child-page.md (Child Page, 567 bytes)
Would download: 2 attachments to docs/attachments/

2 page(s) would be pulled.
```
