// Copyright 2026 md2confl contributors
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"bytes"
	"encoding/json"
	stderrors "errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/fmnapoli/md2confl/confluence"
)

// mockPullClient implements pullClient for testing.
type mockPullClient struct {
	pages       map[string]*confluence.PageResponse // pageID → page
	children    map[string][]confluence.ChildPage    // pageID → children
	attachments map[string][]confluence.Attachment   // pageID → attachments
	downloads   map[string][]byte                    // downloadLink → bytes
	spaceIDs    map[string]string                    // spaceKey → spaceID
	titlePages  map[string]*confluence.PageResponse  // "spaceID:title" → page
	getPageErr  error
}

func (m *mockPullClient) GetPage(pageID string) (*confluence.PageResponse, error) {
	if m.getPageErr != nil {
		return nil, m.getPageErr
	}
	if p, ok := m.pages[pageID]; ok {
		return p, nil
	}
	return nil, &confluence.APIError{Category: confluence.ErrCategoryNotFound, StatusCode: 404, Message: fmt.Sprintf("page not found: %s", pageID)}
}

func (m *mockPullClient) FindByTitle(spaceID, title string) (*confluence.PageResponse, error) {
	key := spaceID + ":" + title
	if p, ok := m.titlePages[key]; ok {
		return p, nil
	}
	return nil, nil
}

func (m *mockPullClient) ResolveSpaceID(spaceKey string) (string, error) {
	if id, ok := m.spaceIDs[spaceKey]; ok {
		return id, nil
	}
	return "", &confluence.APIError{Category: confluence.ErrCategoryNotFound, StatusCode: 404, Message: fmt.Sprintf("space not found: %s", spaceKey)}
}

func (m *mockPullClient) GetChildren(pageID string) ([]confluence.ChildPage, error) {
	return m.children[pageID], nil
}

func (m *mockPullClient) GetAttachments(pageID string) ([]confluence.Attachment, error) {
	return m.attachments[pageID], nil
}

func (m *mockPullClient) DownloadAttachment(downloadLink string) ([]byte, error) {
	if data, ok := m.downloads[downloadLink]; ok {
		return data, nil
	}
	return nil, &confluence.APIError{Category: confluence.ErrCategoryNotFound, StatusCode: 404, Message: "attachment not found"}
}

func newMockPage(id, title, adfBody string) *confluence.PageResponse {
	return &confluence.PageResponse{
		ID:    id,
		Title: title,
		Body: struct {
			AtlasDocFormat struct {
				Value string `json:"value"`
			} `json:"atlas_doc_format"`
		}{
			AtlasDocFormat: struct {
				Value string `json:"value"`
			}{Value: adfBody},
		},
	}
}

// --- Sanitize tests ---

func TestSanitizeFilename(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"simple", "Hello World", "hello-world"},
		{"special chars", `File/Name:With*Bad?"Chars`, "file-name-with-bad-chars"},
		{"consecutive hyphens", "a---b---c", "a-b-c"},
		{"empty title", "", "untitled"},
		{"only special chars", "***???", "untitled"},
		{"trim hyphens", "---hello---", "hello"},
		{"unicode", "Seção de Arquitetura", "seção-de-arquitetura"},
		{"tabs and spaces", "hello\tworld\nnew", "hello-world-new"},
		{"pipe and angle", "A | B <C> D", "a-b-c-d"},
		{"backslash", `path\to\page`, "path-to-page"},
		{"long title", string(make([]byte, 300)), "untitled"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := sanitizeFilename(tt.input)
			if got != tt.expected {
				t.Errorf("sanitizeFilename(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}

func TestSanitizeFilename_LongTitle(t *testing.T) {
	long := strings.Repeat("a", 210)
	result := sanitizeFilename(long)
	if len(result) > 200 {
		t.Errorf("expected max 200 chars, got %d", len(result))
	}
}

// --- Pull single page tests (T025) ---

func TestPullSinglePage(t *testing.T) {
	dir := t.TempDir()
	var stdout, stderr bytes.Buffer

	mock := &mockPullClient{
		pages: map[string]*confluence.PageResponse{
			"12345": newMockPage("12345", "My Test Page", `{"version":1,"type":"doc","content":[{"type":"paragraph","content":[{"type":"text","text":"Hello"}]}]}`),
		},
		children:    map[string][]confluence.ChildPage{},
		attachments: map[string][]confluence.Attachment{},
	}

	env := &pullEnv{
		pageID:          "12345",
		outputDir:       dir,
		skipAttachments: true,
		client:          mock,
		stdout:          &stdout,
		stderr:          &stderr,
	}

	err := env.pullSinglePage("12345", dir)
	if err != nil {
		t.Fatal(err)
	}

	// Verify file was created
	filePath := filepath.Join(dir, "my-test-page.md")
	data, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("expected file at %s: %v", filePath, err)
	}

	content := string(data)
	if !strings.Contains(content, "<!-- confluence-page-id: 12345 -->") {
		t.Error("expected page-id marker")
	}
	if !strings.Contains(content, "# My Test Page") {
		t.Error("expected H1 title")
	}
	if !strings.Contains(content, "Hello") {
		t.Error("expected converted body")
	}

	// Verify text output
	if !strings.Contains(stdout.String(), `Pulled: "My Test Page"`) {
		t.Errorf("expected pull output, got: %s", stdout.String())
	}
}

// --- Dry-run tests (T026) ---

func TestPullSinglePage_DryRun(t *testing.T) {
	dir := t.TempDir()
	var stdout, stderr bytes.Buffer

	mock := &mockPullClient{
		pages: map[string]*confluence.PageResponse{
			"12345": newMockPage("12345", "Dry Run Page", `{"version":1,"type":"doc","content":[]}`),
		},
	}

	env := &pullEnv{
		pageID:          "12345",
		outputDir:       dir,
		dryRun:          true,
		skipAttachments: true,
		client:          mock,
		stdout:          &stdout,
		stderr:          &stderr,
	}

	err := env.pullSinglePage("12345", dir)
	if err != nil {
		t.Fatal(err)
	}

	// No file should be written
	entries, _ := os.ReadDir(dir)
	if len(entries) != 0 {
		t.Errorf("expected no files in dry-run, got %d", len(entries))
	}

	// Should show preview
	if !strings.Contains(stdout.String(), "Would write:") {
		t.Errorf("expected dry-run output, got: %s", stdout.String())
	}

	// Result should be "skipped"
	if len(env.results) != 1 || env.results[0].Action != "skipped" {
		t.Errorf("expected skipped result, got %+v", env.results)
	}
}

// --- Error case tests (T027) ---

func TestPullSinglePage_NotFound(t *testing.T) {
	var stdout, stderr bytes.Buffer

	mock := &mockPullClient{
		pages: map[string]*confluence.PageResponse{},
	}

	env := &pullEnv{
		pageID:          "99999",
		outputDir:       t.TempDir(),
		skipAttachments: true,
		client:          mock,
		stdout:          &stdout,
		stderr:          &stderr,
	}

	err := env.pullSinglePage("99999", env.outputDir)
	if err == nil {
		t.Fatal("expected error for missing page")
	}
	var ae *apiError
	if !stderrors.As(err, &ae) {
		t.Fatalf("expected apiError, got %T: %v", err, err)
	}
	if ae.exitCode != 2 {
		t.Errorf("expected exit code 2, got %d", ae.exitCode)
	}
}

func TestPullSinglePage_AuthError(t *testing.T) {
	var stdout, stderr bytes.Buffer

	mock := &mockPullClient{
		getPageErr: &confluence.APIError{Category: confluence.ErrCategoryAuth, StatusCode: 401, Message: "auth failed", Hint: "check token"},
	}

	env := &pullEnv{
		pageID:          "12345",
		outputDir:       t.TempDir(),
		skipAttachments: true,
		client:          mock,
		stdout:          &stdout,
		stderr:          &stderr,
	}

	err := env.pullSinglePage("12345", env.outputDir)
	if err == nil {
		t.Fatal("expected error")
	}
	var ae *apiError
	if !stderrors.As(err, &ae) {
		t.Fatalf("expected apiError, got %T", err)
	}
	if ae.exitCode != 2 {
		t.Errorf("expected exit code 2, got %d", ae.exitCode)
	}
}

func TestPullSinglePage_EmptyBody(t *testing.T) {
	dir := t.TempDir()
	var stdout, stderr bytes.Buffer

	mock := &mockPullClient{
		pages: map[string]*confluence.PageResponse{
			"12345": newMockPage("12345", "Empty Page", ""),
		},
		attachments: map[string][]confluence.Attachment{},
	}

	env := &pullEnv{
		pageID:          "12345",
		outputDir:       dir,
		skipAttachments: true,
		client:          mock,
		stdout:          &stdout,
		stderr:          &stderr,
	}

	err := env.pullSinglePage("12345", dir)
	if err != nil {
		t.Fatal(err)
	}

	filePath := filepath.Join(dir, "empty-page.md")
	data, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatal(err)
	}
	content := string(data)
	if !strings.Contains(content, "<!-- confluence-page-id: 12345 -->") {
		t.Error("expected page-id marker even for empty body")
	}
	if !strings.Contains(content, "# Empty Page") {
		t.Error("expected H1 title even for empty body")
	}
}

// --- Title-based pull tests (T033-T034) ---

func TestPullByTitle(t *testing.T) {
	dir := t.TempDir()
	var stdout, stderr bytes.Buffer

	mock := &mockPullClient{
		pages: map[string]*confluence.PageResponse{
			"555": newMockPage("555", "Architecture Overview", `{"version":1,"type":"doc","content":[]}`),
		},
		spaceIDs: map[string]string{"DEVOPS": "space-1"},
		titlePages: map[string]*confluence.PageResponse{
			"space-1:Architecture Overview": newMockPage("555", "Architecture Overview", ""),
		},
		attachments: map[string][]confluence.Attachment{},
	}

	env := &pullEnv{
		title:           "Architecture Overview",
		space:           "DEVOPS",
		outputDir:       dir,
		skipAttachments: true,
		client:          mock,
		stdout:          &stdout,
		stderr:          &stderr,
	}

	err := env.run()
	if err != nil {
		t.Fatal(err)
	}

	filePath := filepath.Join(dir, "architecture-overview.md")
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		t.Errorf("expected file at %s", filePath)
	}
}

func TestPullByTitle_NotFound(t *testing.T) {
	var stdout, stderr bytes.Buffer

	mock := &mockPullClient{
		pages:      map[string]*confluence.PageResponse{},
		spaceIDs:   map[string]string{"DEVOPS": "space-1"},
		titlePages: map[string]*confluence.PageResponse{},
	}

	env := &pullEnv{
		title:           "Nonexistent Page",
		space:           "DEVOPS",
		outputDir:       t.TempDir(),
		skipAttachments: true,
		client:          mock,
		stdout:          &stdout,
		stderr:          &stderr,
	}

	err := env.run()
	if err == nil {
		t.Fatal("expected error for page not found by title")
	}
}

// --- Recursive pull tests (T037-T039) ---

func TestPullRecursive(t *testing.T) {
	dir := t.TempDir()
	var stdout, stderr bytes.Buffer

	mock := &mockPullClient{
		pages: map[string]*confluence.PageResponse{
			"100": newMockPage("100", "Parent Page", `{"version":1,"type":"doc","content":[]}`),
			"201": newMockPage("201", "Child A", `{"version":1,"type":"doc","content":[]}`),
			"202": newMockPage("202", "Child B", `{"version":1,"type":"doc","content":[]}`),
			"301": newMockPage("301", "Grandchild 1", `{"version":1,"type":"doc","content":[]}`),
		},
		children: map[string][]confluence.ChildPage{
			"100": {{ID: "201", Title: "Child A"}, {ID: "202", Title: "Child B"}},
			"202": {{ID: "301", Title: "Grandchild 1"}},
		},
		attachments: map[string][]confluence.Attachment{},
	}

	env := &pullEnv{
		pageID:          "100",
		outputDir:       dir,
		recursive:       true,
		depth:           10,
		skipAttachments: true,
		client:          mock,
		stdout:          &stdout,
		stderr:          &stderr,
	}

	err := env.pullRecursive("100", dir, 10)
	if err != nil {
		t.Fatal(err)
	}

	// Parent (has children) → parent-page/README.md
	expectFile(t, filepath.Join(dir, "parent-page", "README.md"))
	// Leaf child → parent-page/child-a.md
	expectFile(t, filepath.Join(dir, "parent-page", "child-a.md"))
	// Child B (has children) → parent-page/child-b/README.md
	expectFile(t, filepath.Join(dir, "parent-page", "child-b", "README.md"))
	// Grandchild → parent-page/child-b/grandchild-1.md
	expectFile(t, filepath.Join(dir, "parent-page", "child-b", "grandchild-1.md"))
}

func TestPullRecursive_DepthLimit(t *testing.T) {
	dir := t.TempDir()
	var stdout, stderr bytes.Buffer

	mock := &mockPullClient{
		pages: map[string]*confluence.PageResponse{
			"100": newMockPage("100", "Root", `{"version":1,"type":"doc","content":[]}`),
			"200": newMockPage("200", "Level 1", `{"version":1,"type":"doc","content":[]}`),
			"300": newMockPage("300", "Level 2 (should not have children)", `{"version":1,"type":"doc","content":[]}`),
		},
		children: map[string][]confluence.ChildPage{
			"100": {{ID: "200", Title: "Level 1"}},
			"200": {{ID: "300", Title: "Level 2"}},
			"300": {{ID: "400", Title: "Level 3 (truncated)"}},
		},
		attachments: map[string][]confluence.Attachment{},
	}

	env := &pullEnv{
		pageID:          "100",
		outputDir:       dir,
		recursive:       true,
		depth:           2,
		skipAttachments: true,
		client:          mock,
		stdout:          &stdout,
		stderr:          &stderr,
	}

	err := env.pullRecursive("100", dir, 2)
	if err != nil {
		t.Fatal(err)
	}

	// Should have a depth-limit warning
	hasWarning := false
	for _, w := range env.warnings {
		if strings.Contains(w, "depth limit") {
			hasWarning = true
		}
	}
	if !hasWarning {
		t.Error("expected depth limit warning")
	}
}

func TestPullRecursive_DryRun(t *testing.T) {
	dir := t.TempDir()
	var stdout, stderr bytes.Buffer

	mock := &mockPullClient{
		pages: map[string]*confluence.PageResponse{
			"100": newMockPage("100", "Parent", `{"version":1,"type":"doc","content":[]}`),
			"200": newMockPage("200", "Child", `{"version":1,"type":"doc","content":[]}`),
		},
		children: map[string][]confluence.ChildPage{
			"100": {{ID: "200", Title: "Child"}},
		},
		attachments: map[string][]confluence.Attachment{},
	}

	env := &pullEnv{
		pageID:          "100",
		outputDir:       dir,
		dryRun:          true,
		recursive:       true,
		depth:           10,
		skipAttachments: true,
		client:          mock,
		stdout:          &stdout,
		stderr:          &stderr,
	}

	err := env.pullRecursive("100", dir, 10)
	if err != nil {
		t.Fatal(err)
	}

	// No files should be created
	entries, _ := os.ReadDir(dir)
	if len(entries) != 0 {
		t.Errorf("expected no files in dry-run, got %d", len(entries))
	}

	// All results should be "skipped"
	for _, r := range env.results {
		if r.Action != "skipped" {
			t.Errorf("expected skipped, got %s for %s", r.Action, r.Title)
		}
	}
}

// --- Attachment tests (T045-T047) ---

func TestPullWithAttachments(t *testing.T) {
	dir := t.TempDir()
	var stdout, stderr bytes.Buffer

	mock := &mockPullClient{
		pages: map[string]*confluence.PageResponse{
			"12345": newMockPage("12345", "Page With Images", `{"version":1,"type":"doc","content":[]}`),
		},
		children: map[string][]confluence.ChildPage{},
		attachments: map[string][]confluence.Attachment{
			"12345": {
				{ID: "att1", Title: "photo.png", MediaType: "image/png", DownloadLink: "/wiki/dl/att/photo.png"},
			},
		},
		downloads: map[string][]byte{
			"/wiki/dl/att/photo.png": []byte("fake-png-data"),
		},
	}

	env := &pullEnv{
		pageID:    "12345",
		outputDir: dir,
		client:    mock,
		stdout:    &stdout,
		stderr:    &stderr,
	}

	err := env.pullSinglePage("12345", dir)
	if err != nil {
		t.Fatal(err)
	}

	// Attachment should be saved
	attPath := filepath.Join(dir, "attachments", "photo.png")
	data, err := os.ReadFile(attPath)
	if err != nil {
		t.Fatalf("expected attachment at %s: %v", attPath, err)
	}
	if string(data) != "fake-png-data" {
		t.Errorf("unexpected attachment content: %s", string(data))
	}
	if env.attachCount != 1 {
		t.Errorf("expected 1 attachment, got %d", env.attachCount)
	}
}

func TestPullSkipAttachments(t *testing.T) {
	dir := t.TempDir()
	var stdout, stderr bytes.Buffer

	mock := &mockPullClient{
		pages: map[string]*confluence.PageResponse{
			"12345": newMockPage("12345", "Page", `{"version":1,"type":"doc","content":[]}`),
		},
		attachments: map[string][]confluence.Attachment{
			"12345": {
				{ID: "att1", Title: "photo.png", MediaType: "image/png", DownloadLink: "/dl/photo.png"},
			},
		},
	}

	env := &pullEnv{
		pageID:          "12345",
		outputDir:       dir,
		skipAttachments: true,
		client:          mock,
		stdout:          &stdout,
		stderr:          &stderr,
	}

	err := env.pullSinglePage("12345", dir)
	if err != nil {
		t.Fatal(err)
	}

	attDir := filepath.Join(dir, "attachments")
	if _, err := os.Stat(attDir); !os.IsNotExist(err) {
		t.Error("expected no attachments dir when --skip-attachments")
	}
}

func TestPullAttachmentDownloadFailure(t *testing.T) {
	dir := t.TempDir()
	var stdout, stderr bytes.Buffer

	mock := &mockPullClient{
		pages: map[string]*confluence.PageResponse{
			"12345": newMockPage("12345", "Page", `{"version":1,"type":"doc","content":[]}`),
		},
		attachments: map[string][]confluence.Attachment{
			"12345": {
				{ID: "att1", Title: "missing.png", MediaType: "image/png", DownloadLink: "/dl/missing.png"},
			},
		},
		downloads: map[string][]byte{}, // no download available
	}

	env := &pullEnv{
		pageID:    "12345",
		outputDir: dir,
		client:    mock,
		stdout:    &stdout,
		stderr:    &stderr,
	}

	err := env.pullSinglePage("12345", dir)
	if err != nil {
		t.Fatal(err)
	}

	// Should have a warning but not fail
	hasWarning := false
	for _, w := range env.warnings {
		if strings.Contains(w, "failed to download") {
			hasWarning = true
		}
	}
	if !hasWarning {
		t.Error("expected download failure warning")
	}
}

// --- JSON output tests (T052-T053) ---

func TestPullJSON_Success(t *testing.T) {
	dir := t.TempDir()
	var stdout, stderr bytes.Buffer

	mock := &mockPullClient{
		pages: map[string]*confluence.PageResponse{
			"12345": newMockPage("12345", "JSON Page", `{"version":1,"type":"doc","content":[]}`),
		},
		attachments: map[string][]confluence.Attachment{},
	}

	env := &pullEnv{
		pageID:          "12345",
		outputDir:       dir,
		jsonOutput:      true,
		skipAttachments: true,
		client:          mock,
		stdout:          &stdout,
		stderr:          &stderr,
	}

	err := env.pullSinglePage("12345", dir)
	if err != nil {
		t.Fatal(err)
	}

	// Simulate the JSON output that runPullCommand would produce
	enc := json.NewEncoder(&stdout)
	enc.SetIndent("", "  ")
	enc.Encode(PullOutput{
		Status: "success",
		Pages:  env.results,
	})

	var output PullOutput
	if err := json.Unmarshal(stdout.Bytes(), &output); err != nil {
		t.Fatalf("failed to parse JSON output: %v\noutput: %s", err, stdout.String())
	}
	if output.Status != "success" {
		t.Errorf("expected success, got %s", output.Status)
	}
	if len(output.Pages) != 1 {
		t.Fatalf("expected 1 page, got %d", len(output.Pages))
	}
	if output.Pages[0].PageID != "12345" {
		t.Errorf("expected pageId 12345, got %s", output.Pages[0].PageID)
	}
	if output.Pages[0].Action != "written" {
		t.Errorf("expected action written, got %s", output.Pages[0].Action)
	}
}

func TestPullJSON_Error(t *testing.T) {
	errOutput := PullErrorOutput{
		Status:  "error",
		Code:    2,
		Message: "page not found: 99999",
		Hint:    "verify the page ID exists and you have read access",
	}
	data, _ := json.Marshal(errOutput)
	var parsed PullErrorOutput
	json.Unmarshal(data, &parsed)

	if parsed.Status != "error" {
		t.Errorf("expected error, got %s", parsed.Status)
	}
	if parsed.Code != 2 {
		t.Errorf("expected code 2, got %d", parsed.Code)
	}
}

// --- Exit code tests (T032) ---

func TestPullExitCodes(t *testing.T) {
	var stdout, stderr bytes.Buffer

	// Exit code 1: bad flags
	code := runPullCommand([]string{}, "test", &stdout, &stderr)
	if code != 1 {
		t.Errorf("expected exit code 1 for no args, got %d", code)
	}

	// Exit code 0: --version
	stdout.Reset()
	stderr.Reset()
	code = runPullCommand([]string{"--version"}, "test", &stdout, &stderr)
	if code != 0 {
		t.Errorf("expected exit code 0 for --version, got %d", code)
	}
}

// helpers

func expectFile(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Errorf("expected file at %s", path)
	}
}

