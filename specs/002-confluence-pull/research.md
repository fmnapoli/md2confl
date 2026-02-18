# Research: Confluence-to-Markdown Pull

**Branch**: `002-confluence-pull` | **Date**: 2026-02-18

## R-001: CLI Structure — Subcommand vs. Flag

**Decision**: Add `pull` as a positional subcommand: `md2confl pull [flags]`

**Rationale**: The current CLI uses a flat flag model (`--input`, `--publish`, `--dry-run`). Adding pull-specific flags (`--page-id`, `--recursive`, `--depth`, `--skip-attachments`) to the same flat namespace would create confusion and invalid flag combinations. A subcommand cleanly separates the two workflows.

**Alternatives considered**:
- `--pull` flag on existing CLI: rejected — too many mutually exclusive flags, hard to document
- Separate binary `confl2md`: rejected — doubles build/release matrix, fragments user experience

**Implementation**: Check `args[0] == "pull"` at the top of `Run()` and delegate to `runPull(args[1:], ...)`. The existing publish flow remains untouched.

## R-002: Config File — `.confl2md.yml`

**Decision**: Use `.confl2md.yml` as the config file for pull operations (mirroring `.md2confl.yml` for publish).

**Rationale**: The user explicitly requested symmetrical config. The pull workflow has different fields (page IDs, output dirs, recursive flags) that don't map cleanly to the publish config. A separate config file avoids overloading the publish config with pull-specific semantics.

**Structure**:
```yaml
url: https://site.atlassian.net
space: DEVOPS
email: user@example.com

pages:
  - page-id: "12345"
    output-dir: ./docs
    recursive: true
    depth: 5
  - title: "Architecture Overview"
    output-dir: ./arch
```

**Config precedence**: CLI flags > config file > env vars (same `CONFLUENCE_*` env vars as publish).

## R-003: ADF-to-Markdown Converter — Package Location

**Decision**: New package `adftomd` at repository root (peer to `parser`, `adf`, `confluence`).

**Rationale**: Mirrors the `parser` package (Markdown-to-ADF) as a standalone, importable library with zero knowledge of Confluence API or CLI. Keeps separation of concerns per Constitution Principle III.

**Alternatives considered**:
- Add `ToMarkdown()` to `adf` package: rejected — `adf` is data structures only, no conversion logic
- Add to `parser` package: rejected — `parser` depends on goldmark; `adftomd` has no external deps

## R-004: Confluence API v2 — Child Pages Endpoint

**Decision**: Use `GET /wiki/api/v2/pages/{id}/children` with cursor-based pagination.

**Rationale**: This is the standard v2 endpoint for hierarchical traversal. It returns child page metadata (id, title, status, childPosition). Each child's full content (ADF body) is then fetched via the existing `GetPage()` method. Pagination uses an opaque `cursor` parameter from `_links.next`.

**Pagination approach**: Iterate until `_links.next` is absent. Default limit per request: 50 (API max: 250).

## R-005: Confluence API — Attachment Download

**Decision**: Use `GET /wiki/api/v2/pages/{id}/attachments` to list attachments, then download via the `downloadLink` field (relative URL prepended with base URL).

**Rationale**: The v2 attachments endpoint returns metadata including `downloadLink`, `mediaType`, `fileSize`, and `title`. The download URL requires authentication (same Basic auth as API calls). The response may be a 302 redirect to cloud storage.

**Implementation**:
1. List attachments for a page: `GET /wiki/api/v2/pages/{id}/attachments`
2. For each image attachment referenced in ADF: download binary content
3. Save to `attachments/` subdirectory relative to the output Markdown file
4. Patch Markdown image references to local relative paths

## R-006: ADF Node Type Coverage for Reverse Conversion

**Decision**: Support all ADF node types that the existing `parser.ConvertToADF()` produces (100% round-trip coverage), plus common Confluence-native nodes.

**Node type mapping (ADF → Markdown)**:

| ADF Node | Markdown Output |
|----------|----------------|
| `heading` (level 1-6) | `# ` through `###### ` |
| `paragraph` | Plain text + `\n\n` |
| `text` | Raw text |
| `text` + `strong` mark | `**text**` |
| `text` + `em` mark | `*text*` |
| `text` + `strike` mark | `~~text~~` |
| `text` + `code` mark | `` `text` `` |
| `text` + `link` mark | `[text](href)` |
| `text` + `subsup` mark (sup) | `^text^` |
| `hardBreak` | `\n` (literal newline) |
| `codeBlock` (+ language) | `` ```lang\n...\n``` `` |
| `blockquote` | `> ` prefix per line |
| `panel` (info/success/note/warning/error) | `> [!NOTE]` / `[!TIP]` / `[!IMPORTANT]` / `[!WARNING]` / `[!CAUTION]` |
| `bulletList` > `listItem` | `- ` prefix |
| `orderedList` > `listItem` | `1. ` prefix |
| `taskList` > `taskItem` (TODO/DONE) | `- [ ] ` / `- [x] ` |
| `table` > `tableRow` > `tableHeader`/`tableCell` | GFM table with `\|` separators |
| `mediaSingle` > `media` | `![alt](url)` |
| `mediaInline` | `![alt](url)` (inline) |
| `emoji` | `:shortName:` |
| `rule` | `---` |
| `expand` | `<details><summary>title</summary>\n...\n</details>` |
| Unsupported nodes | `<!-- unsupported: nodeType -->` |

**Mark stacking**: When a text node has multiple marks (e.g., bold+italic+link), they nest from outermost to innermost: `[***text***](url)`.

## R-007: Directory Structure Convention for Recursive Pull

**Decision**: Mirror the publish convention exactly.

**Mapping**:
- Page with children → directory with `README.md` (page content) + child files
- Leaf page (no children) → `{sanitized-title}.md`
- Nested children → subdirectories

**Example**: A Confluence tree:
```
Parent Page
├── Child A
├── Child B (has children)
│   ├── Grandchild 1
│   └── Grandchild 2
└── Child C
```

Produces:
```
output-dir/
├── README.md          (Parent Page content)
├── child-a.md
├── child-b/
│   ├── README.md      (Child B content)
│   ├── grandchild-1.md
│   └── grandchild-2.md
└── child-c.md
```

**Filename sanitization**: Replace `/\:*?"<>|` with `-`, collapse consecutive hyphens, lowercase, trim hyphens from edges.

## R-008: FindByTitle — Existing v2 API Method

**Decision**: Reuse the existing `confluence.Client.FindByTitle(spaceID, title)` method for title-based pull.

**Rationale**: The client already has `FindByTitle()` using `GET /wiki/api/v2/pages?space-id={id}&title={title}&status=current`. This returns the full page response including ADF body when combined with `body-format=atlas_doc_format`. The existing method doesn't request the body format — we need to either extend it or make a follow-up `GetPage(id)` call.

**Implementation**: Call `FindByTitle()` to get the page ID, then call `GetPage(id)` to get full ADF body. Two requests, but cleaner than modifying the existing method signature.
