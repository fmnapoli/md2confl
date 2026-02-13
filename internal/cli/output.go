// Copyright 2026 md2confl contributors
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"encoding/json"
	"fmt"
	"io"
)

// Result holds the outcome of a publish operation.
type Result struct {
	Status   string `json:"status"`
	PageID   string `json:"pageId,omitempty"`
	PageURL  string `json:"pageUrl,omitempty"`
	Title    string `json:"title,omitempty"`
	SpaceKey string `json:"spaceKey,omitempty"`
	Action   string `json:"action,omitempty"`
	Version  int    `json:"version,omitempty"`
}

// ErrorResult holds an error outcome for JSON output.
type ErrorResult struct {
	Status  string `json:"status"`
	Code    int    `json:"code"`
	Message string `json:"message"`
	Hint    string `json:"hint,omitempty"`
}

// printResult writes a result in text or JSON format.
func printResult(w io.Writer, r Result, jsonOutput bool) {
	if jsonOutput {
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		enc.Encode(r)
		return
	}
	switch r.Action {
	case "created", "updated":
		fmt.Fprintf(w, "✓ Published %q → %s\n", r.Title, r.PageURL)
		fmt.Fprintf(w, "  Page ID: %s\n", r.PageID)
		fmt.Fprintf(w, "  Action: %s\n", r.Action)
		fmt.Fprintf(w, "  Version: %d\n", r.Version)
	case "converted":
		fmt.Fprintf(w, "✓ Converted %s\n", r.Title)
	}
}

// printError writes an error in text or JSON format.
func printError(w io.Writer, msg, hint string, code int, jsonOutput bool) {
	if jsonOutput {
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		enc.Encode(ErrorResult{
			Status:  "error",
			Code:    code,
			Message: msg,
			Hint:    hint,
		})
		return
	}
	fmt.Fprintf(w, "Error: %s\n", msg)
	if hint != "" {
		fmt.Fprintf(w, "  Hint: %s\n", hint)
	}
}
