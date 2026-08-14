// Copyright 2026 md2confl contributors
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"strings"
	"testing"
)

func TestStampSourceMarker_RoundTrip(t *testing.T) {
	html := `<p>Hello <a href="docs/other.md">other</a></p>`
	stamped := stampSourceMarker("My Page", html)

	if !strings.HasSuffix(stamped, html) {
		t.Errorf("stamped body must keep the original HTML intact:\n%s", stamped)
	}
	if got := stripSourceMarker(stamped); got != html {
		t.Errorf("stripSourceMarker(%q) = %q, want %q", stamped, got, html)
	}
	if got := extractSourceMarker(stamped); got != sourceDigest("My Page", html) {
		t.Errorf("extractSourceMarker = %q, want the digest of the source", got)
	}
	// Re-stamping must not nest markers.
	if got := stampSourceMarker("My Page", stamped); got != stamped {
		t.Errorf("re-stamping changed the body:\n%s\n%s", stamped, got)
	}
}

func TestSourceDigest_Sensitivity(t *testing.T) {
	base := sourceDigest("Title", "<p>a</p>")

	tests := []struct {
		name  string
		title string
		html  string
		same  bool
	}{
		{"mesma fonte", "Title", "<p>a</p>", true},
		{"corpo diferente", "Title", "<p>b</p>", false},
		{"título diferente", "Other", "<p>a</p>", false},
		{"link diferente", "Title", `<p><a href="a.md">x</a></p>`, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := sourceDigest(tt.title, tt.html) == base
			if got != tt.same {
				t.Errorf("digest equality = %v, want %v", got, tt.same)
			}
		})
	}
}

func TestServerBodyUnchanged(t *testing.T) {
	title := "Doc"
	html := `<p><a href="other.md">other</a></p>`
	stamped := stampSourceMarker(title, html)
	// O que a segunda fase publica: mesmo corpo com o href já resolvido.
	resolved := strings.Replace(stamped, `href="other.md"`, `href="https://confluence/x"`, 1)

	tests := []struct {
		name      string
		published string
		want      bool
	}{
		{"corpo publicado com links resolvidos", resolved, true},
		{"corpo publicado idêntico", stamped, true},
		{"página nova", "", false},
		// Edição manual no Confluence com o mesmo digest: a comparação é fonte
		// contra fonte, então a página só é reescrita quando o Markdown muda.
		{"corpo editado à mão no Confluence", strings.Replace(stamped, "<p>", "<p>changed", 1), true},
		// Página publicada por uma versão anterior (sem marcador): comparação
		// byte a byte, como antes.
		{"sem marcador e idêntico", html, true},
		{"sem marcador e diferente", `<p><a href="https://confluence/x">other</a></p>`, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := serverBodyUnchanged(tt.published, stamped); got != tt.want {
				t.Errorf("serverBodyUnchanged = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestServerBodyUnchanged_DigestMismatchWins garante que um corpo publicado
// com marcador antigo é republicado mesmo que o texto pareça igual.
func TestServerBodyUnchanged_DigestMismatchWins(t *testing.T) {
	old := stampSourceMarker("Doc", "<p>v1</p>")
	current := stampSourceMarker("Doc", "<p>v2</p>")
	if serverBodyUnchanged(old, current) {
		t.Error("a page whose source changed must not be skipped")
	}
}
