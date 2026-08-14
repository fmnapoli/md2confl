// Copyright 2026 md2confl contributors
// SPDX-License-Identifier: Apache-2.0

package confluence

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestServerContentProperty_CRUD cobre o contrato da API v1 usado para guardar
// o digest da fonte fora do corpo: 404 é "não existe" e não erro, criar é POST
// sem versão e atualizar é PUT com a versão seguinte.
func TestServerContentProperty_CRUD(t *testing.T) {
	var gotMethod, gotBody string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/property/md2conflSource") {
			t.Errorf("unexpected path %q", r.URL.Path)
		}
		gotMethod = r.Method
		if r.Method == http.MethodGet {
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"statusCode":404,"message":"No content property found"}`))
			return
		}
		body, _ := io.ReadAll(r.Body)
		gotBody = string(body)
		_, _ = w.Write([]byte(`{"key":"md2conflSource","value":{"digest":"x"},"version":{"number":1}}`))
	}))
	defer ts.Close()

	client, err := NewServerClient(Config{BaseURL: ts.URL, SpaceKey: "cloud", Token: "t"})
	if err != nil {
		t.Fatal(err)
	}

	prop, err := client.GetContentProperty("123", "md2conflSource")
	if err != nil {
		t.Fatalf("404 must not be an error, got %v", err)
	}
	if prop.Exists() {
		t.Errorf("expected a missing property, got %+v", prop)
	}

	if err := client.SetContentProperty("123", "md2conflSource", map[string]string{"digest": "x"}, 0); err != nil {
		t.Fatal(err)
	}
	if gotMethod != http.MethodPost {
		t.Errorf("creating a property must use POST, used %s", gotMethod)
	}
	if strings.Contains(gotBody, "version") {
		t.Errorf("creating a property must not send a version: %s", gotBody)
	}

	if err := client.SetContentProperty("123", "md2conflSource", map[string]string{"digest": "y"}, 4); err != nil {
		t.Fatal(err)
	}
	if gotMethod != http.MethodPut {
		t.Errorf("updating a property must use PUT, used %s", gotMethod)
	}
	if !strings.Contains(gotBody, `"number":5`) {
		t.Errorf("updating a property must send the next version: %s", gotBody)
	}
}

// TestServerGetPageWithProperty_Expand garante que a property vem no mesmo
// request da página, evitando um GET por página só para ler o digest.
func TestServerGetPageWithProperty_Expand(t *testing.T) {
	var gotExpand string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotExpand = r.URL.Query().Get("expand")
		_, _ = w.Write([]byte(`{
			"id":"123","title":"T",
			"body":{"storage":{"value":"<p>x</p>"}},
			"version":{"number":7},
			"metadata":{"properties":{"md2conflSource":{"key":"md2conflSource","value":{"digest":"abc"},"version":{"number":2}}}}
		}`))
	}))
	defer ts.Close()

	client, err := NewServerClient(Config{BaseURL: ts.URL, SpaceKey: "cloud", Token: "t"})
	if err != nil {
		t.Fatal(err)
	}

	page, err := client.GetPageWithProperty("123", "md2conflSource")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(gotExpand, "metadata.properties.md2conflSource") {
		t.Errorf("expand = %q, want it to request the content property", gotExpand)
	}
	if !page.Property.Exists() {
		t.Fatal("expected the property to come with the page")
	}
	if page.Property.Version.Number != 2 {
		t.Errorf("property version = %d, want 2", page.Property.Version.Number)
	}
	var value struct {
		Digest string `json:"digest"`
	}
	if err := json.Unmarshal(page.Property.Value, &value); err != nil {
		t.Fatal(err)
	}
	if value.Digest != "abc" {
		t.Errorf("digest = %q, want %q", value.Digest, "abc")
	}
}
