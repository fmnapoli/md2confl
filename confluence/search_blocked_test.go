// Copyright 2026 md2confl contributors
// SPDX-License-Identifier: Apache-2.0

package confluence

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// TestServerFindByTitle_BlockedBy403 covers the production failure mode: the
// WAF/proxy in front of Confluence Server answers GET /rest/api/content with an
// HTML 403 page while access by page ID keeps working. The error must say so
// (instead of "transient CDN error" or "invalid token"), must not be retried,
// and must never be reported as "page not found" — that would make --force
// create a duplicate page on every run.
func TestServerFindByTitle_BlockedBy403(t *testing.T) {
	var calls atomic.Int32
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte("<!DOCTYPE html><html><body>403 Forbidden</body></html>"))
	}))
	defer ts.Close()

	client, err := NewServerClient(Config{BaseURL: ts.URL, SpaceKey: "cloud", Token: "t"})
	if err != nil {
		t.Fatal(err)
	}
	client.initialDelay = time.Millisecond

	page, err := client.FindByTitle("cloud", "Some Title")
	if err == nil {
		t.Fatal("expected an error, got a successful search")
	}
	if page != nil {
		t.Fatalf("expected no page, got %+v", page)
	}

	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("expected APIError, got %T", err)
	}
	if apiErr.Category != ErrCategoryBlocked {
		t.Errorf("expected category %q, got %q", ErrCategoryBlocked, apiErr.Category)
	}
	if apiErr.StatusCode != http.StatusForbidden {
		t.Errorf("expected status 403, got %d", apiErr.StatusCode)
	}
	for _, want := range []string{"title search", "403", "Some Title", "cloud"} {
		if !strings.Contains(apiErr.Message, want) {
			t.Errorf("message %q missing %q", apiErr.Message, want)
		}
	}
	if strings.Contains(apiErr.Message, "transient") {
		t.Errorf("message must not claim the failure is transient: %q", apiErr.Message)
	}
	for _, want := range []string{"not transient", "confluence-page-id"} {
		if !strings.Contains(apiErr.Hint, want) {
			t.Errorf("hint %q missing %q", apiErr.Hint, want)
		}
	}
	if got := calls.Load(); got != 1 {
		t.Errorf("expected a single request (403 is not retryable), got %d", got)
	}
}

// TestCloudFindByTitle_BlockedBy403 gives the Cloud client the same treatment.
func TestCloudFindByTitle_BlockedBy403(t *testing.T) {
	ts, client := newTestServer(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"message":"forbidden"}`))
	})
	defer ts.Close()

	_, err := client.FindByTitle("123456", "Some Title")
	if err == nil {
		t.Fatal("expected an error, got a successful search")
	}
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("expected APIError, got %T", err)
	}
	if apiErr.Category != ErrCategoryBlocked {
		t.Errorf("expected category %q, got %q", ErrCategoryBlocked, apiErr.Category)
	}
}

// TestIsRetryable_403 pins the retry policy: 403 is an authorization decision,
// not a transient failure, so it must not be retried even when the proxy
// answers with an HTML error page.
func TestIsRetryable_403(t *testing.T) {
	tests := []struct {
		name        string
		status      int
		contentType string
		want        bool
	}{
		{"403 html", 403, "text/html", false},
		{"403 json", 403, "application/json", false},
		{"400 html stays retryable", 400, "text/html", true},
		{"429", 429, "application/json", true},
		{"503", 503, "application/json", true},
		{"200", 200, "application/json", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp := &http.Response{StatusCode: tt.status, Header: http.Header{}}
			resp.Header.Set("Content-Type", tt.contentType)
			if got := isRetryable(resp); got != tt.want {
				t.Errorf("isRetryable(%d, %s) = %v, want %v", tt.status, tt.contentType, got, tt.want)
			}
		})
	}
}
