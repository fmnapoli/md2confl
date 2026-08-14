// Copyright 2026 md2confl contributors
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"encoding/json"
	"testing"

	"github.com/fmnapoli/md2confl/confluence"
)

// digestProperty monta a content property que a página carrega.
func digestProperty(t *testing.T, digest string) confluence.ContentProperty {
	t.Helper()
	value, err := json.Marshal(pageDigest{Digest: digest})
	if err != nil {
		t.Fatal(err)
	}
	return confluence.ContentProperty{Key: digestPropertyKey, Value: value}
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
			if got := sourceDigest(tt.title, tt.html) == base; got != tt.same {
				t.Errorf("digest equality = %v, want %v", got, tt.same)
			}
		})
	}
}

func TestStoredDigest(t *testing.T) {
	if got := storedDigest(confluence.ContentProperty{}); got != "" {
		t.Errorf("property ausente deveria dar digest vazio, deu %q", got)
	}
	if got := storedDigest(confluence.ContentProperty{Value: json.RawMessage(`"nao e objeto"`)}); got != "" {
		t.Errorf("valor ilegível deveria dar digest vazio, deu %q", got)
	}
	if got := storedDigest(digestProperty(t, "abc")); got != "abc" {
		t.Errorf("digest = %q, want %q", got, "abc")
	}
}

func TestServerPageUnchanged(t *testing.T) {
	const title = "Doc"
	html := `<p><a href="other.md">other</a></p>`
	digest := sourceDigest(title, html)
	// O corpo que o Confluence devolve: links resolvidos pela segunda fase e
	// acentos convertidos em entidades pelo próprio servidor.
	published := `<p><a href="https://confluence/x">other</a></p>`

	tests := []struct {
		name      string
		prop      confluence.ContentProperty
		published string
		want      bool
	}{
		{"digest bate, corpo reescrito", digestProperty(t, digest), published, true},
		{"digest não bate", digestProperty(t, sourceDigest(title, "<p>outro</p>")), published, false},
		{"sem property e corpo idêntico", confluence.ContentProperty{}, html, true},
		{"sem property e corpo reescrito", confluence.ContentProperty{}, published, false},
		{"página nova", confluence.ContentProperty{}, "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := serverPageUnchanged(tt.prop, digest, tt.published, html); got != tt.want {
				t.Errorf("serverPageUnchanged = %v, want %v", got, tt.want)
			}
		})
	}
}
