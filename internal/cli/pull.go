// Copyright 2026 md2confl contributors
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"unicode"

	"github.com/fmnapoli/md2confl/adf"
	"github.com/fmnapoli/md2confl/adftomd"
	"github.com/fmnapoli/md2confl/confluence"
)

// pullClient abstracts the Confluence API methods needed by pull.
type pullClient interface {
	GetPage(pageID string) (*confluence.PageResponse, error)
	FindByTitle(spaceID, title string) (*confluence.PageResponse, error)
	ResolveSpaceID(spaceKey string) (string, error)
	GetChildren(pageID string) ([]confluence.ChildPage, error)
	GetAttachments(pageID string) ([]confluence.Attachment, error)
	DownloadAttachment(downloadLink string) ([]byte, error)
}

// PullResult holds the outcome of pulling a single page.
type PullResult struct {
	PageID   string `json:"pageId"`
	Title    string `json:"title"`
	FilePath string `json:"filePath"`
	Action   string `json:"action"`   // "written", "skipped" (dry-run)
	Children int    `json:"children"` // count of child pages pulled (recursive only)
}

// PullOutput is the top-level JSON output for pull.
type PullOutput struct {
	Status      string       `json:"status"`
	Pages       []PullResult `json:"pages,omitempty"`
	Attachments int          `json:"attachments,omitempty"`
	Warnings    []string     `json:"warnings,omitempty"`
}

// PullErrorOutput is the JSON error output.
type PullErrorOutput struct {
	Status  string `json:"status"`
	Code    int    `json:"code"`
	Message string `json:"message"`
	Hint    string `json:"hint,omitempty"`
}

// pageNode is an internal tree structure for recursive pull.
type pageNode struct {
	id       string
	title    string
	adfBody  string
	children []*pageNode
}

// pullEnv holds the pull subcommand state.
type pullEnv struct {
	pageID          string
	title           string
	space           string
	recursive       bool
	depth           int
	outputDir       string
	skipAttachments bool
	dryRun          bool
	jsonOutput      bool
	url             string
	email           string
	token           string
	configPath      string
	verbose         bool
	showVersion     bool

	config *PullConfig

	// client is set during run(); tests inject a mock here.
	client pullClient

	version     string
	stdout      io.Writer
	stderr      io.Writer
	logger      *slog.Logger
	warnings    []string
	results     []PullResult
	attachCount int
}

// runPullCommand is the entry point for the "pull" subcommand.
func runPullCommand(args []string, version string, stdout, stderr io.Writer) int {
	env := &pullEnv{
		version: version,
		stdout:  stdout,
		stderr:  stderr,
		depth:   10,
	}

	if err := env.parseFlags(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		printError(stderr, err.Error(), "", 1, false)
		return 1
	}

	logLevel := slog.LevelInfo
	if env.verbose {
		logLevel = slog.LevelDebug
	}
	env.logger = slog.New(slog.NewTextHandler(stderr, &slog.HandlerOptions{Level: logLevel}))

	if err := env.run(); err != nil {
		code := 1
		var ae *apiError
		if errors.As(err, &ae) {
			code = ae.exitCode
			if env.jsonOutput {
				enc := json.NewEncoder(stdout)
				enc.SetIndent("", "  ")
				enc.Encode(PullErrorOutput{
					Status:  "error",
					Code:    code,
					Message: ae.Error(),
					Hint:    ae.hint,
				})
			} else {
				printError(stderr, ae.Error(), ae.hint, code, false)
			}
		} else {
			if env.jsonOutput {
				enc := json.NewEncoder(stdout)
				enc.SetIndent("", "  ")
				enc.Encode(PullErrorOutput{
					Status:  "error",
					Code:    code,
					Message: err.Error(),
				})
			} else {
				printError(stderr, err.Error(), "", code, false)
			}
		}
		env.printWarnings()
		return code
	}

	// Print JSON output on success
	if env.jsonOutput {
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		enc.Encode(PullOutput{
			Status:      "success",
			Pages:       env.results,
			Attachments: env.attachCount,
			Warnings:    env.warnings,
		})
	}

	env.printWarnings()
	return 0
}

func (env *pullEnv) parseFlags(args []string) error {
	fs := flag.NewFlagSet("md2confl pull", flag.ContinueOnError)
	fs.SetOutput(env.stderr)

	fs.StringVar(&env.pageID, "page-id", "", "Confluence page ID to pull")
	fs.StringVar(&env.title, "title", "", "Page title to search (requires --space)")
	fs.StringVar(&env.space, "space", "", "Confluence space key")
	fs.BoolVar(&env.recursive, "recursive", false, "Pull child pages recursively")
	fs.IntVar(&env.depth, "depth", 10, "Max recursion depth (1-100)")
	fs.StringVar(&env.outputDir, "output-dir", ".", "Destination directory")
	fs.BoolVar(&env.skipAttachments, "skip-attachments", false, "Skip downloading image attachments")
	fs.BoolVar(&env.dryRun, "dry-run", false, "Preview without writing files")
	fs.BoolVar(&env.jsonOutput, "json", false, "JSON output format")
	fs.StringVar(&env.url, "url", "", "Confluence base URL")
	fs.StringVar(&env.email, "email", "", "Atlassian account email")
	fs.StringVar(&env.token, "token", "", "Atlassian API token")
	fs.StringVar(&env.configPath, "config", "", "Config file path (default: auto-detect .confl2md.yml)")
	fs.BoolVar(&env.verbose, "verbose", false, "Debug logging to stderr")
	fs.BoolVar(&env.showVersion, "version", false, "Show version")

	fs.Usage = func() {
		fmt.Fprintf(env.stderr, "md2confl pull — Pull Confluence pages to local Markdown\n\n")
		fmt.Fprintf(env.stderr, "Usage:\n")
		fmt.Fprintf(env.stderr, "  md2confl pull --page-id <id> [options]\n")
		fmt.Fprintf(env.stderr, "  md2confl pull --title <name> --space <key> [options]\n")
		fmt.Fprintf(env.stderr, "  md2confl pull --config <path>\n\n")
		fmt.Fprintf(env.stderr, "Flags:\n")
		fs.PrintDefaults()
	}

	if err := fs.Parse(args); err != nil {
		return err
	}

	if env.showVersion {
		fmt.Fprintf(env.stdout, "md2confl %s\n", env.version)
		return flag.ErrHelp
	}

	explicitFlags := map[string]bool{}
	fs.Visit(func(f *flag.Flag) {
		explicitFlags[f.Name] = true
	})

	// Load config (explicit or auto-detect)
	if env.configPath != "" {
		cfg, err := loadPullConfig(env.configPath)
		if err != nil {
			return err
		}
		env.config = cfg
	} else if found := findPullConfig(); found != "" {
		cfg, err := loadPullConfig(found)
		if err != nil {
			return err
		}
		env.config = cfg
		env.configPath = found
		fmt.Fprintf(env.stderr, "Using config: %s\n", found)
	}

	// Apply config values for fields not set by flags
	if env.config != nil {
		if !explicitFlags["url"] && env.config.URL != "" {
			env.url = env.config.URL
		}
		if !explicitFlags["space"] && env.config.Space != "" {
			env.space = env.config.Space
		}
		if !explicitFlags["email"] && env.config.Email != "" {
			env.email = env.config.Email
		}
	}

	// Env var fallbacks
	if env.url == "" {
		env.url = os.Getenv("CONFLUENCE_URL")
	}
	if env.email == "" {
		env.email = os.Getenv("CONFLUENCE_EMAIL")
	}
	if env.token == "" {
		env.token = os.Getenv("CONFLUENCE_TOKEN")
	} else if explicitFlags["token"] {
		fmt.Fprintf(env.stderr, "Warning: passing API token via CLI flag is insecure; prefer CONFLUENCE_TOKEN env var\n")
	}

	return env.validateFlags()
}

func (env *pullEnv) validateFlags() error {
	// If using config with pages, no page-id/title required on CLI
	if env.config != nil && len(env.config.Pages) > 0 && env.pageID == "" && env.title == "" {
		return nil
	}

	if env.pageID == "" && env.title == "" {
		return fmt.Errorf("--page-id or --title is required (or use --config with pages)")
	}
	if env.pageID != "" && env.title != "" {
		return fmt.Errorf("--page-id and --title are mutually exclusive")
	}
	if env.title != "" && env.space == "" {
		return fmt.Errorf("--title requires --space")
	}
	if env.depth < 1 || env.depth > 100 {
		return fmt.Errorf("--depth must be between 1 and 100")
	}
	if env.url == "" {
		return fmt.Errorf("--url or CONFLUENCE_URL is required")
	}
	if env.email == "" {
		return fmt.Errorf("--email or CONFLUENCE_EMAIL is required")
	}
	if env.token == "" {
		return fmt.Errorf("--token or CONFLUENCE_TOKEN is required")
	}
	return nil
}

func (env *pullEnv) run() error {
	// Create real API client if not injected (tests inject mocks)
	if env.client == nil {
		client, err := confluence.NewClient(confluence.Config{
			BaseURL:  env.url,
			SpaceKey: env.space,
			Email:    env.email,
			Token:    env.token,
		})
		if err != nil {
			return &apiError{message: err.Error(), exitCode: 1}
		}
		client.SetLogger(env.logger)
		env.client = client
	}

	// Config-based pull: iterate pages[]
	if env.config != nil && len(env.config.Pages) > 0 && env.pageID == "" && env.title == "" {
		return env.runConfigPages()
	}

	// Resolve page ID from title if needed
	pageID := env.pageID
	if env.title != "" {
		spaceID, err := env.client.ResolveSpaceID(env.space)
		if err != nil {
			return env.wrapConfluenceErr(err)
		}
		page, err := env.client.FindByTitle(spaceID, env.title)
		if err != nil {
			return env.wrapConfluenceErr(err)
		}
		if page == nil {
			return &apiError{
				message:  fmt.Sprintf("page not found: %q in space %s", env.title, env.space),
				hint:     "verify the page title and space key are correct",
				exitCode: 2,
			}
		}
		pageID = page.ID
	}

	var err error
	if env.recursive {
		err = env.pullRecursive(pageID, env.outputDir, env.depth)
	} else {
		err = env.pullSinglePage(pageID, env.outputDir)
	}
	if err != nil {
		return err
	}

	// Rewrite inter-document links (Confluence URLs → local relative paths)
	if !env.dryRun {
		env.rewriteInterDocLinks()
	}
	return nil
}

// pullSinglePage fetches a page by ID and writes it as Markdown.
func (env *pullEnv) pullSinglePage(pageID, outputDir string) error {
	page, err := env.client.GetPage(pageID)
	if err != nil {
		return env.wrapConfluenceErr(err)
	}

	// Fetch attachments for file ID resolution before converting
	var atts []confluence.Attachment
	var fileIDMap map[string]string
	if !env.skipAttachments {
		atts, fileIDMap = env.fetchAttachments(pageID)
	}

	adfJSON := page.Body.AtlasDocFormat.Value
	md := env.convertPageToMarkdown(pageID, page.Title, adfJSON, fileIDMap)

	filename := sanitizeFilename(page.Title) + ".md"
	filePath := filepath.Join(outputDir, filename)

	if env.dryRun {
		if !env.jsonOutput {
			fmt.Fprintf(env.stdout, "Would write: %s (%s, %d bytes)\n", filePath, page.Title, len(md))
		}
		env.results = append(env.results, PullResult{
			PageID:   pageID,
			Title:    page.Title,
			FilePath: filePath,
			Action:   "skipped",
		})
		return nil
	}

	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return fmt.Errorf("creating output directory: %w", err)
	}
	if err := os.WriteFile(filePath, md, 0644); err != nil {
		return fmt.Errorf("writing %s: %w", filePath, err)
	}

	// Download attachment files
	if !env.skipAttachments {
		env.downloadAttachments(atts, outputDir)
	}

	if !env.jsonOutput {
		fmt.Fprintf(env.stdout, "Pulled: %q → %s\n", page.Title, filePath)
	}
	env.results = append(env.results, PullResult{
		PageID:   pageID,
		Title:    page.Title,
		FilePath: filePath,
		Action:   "written",
	})
	return nil
}

// convertPageToMarkdown converts an ADF page to Markdown with page-id marker and H1 title.
// fileIDMap maps Confluence file UUIDs to attachment filenames (for type:"file" media nodes).
func (env *pullEnv) convertPageToMarkdown(pageID, title, adfJSON string, fileIDMap map[string]string) []byte {
	var buf strings.Builder

	// Page-id marker
	fmt.Fprintf(&buf, "<!-- confluence-page-id: %s -->\n", pageID)

	// Convert ADF body
	if adfJSON != "" {
		var doc adf.Document
		if err := json.Unmarshal([]byte(adfJSON), &doc); err == nil {
			// Check if ADF body starts with an H1 matching the page title
			if !adfStartsWithTitle(&doc, title) {
				fmt.Fprintf(&buf, "# %s\n\n", title)
			}

			opts := adftomd.Options{}
			if !env.skipAttachments {
				opts.ImageRewriter = func(url string) string {
					parts := strings.Split(url, "/")
					if len(parts) > 0 {
						return "attachments/" + parts[len(parts)-1]
					}
					return url
				}
			}
			if len(fileIDMap) > 0 {
				opts.FileIDResolver = func(fileID string) string {
					if filename, ok := fileIDMap[fileID]; ok {
						return "attachments/" + filename
					}
					return ""
				}
			}
			body := adftomd.ConvertWithOptions(&doc, opts)
			buf.Write(body)
		} else {
			// Failed to parse ADF — still prepend title
			fmt.Fprintf(&buf, "# %s\n\n", title)
		}
	} else {
		fmt.Fprintf(&buf, "# %s\n\n", title)
	}

	return []byte(buf.String())
}

// adfStartsWithTitle checks if the ADF document starts with an H1 heading
// whose text matches the page title.
func adfStartsWithTitle(doc *adf.Document, title string) bool {
	if len(doc.Content) == 0 {
		return false
	}
	first := doc.Content[0]
	if first.Type != "heading" {
		return false
	}
	if level, ok := first.Attrs["level"]; ok {
		var l int
		switch v := level.(type) {
		case float64:
			l = int(v)
		case int:
			l = v
		}
		if l != 1 {
			return false
		}
	}
	// Extract plain text from the heading
	var text strings.Builder
	for _, child := range first.Content {
		if child.Type == "text" {
			text.WriteString(child.Text)
		}
	}
	return text.String() == title
}

// pullRecursive builds a page tree and writes it as a directory tree.
func (env *pullEnv) pullRecursive(pageID, outputDir string, maxDepth int) error {
	root, err := env.buildPageTree(pageID, 0, maxDepth)
	if err != nil {
		return err
	}
	return env.writePageTree(root, outputDir)
}

func (env *pullEnv) buildPageTree(pageID string, currentDepth, maxDepth int) (*pageNode, error) {
	page, err := env.client.GetPage(pageID)
	if err != nil {
		return nil, env.wrapConfluenceErr(err)
	}

	node := &pageNode{
		id:      pageID,
		title:   page.Title,
		adfBody: page.Body.AtlasDocFormat.Value,
	}

	if currentDepth >= maxDepth {
		env.addWarning(fmt.Sprintf("depth limit reached at %q (depth %d)", page.Title, currentDepth))
		return node, nil
	}

	children, err := env.client.GetChildren(pageID)
	if err != nil {
		return nil, env.wrapConfluenceErr(err)
	}

	for _, child := range children {
		childNode, err := env.buildPageTree(child.ID, currentDepth+1, maxDepth)
		if err != nil {
			return nil, err
		}
		node.children = append(node.children, childNode)
	}

	return node, nil
}

func (env *pullEnv) writePageTree(node *pageNode, outputDir string) error {
	hasChildren := len(node.children) > 0

	// Fetch attachments for file ID resolution
	var atts []confluence.Attachment
	var fileIDMap map[string]string
	if !env.skipAttachments {
		atts, fileIDMap = env.fetchAttachments(node.id)
	}

	if hasChildren {
		// Page with children → directory with README.md
		dirName := sanitizeFilename(node.title)
		dirPath := filepath.Join(outputDir, dirName)
		md := env.convertPageToMarkdown(node.id, node.title, node.adfBody, fileIDMap)

		if env.dryRun {
			filePath := filepath.Join(dirPath, "README.md")
			if !env.jsonOutput {
				fmt.Fprintf(env.stdout, "Would write: %s (%s, %d bytes)\n", filePath, node.title, len(md))
			}
			env.results = append(env.results, PullResult{
				PageID:   node.id,
				Title:    node.title,
				FilePath: filePath,
				Action:   "skipped",
				Children: len(node.children),
			})
		} else {
			if err := os.MkdirAll(dirPath, 0755); err != nil {
				return fmt.Errorf("creating directory %s: %w", dirPath, err)
			}
			filePath := filepath.Join(dirPath, "README.md")
			if err := os.WriteFile(filePath, md, 0644); err != nil {
				return fmt.Errorf("writing %s: %w", filePath, err)
			}
			if !env.skipAttachments {
				env.downloadAttachments(atts, dirPath)
			}
			if !env.jsonOutput {
				fmt.Fprintf(env.stdout, "Pulled: %q → %s\n", node.title, filePath)
			}
			env.results = append(env.results, PullResult{
				PageID:   node.id,
				Title:    node.title,
				FilePath: filePath,
				Action:   "written",
				Children: len(node.children),
			})
		}

		// Write children
		for _, child := range node.children {
			if err := env.writePageTree(child, dirPath); err != nil {
				return err
			}
		}
	} else {
		// Leaf page → {sanitized-title}.md
		filename := sanitizeFilename(node.title) + ".md"
		filePath := filepath.Join(outputDir, filename)
		md := env.convertPageToMarkdown(node.id, node.title, node.adfBody, fileIDMap)

		if env.dryRun {
			if !env.jsonOutput {
				fmt.Fprintf(env.stdout, "Would write: %s (%s, %d bytes)\n", filePath, node.title, len(md))
			}
			env.results = append(env.results, PullResult{
				PageID:   node.id,
				Title:    node.title,
				FilePath: filePath,
				Action:   "skipped",
			})
		} else {
			if err := os.MkdirAll(outputDir, 0755); err != nil {
				return fmt.Errorf("creating directory %s: %w", outputDir, err)
			}
			if err := os.WriteFile(filePath, md, 0644); err != nil {
				return fmt.Errorf("writing %s: %w", filePath, err)
			}
			if !env.skipAttachments {
				env.downloadAttachments(atts, outputDir)
			}
			if !env.jsonOutput {
				fmt.Fprintf(env.stdout, "Pulled: %q → %s\n", node.title, filePath)
			}
			env.results = append(env.results, PullResult{
				PageID:   node.id,
				Title:    node.title,
				FilePath: filePath,
				Action:   "written",
			})
		}
	}

	return nil
}

// fetchAttachments returns the attachment list and builds a fileID→filename map.
func (env *pullEnv) fetchAttachments(pageID string) ([]confluence.Attachment, map[string]string) {
	atts, err := env.client.GetAttachments(pageID)
	if err != nil {
		env.addWarning(fmt.Sprintf("failed to list attachments for page %s: %v", pageID, err))
		return nil, nil
	}
	fileIDMap := make(map[string]string)
	for _, att := range atts {
		if att.FileID != "" {
			fileIDMap[att.FileID] = att.Title
		}
	}
	return atts, fileIDMap
}

// downloadAttachments fetches image attachments for a page and saves them locally.
func (env *pullEnv) downloadAttachments(atts []confluence.Attachment, outputDir string) {
	for _, att := range atts {
		if !strings.HasPrefix(att.MediaType, "image/") {
			continue
		}
		data, err := env.client.DownloadAttachment(att.DownloadLink)
		if err != nil {
			env.addWarning(fmt.Sprintf("failed to download %s: %v", att.Title, err))
			continue
		}
		attDir := filepath.Join(outputDir, "attachments")
		if err := os.MkdirAll(attDir, 0755); err != nil {
			env.addWarning(fmt.Sprintf("failed to create attachments dir: %v", err))
			continue
		}
		if err := os.WriteFile(filepath.Join(attDir, att.Title), data, 0644); err != nil {
			env.addWarning(fmt.Sprintf("failed to save %s: %v", att.Title, err))
			continue
		}
		env.attachCount++
	}
}

// runConfigPages iterates over pages[] in the config file.
func (env *pullEnv) runConfigPages() error {
	for _, page := range env.config.Pages {
		pageID := page.PageID
		outputDir := page.OutputDir
		if outputDir == "" {
			outputDir = "."
		}

		if page.Title != "" {
			spaceID, err := env.client.ResolveSpaceID(env.space)
			if err != nil {
				return env.wrapConfluenceErr(err)
			}
			found, err := env.client.FindByTitle(spaceID, page.Title)
			if err != nil {
				return env.wrapConfluenceErr(err)
			}
			if found == nil {
				env.addWarning(fmt.Sprintf("page not found: %q", page.Title))
				continue
			}
			pageID = found.ID
		}

		depth := page.Depth
		if depth == 0 {
			depth = 10
		}

		if page.Recursive {
			if err := env.pullRecursive(pageID, outputDir, depth); err != nil {
				return err
			}
		} else {
			if err := env.pullSinglePage(pageID, outputDir); err != nil {
				return err
			}
		}
	}

	// Rewrite inter-document links (Confluence URLs → local relative paths)
	if !env.dryRun {
		env.rewriteInterDocLinks()
	}

	if !env.jsonOutput {
		fmt.Fprintf(env.stdout, "\n%d page(s) pulled successfully.\n", len(env.results))
	}
	return nil
}

// confluencePageURLPattern matches Confluence page URLs like:
//
//	https://site.atlassian.net/wiki/spaces/KEY/pages/12345
//	https://site.atlassian.net/wiki/spaces/KEY/pages/12345/Page+Title
//	https://site.atlassian.net/wiki/spaces/KEY/pages/12345/Page+Title#fragment
//
// Capture groups: (1) page ID, (2) optional fragment including #
var confluencePageURLPattern = regexp.MustCompile(`https?://[^/]+/wiki/spaces/[^/]+/pages/(\d+)(?:/[^\s)#]*)?(#[^\s)]*)?`)

// rewriteInterDocLinks rewrites Confluence page URLs in pulled files to local relative paths.
// This is the reverse of the inter-document link resolution done during publish.
func (env *pullEnv) rewriteInterDocLinks() {
	if len(env.results) < 2 {
		return // nothing to cross-link with a single page
	}

	// Build pageID → filePath map from pull results
	pageMap := make(map[string]string)
	for _, r := range env.results {
		if r.Action == "written" {
			pageMap[r.PageID] = r.FilePath
		}
	}

	for _, r := range env.results {
		if r.Action != "written" {
			continue
		}
		content, err := os.ReadFile(r.FilePath)
		if err != nil {
			continue
		}

		original := string(content)
		result := confluencePageURLPattern.ReplaceAllStringFunc(original, func(match string) string {
			m := confluencePageURLPattern.FindStringSubmatch(match)
			if len(m) < 2 {
				return match
			}
			targetPageID := m[1]
			fragment := ""
			if len(m) >= 3 {
				fragment = m[2] // includes the # prefix
			}

			targetPath, ok := pageMap[targetPageID]
			if !ok {
				return match // target page not pulled — keep external URL
			}

			// Compute relative path from current file's directory to target file
			fromDir := filepath.Dir(r.FilePath)
			relPath, err := filepath.Rel(fromDir, targetPath)
			if err != nil {
				return match
			}
			return relPath + fragment
		})

		if result != original {
			if err := os.WriteFile(r.FilePath, []byte(result), 0644); err != nil {
				env.addWarning(fmt.Sprintf("failed to rewrite links in %s: %v", r.FilePath, err))
				continue
			}
			count := strings.Count(original, "https://") - strings.Count(result, "https://")
			if count > 0 && !env.jsonOutput {
				fmt.Fprintf(env.stdout, "Resolved %d inter-document link(s) in %q\n", count, filepath.Base(r.FilePath))
			}
		}
	}
}

func (env *pullEnv) addWarning(msg string) {
	env.warnings = append(env.warnings, msg)
}

func (env *pullEnv) printWarnings() {
	if len(env.warnings) == 0 {
		return
	}
	fmt.Fprintf(env.stderr, "\n%d warning(s):\n", len(env.warnings))
	for _, msg := range env.warnings {
		fmt.Fprintf(env.stderr, "  - %s\n", msg)
	}
}

// sanitizeFilename converts a page title to a valid, filesystem-safe filename.
func sanitizeFilename(title string) string {
	invalid := regexp.MustCompile(`[/\\:*?"<>|]`)
	result := invalid.ReplaceAllString(title, "-")

	var buf strings.Builder
	for _, r := range result {
		if unicode.IsControl(r) || unicode.IsSpace(r) {
			buf.WriteRune('-')
		} else {
			buf.WriteRune(r)
		}
	}
	result = buf.String()

	result = strings.ToLower(result)

	multi := regexp.MustCompile(`-{2,}`)
	result = multi.ReplaceAllString(result, "-")

	result = strings.Trim(result, "-")

	if len(result) > 200 {
		result = result[:200]
		result = strings.TrimRight(result, "-")
	}

	if result == "" {
		result = "untitled"
	}

	return result
}

// wrapConfluenceErr converts a confluence.APIError into a CLI apiError.
func (env *pullEnv) wrapConfluenceErr(err error) error {
	var confErr *confluence.APIError
	if errors.As(err, &confErr) {
		return &apiError{
			message:  confErr.Message,
			hint:     confErr.Hint,
			exitCode: confErr.ExitCode(),
		}
	}
	return &apiError{message: err.Error(), exitCode: 2}
}
