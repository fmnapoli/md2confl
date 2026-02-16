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

func TestRetry_HTMLResponse(t *testing.T) {
	attempts := 0
	ts, client := newTestServer(func(w http.ResponseWriter, _ *http.Request) {
		attempts++
		if attempts < 3 {
			w.Header().Set("Content-Type", "text/html")
			w.WriteHeader(400)
			_, _ = w.Write([]byte(`<!DOCTYPE HTML><HTML><BODY><H1>400 ERROR</H1>CloudFront</BODY></HTML>`))
			return
		}
		json.NewEncoder(w).Encode(map[string]any{
			"results": []map[string]any{
				{"id": "123456", "key": "TEST"},
			},
		})
	})
	defer ts.Close()

	id, err := client.ResolveSpaceID("TEST")
	if err != nil {
		t.Fatalf("expected retry to succeed, got: %v", err)
	}
	if id != "123456" {
		t.Errorf("expected 123456, got %s", id)
	}
	if attempts != 3 {
		t.Errorf("expected 3 attempts, got %d", attempts)
	}
}

func TestRetry_HTMLResponse_PUT(t *testing.T) {
	attempts := 0
	ts, client := newTestServer(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		body, _ := io.ReadAll(r.Body)

		if attempts < 3 {
			// Verify the retry sends the body again (not empty)
			if len(body) == 0 {
				t.Errorf("attempt %d: request body was empty on retry", attempts)
			}
			w.Header().Set("Content-Type", "text/html")
			w.WriteHeader(400)
			_, _ = w.Write([]byte(`<!DOCTYPE HTML><HTML><BODY>CloudFront error</BODY></HTML>`))
			return
		}

		// Verify the final attempt also has the body
		if len(body) == 0 {
			t.Errorf("attempt %d: request body was empty", attempts)
		}
		json.NewEncoder(w).Encode(map[string]any{
			"id":      "111",
			"title":   "Test",
			"version": map[string]any{"number": 5},
			"_links":  map[string]any{"webui": "/pages/111", "base": "https://test.atlassian.net/wiki"},
		})
	})
	defer ts.Close()

	result, err := client.UpdatePage("111", "Test", `{"version":1,"type":"doc","content":[]}`, 4)
	if err != nil {
		t.Fatalf("expected retry to succeed, got: %v", err)
	}
	if result.PageID != "111" {
		t.Errorf("expected 111, got %s", result.PageID)
	}
	if attempts != 3 {
		t.Errorf("expected 3 attempts, got %d", attempts)
	}
}

func TestHandleErrorResponse_HTMLBody(t *testing.T) {
	ts, client := newTestServer(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(400)
		_, _ = w.Write([]byte(`<!DOCTYPE HTML><HTML><BODY><H1>400 ERROR</H1></BODY></HTML>`))
	})
	defer ts.Close()
	client.maxRetries = 1 // no retry, just test error categorization

	_, err := client.ResolveSpaceID("TEST")
	if err == nil {
		t.Fatal("expected error")
	}
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("expected APIError, got %T", err)
	}
	if apiErr.Category != ErrCategoryNetwork {
		t.Errorf("expected network error, got %s", apiErr.Category)
	}
	if !strings.Contains(apiErr.Message, "CDN/proxy error") {
		t.Errorf("expected CDN/proxy error message, got %q", apiErr.Message)
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

func TestRetry_429WithRetryAfter(t *testing.T) {
	attempts := 0
	ts, client := newTestServer(func(w http.ResponseWriter, _ *http.Request) {
		attempts++
		if attempts < 2 {
			w.Header().Set("Retry-After", "1")
			w.WriteHeader(429)
			_, _ = w.Write([]byte(`{"message":"Rate limited"}`))
			return
		}
		json.NewEncoder(w).Encode(map[string]any{
			"results": []map[string]any{
				{"id": "123456", "key": "TEST"},
			},
		})
	})
	defer ts.Close()

	start := time.Now()
	id, err := client.ResolveSpaceID("TEST")
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("expected retry to succeed, got: %v", err)
	}
	if id != "123456" {
		t.Errorf("expected 123456, got %s", id)
	}
	if attempts != 2 {
		t.Errorf("expected 2 attempts, got %d", attempts)
	}
	// Should have waited at least the Retry-After duration (1 ms in test, since initialDelay is 1ms)
	// The Retry-After header says 1 second, so it should wait ~1s
	if elapsed < 900*time.Millisecond {
		t.Errorf("expected wait of ~1s from Retry-After, but elapsed only %s", elapsed)
	}
}

func TestRetry_429Exhausted(t *testing.T) {
	attempts := 0
	ts, client := newTestServer(func(w http.ResponseWriter, _ *http.Request) {
		attempts++
		w.WriteHeader(429)
		_, _ = w.Write([]byte(`{"message":"Rate limited"}`))
	})
	defer ts.Close()

	_, err := client.ResolveSpaceID("TEST")
	if err == nil {
		t.Fatal("expected error after exhausting retries")
	}
	if attempts != 3 {
		t.Errorf("expected 3 attempts (maxRetries), got %d", attempts)
	}
}

func TestUploadAttachment_RetryableBody(t *testing.T) {
	dir := t.TempDir()
	testFile := filepath.Join(dir, "test.png")
	if err := os.WriteFile(testFile, []byte("fake-png-data"), 0644); err != nil {
		t.Fatal(err)
	}

	attempts := 0
	ts, client := newTestServer(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		body, _ := io.ReadAll(r.Body)

		if attempts < 2 {
			// Verify body is not empty on first attempt
			if len(body) == 0 {
				t.Errorf("attempt %d: request body was empty", attempts)
			}
			w.WriteHeader(500)
			_, _ = w.Write([]byte("server error"))
			return
		}

		// Second attempt should also have the full body (not empty from consumed buffer)
		if len(body) == 0 {
			t.Errorf("attempt %d: request body was empty on retry", attempts)
		}

		json.NewEncoder(w).Encode(map[string]any{
			"results": []map[string]any{
				{"id": "att-1", "title": "test.png", "extensions": map[string]any{"fileId": "file-1"}},
			},
		})
	})
	defer ts.Close()

	id, err := client.UploadAttachment("999", testFile)
	if err != nil {
		t.Fatalf("expected retry to succeed, got: %v", err)
	}
	if id != "file-1" {
		t.Errorf("expected file-1, got %s", id)
	}
	if attempts != 2 {
		t.Errorf("expected 2 attempts, got %d", attempts)
	}
}
