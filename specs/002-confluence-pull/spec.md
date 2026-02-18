# Feature Specification: Confluence-to-Markdown Pull

**Feature Branch**: `002-confluence-pull`
**Created**: 2026-02-18
**Status**: Draft
**Input**: User description: "Novo subcomando `pull` para o md2confl que faz o caminho inverso — busca página(s) no Confluence Cloud via API v2 e converte o conteúdo ADF para arquivos Markdown locais."

## User Scenarios & Testing *(mandatory)*

### User Story 1 - Pull a Single Page by ID (Priority: P1)

A user wants to export a specific Confluence page to a local Markdown file. They know the page ID (from the URL or from a previous publish) and want to quickly get the content as Markdown for local editing, version control, or migration.

**Why this priority**: This is the most fundamental operation — pulling a single page is the building block for all other scenarios and delivers immediate value for migration and backup use cases.

**Independent Test**: Can be fully tested by running `md2confl pull --page-id 12345 --output-dir ./out` and verifying that a Markdown file is created with content matching the Confluence page.

**Acceptance Scenarios**:

1. **Given** a valid page ID and authentication credentials, **When** the user runs `md2confl pull --page-id 12345`, **Then** a Markdown file is created in the current directory (or `--output-dir`) with the page title as filename, containing the converted content.
2. **Given** valid credentials and `--dry-run` flag, **When** the user runs `md2confl pull --page-id 12345 --dry-run`, **Then** the tool prints the file path and content that would be written, without creating any files.
3. **Given** invalid credentials, **When** the user runs `md2confl pull --page-id 12345`, **Then** a clear authentication error is shown with an actionable hint.
4. **Given** a non-existent page ID, **When** the user runs the pull command, **Then** a "page not found" error is returned.

---

### User Story 2 - Pull a Single Page by Title (Priority: P2)

A user wants to export a Confluence page but only knows its title, not the numeric ID. They provide the page title and space key, and the tool finds and downloads the page.

**Why this priority**: Title-based lookup is more user-friendly than requiring IDs, making the tool more accessible for occasional use.

**Independent Test**: Can be fully tested by running `md2confl pull --title "My Page" --space DEVOPS` and verifying that the matching page is found and converted.

**Acceptance Scenarios**:

1. **Given** a valid page title and space key, **When** the user runs `md2confl pull --title "My Page"`, **Then** the matching page is found, converted to Markdown, and written to a local file.
2. **Given** a title that does not match any page in the space, **When** the user runs the pull command, **Then** a clear "page not found" error is returned.

---

### User Story 3 - Pull a Page Tree Recursively (Priority: P2)

A user wants to export an entire section of Confluence — a parent page and all its children — into a local directory structure that mirrors the hierarchy used by the `publish` command (README.md for parent pages, individual files for child pages, subdirectories for nested children).

**Why this priority**: Recursive pull enables full round-trip workflows (publish then edit in Confluence then pull back) and bulk migration. It shares the same priority as title-based lookup because both extend the core P1 capability.

**Independent Test**: Can be fully tested by running `md2confl pull --page-id 12345 --recursive --output-dir ./docs` and verifying the generated directory tree matches the Confluence page hierarchy.

**Acceptance Scenarios**:

1. **Given** a parent page with 3 child pages, **When** the user runs `md2confl pull --page-id <parent> --recursive`, **Then** a directory is created with `README.md` (parent content), plus one `.md` file per child page.
2. **Given** a page tree with nested children (grandchildren), **When** the user runs the recursive pull, **Then** subdirectories are created for intermediate pages, each containing their own `README.md` and child files — matching the publish directory convention.
3. **Given** `--dry-run` with `--recursive`, **When** the user runs the command, **Then** the full file tree is printed without creating any files or directories.

---

### User Story 4 - ADF-to-Markdown Conversion Fidelity (Priority: P1)

A user expects the pulled Markdown to accurately represent the Confluence page content. Headings, tables, code blocks, lists (ordered, unordered, task lists), links, inline formatting (bold, italic, strikethrough, code), images, blockquotes, panels, horizontal rules, and emoji should all convert correctly.

**Why this priority**: Conversion fidelity is core to the feature's value — without accurate conversion, pulling pages is useless. This shares P1 priority with single-page pull since both are required for a viable MVP.

**Independent Test**: Can be tested by converting known ADF structures to Markdown and comparing output against expected Markdown files.

**Acceptance Scenarios**:

1. **Given** a page with headings (H1-H6), paragraphs, bold, italic, strikethrough, inline code, and links, **When** pulled, **Then** the Markdown output preserves all formatting correctly.
2. **Given** a page with a GFM-compatible table, **When** pulled, **Then** the Markdown output contains a valid GFM table with header row and alignment.
3. **Given** a page with fenced code blocks (with language annotation), **When** pulled, **Then** the Markdown output contains fenced code blocks with the correct language tag.
4. **Given** a page with bullet lists, ordered lists, and task lists, **When** pulled, **Then** the Markdown output contains the correct list syntax including checkbox state for task lists.
5. **Given** a page with ADF panel nodes (info, warning, error, note, success), **When** pulled, **Then** the Markdown output contains GitHub-style alert syntax (`> [!NOTE]`, etc.).
6. **Given** a page with images (attachments or external URLs), **When** pulled, **Then** the Markdown output contains image references (`![alt](url)`).

---

### User Story 5 - Download Attachments (Priority: P3)

When pulling pages, image attachments referenced in the content should be downloaded to a local `attachments/` directory (or alongside the Markdown files), and the Markdown image references should point to the local paths.

**Why this priority**: Attachment download completes the offline experience but is not strictly required for the core pull workflow — external image URLs still work without it.

**Independent Test**: Can be tested by pulling a page with image attachments and verifying files are downloaded and Markdown references updated.

**Acceptance Scenarios**:

1. **Given** a page with inline image attachments, **When** pulled without `--skip-attachments`, **Then** attachment files are downloaded to an `attachments/` subdirectory and Markdown image references point to the relative local paths.
2. **Given** `--skip-attachments` flag, **When** pulling a page with attachments, **Then** no files are downloaded and image references remain as Confluence URLs.

---

### User Story 6 - JSON Output for CI/CD (Priority: P3)

A user running the pull command in a CI/CD pipeline needs structured JSON output to integrate with downstream tooling (e.g., to detect which pages were pulled, their local paths, and any errors).

**Why this priority**: JSON output is a convenience for automation but is not required for human-facing usage.

**Independent Test**: Can be tested by running `md2confl pull --page-id 12345 --json` and validating the JSON structure.

**Acceptance Scenarios**:

1. **Given** `--json` flag, **When** a pull operation completes successfully, **Then** output is valid JSON containing status, page ID, title, and local file path for each pulled page.
2. **Given** `--json` flag and an error condition, **When** the pull fails, **Then** a JSON error object is returned with status, code, message, and optional hint.

---

### Edge Cases

- What happens when the output directory already contains files with the same names? Files are overwritten — pull is inherently a "fetch latest" operation.
- What happens when a page title contains characters invalid for filenames (e.g., `/`, `\`, `:`)? Invalid characters are replaced with hyphens, and a warning is emitted.
- What happens when a page has no content (empty body)? An empty Markdown file is created with just the page-id marker and title as H1.
- What happens when the Confluence API returns paginated child pages? Pagination is handled transparently, fetching all pages.
- What happens when an attachment download fails? A warning is emitted and the Markdown image reference falls back to the Confluence URL.
- What happens when ADF contains node types not yet supported by the converter? Unsupported nodes are skipped with a warning, an HTML comment placeholder is inserted in the Markdown (e.g., `<!-- unsupported: inlineCard -->`), and surrounding content is preserved.
- What happens when recursive pull exceeds the depth limit? Traversal stops at the limit depth, a warning is emitted listing the truncated branches, and all pages up to the limit are still written.

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: System MUST support pulling a single Confluence page by numeric page ID.
- **FR-002**: System MUST support pulling a single Confluence page by title (within a space).
- **FR-003**: System MUST support recursively pulling a page and all its descendant pages.
- **FR-004**: System MUST convert ADF content to Markdown preserving: headings, paragraphs, bold, italic, strikethrough, inline code, code blocks (with language), links, images, bullet lists, ordered lists, task lists, tables, blockquotes, panels (as GitHub alerts), horizontal rules, emoji, and expand/collapse sections.
- **FR-005**: System MUST reuse the same authentication configuration as the publish command (--url, --email, --token, --space, config file, environment variables).
- **FR-006**: System MUST support `--output-dir` flag to specify the destination directory.
- **FR-007**: System MUST support `--dry-run` to preview what would be generated without writing files.
- **FR-008**: System MUST support `--json` flag for structured JSON output.
- **FR-009**: System MUST generate a directory structure matching the publish convention: README.md for parent pages, individual .md files for leaf pages, subdirectories for nested children.
- **FR-010**: System MUST download image attachments referenced in pages and update Markdown references to local paths.
- **FR-011**: System MUST support `--skip-attachments` flag to disable attachment downloads.
- **FR-012**: System MUST sanitize page titles for use as filenames (replacing invalid characters).
- **FR-013**: System MUST handle API pagination when fetching child pages.
- **FR-014**: System MUST insert a `<!-- confluence-page-id: XXXX -->` marker at the top of each generated Markdown file to enable round-trip publish workflows.
- **FR-015**: System MUST always prepend the page title as an H1 heading at the top of the Markdown body, ensuring round-trip fidelity with the publish command (which extracts the first H1 as page title).
- **FR-016**: System MUST enforce a default recursion depth limit of 10 levels for `--recursive`, overridable via `--depth N` flag. When the limit is reached, a warning is emitted indicating truncation.
- **FR-017**: System MUST insert an HTML comment placeholder (`<!-- unsupported: <nodeType> -->`) in the Markdown output for any ADF node type that has no Markdown equivalent, and emit a warning listing the unsupported node types encountered.

### Key Entities

- **ADF Document**: The JSON-based rich content format returned by the Confluence API v2. Input to the new ADF-to-Markdown converter.
- **Markdown File**: The output file generated for each pulled page, containing converted content with a page-id marker.
- **Page Tree**: A hierarchical structure of parent and child pages that maps to a local directory tree.
- **Attachment**: A binary file (image) attached to a Confluence page, downloaded locally and referenced in the Markdown output.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: Users can pull a single Confluence page and receive a valid Markdown file within 5 seconds (excluding network latency).
- **SC-002**: A recursive pull of a 20-page tree produces the correct directory structure with all pages converted in a single command invocation.
- **SC-003**: Round-trip fidelity: content published with `md2confl --publish` then pulled back with `md2confl pull` produces Markdown that, when re-published, generates identical ADF (semantic equivalence, not byte-identical).
- **SC-004**: All ADF node types produced by the existing `parser.ConvertToADF` function are correctly handled by the reverse converter (100% coverage of the project's own ADF output).
- **SC-005**: The pull command reuses the same authentication mechanism as publish — `CONFLUENCE_*` environment variables, `--url`/`--email`/`--token` flags, and the same credential precedence order — requiring zero additional auth setup. Pull-specific settings (page IDs, output dirs, recursion) use a separate `.confl2md.yml` config file.

## Clarifications

### Session 2026-02-18

- Q: Should the pull command prepend the page title as an H1 heading in the Markdown output? → A: Always prepend page title as H1 at top of Markdown body (ensures round-trip with publish).
- Q: Should recursive pull have a depth limit to prevent runaway traversal? → A: Default depth limit of 10 levels, overridable via `--depth N` flag.
- Q: How should the converter handle Confluence-native ADF nodes that have no Markdown equivalent? → A: Skip unsupported nodes and insert an HTML comment placeholder (e.g., `<!-- unsupported: inlineCard -->`).

## Assumptions

- The Confluence Cloud REST API v2 provides page content in ADF format via `?body-format=atlas_doc_format` (confirmed: already used in `client.GetPage`).
- The API v2 endpoint for listing child pages is available at `/wiki/api/v2/pages/{id}/children` (standard Confluence Cloud API).
- Attachment download uses the existing v1 API endpoint pattern already established in the client package.
- The ADF-to-Markdown converter only needs to handle ADF structures that Confluence Cloud actually produces — exotic or deprecated node types can be deferred.
- File overwrite is the default behavior for pull (no `--force` flag needed since pulling is a "sync from server" operation).
- The `--recursive` flag defaults to off (single page pull).
