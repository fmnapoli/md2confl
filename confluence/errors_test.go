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

func TestAPIError_Error(t *testing.T) {
	err := &APIError{
		Category:   ErrCategoryAuth,
		StatusCode: 401,
		Message:    "authentication failed — invalid or expired API token",
		Hint:       "verify your --token or CONFLUENCE_TOKEN environment variable",
	}

	if err.Error() != "authentication failed — invalid or expired API token" {
		t.Errorf("unexpected error message: %s", err.Error())
	}
}

func TestAPIError_ExitCode(t *testing.T) {
	err := &APIError{Category: ErrCategoryNetwork}
	if err.ExitCode() != 2 {
		t.Errorf("expected exit code 2, got %d", err.ExitCode())
	}
}

func TestAuthErrorFactory(t *testing.T) {
	for _, status := range []int{401, 403} {
		err := authError(status)
		if err.Category != ErrCategoryAuth {
			t.Errorf("expected auth category, got %s", err.Category)
		}
		if err.StatusCode != status {
			t.Errorf("expected status %d, got %d", status, err.StatusCode)
		}
		if err.Hint == "" {
			t.Error("expected non-empty hint")
		}
	}
}

func TestNotFoundError(t *testing.T) {
	err := notFoundError("space", "MISSING")
	if err.Category != ErrCategoryNotFound {
		t.Errorf("expected not_found category, got %s", err.Category)
	}
	if err.StatusCode != 404 {
		t.Errorf("expected status 404, got %d", err.StatusCode)
	}
	if err.Error() != "space not found: MISSING" {
		t.Errorf("unexpected message: %s", err.Error())
	}
}

func TestConflictError(t *testing.T) {
	err := conflictError()
	if err.Category != ErrCategoryConflict {
		t.Errorf("expected conflict category, got %s", err.Category)
	}
	if err.StatusCode != 409 {
		t.Errorf("expected status 409, got %d", err.StatusCode)
	}
}

func TestValidationError(t *testing.T) {
	err := validationError("bad content")
	if err.Category != ErrCategoryValidation {
		t.Errorf("expected validation category, got %s", err.Category)
	}
	if err.StatusCode != 422 {
		t.Errorf("expected status 422, got %d", err.StatusCode)
	}
	if err.Error() != "invalid ADF content: bad content" {
		t.Errorf("unexpected message: %s", err.Error())
	}
	if err.Hint == "" {
		t.Error("expected non-empty hint")
	}
}

func TestHandleErrorResponse(t *testing.T) {
	tests := []struct {
		name         string
		statusCode   int
		body         string
		wantCategory string
	}{
		{"conflict 409", 409, "conflict", ErrCategoryConflict},
		{"validation 400", 400, "bad request", ErrCategoryValidation},
		{"validation 422", 422, "unprocessable", ErrCategoryValidation},
		{"not found 404", 404, "not found", ErrCategoryNotFound},
		{"unknown 500", 500, "internal server error", ErrCategoryNetwork},
		{"unknown 503", 503, "service unavailable", ErrCategoryNetwork},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ts := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tt.statusCode)
				_, _ = io.WriteString(w, tt.body)
			}))
			defer ts.Close()

			client, _ := NewClient(Config{
				BaseURL: ts.URL, SpaceKey: "T", Email: "e@e.com", Token: "t",
			})
			client.httpClient = ts.Client()
			client.initialDelay = time.Millisecond

			_, err := client.ResolveSpaceID("T")
			if err == nil {
				t.Fatal("expected error")
			}

			var apiErr *APIError
			if !errors.As(err, &apiErr) {
				t.Fatalf("expected APIError, got %T", err)
			}
			if apiErr.Category != tt.wantCategory {
				t.Errorf("expected category %q, got %q", tt.wantCategory, apiErr.Category)
			}
		})
	}
}

func TestUploadAttachment_ConflictError(t *testing.T) {
	ts := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		// For ResolveSpaceID
		if strings.Contains(w.Header().Get("Content-Type"), "json") || true {
			w.WriteHeader(409)
			_, _ = io.WriteString(w, "conflict")
		}
	}))
	defer ts.Close()

	client, _ := NewClient(Config{
		BaseURL: ts.URL, SpaceKey: "T", Email: "e@e.com", Token: "t",
	})
	client.httpClient = ts.Client()

	_, err := client.GetPage("123")
	if err == nil {
		t.Fatal("expected error")
	}
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("expected APIError, got %T", err)
	}
	if apiErr.Category != ErrCategoryConflict {
		t.Errorf("expected conflict, got %s", apiErr.Category)
	}
}

func TestUpdatePage_ValidationError(t *testing.T) {
	ts := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(422)
		_, _ = io.WriteString(w, "invalid content")
	}))
	defer ts.Close()

	client, _ := NewClient(Config{
		BaseURL: ts.URL, SpaceKey: "T", Email: "e@e.com", Token: "t",
	})
	client.httpClient = ts.Client()

	_, err := client.UpdatePage("123", "title", "{}", 1)
	if err == nil {
		t.Fatal("expected error")
	}
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("expected APIError, got %T", err)
	}
	if apiErr.Category != ErrCategoryValidation {
		t.Errorf("expected validation, got %s", apiErr.Category)
	}
}

func TestCreatePage_AuthError(t *testing.T) {
	ts := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(403)
		_, _ = io.WriteString(w, "forbidden")
	}))
	defer ts.Close()

	client, _ := NewClient(Config{
		BaseURL: ts.URL, SpaceKey: "T", Email: "e@e.com", Token: "t",
	})
	client.httpClient = ts.Client()

	_, err := client.CreatePage("123", "title", "", "{}")
	if err == nil {
		t.Fatal("expected error")
	}
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("expected APIError, got %T", err)
	}
	if apiErr.Category != ErrCategoryAuth {
		t.Errorf("expected auth, got %s", apiErr.Category)
	}
}

func TestFindByTitle_Error(t *testing.T) {
	ts := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(500)
		_, _ = io.WriteString(w, "server error")
	}))
	defer ts.Close()

	client, _ := NewClient(Config{
		BaseURL: ts.URL, SpaceKey: "T", Email: "e@e.com", Token: "t",
	})
	client.httpClient = ts.Client()

	_, err := client.FindByTitle("123", "title")
	if err == nil {
		t.Fatal("expected error")
	}
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("expected APIError, got %T", err)
	}
	if apiErr.Category != ErrCategoryNetwork {
		t.Errorf("expected network, got %s", apiErr.Category)
	}
}

func TestUploadAttachment_Error(t *testing.T) {
	ts := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(401)
		_, _ = io.WriteString(w, "unauthorized")
	}))
	defer ts.Close()

	client, _ := NewClient(Config{
		BaseURL: ts.URL, SpaceKey: "T", Email: "e@e.com", Token: "t",
	})
	client.httpClient = ts.Client()

	dir := t.TempDir()
	f := filepath.Join(dir, "test.png")
	if err := os.WriteFile(f, []byte("data"), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := client.UploadAttachment("123", f)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestUploadAttachment_EmptyResults(t *testing.T) {
	ts := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"results": []any{}})
	}))
	defer ts.Close()

	client, _ := NewClient(Config{
		BaseURL: ts.URL, SpaceKey: "T", Email: "e@e.com", Token: "t",
	})
	client.httpClient = ts.Client()

	dir := t.TempDir()
	f := filepath.Join(dir, "test.png")
	if err := os.WriteFile(f, []byte("data"), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := client.UploadAttachment("123", f)
	if err == nil {
		t.Fatal("expected error for empty results")
	}
	if !strings.Contains(err.Error(), "no attachment ID") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestUploadAttachment_DuplicateFilename(t *testing.T) {
	callCount := 0
	ts := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		if r.Method == "POST" {
			w.WriteHeader(400)
			_, _ = io.WriteString(w, `{"message":"Cannot add a new attachment with same file name as an existing attachment: test.png"}`)
			return
		}
		// GET request to look up existing attachment
		_ = json.NewEncoder(w).Encode(map[string]any{
			"results": []map[string]any{
				{
					"id":    "att456",
					"title": "test.png",
					"extensions": map[string]any{
						"fileId": "existing-file-uuid",
					},
				},
			},
		})
	}))
	defer ts.Close()

	client, _ := NewClient(Config{
		BaseURL: ts.URL, SpaceKey: "T", Email: "e@e.com", Token: "t",
	})
	client.httpClient = ts.Client()

	dir := t.TempDir()
	f := filepath.Join(dir, "test.png")
	if err := os.WriteFile(f, []byte("data"), 0644); err != nil {
		t.Fatal(err)
	}

	id, err := client.UploadAttachment("123", f)
	if err != nil {
		t.Fatalf("expected fallback to existing attachment, got error: %v", err)
	}
	if id != "existing-file-uuid" {
		t.Errorf("expected existing-file-uuid, got %s", id)
	}
	if callCount != 2 {
		t.Errorf("expected 2 API calls (POST + GET), got %d", callCount)
	}
}
