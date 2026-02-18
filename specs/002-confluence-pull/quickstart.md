# Quickstart: Confluence-to-Markdown Pull

**Branch**: `002-confluence-pull` | **Date**: 2026-02-18

## CLI Usage

### Pull a single page by ID

```bash
md2confl pull --page-id 12345 \
  --url https://site.atlassian.net \
  --email user@example.com

# Output: ./my-page-title.md
```

### Pull a single page by title

```bash
md2confl pull --title "Architecture Overview" \
  --space DEVOPS \
  --url https://site.atlassian.net \
  --email user@example.com

# Output: ./architecture-overview.md
```

### Pull a page tree recursively

```bash
md2confl pull --page-id 12345 --recursive \
  --output-dir ./docs \
  --url https://site.atlassian.net \
  --email user@example.com

# Output:
#   docs/README.md           (parent page)
#   docs/child-page-a.md
#   docs/child-page-b/
#   docs/child-page-b/README.md
#   docs/child-page-b/grandchild.md
```

### Preview without writing files

```bash
md2confl pull --page-id 12345 --dry-run
```

### JSON output for CI/CD

```bash
md2confl pull --page-id 12345 --json
```

### Skip attachment downloads

```bash
md2confl pull --page-id 12345 --skip-attachments
```

### Limit recursion depth

```bash
md2confl pull --page-id 12345 --recursive --depth 3
```

## Config File (`.confl2md.yml`)

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
    output-dir: ./arch-docs
```

```bash
# Pull all pages defined in config
md2confl pull --config .confl2md.yml

# Pull a single page from config
md2confl pull --config .confl2md.yml --page-id 12345
```

Config auto-discovery: if `.confl2md.yml` exists in the current directory, it is used automatically (same pattern as `.md2confl.yml` for publish).

## Environment Variables

Same as publish — no new env vars needed:

```bash
export CONFLUENCE_URL=https://site.atlassian.net
export CONFLUENCE_EMAIL=user@example.com
export CONFLUENCE_TOKEN=your-api-token

md2confl pull --page-id 12345 --space DEVOPS
```

## Docker

```bash
# Pull a single page
docker compose run --rm pull-page

# Pull all pages from .confl2md.yml
docker compose run --rm pull-docs

# Dry-run preview
docker compose run --rm pull-dry-run
```

## Output Format

Each generated Markdown file has this structure:

```markdown
<!-- confluence-page-id: 12345 -->
# Page Title

Content converted from ADF...
```

- The page-id marker enables round-trip publish workflows
- The H1 title ensures `md2confl --publish` can extract the title back
- Image attachments are saved to `attachments/` and referenced with relative paths
