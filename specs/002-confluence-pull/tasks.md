# Tasks: Confluence-to-Markdown Pull

**Input**: Design documents from `/specs/002-confluence-pull/`
**Prerequisites**: plan.md, spec.md, research.md, data-model.md, contracts/

**Tests**: Included — spec.md specifies golden-file tests (ADF→MD), HTTP mock tests for API client, and unit tests for CLI.

**Organization**: Tasks are grouped by user story to enable independent implementation and testing of each story.

## Format: `[ID] [P?] [Story] Description`

- **[P]**: Can run in parallel (different files, no dependencies)
- **[Story]**: Which user story this task belongs to (e.g., US1, US2, US3)
- Include exact file paths in descriptions

---

## Phase 1: Setup

**Purpose**: Create the new `adftomd` package structure and wire the `pull` subcommand routing into the existing CLI.

- [x] T001 Create `adftomd/` package directory with `convert.go` skeleton defining `Convert()`, `ConvertWithOptions()`, and `Options` per contract in `contracts/adftomd.md`
- [x] T002 [P] Add `pull` subcommand routing in `internal/cli/cli.go` — detect `args[0] == "pull"` and delegate to `runPull()` stub that returns "not implemented" error
- [x] T003 [P] Create `internal/cli/pull_config.go` with `PullConfig` struct and `.confl2md.yml` loader per data-model.md (auto-discovery, validation rules, config precedence)
- [x] T004 [P] Create `internal/cli/pull_config_test.go` with unit tests for config loading: valid config, missing fields, mutually exclusive page-id/title, token rejection, depth bounds

---

## Phase 2: Foundational (Blocking Prerequisites)

**Purpose**: Extend the Confluence API client with child pages and attachment methods needed by multiple user stories.

**CRITICAL**: No user story work can begin until this phase is complete.

- [x] T005 Implement `GetChildren(pageID string) ([]ChildPage, error)` in `confluence/client.go` with cursor-based pagination per `contracts/confluence-client.md`
- [x] T006 [P] Implement `GetAttachments(pageID string) ([]Attachment, error)` in `confluence/client.go` with cursor-based pagination per `contracts/confluence-client.md`
- [x] T007 [P] Implement `DownloadAttachment(downloadLink string) ([]byte, error)` in `confluence/client.go` per `contracts/confluence-client.md`
- [x] T008 Add HTTP mock tests for `GetChildren`, `GetAttachments`, and `DownloadAttachment` in `confluence/client_test.go` — cover pagination, empty results, auth errors, 404
- [x] T009 Implement `sanitizeFilename(title string) string` in `internal/cli/pull.go` per data-model.md rules (replace invalid chars, collapse hyphens, lowercase, trim, truncate, "untitled" fallback)
- [x] T010 [P] Add unit tests for `sanitizeFilename` in `internal/cli/pull_test.go` — cover special chars, consecutive hyphens, empty title, long title truncation, unicode

**Checkpoint**: API client extensions and filename sanitization ready — user story implementation can now begin.

---

## Phase 3: User Story 4 — ADF-to-Markdown Conversion Fidelity (Priority: P1)

**Goal**: The `adftomd.Convert()` function accurately transforms all supported ADF node types into Markdown, validated by golden-file tests.

**Independent Test**: Run `go test ./adftomd/...` — all golden-file comparisons pass.

> Note: US4 is implemented before US1 because US1 (single page pull) depends on the converter being functional.

### Tests for US4

- [x] T011 [P] [US4] Create golden-file test pairs in `adftomd/testdata/`: `heading.adf.json`/`heading.md`, `paragraph.adf.json`/`paragraph.md`, `code-block.adf.json`/`code-block.md` per conversion rules in `contracts/adftomd.md`
- [x] T012 [P] [US4] Create golden-file test pairs in `adftomd/testdata/`: `table.adf.json`/`table.md`, `panel-note.adf.json`/`panel-note.md`, `task-list.adf.json`/`task-list.md`
- [x] T013 [P] [US4] Create golden-file test pairs in `adftomd/testdata/`: `mixed-marks.adf.json`/`mixed-marks.md`, `expand.adf.json`/`expand.md`, `unsupported.adf.json`/`unsupported.md`
- [x] T014 [P] [US4] Create golden-file test pair `adftomd/testdata/full-page.adf.json`/`full-page.md` covering a realistic page with headings, paragraphs, code, table, list, and panel combined
- [x] T015 [US4] Write golden-file test runner in `adftomd/convert_test.go` — iterate `testdata/*.adf.json`, call `Convert()`, compare output to corresponding `.md` file

### Implementation for US4

- [x] T016 [US4] Implement inline node/mark rendering in `adftomd/convert.go`: `text`, `hardBreak`, `emoji`, `mediaInline`, plus marks (`strong`, `em`, `strike`, `code`, `link`, `subsup`) with mark stacking order per `contracts/adftomd.md`
- [x] T017 [US4] Implement block node rendering in `adftomd/convert.go`: `heading`, `paragraph`, `codeBlock`, `blockquote`, `rule`
- [x] T018 [US4] Implement list rendering in `adftomd/convert.go`: `bulletList`, `orderedList`, `taskList` with nested list support
- [x] T019 [US4] Implement table rendering in `adftomd/convert.go`: `table` → GFM table with `tableRow`, `tableHeader`, `tableCell`
- [x] T020 [US4] Implement panel rendering in `adftomd/convert.go`: `panel` → GitHub alert syntax (`[!NOTE]`, `[!TIP]`, `[!IMPORTANT]`, `[!WARNING]`, `[!CAUTION]`) per panelType mapping
- [x] T021 [US4] Implement `mediaSingle` and `expand` rendering in `adftomd/convert.go`: images → `![alt](url)`, expand → `<details>/<summary>` HTML
- [x] T022 [US4] Implement unsupported node handling in `adftomd/convert.go`: unknown node types → `<!-- unsupported: nodeType -->` comment
- [x] T023 [US4] Implement `ConvertWithOptions()` with `ImageRewriter` callback — apply rewriter to media/mediaInline URLs before emitting Markdown
- [x] T024 [US4] Verify all golden-file tests pass with `go test ./adftomd/...` and fix any conversion discrepancies

**Checkpoint**: `adftomd` package fully functional — `Convert()` handles all ADF node types. Run `go test ./adftomd/...`.

---

## Phase 4: User Story 1 — Pull a Single Page by ID (Priority: P1)

**Goal**: `md2confl pull --page-id 12345 --output-dir ./out` fetches a Confluence page, converts ADF to Markdown, and writes a local file with page-id marker and H1 title.

**Independent Test**: Run `go test ./internal/cli/... -run TestPull` — mock API returns ADF, pull writes correct Markdown file.

### Tests for US1

- [x] T025 [P] [US1] Write unit tests in `internal/cli/pull_test.go` for single-page pull: mock `GetPage()`, verify output file contains `<!-- confluence-page-id: ... -->` marker, H1 title, converted body, and correct sanitized filename
- [x] T026 [P] [US1] Write unit tests in `internal/cli/pull_test.go` for `--dry-run` mode: verify no files are written and stdout shows preview output
- [x] T027 [P] [US1] Write unit tests in `internal/cli/pull_test.go` for error cases: invalid page ID (404), auth failure, empty page body

### Implementation for US1

- [x] T028 [US1] Implement `runPull()` in `internal/cli/pull.go` — parse pull-specific flags (`--page-id`, `--output-dir`, `--dry-run`, `--verbose`), merge with `PullConfig`, validate inputs
- [x] T029 [US1] Implement single-page pull logic in `internal/cli/pull.go` — call `client.GetPage(pageID)`, parse ADF body, call `adftomd.Convert()`, prepend page-id marker and H1 title, write to `{output-dir}/{sanitized-title}.md`
- [x] T030 [US1] Implement `--dry-run` for single-page pull in `internal/cli/pull.go` — print file path, title, and byte count without creating files
- [x] T031 [US1] Implement text output for single-page pull per `contracts/pull-cli.md` — `Pulled: "Title" → path` on success, actionable error messages on failure
- [x] T032 [US1] Wire exit codes per `contracts/pull-cli.md`: 0 = success, 1 = user error (bad flags), 2 = API error (auth, not found)

**Checkpoint**: Single page pull by ID works end-to-end. Run `go test ./internal/cli/... -run TestPull`.

---

## Phase 5: User Story 2 — Pull a Single Page by Title (Priority: P2)

**Goal**: `md2confl pull --title "My Page" --space DEVOPS` finds the page by title and pulls it.

**Independent Test**: Run `go test ./internal/cli/... -run TestPullByTitle`.

### Tests for US2

- [x] T033 [P] [US2] Write unit tests in `internal/cli/pull_test.go` for title-based pull: mock `FindByTitle()` → page ID, then mock `GetPage()`, verify correct file output
- [x] T034 [P] [US2] Write unit tests in `internal/cli/pull_test.go` for title errors: title not found, missing `--space` flag

### Implementation for US2

- [x] T035 [US2] Add `--title` and `--space` flag handling in `internal/cli/pull.go` — validate mutual exclusivity with `--page-id`, require `--space` when `--title` is set
- [x] T036 [US2] Implement title-based lookup in `internal/cli/pull.go` — call `client.FindByTitle(spaceID, title)` to get page ID, then reuse single-page pull logic from US1 (per research.md R-008)

**Checkpoint**: Title-based pull works. Run `go test ./internal/cli/... -run TestPullByTitle`.

---

## Phase 6: User Story 3 — Pull a Page Tree Recursively (Priority: P2)

**Goal**: `md2confl pull --page-id 12345 --recursive --output-dir ./docs` fetches a page and all descendants, mirroring the Confluence hierarchy as a local directory tree.

**Independent Test**: Run `go test ./internal/cli/... -run TestPullRecursive` — mock API returns parent + children + grandchildren, verify directory structure matches convention.

### Tests for US3

- [x] T037 [P] [US3] Write unit tests in `internal/cli/pull_test.go` for recursive pull: mock page tree (parent + 3 children + 2 grandchildren), verify directory structure per research.md R-007 (README.md for parents, `{title}.md` for leaves, subdirectories for nested)
- [x] T038 [P] [US3] Write unit tests in `internal/cli/pull_test.go` for `--depth` limit: mock deep tree, verify traversal stops at limit and warning is emitted
- [x] T039 [P] [US3] Write unit test in `internal/cli/pull_test.go` for `--dry-run --recursive`: verify full tree is printed without creating files/directories

### Implementation for US3

- [x] T040 [US3] Implement `pageNode` tree builder in `internal/cli/pull.go` — recursive fetch using `client.GetChildren()` + `client.GetPage()` per page, respecting `--depth` limit (default 10, max 100)
- [x] T041 [US3] Implement tree-to-filesystem writer in `internal/cli/pull.go` — walk `pageNode` tree, create directories for pages with children (parent content → `README.md`), write leaf pages as `{sanitized-title}.md`
- [x] T042 [US3] Implement `--dry-run` for recursive pull — print full tree structure (`Would write: ...`) without creating files/directories
- [x] T043 [US3] Implement depth-limit warning — when traversal is truncated, emit warning listing the branches that were not fully traversed
- [x] T044 [US3] Handle API pagination in recursive fetch — `GetChildren()` already paginates, but ensure parent pages with >50 children are fully traversed

**Checkpoint**: Recursive pull produces correct directory tree. Run `go test ./internal/cli/... -run TestPullRecursive`.

---

## Phase 7: User Story 5 — Download Attachments (Priority: P3)

**Goal**: When pulling pages, image attachments are downloaded to `attachments/` and Markdown image references are rewritten to local relative paths.

**Independent Test**: Run `go test ./internal/cli/... -run TestPullAttachment` — mock attachment list and download, verify files saved and Markdown references updated.

### Tests for US5

- [x] T045 [P] [US5] Write unit tests in `internal/cli/pull_test.go` for attachment download: mock `GetAttachments()` + `DownloadAttachment()`, verify files saved to `{output-dir}/attachments/`, Markdown image refs updated to relative paths
- [x] T046 [P] [US5] Write unit tests in `internal/cli/pull_test.go` for `--skip-attachments`: verify no downloads, image refs remain as Confluence URLs
- [x] T047 [P] [US5] Write unit test in `internal/cli/pull_test.go` for attachment download failure: verify warning emitted and image ref falls back to Confluence URL

### Implementation for US5

- [x] T048 [US5] Implement attachment download logic in `internal/cli/pull.go` — after converting ADF, call `GetAttachments(pageID)`, download each image attachment, save to `{output-dir}/attachments/{filename}`
- [x] T049 [US5] Implement `ImageRewriter` callback for `ConvertWithOptions()` in `internal/cli/pull.go` — rewrite Confluence attachment URLs to relative `attachments/{filename}` paths
- [x] T050 [US5] Implement `--skip-attachments` flag — when set, skip download and leave image URLs as-is (pass nil `ImageRewriter`)
- [x] T051 [US5] Handle attachment download errors gracefully — emit warning, fall back to Confluence URL for that image

**Checkpoint**: Attachments download and Markdown refs update correctly. Run `go test ./internal/cli/... -run TestPullAttachment`.

---

## Phase 8: User Story 6 — JSON Output for CI/CD (Priority: P3)

**Goal**: `md2confl pull --page-id 12345 --json` outputs structured JSON per `contracts/pull-cli.md`.

**Independent Test**: Run `go test ./internal/cli/... -run TestPullJSON` — verify JSON structure on success and error.

### Tests for US6

- [x] T052 [P] [US6] Write unit tests in `internal/cli/pull_test.go` for `--json` output: verify success JSON contains `status`, `pages[]` with `pageId`/`title`/`filePath`/`action`/`children`, `attachments` count
- [x] T053 [P] [US6] Write unit test in `internal/cli/pull_test.go` for `--json` error output: verify error JSON contains `status`, `code`, `message`, `hint`

### Implementation for US6

- [x] T054 [US6] Define `PullResult` and `PullOutput` JSON structs in `internal/cli/pull.go` per data-model.md and `contracts/pull-cli.md`
- [x] T055 [US6] Implement `--json` flag in `internal/cli/pull.go` — collect `PullResult` per page during pull, marshal to JSON on completion (success or error)
- [x] T056 [US6] Ensure JSON error output uses structured format with `status: "error"`, `code`, `message`, and `hint` fields per contract

**Checkpoint**: JSON output works for both success and error. Run `go test ./internal/cli/... -run TestPullJSON`.

---

## Phase 9: Polish & Cross-Cutting Concerns

**Purpose**: Integration testing, docker-compose, config file support, edge cases.

- [x] T057 Implement config-file-based pull in `internal/cli/pull.go` — when `--config` is specified (or `.confl2md.yml` auto-detected), iterate `pages[]` entries and pull each
- [x] T058 [P] Add docker-compose services (`pull-page`, `pull-docs`, `pull-dry-run`) in `docker-compose.yml` per plan.md integration testing section
- [x] T059 [P] Add edge-case handling in `internal/cli/pull.go`: overwrite existing files, empty page body (write page-id + H1 only), paginated child pages
- [x] T060 Run `go test ./...` and fix any failures across all packages
- [x] T061 Run `go vet ./...` and `golangci-lint run` — fix any warnings
- [x] T062 Validate quickstart.md scenarios manually or via docker-compose integration tests
- [x] T063 Add round-trip fidelity test: publish a known Markdown file with `publish-docs`, pull it back with `pull-page`, then compare the pulled Markdown against the original source to validate SC-003 (semantic equivalence via docker-compose integration test)

---

## Dependencies & Execution Order

### Phase Dependencies

- **Setup (Phase 1)**: No dependencies — can start immediately
- **Foundational (Phase 2)**: Depends on Setup — BLOCKS all user stories
- **US4 — ADF Converter (Phase 3)**: Depends on Phase 1 (adftomd package skeleton) — BLOCKS US1
- **US1 — Single Page by ID (Phase 4)**: Depends on Phase 2 + Phase 3 (converter must work)
- **US2 — Single Page by Title (Phase 5)**: Depends on Phase 4 (reuses single-page pull logic)
- **US3 — Recursive Pull (Phase 6)**: Depends on Phase 4 (reuses single-page pull logic) + Phase 2 (`GetChildren`)
- **US5 — Attachments (Phase 7)**: Depends on Phase 4 + Phase 2 (`GetAttachments`, `DownloadAttachment`)
- **US6 — JSON Output (Phase 8)**: Depends on Phase 4 (needs pull logic to wrap with JSON)
- **Polish (Phase 9)**: Depends on all user story phases

### User Story Dependencies

```
Phase 1 (Setup) ──→ Phase 2 (Foundational) ──→ Phase 3 (US4: Converter) ──→ Phase 4 (US1: Page by ID)
                                                                                ├──→ Phase 5 (US2: By Title)
                                                                                ├──→ Phase 6 (US3: Recursive)
                                                                                ├──→ Phase 7 (US5: Attachments)
                                                                                └──→ Phase 8 (US6: JSON)
                                                                                         │
                                                                              Phase 9 (Polish) ◀──┘
```

### Within Each User Story

- Tests MUST be written and FAIL before implementation
- Golden files before test runner (US4)
- Models/types before services
- Core logic before output formatting
- Story complete before moving to next priority

### Parallel Opportunities

- **Phase 1**: T002, T003, T004 can run in parallel (different files)
- **Phase 2**: T006, T007 can run in parallel with T005; T009, T010 can run in parallel
- **Phase 3**: T011, T012, T013, T014 (golden files) can all run in parallel
- **Phase 4**: T025, T026, T027 (tests) can run in parallel
- **Phases 5, 6, 7, 8**: Can run in parallel after Phase 4 completes (different concerns, but share `pull.go` — coordinate carefully)

---

## Parallel Example: User Story 4 (ADF Converter)

```bash
# Launch all golden-file creation tasks in parallel:
Task: T011 "Create heading/paragraph/code-block golden files"
Task: T012 "Create table/panel/task-list golden files"
Task: T013 "Create mixed-marks/expand/unsupported golden files"
Task: T014 "Create full-page golden file"

# Then sequentially:
Task: T015 "Write golden-file test runner"

# Then implementation (some parallelizable, but same file):
Task: T016-T023 "Implement converter node by node"
Task: T024 "Verify all tests pass"
```

---

## Implementation Strategy

### MVP First (US4 + US1 Only)

1. Complete Phase 1: Setup — package skeleton and CLI routing
2. Complete Phase 2: Foundational — API client extensions
3. Complete Phase 3: US4 — ADF-to-Markdown converter with golden-file tests
4. Complete Phase 4: US1 — Single page pull by ID
5. **STOP and VALIDATE**: `md2confl pull --page-id <id>` works end-to-end
6. This covers P1 stories and delivers immediate value

### Incremental Delivery

1. Setup + Foundational + US4 + US1 → MVP: pull a single page
2. Add US2 (title lookup) → more user-friendly access
3. Add US3 (recursive) → bulk migration capability
4. Add US5 (attachments) → complete offline experience
5. Add US6 (JSON) → CI/CD integration
6. Polish → docker-compose, edge cases, validation

### Suggested MVP Scope

**Phases 1–4** (T001–T032): Setup + Foundational + US4 + US1 = **32 tasks**
This delivers a working `pull` command that can fetch any single page by ID with accurate ADF-to-Markdown conversion.

---

## Notes

- [P] tasks = different files, no dependencies
- [Story] label maps task to specific user story for traceability
- Each user story should be independently completable and testable
- Verify tests fail before implementing
- Commit after each task or logical group
- Stop at any checkpoint to validate story independently
- US4 (converter) is placed before US1 because it's a prerequisite — US1 calls `Convert()`
