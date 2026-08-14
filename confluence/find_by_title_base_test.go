// Copyright 2026 md2confl contributors
// SPDX-License-Identifier: Apache-2.0

package confluence

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestServerFindByTitle_BaseFromEnvelope cobre um detalhe da API v1: na busca,
// o "base" da URL vem no _links do envelope, não no de cada resultado. Sem
// copiá-lo, a URL da página sairia relativa — e uma página encontrada por
// título (caminho do --force) viraria destino de link quebrado, além de mudar
// de valor entre execuções e forçar a republicação de quem aponta para ela.
func TestServerFindByTitle_BaseFromEnvelope(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{
			"results": [{
				"id": "123",
				"title": "Some Title",
				"body": {"storage": {"value": "<p>x</p>"}},
				"version": {"number": 3},
				"_links": {"webui": "/display/CLOUD/Some+Title"}
			}],
			"_links": {"base": "https://confluence.example.com"}
		}`))
	}))
	defer ts.Close()

	client, err := NewServerClient(Config{BaseURL: ts.URL, SpaceKey: "cloud", Token: "t"})
	if err != nil {
		t.Fatal(err)
	}

	page, err := client.FindByTitle("cloud", "Some Title")
	if err != nil {
		t.Fatal(err)
	}
	if page == nil {
		t.Fatal("expected a page")
	}
	const want = "https://confluence.example.com/display/CLOUD/Some+Title"
	if got := page.Links.Base + page.Links.WebUI; got != want {
		t.Errorf("page URL = %q, want %q", got, want)
	}
}
