<!--
  Sync Impact Report
  ===================
  Version change: N/A → 1.0.0 (initial ratification)
  Modified principles: N/A (initial creation)
  Added sections:
    - Core Principles (7 principles)
    - DevOps & GitOps Alignment
    - Licensing & Open Source
    - Governance
  Removed sections: N/A
  Templates requiring updates:
    - .specify/templates/plan-template.md ✅ compatible (Constitution Check section exists)
    - .specify/templates/spec-template.md ✅ compatible (no constitution-specific constraints)
    - .specify/templates/tasks-template.md ✅ compatible (test phase aligns with Principle VI)
  Follow-up TODOs: None
-->

# md2confl Constitution

## Core Principles

### I. Minimal CLI

md2confl MUST be a single-purpose command-line tool that converts Markdown to
Confluence ADF and optionally publishes. It MUST ship as a single static binary
with zero runtime dependencies. The CLI MUST follow Unix conventions: args/stdin
for input, stdout for output, stderr for errors. Flags MUST be self-documenting
via `--help`. No interactive prompts, no config files required for basic usage.

**Rationale**: DevOps and CI/CD workflows demand predictable, scriptable tools
that integrate into pipelines without setup friction.

### II. Stdlib-First Dependencies

The project MUST minimize external dependencies. Allowed dependencies:
- `goldmark` (+ GFM extensions) for Markdown parsing
- Go `net/http` for Confluence API calls
- Go `encoding/json` for ADF serialization
- Go standard library for everything else (flags, I/O, testing, errors)

New dependencies MUST be justified in a PR description with: (a) what stdlib
alternative was considered, (b) why it was insufficient. Transitive dependency
count MUST remain under 10 total.

**Rationale**: Fewer dependencies mean smaller binaries, fewer CVEs, faster
builds, and simpler audits — critical for a tool handling API credentials.

### III. Modular Architecture

The codebase MUST separate concerns into distinct packages:
- **parser**: Markdown-to-ADF conversion (zero knowledge of Confluence API)
- **confluence**: API client (zero knowledge of Markdown)
- **cmd**: CLI wiring only (flags, I/O orchestration)

The parser package MUST be importable as a standalone Go library
(`go get ... /parser`). No circular dependencies between packages. Each package
MUST have a clear, single responsibility.

**Rationale**: Separation enables independent testing, reuse of the parser in
other contexts (e.g., GitHub Actions, libraries), and clean extension points.

### IV. Security by Default

API tokens and credentials MUST never appear in:
- Log output (any verbosity level)
- Error messages
- Debug/dry-run output
- Process listing (`/proc/*/cmdline`)

Credentials MUST be accepted via (in order of preference): environment variables,
flag values. The tool SHOULD also support file reference (`--token-file`) for
environments where env vars are restricted. The tool MUST warn if a token is
passed via CLI flag (visible in process listings) and suggest env var instead.
All HTTP connections to Confluence MUST use HTTPS exclusively.

**Rationale**: DevOps tools handle sensitive credentials routinely. Leaking tokens
in CI logs or shell history is a common and preventable security incident.

### V. Performance

Conversion of a 10,000-line Markdown file with 5 Mermaid diagrams MUST complete
in under 1 second on commodity hardware (4-core, 8GB RAM). Memory usage MUST
remain proportional to input size — no unbounded allocations. The tool MUST
stream output where possible rather than buffering entire documents in memory.

**Rationale**: Fast feedback loops are essential for the edit-convert-review
cycle. Slow tools get replaced or bypassed.

### VI. Test Discipline

The project MUST maintain a minimum of 70% code coverage measured by
`go test -coverprofile`. Test organization:
- **Unit tests**: Parser package (Markdown element → ADF node mapping). Each
  supported GFM element MUST have at least one golden-file test.
- **Integration tests**: Confluence API client using HTTP mock server (no real
  API calls in CI).
- **CLI tests**: End-to-end flag parsing and output validation.

Tests MUST run without network access or external services. Test data (golden
files) MUST live alongside test code, not in a separate repository.

**Rationale**: 70% coverage balances confidence with pragmatism for a CLI tool.
Golden-file tests catch ADF format regressions that unit assertions would miss.

### VII. Extensible ADF Mapping

The Markdown-to-ADF mapping layer MUST be designed for extension without
modifying core conversion logic. Mermaid diagrams MUST be preserved as
`codeBlock` with `language: "mermaid"` by default (portable, works without
third-party apps). The architecture MUST support future `bodiedExtension`
output via optional flags (`--mermaid-extension-type`, `--mermaid-extension-key`)
for workspaces with a Mermaid app installed. Mermaid MUST NOT be rendered as
images (SVG/PNG). Adding support for new Markdown extensions or Confluence
macros MUST require only: (a) a new AST node handler, (b) a corresponding
ADF node builder — no changes to the walker/visitor infrastructure.

**Rationale**: The Confluence ADF format evolves, and users will request support
for additional macros (draw.io, Jira issues, etc.). An extensible mapping avoids
rewrites.

## DevOps & GitOps Alignment

md2confl MUST fit naturally into GitOps documentation workflows:

- **MD-first**: Markdown files in Git are the source of truth. Confluence is a
  read-only publication target. The tool MUST never modify source Markdown files
  (except optionally writing back the `confluence-page-id` marker after first
  publish, if explicitly requested via flag).
- **CI/CD friendly**: Exit codes MUST be meaningful (0 = success, 1 = user error,
  2 = API error). Output MUST support both human-readable and JSON formats
  (`--json` flag) for pipeline parsing.
- **Idempotent operations**: Running the same publish command twice with unchanged
  input MUST produce the same result without errors or duplicate pages.
- **Cross-platform**: Binaries MUST be provided for linux/amd64, linux/arm64,
  darwin/amd64, darwin/arm64, and windows/amd64. Build MUST use `GOOS`/`GOARCH`
  cross-compilation with no CGO dependencies.

## Licensing & Open Source

- The project MUST be licensed under Apache License 2.0.
- All source files MUST include the Apache 2.0 header.
- Third-party dependencies MUST have licenses compatible with Apache 2.0
  (MIT, BSD-2, BSD-3, ISC, Apache 2.0). GPL-licensed dependencies are NOT
  permitted.
- A `NOTICE` file MUST be maintained listing all third-party dependencies and
  their licenses.

## Governance

This constitution defines the non-negotiable principles for md2confl development.
All pull requests and code reviews MUST verify compliance with these principles.

**Amendment procedure**:
1. Propose amendment via PR with rationale in description.
2. Amendment MUST document impact on existing code and templates.
3. Version bump follows semantic versioning (MAJOR for principle
   removal/redefinition, MINOR for new principles, PATCH for clarifications).

**Compliance review**:
- Every PR MUST pass the Constitution Check gate in the implementation plan.
- Complexity additions MUST be justified in the Complexity Tracking table.
- Violations discovered post-merge MUST be addressed in the next sprint/cycle.

**Version**: 1.0.0 | **Ratified**: 2026-02-12 | **Last Amended**: 2026-02-12
