// Copyright 2026 md2confl contributors
// SPDX-License-Identifier: Apache-2.0

package confluence

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// ServerClient é um client para Confluence Server/Data Center (REST API v1).
// Usa Storage Format (XHTML) em vez de ADF (JSON).
type ServerClient struct {
	config       Config
	httpClient   *http.Client
	baseAPIURL   string // e.g. https://tdn.totvs.com/rest/api
	logger       *slog.Logger
	maxRetries   int
	initialDelay time.Duration
}

// NewServerClient cria um client para Confluence Server/Data Center.
func NewServerClient(cfg Config) (*ServerClient, error) {
	baseURL := strings.TrimRight(cfg.BaseURL, "/")
	return &ServerClient{
		config:       cfg,
		httpClient:   &http.Client{Timeout: 2 * time.Minute},
		baseAPIURL:   baseURL + "/rest/api",
		logger:       slog.Default(),
		maxRetries:   3,
		initialDelay: 1 * time.Second,
	}, nil
}

// SetLogger configura um logger customizado.
func (c *ServerClient) SetLogger(l *slog.Logger) {
	c.logger = l
}

// SetHTTPClient permite injetar um HTTP client (para testes).
func (c *ServerClient) SetHTTPClient(hc *http.Client) {
	c.httpClient = hc
}

func (c *ServerClient) authHeader() string {
	return (&Client{config: c.config}).authHeader()
}

func (c *ServerClient) doRequest(req *http.Request) (*http.Response, error) {
	req.Header.Set("Authorization", c.authHeader())
	if req.Header.Get("Content-Type") == "" {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Accept", "application/json")
	if c.config.UserAgent != "" {
		req.Header.Set("User-Agent", c.config.UserAgent)
	}

	// Reutiliza a lógica de retry do client base
	cl := &Client{
		config:       c.config,
		httpClient:   c.httpClient,
		logger:       c.logger,
		maxRetries:   c.maxRetries,
		initialDelay: c.initialDelay,
	}
	return cl.doWithRetry(req)
}

func (c *ServerClient) handleErrorResponse(resp *http.Response) error {
	cl := &Client{config: c.config, logger: c.logger}
	return cl.handleErrorResponse(resp)
}

// v1PageResponse representa a resposta da API v1 para operações de página.
type v1PageResponse struct {
	ID    string `json:"id"`
	Type  string `json:"type"`
	Title string `json:"title"`
	Body  struct {
		Storage struct {
			Value string `json:"value"`
		} `json:"storage"`
	} `json:"body"`
	Version struct {
		Number int `json:"number"`
	} `json:"version"`
	Ancestors []struct {
		ID string `json:"id"`
	} `json:"ancestors"`
	Links struct {
		WebUI string `json:"webui"`
		Base  string `json:"base"`
	} `json:"_links"`
}

func (r *v1PageResponse) toPublishResult(spaceKey, action string) *PublishResult {
	pageURL := r.Links.Base + r.Links.WebUI
	return &PublishResult{
		PageID:   r.ID,
		PageURL:  pageURL,
		Title:    r.Title,
		SpaceKey: spaceKey,
		Version:  r.Version.Number,
		Action:   action,
	}
}

func (r *v1PageResponse) toPageResponse() *PageResponse {
	pr := &PageResponse{
		ID:    r.ID,
		Title: r.Title,
	}
	pr.Version.Number = r.Version.Number
	pr.Body.AtlasDocFormat.Value = r.Body.Storage.Value
	pr.Links.WebUI = r.Links.WebUI
	pr.Links.Base = r.Links.Base
	if len(r.Ancestors) > 0 {
		pr.ParentID = r.Ancestors[len(r.Ancestors)-1].ID
	}
	return pr
}

// CreatePage cria uma nova página no Confluence Server/DC.
func (c *ServerClient) CreatePage(spaceKey, title, parentID, storageContent string) (*PublishResult, error) {
	body := map[string]any{
		"type":  "page",
		"title": title,
		"space": map[string]any{
			"key": spaceKey,
		},
		"body": map[string]any{
			"storage": map[string]any{
				"value":          storageContent,
				"representation": "storage",
			},
		},
	}
	if parentID != "" {
		body["ancestors"] = []map[string]any{{"id": parentID}}
	}

	jsonBody, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}

	reqURL := fmt.Sprintf("%s/content", c.baseAPIURL)
	req, err := http.NewRequest("POST", reqURL, bytes.NewReader(jsonBody))
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

	var page v1PageResponse
	if err := json.NewDecoder(resp.Body).Decode(&page); err != nil {
		return nil, fmt.Errorf("decoding page response: %w", err)
	}

	return page.toPublishResult(spaceKey, "created"), nil
}

// GetPage busca uma página por ID.
func (c *ServerClient) GetPage(pageID string) (*PageResponse, error) {
	reqURL := fmt.Sprintf("%s/content/%s?expand=body.storage,version,ancestors", c.baseAPIURL, pageID)
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

	var page v1PageResponse
	if err := json.NewDecoder(resp.Body).Decode(&page); err != nil {
		return nil, fmt.Errorf("decoding page response: %w", err)
	}

	return page.toPageResponse(), nil
}

// UpdatePage atualiza uma página existente.
func (c *ServerClient) UpdatePage(pageID, title, storageContent string, currentVersion int) (*PublishResult, error) {
	body := map[string]any{
		"id":    pageID,
		"type":  "page",
		"title": title,
		"body": map[string]any{
			"storage": map[string]any{
				"value":          storageContent,
				"representation": "storage",
			},
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

	reqURL := fmt.Sprintf("%s/content/%s", c.baseAPIURL, pageID)
	req, err := http.NewRequest("PUT", reqURL, bytes.NewReader(jsonBody))
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

	var page v1PageResponse
	if err := json.NewDecoder(resp.Body).Decode(&page); err != nil {
		return nil, fmt.Errorf("decoding page response: %w", err)
	}

	return page.toPublishResult(c.config.SpaceKey, "updated"), nil
}

// FindByTitle busca uma página por título no space usando a API v1 direta.
func (c *ServerClient) FindByTitle(spaceKey, title string) (*PageResponse, error) {
	reqURL := fmt.Sprintf("%s/content?spaceKey=%s&title=%s&type=page&expand=body.storage,version,ancestors",
		c.baseAPIURL, url.QueryEscape(spaceKey), url.QueryEscape(title))
	req, err := http.NewRequest("GET", reqURL, nil)
	if err != nil {
		return nil, err
	}

	resp, err := c.doRequest(req)
	if err != nil {
		return nil, &APIError{Category: ErrCategoryNetwork, Message: fmt.Sprintf("network error: %v", err)}
	}
	defer resp.Body.Close()

	// 403 aqui é o caso do WAF/proxy que bloqueia a busca por título mesmo
	// com o acesso por ID liberado — mensagem dedicada, sem retry.
	if resp.StatusCode == 403 {
		return nil, searchBlockedError(spaceKey, title)
	}
	if resp.StatusCode != 200 {
		return nil, c.handleErrorResponse(resp)
	}

	var result struct {
		Results []v1PageResponse `json:"results"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decoding search response: %w", err)
	}

	if len(result.Results) == 0 {
		return nil, nil
	}
	return result.Results[0].toPageResponse(), nil
}

// UploadAttachment faz upload de um arquivo como attachment (API v1).
func (c *ServerClient) UploadAttachment(pageID, filePath string) (string, error) {
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

	apiURL := fmt.Sprintf("%s/content/%s/child/attachment", c.baseAPIURL, pageID)
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

func (c *ServerClient) getAttachmentFileID(pageID, filename string) (string, error) {
	apiURL := fmt.Sprintf("%s/content/%s/child/attachment?filename=%s",
		c.baseAPIURL, pageID, url.QueryEscape(filename))
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

// GetAttachments retorna todos os attachments de uma página (paginado).
func (c *ServerClient) GetAttachments(pageID string) ([]Attachment, error) {
	var attachments []Attachment
	start := 0

	for {
		reqURL := fmt.Sprintf("%s/content/%s/child/attachment?start=%d&limit=50",
			c.baseAPIURL, pageID, start)
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

		var result struct {
			Results []struct {
				ID    string `json:"id"`
				Title string `json:"title"`
				Links struct {
					Download string `json:"download"`
				} `json:"_links"`
				Extensions struct {
					FileID    string `json:"fileId"`
					MediaType string `json:"mediaType"`
				} `json:"extensions"`
			} `json:"results"`
			Size int `json:"size"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			resp.Body.Close()
			return nil, fmt.Errorf("decoding attachments response: %w", err)
		}
		resp.Body.Close()

		for _, att := range result.Results {
			fileID := att.Extensions.FileID
			if fileID == "" {
				fileID = att.ID
			}
			attachments = append(attachments, Attachment{
				ID:           att.ID,
				FileID:       fileID,
				Title:        att.Title,
				MediaType:    att.Extensions.MediaType,
				DownloadLink: att.Links.Download,
			})
		}

		if result.Size < 50 {
			break
		}
		start += 50
	}

	return attachments, nil
}

// DownloadAttachment faz download de um attachment pelo link relativo.
func (c *ServerClient) DownloadAttachment(downloadLink string) ([]byte, error) {
	base := strings.TrimRight(c.config.BaseURL, "/")
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

// ApproveWorkflow aprova uma página via Comala Document Management API.
// O approvalName é o nome da aprovação no workflow (ex: "Review").
// Retorna nil se a aprovação foi bem-sucedida ou se o workflow não está configurado (404).
func (c *ServerClient) ApproveWorkflow(pageID, approvalName string) error {
	body := map[string]any{"name": approvalName}
	jsonBody, err := json.Marshal(body)
	if err != nil {
		return err
	}

	base := strings.TrimRight(c.config.BaseURL, "/")
	reqURL := fmt.Sprintf("%s/rest/cw/1/content/%s/approvals/approve", base, pageID)
	req, err := http.NewRequest("PATCH", reqURL, bytes.NewReader(jsonBody))
	if err != nil {
		return err
	}

	resp, err := c.doRequest(req)
	if err != nil {
		return fmt.Errorf("approving page: %w", err)
	}
	defer resp.Body.Close()

	// 404 = workflow não configurado na página (não é erro)
	if resp.StatusCode == 404 {
		return nil
	}

	if resp.StatusCode != 200 {
		return c.handleErrorResponse(resp)
	}

	return nil
}
