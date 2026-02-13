// Copyright 2026 md2confl contributors
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestPrintResult_TextCreated(t *testing.T) {
	var buf bytes.Buffer
	printResult(&buf, Result{
		Status:   "success",
		PageID:   "123",
		PageURL:  "https://test.atlassian.net/wiki/pages/123",
		Title:    "My Page",
		SpaceKey: "TEST",
		Action:   "created",
		Version:  1,
	}, false)

	out := buf.String()
	if !strings.Contains(out, `Published "My Page"`) {
		t.Errorf("expected Published message, got %q", out)
	}
	if !strings.Contains(out, "Page ID: 123") {
		t.Errorf("expected page ID, got %q", out)
	}
	if !strings.Contains(out, "Action: created") {
		t.Errorf("expected action, got %q", out)
	}
	if !strings.Contains(out, "Version: 1") {
		t.Errorf("expected version, got %q", out)
	}
}

func TestPrintResult_TextUpdated(t *testing.T) {
	var buf bytes.Buffer
	printResult(&buf, Result{
		Status:  "success",
		PageID:  "456",
		PageURL: "https://test.atlassian.net/wiki/pages/456",
		Title:   "Updated Page",
		Action:  "updated",
		Version: 3,
	}, false)

	out := buf.String()
	if !strings.Contains(out, `Published "Updated Page"`) {
		t.Errorf("expected Published message, got %q", out)
	}
	if !strings.Contains(out, "Action: updated") {
		t.Errorf("expected action updated, got %q", out)
	}
}

func TestPrintResult_TextConverted(t *testing.T) {
	var buf bytes.Buffer
	printResult(&buf, Result{
		Status: "success",
		Action: "converted",
		Title:  "doc.md → doc.json",
	}, false)

	out := buf.String()
	if !strings.Contains(out, "Converted doc.md") {
		t.Errorf("expected Converted message, got %q", out)
	}
}

func TestPrintResult_JSON(t *testing.T) {
	var buf bytes.Buffer
	printResult(&buf, Result{
		Status:   "success",
		PageID:   "789",
		PageURL:  "https://test.atlassian.net/wiki/pages/789",
		Title:    "JSON Page",
		SpaceKey: "DEV",
		Action:   "created",
		Version:  1,
	}, true)

	var result Result
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("invalid JSON output: %v", err)
	}
	if result.Status != "success" {
		t.Errorf("expected status success, got %q", result.Status)
	}
	if result.PageID != "789" {
		t.Errorf("expected pageId 789, got %q", result.PageID)
	}
}

func TestPrintError_Text(t *testing.T) {
	var buf bytes.Buffer
	printError(&buf, "something failed", "try again", 2, false)

	out := buf.String()
	if !strings.Contains(out, "Error: something failed") {
		t.Errorf("expected error message, got %q", out)
	}
	if !strings.Contains(out, "Hint: try again") {
		t.Errorf("expected hint, got %q", out)
	}
}

func TestPrintError_TextNoHint(t *testing.T) {
	var buf bytes.Buffer
	printError(&buf, "something failed", "", 1, false)

	out := buf.String()
	if !strings.Contains(out, "Error: something failed") {
		t.Errorf("expected error message, got %q", out)
	}
	if strings.Contains(out, "Hint") {
		t.Errorf("expected no hint, got %q", out)
	}
}

func TestPrintError_JSON(t *testing.T) {
	var buf bytes.Buffer
	printError(&buf, "auth failed", "check token", 2, true)

	var result ErrorResult
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("invalid JSON output: %v", err)
	}
	if result.Status != "error" {
		t.Errorf("expected status error, got %q", result.Status)
	}
	if result.Code != 2 {
		t.Errorf("expected code 2, got %d", result.Code)
	}
	if result.Message != "auth failed" {
		t.Errorf("expected message 'auth failed', got %q", result.Message)
	}
	if result.Hint != "check token" {
		t.Errorf("expected hint 'check token', got %q", result.Hint)
	}
}
