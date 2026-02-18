# Implementation Plan: Confluence-to-Markdown Pull

**Branch**: `002-confluence-pull` | **Date**: 2026-02-18 | **Spec**: [spec.md](spec.md)
**Input**: Feature specification from `/specs/002-confluence-pull/spec.md`

## Summary

New `pull` subcommand for md2confl that fetches Confluence Cloud pages via API v2 and converts ADF content to local Markdown files. Supports single-page pull (by ID or title), recursive page tree pull with directory mirroring, attachment download, and the same auth/config patterns as publish. Uses `.confl2md.yml` as its config file (mirroring `.md2confl.yml` for the reverse direction).

## Technical Context

**Language/Version**: Go 1.25 (same as existing project)
**Primary Dependencies**: None new — stdlib only for ADF-to-Markdown conversion
**Storage**: Local filesystem (Markdown files + attachment binaries)
**Testing**: `go test ./...` (unit: golden-file ADF→MD, integration: HTTP mock, docker-compose: real API)
**Target Platform**: linux/amd64, linux/arm64, darwin/amd64, darwin/arm64, windows/amd64
**Project Type**: Single binary CLI (same as existing)
**Performance Goals**: Single page pull < 5s excluding network (SC-001)
**Constraints**: Memory proportional to page content size; no unbounded allocations
**Scale/Scope**: Support page trees up to 100 levels deep, handle paginated child lists

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

| Principle | Status | Notes |
|-----------|--------|-------|
| I. Minimal CLI | PASS | `pull` is a natural complement to publish; same binary, no new runtime deps |
| II. Stdlib-First | PASS | `adftomd` package uses only stdlib; no new external dependencies |
| III. Modular Architecture | PASS | New `adftomd` package (standalone, importable); CLI wiring in `internal/cli`; API extensions in `confluence` |
| IV. Security by Default | PASS | Same credential handling as publish; same env var precedence; same HTTPS-only |
| V. Performance | PASS | Single page conversion is trivial; recursive pull parallelizes API calls |
| VI. Test Discipline | PASS | Golden-file tests for ADF→MD; HTTP mock for API; docker-compose for integration |
| VII. Extensible ADF Mapping | PASS | `adftomd` converter uses same node-handler pattern; new ADF nodes = new handler function |

**Post-design re-check**: All principles still pass. No new dependencies added. `adftomd` is importable as standalone library.

## Project Structure

### Documentation (this feature)

```text
specs/002-confluence-pull/
├── plan.md              # This file
├── research.md          # Phase 0: decisions and rationale
├── data-model.md        # Phase 1: entities and types
├── quickstart.md        # Phase 1: usage guide
├── contracts/           # Phase 1: interface contracts
│   ├── pull-cli.md      # CLI flags, exit codes, output formats
│   ├── adftomd.md       # ADF-to-Markdown package API
│   └── confluence-client.md  # New API client methods
└── tasks.md             # Phase 2: task breakdown (via /speckit.tasks)
```

### Source Code (repository root)

```text
adftomd/                          # NEW: ADF → Markdown converter
├── convert.go                    # Convert() and ConvertWithOptions()
├── convert_test.go               # Golden-file tests
└── testdata/                     # ADF JSON input + expected Markdown output
    ├── heading.adf.json / heading.md
    ├── paragraph.adf.json / paragraph.md
    ├── code-block.adf.json / code-block.md
    ├── table.adf.json / table.md
    ├── panel-note.adf.json / panel-note.md
    ├── task-list.adf.json / task-list.md
    ├── mixed-marks.adf.json / mixed-marks.md
    ├── expand.adf.json / expand.md
    ├── image.adf.json / image.md
    ├── emoji.adf.json / emoji.md
    ├── unsupported.adf.json / unsupported.md
    └── full-page.adf.json / full-page.md

confluence/                       # EXISTING: API client
├── client.go                     # + GetChildren(), GetAttachments(), DownloadAttachment()
└── client_test.go                # + tests for new methods

internal/cli/                     # EXISTING: CLI orchestration
├── cli.go                        # + subcommand routing: args[0] == "pull"
├── pull.go                       # NEW: pull subcommand logic
├── pull_test.go                  # NEW: pull CLI tests
├── pull_config.go                # NEW: .confl2md.yml loading
└── pull_config_test.go           # NEW: config tests

docker-compose.yml                # + pull-page, pull-docs, pull-dry-run services
```

**Structure Decision**: Follows existing project layout. New `adftomd` package at root (peer to `parser`, `adf`, `confluence`). Pull CLI logic in `internal/cli/pull.go` (peer to `publish.go`, `dir.go`). No new directories beyond `adftomd/`.

## Integration Testing (docker-compose)

New docker-compose services for real-API integration tests:

```yaml
# New services in docker-compose.yml
pull-page:
  build: .
  volumes:
    - .:/workspace
  env_file: .env
  command: ["pull", "--page-id", "${PULL_TEST_PAGE_ID}", "--output-dir", "/workspace/pull-output"]

pull-docs:
  build: .
  volumes:
    - .:/workspace
  env_file: .env
  command: ["pull", "--config", ".confl2md.yml"]

pull-dry-run:
  build: .
  volumes:
    - .:/workspace
  env_file: .env
  command: ["pull", "--page-id", "${PULL_TEST_PAGE_ID}", "--dry-run"]
```

Test workflow:
1. `docker compose run --rm publish-docs` — publish test pages to Confluence
2. `docker compose run --rm pull-dry-run` — verify pull previews correct structure
3. `docker compose run --rm pull-page` — pull a page and verify Markdown output
4. Compare pulled Markdown against original source for round-trip fidelity

## Complexity Tracking

> No constitution violations — table intentionally empty.

| Violation | Why Needed | Simpler Alternative Rejected Because |
|-----------|------------|-------------------------------------|
| — | — | — |
