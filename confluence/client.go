// Copyright 2026 md2confl contributors
// SPDX-License-Identifier: Apache-2.0

// Package confluence provides a REST API v2 client for Confluence Cloud.
// It supports creating and updating pages, resolving space IDs, searching by
// title, and uploading file attachments. All API errors are returned as
// [*APIError] with a category, HTTP status code, and actionable hint.
package confluence

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"strconv"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Config holds Confluence connection settings.
type Config struct {
	BaseURL  string
	SpaceKey string
	SpaceID  string
	ParentID string
	Email    string
	Token    string
}

// PublishResult holds the outcome of a publish operation.
type PublishResult struct {
	PageID   string `json:"pageId"`
	PageURL  string `json:"pageUrl"`
	Title    string `json:"title"`
	SpaceKey string `json:"spaceKey"`
	Version  int    `json:"version"`
	Action   string `json:"action"`
}

// Client is a Confluence Cloud REST API client.
type Client struct {
	config       Config
	httpClient   *http.Client
	baseAPIURL   string
	logger       *slog.Logger
	maxRetries   int
	initialDelay time.Duration
}

// NewClient creates a new Confluence API client.
func NewClient(cfg Config) (*Client, error) {
	if !strings.HasPrefix(cfg.BaseURL, "https://") {
		return nil, fmt.Errorf("base URL must use HTTPS: %s", cfg.BaseURL)
	}
	baseURL := strings.TrimRight(cfg.BaseURL, "/")
	return &Client{
		config:       cfg,
		httpClient:   &http.Client{Timeout: 2 * time.Minute},
		baseAPIURL:   baseURL + "/wiki/api/v2",
		logger:       slog.Default(),
		maxRetries:   3,
		initialDelay: 1 * time.Second,
	}, nil
}

// SetLogger configures a custom logger for the client.
func (c *Client) SetLogger(l *slog.Logger) {
	c.logger = l
}

// SetHTTPClient overrides the default HTTP client (useful for testing with TLS test servers).
func (c *Client) SetHTTPClient(hc *http.Client) {
	c.httpClient = hc
}

func (c *Client) authHeader() string {
	creds := c.config.Email + ":" + c.config.Token
	return "Basic " + base64.StdEncoding.EncodeToString([]byte(creds))
}

func (c *Client) doRequest(req *http.Request) (*http.Response, error) {
	req.Header.Set("Authorization", c.authHeader())
	if req.Header.Get("Content-Type") == "" {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Accept", "application/json")

	return c.doWithRetry(req)
}

// isRetryable returns true if the response should be retried.
// Retries on 429 (rate limit), 5xx (server error), and any response with
// HTML content type (CDN/proxy errors like CloudFront 400).
func isRetryable(resp *http.Response) bool {
	if resp.StatusCode == 429 || resp.StatusCode >= 500 {
		return true
	}
	ct := resp.Header.Get("Content-Type")
	return strings.Contains(ct, "text/html")
}

// doWithRetry executes an HTTP request with retry on transient errors.
// Retries on 429 (rate limit), 5xx (server error), and HTML responses
// (CDN/proxy errors). Uses exponential backoff: 1s, 2s, 4s (max 3 attempts).
// Respects the Retry-After header on 429 responses.
func (c *Client) doWithRetry(req *http.Request) (*http.Response, error) {
	backoff := c.initialDelay

	var resp *http.Response
	var err error

	for attempt := range c.maxRetries {
		start := time.Now()
		resp, err = c.httpClient.Do(req)
		elapsed := time.Since(start)

		if err != nil {
			c.logger.Debug("API request failed", "method", req.Method, "url", req.URL.Path, "elapsed", elapsed, "error", err)
			return nil, err
		}

		c.logger.Debug("API request", "method", req.Method, "url", req.URL.Path, "status", resp.StatusCode, "elapsed", elapsed)

		if !isRetryable(resp) {
			return resp, nil
		}

		// Retryable error
		if attempt == c.maxRetries-1 {
			return resp, nil // last attempt, return as-is
		}

		wait := backoff
		if resp.StatusCode == 429 {
			if ra := resp.Header.Get("Retry-After"); ra != "" {
				if secs, err := strconv.Atoi(ra); err == nil && secs > 0 {
					wait = time.Duration(secs) * time.Second
				}
			}
		}

		c.logger.Debug("Retrying API request", "attempt", attempt+1, "status", resp.StatusCode, "backoff", wait)
		resp.Body.Close()
		time.Sleep(wait)
		backoff *= 2

		// Reset the request body for the next attempt (POST/PUT bodies
		// are consumed by Do and need to be re-created from GetBody).
		if req.GetBody != nil {
			req.Body, err = req.GetBody()
			if err != nil {
				return nil, fmt.Errorf("resetting request body for retry: %w", err)
			}
		}
	}

	return resp, nil
}

func (c *Client) handleErrorResponse(resp *http.Response) error {
	body, _ := io.ReadAll(resp.Body)
	bodyStr := string(body)

	// Detect CDN/proxy HTML error pages (e.g. CloudFront 400)
	if strings.Contains(resp.Header.Get("Content-Type"), "text/html") || strings.HasPrefix(bodyStr, "<!DOCTYPE") {
		return &APIError{
			Category:   ErrCategoryNetwork,
			StatusCode: resp.StatusCode,
			Message:    fmt.Sprintf("CDN/proxy error (HTTP %d) — likely a transient issue", resp.StatusCode),
			Hint:       "retry the operation; if it persists, check Atlassian status at https://status.atlassian.com",
		}
	}

	switch resp.StatusCode {
	case 401, 403:
		return authError(resp.StatusCode)
	case 404:
		return notFoundError("resource", bodyStr)
	case 409:
		return conflictError()
	case 400, 422:
		return validationError(bodyStr)
	default:
		return &APIError{
			Category:   ErrCategoryNetwork,
			StatusCode: resp.StatusCode,
			Message:    fmt.Sprintf("unexpected API response %d: %s", resp.StatusCode, bodyStr),
		}
	}
}

// spaceResponse represents the API response for space queries.
type spaceResponse struct {
	Results []struct {
		ID   string `json:"id"`
		Key  string `json:"key"`
		Name string `json:"name"`
	} `json:"results"`
}

// ResolveSpaceID resolves a space key to a space ID.
func (c *Client) ResolveSpaceID(spaceKey string) (string, error) {
	url := fmt.Sprintf("%s/spaces?keys=%s", c.baseAPIURL, spaceKey)
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return "", err
	}

	resp, err := c.doRequest(req)
	if err != nil {
		return "", &APIError{Category: ErrCategoryNetwork, Message: fmt.Sprintf("network error: %v", err)}
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return "", c.handleErrorResponse(resp)
	}

	var result spaceResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("decoding space response: %w", err)
	}

	if len(result.Results) == 0 {
		return "", notFoundError("space", spaceKey)
	}

	return result.Results[0].ID, nil
}

// PageResponse represents the API response for page operations.
type PageResponse struct {
	ID       string `json:"id"`
	ParentID string `json:"parentId"`
	Title    string `json:"title"`
	Version struct {
		Number int `json:"number"`
	} `json:"version"`
	Body struct {
		AtlasDocFormat struct {
			Value string `json:"value"`
		} `json:"atlas_doc_format"`
	} `json:"body"`
	Links struct {
		WebUI string `json:"webui"`
		Base  string `json:"base"`
	} `json:"_links"`
}

// pagesResponse represents the API response for page list queries.
type pagesResponse struct {
	Results []PageResponse `json:"results"`
}

// CreatePage creates a new page in Confluence.
func (c *Client) CreatePage(spaceID, title, parentID, adfJSON string) (*PublishResult, error) {
	body := map[string]any{
		"spaceId": spaceID,
		"status":  "current",
		"title":   title,
		"body": map[string]any{
			"representation": "atlas_doc_format",
			"value":          adfJSON,
		},
	}
	if parentID != "" {
		body["parentId"] = parentID
	}

	jsonBody, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}

	url := fmt.Sprintf("%s/pages", c.baseAPIURL)
	req, err := http.NewRequest("POST", url, bytes.NewReader(jsonBody))
	if err != nil {
		return nil, err
	}

	resp, err := c.doRequest(req)
	if err != nil {
		return nil, &APIError{Category: ErrCategoryNetwork, Message: fmt.Sprintf("network error: %v", err)}
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return nil, c.handleErrorResponse(resp)
	}

	var page PageResponse
	if err := json.NewDecoder(resp.Body).Decode(&page); err != nil {
		return nil, fmt.Errorf("decoding page response: %w", err)
	}

	return &PublishResult{
		PageID:   page.ID,
		PageURL:  page.Links.Base + page.Links.WebUI,
		Title:    page.Title,
		SpaceKey: c.config.SpaceKey,
		Version:  page.Version.Number,
		Action:   "created",
	}, nil
}

// GetPage retrieves a page by ID.
func (c *Client) GetPage(pageID string) (*PageResponse, error) {
	url := fmt.Sprintf("%s/pages/%s?body-format=atlas_doc_format", c.baseAPIURL, pageID)
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}

	resp, err := c.doRequest(req)
	if err != nil {
		return nil, &APIError{Category: ErrCategoryNetwork, Message: fmt.Sprintf("network error: %v", err)}
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return nil, c.handleErrorResponse(resp)
	}

	var page PageResponse
	if err := json.NewDecoder(resp.Body).Decode(&page); err != nil {
		return nil, fmt.Errorf("decoding page response: %w", err)
	}
	return &page, nil
}

// UpdatePage updates an existing page.
func (c *Client) UpdatePage(pageID, title, adfJSON string, currentVersion int) (*PublishResult, error) {
	body := map[string]any{
		"id":     pageID,
		"status": "current",
		"title":  title,
		"body": map[string]any{
			"representation": "atlas_doc_format",
			"value":          adfJSON,
		},
		"version": map[string]any{
			"number":  currentVersion + 1,
			"message": "Updated by md2confl",
		},
	}

	jsonBody, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}

	url := fmt.Sprintf("%s/pages/%s", c.baseAPIURL, pageID)
	req, err := http.NewRequest("PUT", url, bytes.NewReader(jsonBody))
	if err != nil {
		return nil, err
	}

	resp, err := c.doRequest(req)
	if err != nil {
		return nil, &APIError{Category: ErrCategoryNetwork, Message: fmt.Sprintf("network error: %v", err)}
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return nil, c.handleErrorResponse(resp)
	}

	var page PageResponse
	if err := json.NewDecoder(resp.Body).Decode(&page); err != nil {
		return nil, fmt.Errorf("decoding page response: %w", err)
	}

	return &PublishResult{
		PageID:   page.ID,
		PageURL:  page.Links.Base + page.Links.WebUI,
		Title:    page.Title,
		SpaceKey: c.config.SpaceKey,
		Version:  page.Version.Number,
		Action:   "updated",
	}, nil
}

// FindByTitle searches for a page by exact title in a space.
func (c *Client) FindByTitle(spaceID, title string) (*PageResponse, error) {
	reqURL := fmt.Sprintf("%s/pages?space-id=%s&title=%s&status=current", c.baseAPIURL, spaceID, url.QueryEscape(title))
	req, err := http.NewRequest("GET", reqURL, nil)
	if err != nil {
		return nil, err
	}

	resp, err := c.doRequest(req)
	if err != nil {
		return nil, &APIError{Category: ErrCategoryNetwork, Message: fmt.Sprintf("network error: %v", err)}
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return nil, c.handleErrorResponse(resp)
	}

	var result pagesResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decoding pages response: %w", err)
	}

	if len(result.Results) == 0 {
		return nil, nil
	}
	return &result.Results[0], nil
}

// MovePage moves a page to be a child of the target parent using the v1 API.
// Endpoint: PUT /wiki/rest/api/content/{pageID}/move/append/{targetParentID}
func (c *Client) MovePage(pageID, targetParentID string) error {
	baseURL := strings.TrimRight(c.config.BaseURL, "/")
	apiURL := fmt.Sprintf("%s/wiki/rest/api/content/%s/move/append/%s", baseURL, pageID, targetParentID)
	req, err := http.NewRequest("PUT", apiURL, nil)
	if err != nil {
		return err
	}

	resp, err := c.doRequest(req)
	if err != nil {
		return &APIError{Category: ErrCategoryNetwork, Message: fmt.Sprintf("network error: %v", err)}
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return c.handleErrorResponse(resp)
	}

	return nil
}

// attachmentResult holds the parsed fields from an attachment API response entry.
type attachmentResult struct {
	ID         string `json:"id"`
	Title      string `json:"title"`
	Extensions struct {
		FileID string `json:"fileId"`
	} `json:"extensions"`
}

// fileID returns the file ID (UUID) needed for ADF media nodes,
// falling back to the attachment ID if fileId is absent.
func (a *attachmentResult) fileID() string {
	if a.Extensions.FileID != "" {
		return a.Extensions.FileID
	}
	return a.ID
}

// UploadAttachment uploads a file as an attachment to a page (uses API v1).
// If an attachment with the same filename already exists, it returns the
// existing attachment's file ID instead of failing.
func (c *Client) UploadAttachment(pageID, filePath string) (string, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return "", fmt.Errorf("opening attachment %q: %w", filePath, err)
	}
	defer file.Close()

	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)
	part, err := writer.CreateFormFile("file", filepath.Base(filePath))
	if err != nil {
		return "", err
	}
	if _, err := io.Copy(part, file); err != nil {
		return "", err
	}
	if err := writer.Close(); err != nil {
		return "", fmt.Errorf("closing multipart writer: %w", err)
	}

	baseURL := strings.TrimRight(c.config.BaseURL, "/")
	apiURL := fmt.Sprintf("%s/wiki/rest/api/content/%s/child/attachment", baseURL, pageID)
	req, err := http.NewRequest("POST", apiURL, bytes.NewReader(buf.Bytes()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("X-Atlassian-Token", "no-check")

	resp, err := c.doRequest(req)
	if err != nil {
		return "", &APIError{Category: ErrCategoryNetwork, Message: fmt.Sprintf("network error: %v", err)}
	}
	defer resp.Body.Close()

	if resp.StatusCode == 400 {
		// Check if the error is a duplicate filename — look up the existing attachment.
		body, _ := io.ReadAll(resp.Body)
		if strings.Contains(string(body), "same file name") {
			return c.getAttachmentFileID(pageID, filepath.Base(filePath))
		}
		return "", validationError(string(body))
	}

	if resp.StatusCode != 200 {
		return "", c.handleErrorResponse(resp)
	}

	var result struct {
		Results []attachmentResult `json:"results"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("decoding attachment response: %w", err)
	}

	if len(result.Results) == 0 {
		return "", fmt.Errorf("no attachment ID in response")
	}

	return result.Results[0].fileID(), nil
}

// getAttachmentFileID looks up an existing attachment by filename and returns its file ID.
func (c *Client) getAttachmentFileID(pageID, filename string) (string, error) {
	baseURL := strings.TrimRight(c.config.BaseURL, "/")
	apiURL := fmt.Sprintf("%s/wiki/rest/api/content/%s/child/attachment?filename=%s",
		baseURL, pageID, url.QueryEscape(filename))
	req, err := http.NewRequest("GET", apiURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("X-Atlassian-Token", "no-check")

	resp, err := c.doRequest(req)
	if err != nil {
		return "", &APIError{Category: ErrCategoryNetwork, Message: fmt.Sprintf("network error: %v", err)}
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return "", c.handleErrorResponse(resp)
	}

	var result struct {
		Results []attachmentResult `json:"results"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("decoding attachment response: %w", err)
	}

	if len(result.Results) == 0 {
		return "", fmt.Errorf("attachment %q not found on page %s", filename, pageID)
	}

	return result.Results[0].fileID(), nil
}

// ChildPage holds the ID and title of a child page.
type ChildPage struct {
	ID    string
	Title string
}

// childrenResponse represents the v2 API response for child pages.
type childrenResponse struct {
	Results []struct {
		ID    string `json:"id"`
		Title string `json:"title"`
	} `json:"results"`
	Links struct {
		Next string `json:"next"`
	} `json:"_links"`
}

// GetChildren returns all direct child pages of a page.
// Paginates automatically until all children are fetched.
func (c *Client) GetChildren(pageID string) ([]ChildPage, error) {
	var children []ChildPage
	reqURL := fmt.Sprintf("%s/pages/%s/children?limit=50", c.baseAPIURL, pageID)

	for reqURL != "" {
		req, err := http.NewRequest("GET", reqURL, nil)
		if err != nil {
			return nil, err
		}

		resp, err := c.doRequest(req)
		if err != nil {
			return nil, &APIError{Category: ErrCategoryNetwork, Message: fmt.Sprintf("network error: %v", err)}
		}

		if resp.StatusCode != 200 {
			err := c.handleErrorResponse(resp)
			resp.Body.Close()
			return nil, err
		}

		var result childrenResponse
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			resp.Body.Close()
			return nil, fmt.Errorf("decoding children response: %w", err)
		}
		resp.Body.Close()

		for _, child := range result.Results {
			children = append(children, ChildPage{ID: child.ID, Title: child.Title})
		}

		if result.Links.Next == "" {
			break
		}
		// Next link is relative — prepend base URL
		reqURL = strings.TrimRight(c.config.BaseURL, "/") + result.Links.Next
	}

	return children, nil
}

// Attachment holds metadata for a page attachment.
type Attachment struct {
	ID           string
	FileID       string // UUID used in ADF media nodes (type:"file")
	Title        string // filename
	MediaType    string
	DownloadLink string // relative URL
}

// attachmentsResponse represents the v2 API response for attachments.
type attachmentsResponse struct {
	Results []struct {
		ID           string `json:"id"`
		FileID       string `json:"fileId"`
		Title        string `json:"title"`
		MediaType    string `json:"mediaType"`
		DownloadLink string `json:"downloadLink"`
	} `json:"results"`
	Links struct {
		Next string `json:"next"`
	} `json:"_links"`
}

// GetAttachments returns all attachments for a page.
// Paginates automatically until all attachments are fetched.
func (c *Client) GetAttachments(pageID string) ([]Attachment, error) {
	var attachments []Attachment
	reqURL := fmt.Sprintf("%s/pages/%s/attachments?limit=50", c.baseAPIURL, pageID)

	for reqURL != "" {
		req, err := http.NewRequest("GET", reqURL, nil)
		if err != nil {
			return nil, err
		}

		resp, err := c.doRequest(req)
		if err != nil {
			return nil, &APIError{Category: ErrCategoryNetwork, Message: fmt.Sprintf("network error: %v", err)}
		}

		if resp.StatusCode != 200 {
			err := c.handleErrorResponse(resp)
			resp.Body.Close()
			return nil, err
		}

		var result attachmentsResponse
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			resp.Body.Close()
			return nil, fmt.Errorf("decoding attachments response: %w", err)
		}
		resp.Body.Close()

		for _, att := range result.Results {
			attachments = append(attachments, Attachment{
				ID:           att.ID,
				FileID:       att.FileID,
				Title:        att.Title,
				MediaType:    att.MediaType,
				DownloadLink: att.DownloadLink,
			})
		}

		if result.Links.Next == "" {
			break
		}
		reqURL = strings.TrimRight(c.config.BaseURL, "/") + result.Links.Next
	}

	return attachments, nil
}

// DownloadAttachment downloads an attachment by its relative download link.
// The downloadLink from the v2 API is relative (e.g. "/download/attachments/..."),
// and the actual endpoint lives under /wiki on Confluence Cloud.
// Returns the raw bytes of the file content.
func (c *Client) DownloadAttachment(downloadLink string) ([]byte, error) {
	base := strings.TrimRight(c.config.BaseURL, "/")
	// The v2 API returns download links without the /wiki prefix,
	// but the actual download endpoint requires it.
	if !strings.HasPrefix(downloadLink, "/wiki/") {
		downloadLink = "/wiki" + downloadLink
	}
	dlURL := base + downloadLink
	req, err := http.NewRequest("GET", dlURL, nil)
	if err != nil {
		return nil, err
	}

	resp, err := c.doRequest(req)
	if err != nil {
		return nil, &APIError{Category: ErrCategoryNetwork, Message: fmt.Sprintf("network error: %v", err)}
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return nil, c.handleErrorResponse(resp)
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading attachment body: %w", err)
	}

	return data, nil
}
