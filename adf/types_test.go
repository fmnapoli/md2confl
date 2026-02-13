// Copyright 2026 md2confl contributors
// SPDX-License-Identifier: Apache-2.0

package adf

import "testing"

func TestNewDocument(t *testing.T) {
	doc := NewDocument()

	if doc.Version != 1 {
		t.Errorf("expected version 1, got %d", doc.Version)
	}
	if doc.Type != "doc" {
		t.Errorf("expected type 'doc', got %q", doc.Type)
	}
	if len(doc.Content) != 0 {
		t.Errorf("expected empty content, got %d nodes", len(doc.Content))
	}
}
