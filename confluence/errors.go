// Copyright 2026 md2confl contributors
// SPDX-License-Identifier: Apache-2.0

package confluence

import "fmt"

// Error categories for the Confluence API client.
const (
	ErrCategoryAuth       = "auth"
	ErrCategoryNotFound   = "not_found"
	ErrCategoryConflict   = "conflict"
	ErrCategoryValidation = "validation"
	ErrCategoryNetwork    = "network"
	ErrCategoryBlocked    = "blocked"
)

// APIError represents a categorized Confluence API error.
type APIError struct {
	Category   string
	StatusCode int
	Message    string
	Hint       string
}

func (e *APIError) Error() string {
	return e.Message
}

// ExitCode returns the CLI exit code for this error category.
func (e *APIError) ExitCode() int {
	return 2
}

func authError(statusCode int) *APIError {
	return &APIError{
		Category:   ErrCategoryAuth,
		StatusCode: statusCode,
		Message:    "authentication failed — invalid or expired API token",
		Hint:       "verify your --token or CONFLUENCE_TOKEN environment variable",
	}
}

func notFoundError(resource, id string) *APIError {
	return &APIError{
		Category:   ErrCategoryNotFound,
		StatusCode: 404,
		Message:    fmt.Sprintf("%s not found: %s", resource, id),
		Hint:       fmt.Sprintf("verify the %s ID or key is correct", resource),
	}
}

func conflictError() *APIError {
	return &APIError{
		Category:   ErrCategoryConflict,
		StatusCode: 409,
		Message:    "version conflict — page was updated concurrently",
		Hint:       "retry the operation",
	}
}

// searchBlockedError reports a title search rejected with HTTP 403.
//
// A generic 403 is reported as an auth error (or, when the proxy answers with
// an HTML page, as a transient CDN error), and both are misleading here: the
// search endpoint is commonly blocked by the WAF/proxy sitting in front of
// Confluence while access by page ID keeps working. Retrying does not help,
// and the search result is ambiguous — the page may well exist — so callers
// must fail instead of falling through to page creation.
func searchBlockedError(spaceKey, title string) *APIError {
	return &APIError{
		Category:   ErrCategoryBlocked,
		StatusCode: 403,
		Message: fmt.Sprintf("title search rejected with HTTP 403 (space %q, title %q) — "+
			"the search endpoint is blocked by the proxy/WAF in front of Confluence, "+
			"or the account cannot read this space", spaceKey, title),
		Hint: "not transient — retrying will not help; add a <!-- confluence-page-id: N --> " +
			"marker to the document so it is published by ID instead of by title search",
	}
}

func validationError(detail string) *APIError {
	return &APIError{
		Category:   ErrCategoryValidation,
		StatusCode: 422,
		Message:    fmt.Sprintf("invalid ADF content: %s", detail),
		Hint:       "check your Markdown for unsupported elements",
	}
}

// comalaApprovalError reports a Comala Document Management approval that did
// not reach the target ("Approved") state. Deliberately separate from
// validationError: that message ("invalid ADF content") is specific to the
// Cloud/ADF publish path and is misleading here — this is the Server/DC
// Storage Format path, and the failure is about a workflow state transition,
// not document content.
func comalaApprovalError(statusCode int, stateName, workflowMessage string) *APIError {
	detail := workflowMessage
	if detail == "" {
		detail = fmt.Sprintf("workflow state is %q", stateName)
	}
	return &APIError{
		Category:   ErrCategoryValidation,
		StatusCode: statusCode,
		Message:    fmt.Sprintf("Comala workflow approval was not applied (HTTP %d): %s", statusCode, detail),
		Hint:       "check the space's Comala workflow configuration and the approval name being used",
	}
}
