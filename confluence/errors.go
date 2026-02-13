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

func validationError(detail string) *APIError {
	return &APIError{
		Category:   ErrCategoryValidation,
		StatusCode: 422,
		Message:    fmt.Sprintf("invalid ADF content: %s", detail),
		Hint:       "check your Markdown for unsupported elements",
	}
}
