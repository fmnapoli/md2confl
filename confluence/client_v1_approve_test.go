// Copyright 2026 md2confl contributors
// SPDX-License-Identifier: Apache-2.0

package confluence

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// comalaFalseAlarmBody is the real response body captured from a TDN publish
// run (ago/2026): the page WAS approved (state.name and publishedState.name
// are both "Approved"), but the Comala API still answered with an HTTP
// validation status and a messages[] entry complaining that the named
// approval "Review" does not exist in the space's current workflow
// configuration. 13 of ~27 pages in that run hit this exact body and were
// wrongly logged as "Could not approve page" even though every one of them
// showed Approved when checked directly in Confluence.
const comalaFalseAlarmBody = `{"expand":"actions,tasks","workflowName":"Simple approval workflow",
 "state":{"name":"Approved","initial":false,"colour":"#14892c","final":true},
 "publishedState":{"name":"Approved","initial":false,"colour":"#14892c","final":true},
 "approvals":[],
 "states":[{"name":"In Progress","approvals":[{"name":"Review","initial":false,"colour":"#deebff","final":false}]},
           {"name":"Approved","final":true}],
 "messages":[{"type":"ERROR","title":"Error",
              "html":"Erro de fluxo de trabalho, revise os registros do servidor ( approval Review does not exist)",
              "closeable":true,"code":"CUSTOM"}],
 "displayProgressTracker":true}`

// comalaGenuineFailureBody is the opposite case: the named approval really
// does not exist AND the page never left the pre-approval state — no
// transition happened, so this must still be reported as a failure.
const comalaGenuineFailureBody = `{"expand":"actions,tasks","workflowName":"Simple approval workflow",
 "state":{"name":"In Progress","initial":true,"colour":"#deebff","final":false},
 "publishedState":{"name":"In Progress","initial":true,"colour":"#deebff","final":false},
 "approvals":[],
 "states":[{"name":"In Progress","approvals":[{"name":"Review","initial":false,"colour":"#deebff","final":false}]},
           {"name":"Approved","final":true}],
 "messages":[{"type":"ERROR","title":"Error",
              "html":"Erro de fluxo de trabalho, revise os registros do servidor ( approval Review does not exist)",
              "closeable":true,"code":"CUSTOM"}],
 "displayProgressTracker":true}`

// newTestServerClient builds a ServerClient pointed at the given httptest
// server, with a discard logger so warnings emitted by ApproveWorkflow don't
// spam test output.
func newTestServerClient(t *testing.T, ts *httptest.Server) *ServerClient {
	t.Helper()
	client, err := NewServerClient(Config{
		BaseURL: ts.URL, SpaceKey: "T", Email: "e@e.com", Token: "t",
	})
	if err != nil {
		t.Fatalf("NewServerClient: %v", err)
	}
	client.SetHTTPClient(ts.Client())
	client.SetLogger(slog.New(slog.NewTextHandler(io.Discard, nil)))
	return client
}

// TestApproveWorkflow_StateApprovedOverridesErrorStatus is the regression
// test for the false alarm: an HTTP validation status (400/422) whose body
// nonetheless shows the target "Approved" state must be reported as success,
// not as "Could not approve page".
func TestApproveWorkflow_StateApprovedOverridesErrorStatus(t *testing.T) {
	for _, status := range []int{400, 422} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodPatch {
					t.Errorf("expected PATCH, got %s", r.Method)
				}
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(status)
				_, _ = io.WriteString(w, comalaFalseAlarmBody)
			}))
			defer ts.Close()

			client := newTestServerClient(t, ts)

			if err := client.ApproveWorkflow("1054146413", "Review"); err != nil {
				t.Fatalf("expected the approval to be reported as successful (state is Approved), got error: %v", err)
			}
		})
	}
}

// TestApproveWorkflow_GenuineFailureStillReported is the counterpart: a
// response whose state never reached "Approved" is a real failure and must
// keep being reported as one, with the workflow's own message surfaced.
func TestApproveWorkflow_GenuineFailureStillReported(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(400)
		_, _ = io.WriteString(w, comalaGenuineFailureBody)
	}))
	defer ts.Close()

	client := newTestServerClient(t, ts)

	err := client.ApproveWorkflow("1054146414", "Review")
	if err == nil {
		t.Fatal("expected an error: the page never reached the Approved state")
	}

	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("expected *APIError, got %T", err)
	}
	if apiErr.Category != ErrCategoryValidation {
		t.Errorf("expected validation category, got %s", apiErr.Category)
	}
	// A mensagem genérica de ADF é do path Cloud e engana neste path
	// Server/DC (Storage Format) — não pode aparecer aqui.
	if strings.Contains(apiErr.Error(), "ADF") {
		t.Errorf("error message must not mention ADF on the Server/DC path: %s", apiErr.Error())
	}
	if !strings.Contains(apiErr.Error(), "approval Review does not exist") {
		t.Errorf("expected the workflow's own message to be surfaced, got: %s", apiErr.Error())
	}
}

// TestApproveWorkflow_NotConfiguredIsSilentlyIgnored covers the pre-existing
// 404 behavior (workflow not enabled on the page), which must be untouched.
func TestApproveWorkflow_NotConfiguredIsSilentlyIgnored(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "no workflow", http.StatusNotFound)
	}))
	defer ts.Close()

	client := newTestServerClient(t, ts)

	if err := client.ApproveWorkflow("1054146413", "Review"); err != nil {
		t.Errorf("expected 404 (workflow not configured) to be silently ignored, got: %v", err)
	}
}

// TestApproveWorkflow_PlainSuccess covers the ordinary case: HTTP 200 with a
// body that already names the Approved state, no workflow messages at all.
func TestApproveWorkflow_PlainSuccess(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"state":          map[string]any{"name": "Approved", "final": true},
			"publishedState": map[string]any{"name": "Approved", "final": true},
			"messages":       []any{},
		})
	}))
	defer ts.Close()

	client := newTestServerClient(t, ts)

	if err := client.ApproveWorkflow("1054146413", "Review"); err != nil {
		t.Errorf("expected plain success, got: %v", err)
	}
}

// TestApproveWorkflow_NonWorkflowErrorStillCategorized covers a response that
// isn't a Comala workflow body at all (e.g. an auth failure from a proxy in
// front of Confluence) — it must still fall back to the shared, generic
// error categorization instead of being swallowed or mislabeled.
func TestApproveWorkflow_NonWorkflowErrorStillCategorized(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(403)
		_, _ = io.WriteString(w, "forbidden")
	}))
	defer ts.Close()

	client := newTestServerClient(t, ts)

	err := client.ApproveWorkflow("1054146413", "Review")
	if err == nil {
		t.Fatal("expected an error")
	}
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("expected *APIError, got %T", err)
	}
	if apiErr.Category != ErrCategoryAuth {
		t.Errorf("expected auth category, got %s", apiErr.Category)
	}
}
