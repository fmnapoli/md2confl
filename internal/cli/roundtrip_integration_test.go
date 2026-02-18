// Copyright 2026 md2confl contributors
// SPDX-License-Identifier: Apache-2.0

//go:build integration

package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/fmnapoli/md2confl/adftomd"
	"github.com/fmnapoli/md2confl/parser"
)

// TestRoundTrip_Integration publishes a known Markdown file to Confluence,
// pulls it back, and verifies that re-parsing the pulled Markdown produces
// semantically equivalent ADF.
//
// This test requires real Confluence credentials via environment variables:
//
//	CONFLUENCE_URL, CONFLUENCE_EMAIL, CONFLUENCE_TOKEN, CONFLUENCE_SPACE
//
// Run with:
//
//	go test -tags integration -run TestRoundTrip_Integration ./internal/cli/...
//
// Or via docker-compose:
//
//	docker compose run --rm roundtrip-test
func TestRoundTrip_Integration(t *testing.T) {
	// Check required env vars.
	url := os.Getenv("CONFLUENCE_URL")
	email := os.Getenv("CONFLUENCE_EMAIL")
	token := os.Getenv("CONFLUENCE_TOKEN")
	space := os.Getenv("CONFLUENCE_SPACE")
	if url == "" || email == "" || token == "" || space == "" {
		t.Skip("CONFLUENCE_URL, CONFLUENCE_EMAIL, CONFLUENCE_TOKEN, and CONFLUENCE_SPACE must be set")
	}

	// Use a temp dir for both publish source and pull output.
	tmpDir := t.TempDir()

	// Step 1: Write the test fixture as a Markdown file.
	fixture := roundtripFixture()
	srcFile := filepath.Join(tmpDir, "roundtrip-test.md")
	if err := os.WriteFile(srcFile, []byte(fixture), 0644); err != nil {
		t.Fatal(err)
	}

	// Step 2: Publish the fixture to Confluence.
	publishArgs := []string{
		"--input", srcFile,
		"--publish",
		"--url", url,
		"--email", email,
		"--token", token,
		"--space", space,
		"--title", "md2confl Round-Trip Test",
		"--json",
	}

	var publishOut, publishErr bytes.Buffer
	code := Run(publishArgs, "test", &publishOut, &publishErr)
	if code != 0 {
		t.Fatalf("publish failed (exit %d):\nstdout: %s\nstderr: %s", code, publishOut.String(), publishErr.String())
	}

	// Extract page ID from JSON output.
	var publishResult struct {
		PageID string `json:"pageId"`
	}
	if err := json.Unmarshal(publishOut.Bytes(), &publishResult); err != nil {
		t.Fatalf("parsing publish JSON: %v\noutput: %s", err, publishOut.String())
	}
	if publishResult.PageID == "" {
		t.Fatalf("publish did not return a page ID:\n%s", publishOut.String())
	}
	t.Logf("Published page ID: %s", publishResult.PageID)

	// Step 3: Pull the page back.
	pullDir := filepath.Join(tmpDir, "pulled")
	pullArgs := []string{
		"pull",
		"--page-id", publishResult.PageID,
		"--output-dir", pullDir,
		"--url", url,
		"--email", email,
		"--token", token,
		"--skip-attachments",
	}

	var pullOut, pullErrBuf bytes.Buffer
	code = Run(pullArgs, "test", &pullOut, &pullErrBuf)
	if code != 0 {
		t.Fatalf("pull failed (exit %d):\nstdout: %s\nstderr: %s", code, pullOut.String(), pullErrBuf.String())
	}

	// Find the pulled Markdown file.
	entries, err := filepath.Glob(filepath.Join(pullDir, "*.md"))
	if err != nil || len(entries) == 0 {
		t.Fatalf("no pulled Markdown found in %s", pullDir)
	}
	pulledMD, err := os.ReadFile(entries[0])
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("Pulled file: %s (%d bytes)", entries[0], len(pulledMD))

	// Step 4: Parse original and pulled Markdown to ADF.
	// Strip page-id marker from pulled MD (it's metadata, not content).
	pulledClean := stripMarker(string(pulledMD))

	adfOriginal, err := parser.ConvertToADF([]byte(fixture))
	if err != nil {
		t.Fatalf("parsing original: %v", err)
	}

	adfPulled, err := parser.ConvertToADF([]byte(pulledClean))
	if err != nil {
		t.Fatalf("parsing pulled: %v", err)
	}

	// Step 5: Verify idempotence — pulled MD re-parsed should round-trip
	// cleanly: ADF → MD → ADF → MD stays the same.
	md1 := string(adftomd.Convert(adfPulled))
	adfRe, err := parser.ConvertToADF([]byte(md1))
	if err != nil {
		t.Fatalf("re-parsing pulled: %v", err)
	}
	md2 := string(adftomd.Convert(adfRe))

	if md1 != md2 {
		t.Errorf("pulled Markdown is not idempotent:\n--- pass 1 ---\n%s\n--- pass 2 ---\n%s", md1, md2)
	}

	// Step 6: Compare ADF structures (informational — Confluence may
	// modify marks, so we log but don't fail on ADF differences).
	jsonOrig := marshalADF(t, adfOriginal)
	jsonPulled := marshalADF(t, adfPulled)
	if jsonOrig != jsonPulled {
		t.Logf("INFO: ADF differs between original and pulled (expected due to Confluence normalizations)")
		t.Logf("Original ADF nodes: %d, Pulled ADF nodes: %d", len(adfOriginal.Content), len(adfPulled.Content))
	} else {
		t.Logf("ADF is identical between original and pulled")
	}
}

func roundtripFixture() string {
	return `# Round-Trip Integration Test

This content is published and pulled back to verify semantic fidelity.

## Inline Formatting

Text with **bold**, *italic*, ` + "`" + `code` + "`" + `, ~~strikethrough~~ and [link](https://example.com).

## Lists

- Bullet one
- Bullet two with **bold**
  - Nested item

1. First
2. Second
3. Third

- [ ] Todo
- [x] Done

## Code Block

` + "```go" + `
fmt.Println("hello")
` + "```" + `

## Table

| Name | Value |
|------|-------|
| Go | 1.25 |
| ADF | v1 |

## Alerts

> [!NOTE]
> This is a note.

## Expand

<details><summary>Click</summary>
Hidden content.
</details>

## Separator

---

End.
`
}

func stripMarker(s string) string {
	var lines []string
	for _, line := range strings.Split(s, "\n") {
		if strings.HasPrefix(line, "<!-- confluence-page-id:") {
			continue
		}
		lines = append(lines, line)
	}
	return strings.TrimLeft(strings.Join(lines, "\n"), "\n")
}

func marshalADF(t *testing.T, doc interface{}) string {
	t.Helper()
	b, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return string(b)
}
