// Copyright 2026 md2confl contributors
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// comalaFalseAlarmBody is the real response body captured from a TDN publish
// run (ago/2026): the page WAS approved (state.name and publishedState.name
// are both "Approved"), but the Comala API still answered with an HTTP
// validation status and a messages[] entry complaining that the named
// approval "Review" does not exist in the space's current workflow
// configuration. 13 of ~27 pages in that run hit this exact body and were
// wrongly logged as "Could not approve page", even though every one of them
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

// comalaGenuineFailureBody is the opposite case: the page never left the
// pre-approval state — no transition happened, so this must still surface as
// a failure.
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

// serverConfigWithApprove mirrors serverConfig with --approve turned on, the
// setup that drives the Comala approve pass in internal/cli/config.go.
func serverConfigWithApprove(baseURL string) string {
	return strings.Replace(serverConfig(baseURL), "write-marker: false", "write-marker: false\napprove: true", 1)
}

// TestServerPublish_ComalaFalseAlarmIsReportedAsApproved is the end-to-end
// regression test for the incident: publishing with --approve against a
// Comala instance whose workflow config no longer has the "Review" approval
// step must not log "Could not approve page" for pages that the response body
// already shows as Approved.
func TestServerPublish_ComalaFalseAlarmIsReportedAsApproved(t *testing.T) {
	dir := t.TempDir()
	ts, fake := newFakeConfluenceServer(t)
	fake.approveStatus = 400
	fake.approveBody = comalaFalseAlarmBody

	cfgPath := writeConfigAndDocs(t, dir, serverConfigWithApprove(ts.URL), consumerDocs())
	if err := os.MkdirAll(filepath.Join(dir, ".git"), 0755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(dir)
	t.Setenv("CONFLUENCE_TOKEN", "fake-token")

	stderr := runPublish(t, cfgPath)

	if strings.Contains(stderr, "Could not approve page") {
		t.Errorf("a response whose state is already Approved must not be logged as a failed approval; stderr:\n%s", stderr)
	}
	// The workflow's own configuration complaint must still be visible
	// somewhere in the log — dropping it entirely would hide a real
	// space-configuration problem from whoever reads the run.
	if !strings.Contains(stderr, "approval Review does not exist") {
		t.Errorf("the workflow message must still surface as informational log; stderr:\n%s", stderr)
	}
}

// TestServerPublish_ComalaGenuineApprovalFailureIsReported is the opposite
// case: a page that truly never reached the Approved state must keep being
// reported as a failed approval.
func TestServerPublish_ComalaGenuineApprovalFailureIsReported(t *testing.T) {
	dir := t.TempDir()
	ts, fake := newFakeConfluenceServer(t)
	fake.approveStatus = 400
	fake.approveBody = comalaGenuineFailureBody

	cfgPath := writeConfigAndDocs(t, dir, serverConfigWithApprove(ts.URL), consumerDocs())
	if err := os.MkdirAll(filepath.Join(dir, ".git"), 0755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(dir)
	t.Setenv("CONFLUENCE_TOKEN", "fake-token")

	stderr := runPublish(t, cfgPath)

	if !strings.Contains(stderr, "Could not approve page") {
		t.Errorf("a response whose state never reached Approved must still be reported as a failed approval; stderr:\n%s", stderr)
	}
	if strings.Contains(stderr, "invalid ADF content") {
		t.Errorf("the Server/DC path uses Storage Format, not ADF — the error message must not claim otherwise; stderr:\n%s", stderr)
	}
}
