#!/bin/bash
# Copyright 2026 md2confl contributors
# SPDX-License-Identifier: Apache-2.0
#
# Round-trip fidelity test: publish → pull → compare
#
# Usage:
#   ./scripts/test-roundtrip.sh               # run locally
#   docker compose run --rm roundtrip-test     # run via docker
#
# Requires: CONFLUENCE_URL, CONFLUENCE_EMAIL, CONFLUENCE_TOKEN, CONFLUENCE_SPACE
set -euo pipefail

: "${CONFLUENCE_URL:?CONFLUENCE_URL is required}"
: "${CONFLUENCE_EMAIL:?CONFLUENCE_EMAIL is required}"
: "${CONFLUENCE_TOKEN:?CONFLUENCE_TOKEN is required}"
: "${CONFLUENCE_SPACE:?CONFLUENCE_SPACE is required}"

BIN="${MD2CONFL_BIN:-bin/md2confl}"
TMPDIR=$(mktemp -d)
trap 'rm -rf "$TMPDIR"' EXIT

TITLE="md2confl-roundtrip-$(date +%s)"
SRC="$TMPDIR/source.md"
PULL_DIR="$TMPDIR/pulled"

echo "=== Round-Trip Fidelity Test ==="
echo ""

# Step 1: Create test fixture
cat > "$SRC" << 'FIXTURE'
# Round-Trip Test Page

Content for automated round-trip testing.

## Inline Formatting

Text with **bold**, *italic*, `code`, ~~strike~~ and [link](https://example.com).

## Lists

- Bullet one
- Bullet two
  - Nested

1. First
2. Second

- [ ] Todo
- [x] Done

## Code Block

```go
fmt.Println("hello")
```

## Table

| Name | Value |
|------|-------|
| Go | 1.25 |

> [!NOTE]
> A note.

<details><summary>Expand</summary>
Hidden.
</details>

---

End.
FIXTURE

echo "1. Publishing fixture as '$TITLE'..."
PUBLISH_OUT=$("$BIN" --input "$SRC" --publish \
  --url "$CONFLUENCE_URL" --email "$CONFLUENCE_EMAIL" --token "$CONFLUENCE_TOKEN" \
  --space "$CONFLUENCE_SPACE" --title "$TITLE" --json 2>/dev/null)
PAGE_ID=$(echo "$PUBLISH_OUT" | python3 -c "import sys,json; print(json.load(sys.stdin)['pageId'])")
echo "   Page ID: $PAGE_ID"

# Step 2: Pull the page back
echo "2. Pulling page $PAGE_ID..."
"$BIN" pull --page-id "$PAGE_ID" --output-dir "$PULL_DIR" \
  --url "$CONFLUENCE_URL" --email "$CONFLUENCE_EMAIL" --token "$CONFLUENCE_TOKEN" \
  --skip-attachments 2>/dev/null

PULLED_FILE=$(find "$PULL_DIR" -name '*.md' | head -1)
if [ -z "$PULLED_FILE" ]; then
  echo "FAIL: No pulled file found"
  exit 1
fi
echo "   Pulled: $PULLED_FILE"

# Step 3: Strip page-id marker and compare structure
echo "3. Comparing..."
PULLED_CLEAN="$TMPDIR/pulled-clean.md"
grep -v '^<!-- confluence-page-id:' "$PULLED_FILE" | sed '/^$/N;/^\n$/d' > "$PULLED_CLEAN"

# Step 4: Re-publish the pulled version and compare ADF
echo "4. Re-publishing pulled content..."
REPUB_OUT=$("$BIN" --input "$PULLED_FILE" --publish \
  --url "$CONFLUENCE_URL" --email "$CONFLUENCE_EMAIL" --token "$CONFLUENCE_TOKEN" \
  --space "$CONFLUENCE_SPACE" --title "$TITLE" --json 2>/dev/null)

# Step 5: Pull again and compare
echo "5. Pulling again for idempotence check..."
PULL_DIR2="$TMPDIR/pulled2"
"$BIN" pull --page-id "$PAGE_ID" --output-dir "$PULL_DIR2" \
  --url "$CONFLUENCE_URL" --email "$CONFLUENCE_EMAIL" --token "$CONFLUENCE_TOKEN" \
  --skip-attachments 2>/dev/null

PULLED_FILE2=$(find "$PULL_DIR2" -name '*.md' | head -1)

if diff -u "$PULLED_FILE" "$PULLED_FILE2" > /dev/null 2>&1; then
  echo ""
  echo "=== PASS: Round-trip is idempotent ==="
  echo "  publish → pull → re-publish → pull produces identical Markdown"
else
  echo ""
  echo "=== FAIL: Round-trip is NOT idempotent ==="
  diff -u "$PULLED_FILE" "$PULLED_FILE2" || true
  exit 1
fi
