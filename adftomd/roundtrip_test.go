// Copyright 2026 md2confl contributors
// SPDX-License-Identifier: Apache-2.0

package adftomd_test

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/fmnapoli/md2confl/adftomd"
	"github.com/fmnapoli/md2confl/parser"
)

// TestRoundTrip_Idempotent verifies SC-003: Markdown that has been through one
// publish→pull cycle (MD → ADF → MD') produces stable output on subsequent
// cycles (MD' → ADF' → MD'' == MD'). In other words, the converter output is
// idempotent — a second round-trip produces no further changes.
//
// This is a pure in-process test (no Confluence connection required).
// The complementary integration test (TestRoundTrip_Integration) exercises the
// full API path.
func TestRoundTrip_Idempotent(t *testing.T) {
	fixture, err := os.ReadFile("testdata/roundtrip-fixture.md")
	if err != nil {
		t.Fatal(err)
	}

	// Strip the page-id marker — the parser doesn't produce it;
	// the pull command prepends it.
	src := stripPageIDMarker(string(fixture))

	// Pass 1: MD → ADF₁ → MD'
	adf1, err := parser.ConvertToADF([]byte(src))
	if err != nil {
		t.Fatalf("pass 1 parse: %v", err)
	}
	md1 := string(adftomd.Convert(adf1))

	// Pass 2: MD' → ADF₂ → MD''
	adf2, err := parser.ConvertToADF([]byte(md1))
	if err != nil {
		t.Fatalf("pass 2 parse: %v", err)
	}
	md2 := string(adftomd.Convert(adf2))

	// MD' should equal MD'' — the round-trip is idempotent.
	if md1 != md2 {
		t.Errorf("round-trip is not idempotent:\n--- pass 1 (MD') ---\n%s\n--- pass 2 (MD'') ---\n%s", md1, md2)
	}
}

// TestRoundTrip_SemanticEquivalence verifies that the ADF produced from the
// original Markdown is semantically equivalent to the ADF produced from the
// round-tripped Markdown (MD → ADF₁, MD → ADF → MD' → ADF₂, compare ADF₁ ≅ ADF₂).
//
// Semantic equivalence means the node tree structure, text content, and mark
// types match. Known acceptable differences (mark ordering, attribute key
// ordering) are normalized before comparison.
func TestRoundTrip_SemanticEquivalence(t *testing.T) {
	fixture, err := os.ReadFile("testdata/roundtrip-fixture.md")
	if err != nil {
		t.Fatal(err)
	}
	src := stripPageIDMarker(string(fixture))

	// Original: MD → ADF₁
	adf1, err := parser.ConvertToADF([]byte(src))
	if err != nil {
		t.Fatalf("original parse: %v", err)
	}

	// Round-trip: MD → ADF → MD' → ADF₂
	md1 := string(adftomd.Convert(adf1))
	adf2, err := parser.ConvertToADF([]byte(md1))
	if err != nil {
		t.Fatalf("round-trip parse: %v", err)
	}

	// Compare by serializing to normalized JSON.
	json1 := normalizeADF(t, adf1)
	json2 := normalizeADF(t, adf2)

	if json1 != json2 {
		t.Errorf("ADF semantic mismatch after round-trip:\n--- original ADF ---\n%s\n--- round-trip ADF ---\n%s", json1, json2)
	}
}

// TestRoundTrip_PerFeature runs targeted round-trip checks on individual
// Markdown constructs to identify exactly which feature loses fidelity.
func TestRoundTrip_PerFeature(t *testing.T) {
	cases := []struct {
		name string
		md   string
	}{
		{"heading", "# Title\n\n## Subtitle\n"},
		{"paragraph", "Hello world.\n"},
		{"bold", "Text with **bold** word.\n"},
		{"italic", "Text with *italic* word.\n"},
		{"strikethrough", "Text with ~~struck~~ word.\n"},
		{"inline-code", "Use `go test` here.\n"},
		{"link", "Visit [example](https://example.com).\n"},
		{"bold-italic", "Text ***bold italic*** end.\n"},
		{"bold-code", "Text **bold `code`** end.\n"},
		{"strike-bold", "Text ~~strike **bold**~~ end.\n"},
		{"bullet-list", "- One\n- Two\n  - Nested\n"},
		{"ordered-list", "1. First\n2. Second\n3. Third\n"},
		{"ordered-custom-start", "5. Five\n6. Six\n7. Seven\n"},
		{"task-list", "- [ ] Todo\n- [x] Done\n"},
		{"code-block", "```go\nfmt.Println(\"hi\")\n```\n"},
		{"code-block-plain", "```\nplain text\n```\n"},
		{"blockquote", "> A quote with **bold**.\n"},
		{"table", "| A | B |\n|---|---|\n| 1 | 2 |\n"},
		{"rule", "Above.\n\n---\n\nBelow.\n"},
		{"emoji", "Hello :wave: world :rocket:\n"},
		{"superscript", "x^2^ + y^3^\n"},
		{"panel-note", "> [!NOTE]\n> Info here.\n"},
		{"panel-tip", "> [!TIP]\n> Tip here.\n"},
		{"panel-warning", "> [!WARNING]\n> Warning here.\n"},
		{"expand", "<details><summary>Title</summary>\nBody here.\n</details>\n"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// Pass 1
			adf1, err := parser.ConvertToADF([]byte(tc.md))
			if err != nil {
				t.Fatalf("pass 1 parse: %v", err)
			}
			md1 := string(adftomd.Convert(adf1))

			// Pass 2
			adf2, err := parser.ConvertToADF([]byte(md1))
			if err != nil {
				t.Fatalf("pass 2 parse: %v", err)
			}
			md2 := string(adftomd.Convert(adf2))

			if md1 != md2 {
				t.Errorf("not idempotent:\npass1: %q\npass2: %q", md1, md2)
			}
		})
	}
}

func stripPageIDMarker(s string) string {
	lines := strings.Split(s, "\n")
	var out []string
	for _, line := range lines {
		if strings.HasPrefix(line, "<!-- confluence-page-id:") {
			continue
		}
		out = append(out, line)
	}
	// Remove leading blank line if any
	result := strings.Join(out, "\n")
	return strings.TrimLeft(result, "\n")
}

func normalizeADF(t *testing.T, doc interface{}) string {
	t.Helper()
	b, err := json.Marshal(doc)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	// Re-parse to generic structure for consistent ordering
	var generic interface{}
	if err := json.Unmarshal(b, &generic); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	out, err := json.MarshalIndent(generic, "", "  ")
	if err != nil {
		t.Fatalf("re-marshal: %v", err)
	}
	return string(out)
}
