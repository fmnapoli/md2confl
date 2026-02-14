// Copyright 2026 md2confl contributors
// SPDX-License-Identifier: Apache-2.0

package confluence

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func newTestServer(handler http.HandlerFunc) (*httptest.Server, *Client) {
	// Use TLS server so we can satisfy the HTTPS requirement
	ts := httptest.NewTLSServer(handler)
	client, _ := NewClient(Config{
		BaseURL:  ts.URL,
		SpaceKey: "TEST",
		Email:    "test@example.com",
		Token:    "test-token",
	})
	client.httpClient = ts.Client()
	client.initialDelay = time.Millisecond // fast retries for tests
	return ts, client
}

func TestNewClient_RequiresHTTPS(t *testing.T) {
	_, err := NewClient(Config{BaseURL: "http://insecure.example.com"})
	if err == nil {
		t.Fatal("expected error for non-HTTPS URL")
	}
}

func TestResolveSpaceID(t *testing.T) {
	ts, client := newTestServer(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/wiki/api/v2/spaces" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if r.URL.Query().Get("keys") != "DEVOPS" {
			t.Errorf("unexpected keys param: %s", r.URL.Query().Get("keys"))
		}
		json.NewEncoder(w).Encode(map[string]any{
			"results": []map[string]any{
				{"id": "123456", "key": "DEVOPS", "name": "DevOps Space"},
			},
		})
	})
	defer ts.Close()

	id, err := client.ResolveSpaceID("DEVOPS")
	if err != nil {
		t.Fatal(err)
	}
	if id != "123456" {
		t.Errorf("expected space ID 123456, got %s", id)
	}
}

func TestResolveSpaceID_NotFound(t *testing.T) {
	ts, client := newTestServer(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"results": []any{}})
	})
	defer ts.Close()

	_, err := client.ResolveSpaceID("MISSING")
	if err == nil {
		t.Fatal("expected error for missing space")
	}
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("expected APIError, got %T", err)
	}
	if apiErr.Category != ErrCategoryNotFound {
		t.Errorf("expected not_found, got %s", apiErr.Category)
	}
}

func TestCreatePage(t *testing.T) {
	ts, client := newTestServer(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || r.URL.Path != "/wiki/api/v2/pages" {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}

		body, _ := io.ReadAll(r.Body)
		var req map[string]any
		_ = json.Unmarshal(body, &req)

		if req["spaceId"] != "123" {
			t.Errorf("unexpected spaceId: %v", req["spaceId"])
		}
		if req["title"] != "Test Page" {
			t.Errorf("unexpected title: %v", req["title"])
		}

		json.NewEncoder(w).Encode(map[string]any{
			"id":    "999",
			"title": "Test Page",
			"version": map[string]any{
				"number": 1,
			},
			"_links": map[string]any{
				"webui": "/spaces/TEST/pages/999/Test+Page",
				"base":  "https://test.atlassian.net/wiki",
			},
		})
	})
	defer ts.Close()

	result, err := client.CreatePage("123", "Test Page", "", `{"version":1,"type":"doc","content":[]}`)
	if err != nil {
		t.Fatal(err)
	}
	if result.PageID != "999" {
		t.Errorf("expected page ID 999, got %s", result.PageID)
	}
	if result.Action != "created" {
		t.Errorf("expected action 'created', got %s", result.Action)
	}
	if result.Version != 1 {
		t.Errorf("expected version 1, got %d", result.Version)
	}
}

func TestCreatePage_WithParent(t *testing.T) {
	ts, client := newTestServer(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req map[string]any
		_ = json.Unmarshal(body, &req)

		if req["parentId"] != "888" {
			t.Errorf("expected parentId 888, got %v", req["parentId"])
		}

		json.NewEncoder(w).Encode(map[string]any{
			"id":      "999",
			"title":   "Child Page",
			"version": map[string]any{"number": 1},
			"_links":  map[string]any{"webui": "/pages/999", "base": "https://test.atlassian.net/wiki"},
		})
	})
	defer ts.Close()

	result, err := client.CreatePage("123", "Child Page", "888", `{}`)
	if err != nil {
		t.Fatal(err)
	}
	if result.PageID != "999" {
		t.Errorf("expected page ID 999, got %s", result.PageID)
	}
}

func TestGetPage(t *testing.T) {
	ts, client := newTestServer(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" || r.URL.Path != "/wiki/api/v2/pages/111" {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		if r.URL.Query().Get("body-format") != "atlas_doc_format" {
			t.Errorf("expected body-format query param")
		}
		json.NewEncoder(w).Encode(map[string]any{
			"id":      "111",
			"title":   "Existing Page",
			"version": map[string]any{"number": 3},
			"_links":  map[string]any{"webui": "/pages/111", "base": "https://test.atlassian.net/wiki"},
		})
	})
	defer ts.Close()

	page, err := client.GetPage("111")
	if err != nil {
		t.Fatal(err)
	}
	if page.ID != "111" {
		t.Errorf("expected ID 111, got %s", page.ID)
	}
	if page.Version.Number != 3 {
		t.Errorf("expected version 3, got %d", page.Version.Number)
	}
}

func TestUpdatePage(t *testing.T) {
	ts, client := newTestServer(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "PUT" || r.URL.Path != "/wiki/api/v2/pages/111" {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}

		body, _ := io.ReadAll(r.Body)
		var req map[string]any
		_ = json.Unmarshal(body, &req)

		version := req["version"].(map[string]any)
		if version["number"] != float64(4) {
			t.Errorf("expected version 4, got %v", version["number"])
		}

		json.NewEncoder(w).Encode(map[string]any{
			"id":      "111",
			"title":   "Updated Page",
			"version": map[string]any{"number": 4},
			"_links":  map[string]any{"webui": "/pages/111", "base": "https://test.atlassian.net/wiki"},
		})
	})
	defer ts.Close()

	result, err := client.UpdatePage("111", "Updated Page", `{}`, 3)
	if err != nil {
		t.Fatal(err)
	}
	if result.Action != "updated" {
		t.Errorf("expected action 'updated', got %s", result.Action)
	}
	if result.Version != 4 {
		t.Errorf("expected version 4, got %d", result.Version)
	}
}

func TestFindByTitle(t *testing.T) {
	ts, client := newTestServer(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("title") != "My Page" {
			t.Errorf("unexpected title: %s", r.URL.Query().Get("title"))
		}
		json.NewEncoder(w).Encode(map[string]any{
			"results": []map[string]any{
				{"id": "555", "title": "My Page", "version": map[string]any{"number": 2}},
			},
		})
	})
	defer ts.Close()

	page, err := client.FindByTitle("123", "My Page")
	if err != nil {
		t.Fatal(err)
	}
	if page == nil {
		t.Fatal("expected page, got nil")
	}
	if page.ID != "555" {
		t.Errorf("expected ID 555, got %s", page.ID)
	}
}

func TestFindByTitle_NotFound(t *testing.T) {
	ts, client := newTestServer(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"results": []any{}})
	})
	defer ts.Close()

	page, err := client.FindByTitle("123", "Nonexistent")
	if err != nil {
		t.Fatal(err)
	}
	if page != nil {
		t.Errorf("expected nil for not found, got %+v", page)
	}
}

func TestAuthError(t *testing.T) {
	ts, client := newTestServer(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(401)
		_, _ = w.Write([]byte("Unauthorized"))
	})
	defer ts.Close()

	_, err := client.ResolveSpaceID("TEST")
	if err == nil {
		t.Fatal("expected error")
	}
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("expected APIError, got %T", err)
	}
	if apiErr.Category != ErrCategoryAuth {
		t.Errorf("expected auth error, got %s", apiErr.Category)
	}
}

func TestAuthHeader(t *testing.T) {
	ts, client := newTestServer(func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		if !strings.HasPrefix(auth, "Basic ") {
			t.Errorf("expected Basic auth, got %s", auth)
		}
		json.NewEncoder(w).Encode(map[string]any{"results": []any{}})
	})
	defer ts.Close()

	_, _ = client.ResolveSpaceID("X")
}

func TestUploadAttachment(t *testing.T) {
	dir := t.TempDir()
	testFile := filepath.Join(dir, "test.png")
	if err := os.WriteFile(testFile, []byte("fake-png-data"), 0644); err != nil {
		t.Fatal(err)
	}

	ts, client := newTestServer(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if !strings.Contains(r.URL.Path, "/child/attachment") {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if r.Header.Get("X-Atlassian-Token") != "no-check" {
			t.Error("expected X-Atlassian-Token: no-check")
		}
		if !strings.Contains(r.Header.Get("Content-Type"), "multipart/form-data") {
			t.Errorf("expected multipart content type, got %s", r.Header.Get("Content-Type"))
		}
		json.NewEncoder(w).Encode(map[string]any{
			"results": []map[string]any{
				{
					"id":    "att123",
					"title": "test.png",
					"extensions": map[string]any{
						"fileId": "a1b2c3d4-e5f6-7890-abcd-ef1234567890",
					},
				},
			},
		})
	})
	defer ts.Close()

	id, err := client.UploadAttachment("999", testFile)
	if err != nil {
		t.Fatal(err)
	}
	if id != "a1b2c3d4-e5f6-7890-abcd-ef1234567890" {
		t.Errorf("expected file ID UUID, got %s", id)
	}
}

func TestUploadAttachment_FallbackToAttID(t *testing.T) {
	dir := t.TempDir()
	testFile := filepath.Join(dir, "test.png")
	if err := os.WriteFile(testFile, []byte("fake-png-data"), 0644); err != nil {
		t.Fatal(err)
	}

	ts, client := newTestServer(func(w http.ResponseWriter, _ *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"results": []map[string]any{
				{"id": "att123", "title": "test.png"},
			},
		})
	})
	defer ts.Close()

	id, err := client.UploadAttachment("999", testFile)
	if err != nil {
		t.Fatal(err)
	}
	if id != "att123" {
		t.Errorf("expected fallback to att123, got %s", id)
	}
}
