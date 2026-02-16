// Copyright 2026 md2confl contributors
// SPDX-License-Identifier: Apache-2.0

// Package cli implements the md2confl command-line interface.
// It wires together flag parsing, Markdown→ADF conversion, and Confluence
// publishing. This package is internal — only cmd/md2confl/main.go imports it.
package cli

import (
	"bytes"
	"context"
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
	"sync"

	"golang.org/x/sync/errgroup"

	"github.com/fmnapoli/md2confl/adf"
	"github.com/fmnapoli/md2confl/confluence"
	"github.com/fmnapoli/md2confl/mermaid"
	"github.com/fmnapoli/md2confl/parser"
)

// docPublishResult holds the publish outcome for a single document,
// used to resolve inter-document links in the second pass.
type docPublishResult struct {
	pageID   string
	pageURL  string
	title    string
	finalADF *adf.Document
}

type appEnv struct {
	input       string
	output      string
	dryRun      bool
	publish     bool
	url         string
	space       string
	parentID    string
	title       string
	email       string
	token       string
	force       bool
	writeMarker    bool
	jsonOutput     bool
	renderMermaid  bool
	showVersion    bool
	verbose        bool
	concurrency    int
	configPath  string
	repoURL     string
	repoRoot    string
	config       *Config
	docResults   map[string]*docPublishResult // abs input path → result
	docResultsMu *sync.Mutex
	warnings     *[]string
	warningsMu   *sync.Mutex

	version  string
	stdout   io.Writer
	stderr   io.Writer
	outputMu *sync.Mutex
	logger   *slog.Logger
}

// Run parses CLI arguments and executes the requested operation.
// Returns an exit code: 0 for success, 1 for user error, 2 for API error.
func Run(args []string, version string, stdout, stderr io.Writer) int {
	warnings := make([]string, 0)
	outputMu := &sync.Mutex{}
	app := &appEnv{
		version:    version,
		stdout:     stdout,
		stderr:     stderr,
		outputMu:   outputMu,
		warnings:   &warnings,
		warningsMu: &sync.Mutex{},
	}

	if err := app.fromArgs(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		printError(stderr, err.Error(), "", 1, false)
		return 1
	}

	// Configure structured logger
	logLevel := slog.LevelInfo
	if app.verbose {
		logLevel = slog.LevelDebug
	}
	app.logger = slog.New(slog.NewTextHandler(stderr, &slog.HandlerOptions{Level: logLevel}))

	if err := app.run(); err != nil {
		code := 1
		var apiErr *apiError
		if errors.As(err, &apiErr) {
			code = apiErr.exitCode
			printError(stderr, apiErr.Error(), apiErr.hint, code, app.jsonOutput)
		} else {
			printError(stderr, err.Error(), "", code, app.jsonOutput)
		}
		app.printWarningSummary()
		return code
	}
	app.printWarningSummary()
	return 0
}

type apiError struct {
	message  string
	hint     string
	exitCode int
}

func (e *apiError) Error() string { return e.message }

func (app *appEnv) fromArgs(args []string) error {
	fs := flag.NewFlagSet("md2confl", flag.ContinueOnError)
	fs.SetOutput(app.stderr)

	fs.StringVar(&app.input, "input", "", "Path to Markdown file or directory")
	fs.StringVar(&app.output, "output", "", "Path to output ADF JSON file")
	fs.BoolVar(&app.dryRun, "dry-run", false, "Preview ADF output without publishing")
	fs.BoolVar(&app.publish, "publish", false, "Publish to Confluence Cloud")
	fs.StringVar(&app.url, "url", "", "Confluence base URL (e.g., https://site.atlassian.net)")
	fs.StringVar(&app.space, "space", "", "Confluence space key")
	fs.StringVar(&app.parentID, "parent-id", "", "Parent page ID")
	fs.StringVar(&app.title, "title", "", "Page title (default: first H1 or filename)")
	fs.StringVar(&app.email, "email", "", "Atlassian account email")
	fs.StringVar(&app.token, "token", "", "Atlassian API token")
	fs.BoolVar(&app.force, "force", false, "Overwrite existing page with same title")
	fs.BoolVar(&app.writeMarker, "write-marker", false, "Write page ID marker back to Markdown file")
	fs.BoolVar(&app.renderMermaid, "mermaid", false, "Render mermaid diagrams to SVG (always enabled with --publish)")
	fs.BoolVar(&app.jsonOutput, "json", false, "Output in JSON format")
	fs.BoolVar(&app.showVersion, "version", false, "Show version")
	fs.BoolVar(&app.verbose, "verbose", false, "Enable verbose debug logging")
	fs.IntVar(&app.concurrency, "concurrency", 4, "Max parallel operations (1-16)")
	fs.StringVar(&app.configPath, "config", "", "Path to config file (default: auto-detect .md2confl.yml)")
	fs.StringVar(&app.repoURL, "repo-url", "", "Repository base URL for resolving non-Markdown links (auto-detected from git)")

	fs.Usage = func() {
		fmt.Fprintf(app.stderr, "md2confl — Convert Markdown to Confluence ADF and publish\n\n")
		fmt.Fprintf(app.stderr, "Usage:\n")
		fmt.Fprintf(app.stderr, "  md2confl --input <path> [options]\n")
		fmt.Fprintf(app.stderr, "  md2confl --config <path> [options]\n\n")
		fmt.Fprintf(app.stderr, "Examples:\n")
		fmt.Fprintf(app.stderr, "  md2confl --input doc.md --output doc.json     Convert to ADF file\n")
		fmt.Fprintf(app.stderr, "  md2confl --input doc.md --dry-run             Preview ADF in terminal\n")
		fmt.Fprintf(app.stderr, "  md2confl --input doc.md --dry-run --mermaid   Preview with Mermaid → SVG\n")
		fmt.Fprintf(app.stderr, "  md2confl --input doc.md --publish \\           Publish to Confluence\n")
		fmt.Fprintf(app.stderr, "    --url https://site.atlassian.net \\\n")
		fmt.Fprintf(app.stderr, "    --space DEVOPS --title \"My Page\"\n")
		fmt.Fprintf(app.stderr, "  md2confl --input docs/ --publish \\            Publish folder hierarchy\n")
		fmt.Fprintf(app.stderr, "    --url https://site.atlassian.net \\\n")
		fmt.Fprintf(app.stderr, "    --space DEVOPS\n")
		fmt.Fprintf(app.stderr, "  md2confl --config .md2confl.yml               Process all config documents\n")
		fmt.Fprintf(app.stderr, "  md2confl --config .md2confl.yml --input x.md  Process single config document\n\n")
		fmt.Fprintf(app.stderr, "Flags:\n")
		fs.PrintDefaults()
		fmt.Fprintf(app.stderr, "\nEnvironment variables:\n")
		fmt.Fprintf(app.stderr, "  CONFLUENCE_URL     Base URL (equivalent to --url)\n")
		fmt.Fprintf(app.stderr, "  CONFLUENCE_EMAIL   Account email (equivalent to --email)\n")
		fmt.Fprintf(app.stderr, "  CONFLUENCE_TOKEN   API token (equivalent to --token)\n")
	}

	if err := fs.Parse(args); err != nil {
		return err
	}

	if app.showVersion {
		fmt.Fprintf(app.stdout, "md2confl %s\n", app.version)
		return flag.ErrHelp
	}

	if len(args) == 0 {
		fs.Usage()
		return fmt.Errorf("no arguments provided")
	}

	// Collect explicitly set flags for precedence
	explicitFlags := map[string]bool{}
	fs.Visit(func(f *flag.Flag) {
		explicitFlags[f.Name] = true
	})

	// Load config: explicit --config or auto-discovery
	if app.configPath != "" {
		cfg, err := loadConfig(app.configPath)
		if err != nil {
			return err
		}
		app.config = cfg
	} else if found := findConfig(); found != "" {
		cfg, err := loadConfig(found)
		if err != nil {
			return err
		}
		app.config = cfg
		app.configPath = found
		// Logger not initialized yet; use stderr directly for pre-init messages.
		fmt.Fprintf(app.stderr, "Using config: %s\n", found)
	}

	// Apply config globals (fills gaps not set by flags)
	app.applyConfig(explicitFlags)

	// --input is optional when config has documents
	if app.input == "" && (app.config == nil || len(app.config.Documents) == 0) {
		return fmt.Errorf("--input is required (or define documents in config file)")
	}

	if app.concurrency < 1 || app.concurrency > 16 {
		return fmt.Errorf("--concurrency must be between 1 and 16")
	}

	if app.output != "" && app.publish {
		return fmt.Errorf("--output and --publish are mutually exclusive")
	}
	if app.output != "" && app.dryRun {
		return fmt.Errorf("--output and --dry-run are mutually exclusive")
	}
	if app.writeMarker && !app.publish {
		// In config mode, write-marker can be set via config without --publish flag
		// since publish is determined per-document
		if app.config == nil {
			return fmt.Errorf("--write-marker requires --publish")
		}
	}
	if app.force && !app.publish {
		if app.config == nil {
			return fmt.Errorf("--force requires --publish")
		}
	}

	// Resolve credentials from env vars if not set via flags or config
	if app.url == "" {
		app.url = os.Getenv("CONFLUENCE_URL")
	}
	if app.email == "" {
		app.email = os.Getenv("CONFLUENCE_EMAIL")
	}
	if app.token == "" {
		app.token = os.Getenv("CONFLUENCE_TOKEN")
	} else if explicitFlags["token"] {
		// Logger not initialized yet; use stderr directly for pre-init messages.
		fmt.Fprintf(app.stderr, "Warning: passing API token via CLI flag is insecure; prefer CONFLUENCE_TOKEN env var\n")
	}

	// Only validate publish requirements when --publish is explicitly set (not in config multi-doc mode)
	if app.publish && app.input != "" {
		if app.url == "" {
			return fmt.Errorf("--publish requires --url or CONFLUENCE_URL")
		}
		if app.space == "" {
			return fmt.Errorf("--publish requires --space")
		}
		if app.email == "" {
			return fmt.Errorf("--publish requires --email or CONFLUENCE_EMAIL")
		}
		if app.token == "" {
			return fmt.Errorf("--publish requires --token or CONFLUENCE_TOKEN")
		}
	}

	return nil
}

func (app *appEnv) run() error {
	// Auto-detect repo URL for resolving non-Markdown links
	if app.repoURL == "" {
		app.repoURL, app.repoRoot = detectRepoURL()
	}
	// Ensure trailing slash for correct URL concatenation
	if app.repoURL != "" && !strings.HasSuffix(app.repoURL, "/") {
		app.repoURL += "/"
	}
	if app.repoURL != "" && app.repoRoot == "" {
		// Explicit repo-url but no root — try git, fall back to .git walk
		app.repoRoot, _ = gitOutput("rev-parse", "--show-toplevel")
		if app.repoRoot == "" {
			app.repoRoot = findRepoRoot()
		}
	}

	// Multi-document mode: no --input but config has documents
	if app.input == "" && app.config != nil && len(app.config.Documents) > 0 {
		return app.runDocuments("")
	}

	// If --input matches a document in config, apply document overrides
	if app.input != "" && app.config != nil && len(app.config.Documents) > 0 {
		for _, doc := range app.config.Documents {
			absInput, _ := filepath.Abs(doc.Input)
			absFlag, _ := filepath.Abs(app.input)
			if absInput == absFlag {
				return app.runDocuments(app.input)
			}
		}
	}

	// Check input path exists
	info, err := os.Stat(app.input)
	if err != nil {
		return fmt.Errorf("input path %q: %w", app.input, err)
	}

	if info.IsDir() {
		return app.runDir()
	}
	return app.runFile(app.input)
}

func (app *appEnv) runFile(path string) error {
	source, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("reading %q: %w", path, err)
	}

	doc, err := parser.ConvertToADF(source)
	if err != nil {
		return fmt.Errorf("converting %q: %w", path, err)
	}

	// Render mermaid diagrams to SVG when requested or publishing.
	if app.publish || app.renderMermaid {
		rendered, err := renderMermaidBlocks(doc)
		if err != nil {
			return err
		}
		if rendered {
			count := countMermaidSVGs(doc)
			app.logger.Info("Rendered mermaid diagrams", "count", count)
		}
	}

	adfJSON, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling ADF: %w", err)
	}

	if app.dryRun {
		return app.handleDryRun(path, adfJSON, doc)
	}

	if app.output != "" {
		return app.handleFileOutput(path, adfJSON)
	}

	if app.publish {
		return app.handlePublish(path, source, adfJSON, doc)
	}

	// Default: dry-run behavior when no output/publish specified
	return app.handleDryRun(path, adfJSON, doc)
}

func (app *appEnv) handleDryRun(path string, adfJSON []byte, doc *adf.Document) error {
	unlock := app.lockOutput()
	defer unlock()

	if app.publish {
		// Show simulation when publish flags are present
		title := app.deriveTitle(path, nil)
		fmt.Fprintf(app.stderr, "Dry-run: would publish to Confluence\n")
		fmt.Fprintf(app.stderr, "  Title: %s\n", title)
		fmt.Fprintf(app.stderr, "  Space: %s\n", app.space)
		if app.parentID != "" {
			fmt.Fprintf(app.stderr, "  Parent: %s\n", app.parentID)
		}
		fmt.Fprintf(app.stderr, "  URL: %s\n", app.url)
		fmt.Fprintf(app.stderr, "\n")
	}

	// Save to docResults for dry-run link preview (config mode)
	if app.docResults != nil && app.output == "" {
		absPath, _ := filepath.Abs(path)
		title := app.deriveTitle(path, nil)
		app.addDocResult(absPath, &docPublishResult{
			title:    title,
			finalADF: doc,
		})
	}

	fmt.Fprintln(app.stdout, string(adfJSON))
	return nil
}

func (app *appEnv) handleFileOutput(path string, adfJSON []byte) error {
	if err := os.WriteFile(app.output, append(adfJSON, '\n'), 0644); err != nil {
		return fmt.Errorf("writing output: %w", err)
	}
	unlock := app.lockOutput()
	defer unlock()
	printResult(app.stdout, Result{
		Status: "success",
		Action: "converted",
		Title:  fmt.Sprintf("%s → %s", filepath.Base(path), app.output),
	}, app.jsonOutput)
	return nil
}

var pageIDRegex = regexp.MustCompile(`<!--\s*confluence-page-id:\s*(\d+)\s*-->`)

func extractPageID(source []byte) string {
	// Only check the first line — the marker is always prepended at the top.
	// This avoids false matches inside code blocks or documentation examples.
	firstLine := source
	if idx := bytes.IndexByte(source, '\n'); idx >= 0 {
		firstLine = source[:idx]
	}
	match := pageIDRegex.FindSubmatch(firstLine)
	if match != nil {
		return string(match[1])
	}
	return ""
}

func (app *appEnv) handlePublish(path string, source, adfJSON []byte, doc *adf.Document) error {
	client, err := confluence.NewClient(confluence.Config{
		BaseURL:  app.url,
		SpaceKey: app.space,
		Email:    app.email,
		Token:    app.token,
	})
	if err != nil {
		return &apiError{message: err.Error(), exitCode: 1}
	}
	client.SetLogger(app.logger)

	// Resolve space ID
	spaceID, err := client.ResolveSpaceID(app.space)
	if err != nil {
		return app.wrapConfluenceError(err)
	}

	title := app.deriveTitle(path, source)
	adfStr := string(adfJSON)
	pageID := extractPageID(source)

	var result *confluence.PublishResult

	if pageID != "" {
		// Update existing page by ID
		page, err := client.GetPage(pageID)
		if err != nil {
			return app.wrapConfluenceError(err)
		}
		if adfUnchanged(page.Body.AtlasDocFormat.Value, adfStr) {
			app.logger.Info("Skipped (unchanged)", "title", title)
			result = &confluence.PublishResult{
				PageID:   page.ID,
				PageURL:  page.Links.Base + page.Links.WebUI,
				Title:    title,
				SpaceKey: app.space,
				Version:  page.Version.Number,
				Action:   "skipped",
			}
		} else {
			result, err = client.UpdatePage(pageID, title, adfStr, page.Version.Number)
			if err != nil {
				return app.wrapConfluenceError(err)
			}
		}
	} else if app.force {
		// Find by title and update, or create
		existing, err := client.FindByTitle(spaceID, title)
		if err != nil {
			return app.wrapConfluenceError(err)
		}
		if existing != nil {
			page, err := client.GetPage(existing.ID)
			if err != nil {
				return app.wrapConfluenceError(err)
			}
			if adfUnchanged(page.Body.AtlasDocFormat.Value, adfStr) {
				app.logger.Info("Skipped (unchanged)", "title", title)
				result = &confluence.PublishResult{
					PageID:   page.ID,
					PageURL:  page.Links.Base + page.Links.WebUI,
					Title:    title,
					SpaceKey: app.space,
					Version:  page.Version.Number,
					Action:   "skipped",
				}
			} else {
				result, err = client.UpdatePage(existing.ID, title, adfStr, existing.Version.Number)
				if err != nil {
					return app.wrapConfluenceError(err)
				}
			}
		} else {
			result, err = client.CreatePage(spaceID, title, app.parentID, adfStr)
			if err != nil {
				return app.wrapConfluenceError(err)
			}
		}
	} else {
		// Create new page
		result, err = client.CreatePage(spaceID, title, app.parentID, adfStr)
		if err != nil {
			return app.wrapConfluenceError(err)
		}
	}

	// Upload local images as attachments and patch ADF
	if err := app.uploadAndPatchImages(client, doc, result, filepath.Dir(path)); err != nil {
		return err
	}

	// Save result for inter-document link resolution (second pass)
	if app.docResults != nil {
		absPath, _ := filepath.Abs(path)
		app.addDocResult(absPath, &docPublishResult{
			pageID:   result.PageID,
			pageURL:  result.PageURL,
			title:    result.Title,
			finalADF: doc,
		})
	}

	// Write page-id marker back to file if requested
	if app.writeMarker && result.PageID != "" {
		if err := app.writePageIDMarker(path, source, result.PageID); err != nil {
			app.logger.Warn("Could not write page-id marker", "error", err)
		}
	}

	app.logger.Info("Published", "title", result.Title, "action", result.Action, "pageID", result.PageID)

	func() {
		unlock := app.lockOutput()
		defer unlock()
		printResult(app.stdout, Result{
			Status:   "success",
			PageID:   result.PageID,
			PageURL:  result.PageURL,
			Title:    result.Title,
			SpaceKey: result.SpaceKey,
			Action:   result.Action,
			Version:  result.Version,
		}, app.jsonOutput)
	}()

	return nil
}

// uploadAndPatchImages finds local images in the ADF, uploads them as
// Confluence attachments, patches the ADF media nodes, and updates the page.
func (app *appEnv) uploadAndPatchImages(client *confluence.Client, doc *adf.Document, result *confluence.PublishResult, basePath string) error {
	localImages := findLocalImages(doc)
	if len(localImages) == 0 {
		return nil
	}

	attachmentMap := map[string]string{} // url -> attachment ID
	var attMu sync.Mutex
	g, ctx := errgroup.WithContext(context.Background())
	g.SetLimit(8)
	for _, imgURL := range localImages {
		g.Go(func() error {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			imgPath := imgURL
			if !filepath.IsAbs(imgURL) {
				imgPath = filepath.Join(basePath, imgURL)
			}
			if _, err := os.Stat(imgPath); err != nil {
				app.logger.Warn("Local image not found", "path", imgPath)
				app.addWarning(fmt.Sprintf("local image not found: %s", imgPath))
				return nil // non-fatal
			}
			attID, err := client.UploadAttachment(result.PageID, imgPath)
			if err != nil {
				app.logger.Warn("Failed to upload image", "image", imgURL, "error", err)
				app.addWarning(fmt.Sprintf("failed to upload %s: %v", imgURL, err))
				return nil // non-fatal
			}
			attMu.Lock()
			attachmentMap[imgURL] = attID
			attMu.Unlock()
			return nil
		})
	}
	_ = g.Wait()

	if len(attachmentMap) > 0 {
		patchLocalImages(doc, attachmentMap, result.PageID)
		patchedJSON, err := json.MarshalIndent(doc, "", "  ")
		if err != nil {
			app.logger.Warn("Failed to marshal patched ADF", "error", err)
			app.addWarning(fmt.Sprintf("failed to marshal patched ADF: %v", err))
			return nil
		}
		currentPage, err := client.GetPage(result.PageID)
		if err != nil {
			app.logger.Warn("Failed to get page for image patch", "pageID", result.PageID, "error", err)
			app.addWarning(fmt.Sprintf("failed to get page %s for image patch: %v", result.PageID, err))
			return nil
		}
		updated, err := client.UpdatePage(result.PageID, result.Title, string(patchedJSON), currentPage.Version.Number)
		if err != nil {
			app.logger.Warn("Failed to update page with patched images", "pageID", result.PageID, "error", err)
			app.addWarning(fmt.Sprintf("failed to update page %s with patched images: %v", result.PageID, err))
			return nil
		}
		*result = *updated
	}
	return nil
}

func (app *appEnv) wrapConfluenceError(err error) error {
	var apiErr *confluence.APIError
	if errors.As(err, &apiErr) {
		return &apiError{
			message:  apiErr.Message,
			hint:     apiErr.Hint,
			exitCode: apiErr.ExitCode(),
		}
	}
	return &apiError{message: err.Error(), exitCode: 2}
}

func (app *appEnv) writePageIDMarker(path string, source []byte, pageID string) error {
	marker := fmt.Sprintf("<!-- confluence-page-id: %s -->", pageID)

	// Preserve original file permissions
	perm := os.FileMode(0644)
	if info, err := os.Stat(path); err == nil {
		perm = info.Mode().Perm()
	}

	// If marker already exists on the first line, replace only that occurrence
	if pageIDRegex.Match(source) {
		firstLine := source
		if idx := bytes.IndexByte(source, '\n'); idx >= 0 {
			firstLine = source[:idx]
		}
		if pageIDRegex.Match(firstLine) {
			updated := make([]byte, 0, len(source))
			updated = append(updated, pageIDRegex.ReplaceAll(firstLine, []byte(marker))...)
			if idx := bytes.IndexByte(source, '\n'); idx >= 0 {
				updated = append(updated, source[idx:]...)
			}
			return os.WriteFile(path, updated, perm)
		}
	}

	// Prepend marker to file
	updated := append([]byte(marker+"\n"), source...)
	return os.WriteFile(path, updated, perm)
}

// DirEntry represents a directory in the hierarchy.
type DirEntry struct {
	Path     string
	Name     string
	Readme   *mdFile
	Files    []mdFile
	Children []DirEntry
}

func (d *DirEntry) hasMarkdown() bool {
	if d.Readme != nil || len(d.Files) > 0 {
		return true
	}
	for i := range d.Children {
		if d.Children[i].hasMarkdown() {
			return true
		}
	}
	return false
}

type mdFile struct {
	Path    string
	Content []byte
}

func (app *appEnv) runDir() error {
	tree, err := buildDirTree(app.input)
	if err != nil {
		return fmt.Errorf("scanning directory %q: %w", app.input, err)
	}

	if !app.publish || app.dryRun {
		// In non-publish or dry-run mode, convert all files and output ADF
		return app.convertDirTree(tree)
	}

	client, err := confluence.NewClient(confluence.Config{
		BaseURL:  app.url,
		SpaceKey: app.space,
		Email:    app.email,
		Token:    app.token,
	})
	if err != nil {
		return &apiError{message: err.Error(), exitCode: 1}
	}
	client.SetLogger(app.logger)

	spaceID, err := client.ResolveSpaceID(app.space)
	if err != nil {
		return app.wrapConfluenceError(err)
	}

	// When called from config mode, docResults is already initialized by
	// runDocuments and shared via shallow copy — don't overwrite it.
	standaloneDir := app.docResults == nil
	if standaloneDir {
		app.docResults = make(map[string]*docPublishResult)
		app.docResultsMu = &sync.Mutex{}
	}

	if err := app.publishDirTree(client, spaceID, app.parentID, tree, true); err != nil {
		return err
	}

	// Second pass: resolve inter-document links (only in standalone dir mode;
	// config mode defers to runDocuments for cross-document resolution).
	if standaloneDir && len(app.docResults) > 1 {
		if err := app.resolveInterDocLinksFromResults(); err != nil {
			return fmt.Errorf("resolving inter-document links: %w", err)
		}
	}

	return nil
}

func buildDirTree(root string) (*DirEntry, error) {
	info, err := os.Stat(root)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("%q is not a directory", root)
	}

	entry := &DirEntry{
		Path: root,
		Name: filepath.Base(root),
	}

	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, err
	}

	for _, e := range entries {
		name := e.Name()
		// Skip hidden directories and common non-content directories
		if strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_") ||
			name == "node_modules" || name == "vendor" {
			continue
		}
		fullPath := filepath.Join(root, name)
		if e.IsDir() {
			child, err := buildDirTree(fullPath)
			if err != nil {
				return nil, err
			}
			if child.hasMarkdown() {
				entry.Children = append(entry.Children, *child)
			}
		} else if strings.HasSuffix(strings.ToLower(e.Name()), ".md") {
			content, err := os.ReadFile(fullPath)
			if err != nil {
				return nil, err
			}
			f := mdFile{Path: fullPath, Content: content}
			if strings.EqualFold(e.Name(), "README.md") {
				entry.Readme = &f
			} else {
				entry.Files = append(entry.Files, f)
			}
		}
	}

	return entry, nil
}

func (app *appEnv) convertDirTree(tree *DirEntry) error {
	// Convert README if present
	if tree.Readme != nil {
		if err := app.runFile(tree.Readme.Path); err != nil {
			return err
		}
	}
	// Convert other files
	for _, f := range tree.Files {
		if err := app.runFile(f.Path); err != nil {
			return err
		}
	}
	// Recurse into children
	for _, child := range tree.Children {
		if err := app.convertDirTree(&child); err != nil {
			return err
		}
	}
	return nil
}

func (app *appEnv) publishDirTree(client *confluence.Client, spaceID, parentID string, tree *DirEntry, isRoot bool) error {
	// Determine content for this directory's page
	var pageContent []byte
	var pagePath string
	var pageSource []byte
	var pageDoc *adf.Document

	if tree.Readme != nil {
		pagePath = tree.Readme.Path
		pageSource = tree.Readme.Content
		doc, err := parser.ConvertToADF(tree.Readme.Content)
		if err != nil {
			return fmt.Errorf("converting %q: %w", pagePath, err)
		}
		pageDoc = doc
		rendered, merr := renderMermaidBlocks(doc)
		if merr != nil {
			return merr
		}
		if rendered {
			app.logger.Info("Rendered mermaid diagrams", "count", countMermaidSVGs(doc), "file", pagePath)
		}
		adfJSON, err := json.MarshalIndent(doc, "", "  ")
		if err != nil {
			return fmt.Errorf("marshaling ADF: %w", err)
		}
		pageContent = adfJSON
	} else {
		// Create empty page for directory
		emptyDoc := adf.NewDocument()
		adfJSON, _ := json.MarshalIndent(emptyDoc, "", "  ")
		pageContent = adfJSON
	}

	// Determine title
	var title string
	if isRoot && app.title != "" {
		title = app.title
	} else if tree.Readme != nil {
		title = app.deriveTitleFromSource(tree.Readme.Content, tree.Name)
	} else {
		title = tree.Name
	}

	// Check for page-id marker
	existingPageID := ""
	if pageSource != nil {
		existingPageID = extractPageID(pageSource)
	}

	adfStr := string(pageContent)
	var dirResult *confluence.PublishResult

	if existingPageID != "" {
		page, err := client.GetPage(existingPageID)
		if err != nil {
			return app.wrapConfluenceError(err)
		}
		if adfUnchanged(page.Body.AtlasDocFormat.Value, adfStr) {
			app.logger.Info("Skipped (unchanged)", "title", title)
			dirResult = &confluence.PublishResult{
				PageID:   page.ID,
				PageURL:  page.Links.Base + page.Links.WebUI,
				Title:    title,
				SpaceKey: app.space,
				Version:  page.Version.Number,
				Action:   "skipped",
			}
		} else {
			dirResult, err = client.UpdatePage(existingPageID, title, adfStr, page.Version.Number)
			if err != nil {
				return app.wrapConfluenceError(err)
			}
		}
	} else if app.force {
		existing, err := client.FindByTitle(spaceID, title)
		if err != nil {
			return app.wrapConfluenceError(err)
		}
		if existing != nil {
			page, err := client.GetPage(existing.ID)
			if err != nil {
				return app.wrapConfluenceError(err)
			}
			if adfUnchanged(page.Body.AtlasDocFormat.Value, adfStr) {
				app.logger.Info("Skipped (unchanged)", "title", title)
				dirResult = &confluence.PublishResult{
					PageID:   page.ID,
					PageURL:  page.Links.Base + page.Links.WebUI,
					Title:    title,
					SpaceKey: app.space,
					Version:  page.Version.Number,
					Action:   "skipped",
				}
			} else {
				dirResult, err = client.UpdatePage(existing.ID, title, adfStr, existing.Version.Number)
				if err != nil {
					return app.wrapConfluenceError(err)
				}
			}
		} else {
			dirResult, err = client.CreatePage(spaceID, title, parentID, adfStr)
			if err != nil {
				return app.wrapConfluenceError(err)
			}
		}
	} else {
		var err error
		dirResult, err = client.CreatePage(spaceID, title, parentID, adfStr)
		if err != nil {
			return app.wrapConfluenceError(err)
		}
	}

	// Upload local images (including mermaid SVGs) for README
	if pageDoc != nil {
		if err := app.uploadAndPatchImages(client, pageDoc, dirResult, filepath.Dir(pagePath)); err != nil {
			return err
		}
	}

	func() {
		unlock := app.lockOutput()
		defer unlock()
		printResult(app.stdout, Result{
			Status: "success", PageID: dirResult.PageID, PageURL: dirResult.PageURL,
			Title: dirResult.Title, SpaceKey: dirResult.SpaceKey, Action: dirResult.Action, Version: dirResult.Version,
		}, app.jsonOutput)
	}()

	// Save result for inter-document link resolution
	if app.docResults != nil && pagePath != "" {
		absPath, _ := filepath.Abs(pagePath)
		app.addDocResult(absPath, &docPublishResult{
			pageID:   dirResult.PageID,
			pageURL:  dirResult.PageURL,
			title:    dirResult.Title,
			finalADF: pageDoc,
		})
	}

	// Write marker for README if requested
	if app.writeMarker && tree.Readme != nil && dirResult.PageID != "" {
		if err := app.writePageIDMarker(tree.Readme.Path, tree.Readme.Content, dirResult.PageID); err != nil {
			app.logger.Warn("Could not write page-id marker", "error", err)
		}
	}

	// Publish other markdown files as children
	for _, f := range tree.Files {
		doc, err := parser.ConvertToADF(f.Content)
		if err != nil {
			return fmt.Errorf("converting %q: %w", f.Path, err)
		}
		rendered, merr := renderMermaidBlocks(doc)
		if merr != nil {
			return merr
		}
		if rendered {
			app.logger.Info("Rendered mermaid diagrams", "count", countMermaidSVGs(doc), "file", f.Path)
		}
		adfJSON, err := json.MarshalIndent(doc, "", "  ")
		if err != nil {
			return fmt.Errorf("marshaling ADF: %w", err)
		}
		childTitle := app.deriveTitleFromSource(f.Content, strings.TrimSuffix(filepath.Base(f.Path), ".md"))
		childPageID := extractPageID(f.Content)
		childADF := string(adfJSON)

		var childResult *confluence.PublishResult
		if childPageID != "" {
			page, err := client.GetPage(childPageID)
			if err != nil {
				return app.wrapConfluenceError(err)
			}
			if adfUnchanged(page.Body.AtlasDocFormat.Value, childADF) {
				app.logger.Info("Skipped (unchanged)", "title", childTitle)
				childResult = &confluence.PublishResult{
					PageID:   page.ID,
					PageURL:  page.Links.Base + page.Links.WebUI,
					Title:    childTitle,
					SpaceKey: app.space,
					Version:  page.Version.Number,
					Action:   "skipped",
				}
			} else {
				childResult, err = client.UpdatePage(childPageID, childTitle, childADF, page.Version.Number)
				if err != nil {
					return app.wrapConfluenceError(err)
				}
			}
		} else if app.force {
			existing, err := client.FindByTitle(spaceID, childTitle)
			if err != nil {
				return app.wrapConfluenceError(err)
			}
			if existing != nil {
				page, err := client.GetPage(existing.ID)
				if err != nil {
					return app.wrapConfluenceError(err)
				}
				if adfUnchanged(page.Body.AtlasDocFormat.Value, childADF) {
					app.logger.Info("Skipped (unchanged)", "title", childTitle)
					childResult = &confluence.PublishResult{
						PageID:   page.ID,
						PageURL:  page.Links.Base + page.Links.WebUI,
						Title:    childTitle,
						SpaceKey: app.space,
						Version:  page.Version.Number,
						Action:   "skipped",
					}
				} else {
					childResult, err = client.UpdatePage(existing.ID, childTitle, childADF, existing.Version.Number)
					if err != nil {
						return app.wrapConfluenceError(err)
					}
				}
			} else {
				childResult, err = client.CreatePage(spaceID, childTitle, dirResult.PageID, childADF)
				if err != nil {
					return app.wrapConfluenceError(err)
				}
			}
		} else {
			childResult, err = client.CreatePage(spaceID, childTitle, dirResult.PageID, childADF)
			if err != nil {
				return app.wrapConfluenceError(err)
			}
		}

		// Write marker for child file if requested
		if app.writeMarker && childResult.PageID != "" {
			if err := app.writePageIDMarker(f.Path, f.Content, childResult.PageID); err != nil {
				app.logger.Warn("Could not write page-id marker", "error", err)
			}
		}

		// Upload local images (including mermaid SVGs) for child file
		if err := app.uploadAndPatchImages(client, doc, childResult, filepath.Dir(f.Path)); err != nil {
			return err
		}

		func() {
			unlock := app.lockOutput()
			defer unlock()
			printResult(app.stdout, Result{
				Status: "success", PageID: childResult.PageID, PageURL: childResult.PageURL,
				Title: childResult.Title, SpaceKey: childResult.SpaceKey, Action: childResult.Action, Version: childResult.Version,
			}, app.jsonOutput)
		}()

		// Save result for inter-document link resolution
		if app.docResults != nil {
			absPath, _ := filepath.Abs(f.Path)
			app.addDocResult(absPath, &docPublishResult{
				pageID:   childResult.PageID,
				pageURL:  childResult.PageURL,
				title:    childTitle,
				finalADF: doc,
			})
		}
	}

	// Recurse into subdirectories
	for _, child := range tree.Children {
		if err := app.publishDirTree(client, spaceID, dirResult.PageID, &child, false); err != nil {
			return err
		}
	}

	return nil
}

func (app *appEnv) deriveTitleFromSource(source []byte, fallback string) string {
	inFence := false
	for _, line := range strings.Split(string(source), "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "```") {
			inFence = !inFence
			continue
		}
		if inFence {
			continue
		}
		if after, found := strings.CutPrefix(trimmed, "# "); found {
			return after
		}
	}
	return fallback
}

func (app *appEnv) deriveTitle(path string, source []byte) string {
	if app.title != "" {
		return app.title
	}
	if source != nil {
		inFence := false
		for _, line := range strings.Split(string(source), "\n") {
			trimmed := strings.TrimSpace(line)
			if strings.HasPrefix(trimmed, "```") {
				inFence = !inFence
				continue
			}
			if inFence {
				continue
			}
			if after, found := strings.CutPrefix(trimmed, "# "); found {
				return after
			}
		}
	}
	base := filepath.Base(path)
	return strings.TrimSuffix(base, filepath.Ext(base))
}

// isLocalPath returns true if the given path is a local file reference (not a URL).
func isLocalPath(path string) bool {
	return !strings.HasPrefix(path, "http://") &&
		!strings.HasPrefix(path, "https://") &&
		!strings.HasPrefix(path, "//") &&
		!strings.HasPrefix(path, "data:")
}

// findLocalImages walks the ADF tree and collects local image URLs.
func findLocalImages(doc *adf.Document) []string {
	var images []string
	for _, node := range doc.Content {
		collectLocalImages(&node, &images)
	}
	return images
}

func collectLocalImages(node *adf.Node, images *[]string) {
	if node.Type == "media" {
		if t, ok := node.Attrs["type"].(string); ok && t == "external" {
			if u, ok := node.Attrs["url"].(string); ok && isLocalPath(u) {
				*images = append(*images, u)
			}
		}
	}
	for i := range node.Content {
		collectLocalImages(&node.Content[i], images)
	}
}

// patchLocalImages replaces external media nodes for local images with file attachment references.
func patchLocalImages(doc *adf.Document, attachmentMap map[string]string, pageID string) {
	for i := range doc.Content {
		patchNode(&doc.Content[i], attachmentMap, pageID)
	}
}

func patchNode(node *adf.Node, attachmentMap map[string]string, pageID string) {
	if node.Type == "media" {
		if t, ok := node.Attrs["type"].(string); ok && t == "external" {
			if u, ok := node.Attrs["url"].(string); ok {
				if fileID, found := attachmentMap[u]; found {
					node.Attrs["type"] = "file"
					node.Attrs["id"] = fileID
					node.Attrs["collection"] = "contentId-" + pageID
					delete(node.Attrs, "url")
				}
			}
		}
	}
	for i := range node.Content {
		patchNode(&node.Content[i], attachmentMap, pageID)
	}
}

// mermaidBlock holds information about a mermaid codeBlock in the ADF tree.
type mermaidBlock struct {
	index  int        // position in parent's Content slice
	parent *[]adf.Node // pointer to parent's Content slice
	source string     // the mermaid diagram source text
}

// findMermaidBlocks walks the ADF document and returns all codeBlocks with language "mermaid".
func findMermaidBlocks(doc *adf.Document) []mermaidBlock {
	var blocks []mermaidBlock
	collectMermaidBlocks(&doc.Content, &blocks)
	return blocks
}

func collectMermaidBlocks(nodes *[]adf.Node, blocks *[]mermaidBlock) {
	for i := range *nodes {
		node := &(*nodes)[i]
		if node.Type == "codeBlock" {
			if lang, ok := node.Attrs["language"].(string); ok && lang == "mermaid" {
				source := extractCodeBlockText(node)
				*blocks = append(*blocks, mermaidBlock{
					index:  i,
					parent: nodes,
					source: source,
				})
			}
		}
		if len(node.Content) > 0 {
			collectMermaidBlocks(&node.Content, blocks)
		}
	}
}

func extractCodeBlockText(node *adf.Node) string {
	var sb strings.Builder
	for _, child := range node.Content {
		if child.Type == "text" {
			sb.WriteString(child.Text)
		}
	}
	return sb.String()
}

// patchMermaidBlock replaces a mermaid codeBlock with a mediaSingle > media node
// pointing to the local SVG file path.
func patchMermaidBlock(block mermaidBlock, svgPath string) {
	(*block.parent)[block.index] = adf.Node{
		Type:  "mediaSingle",
		Attrs: map[string]any{"layout": "wide"},
		Content: []adf.Node{
			{
				Type: "media",
				Attrs: map[string]any{
					"type": "external",
					"url":  svgPath,
				},
			},
		},
	}
}

// renderMermaidBlocks renders all mermaid codeBlocks in the ADF document to SVG
// and patches them in-place. Returns true if any blocks were rendered.
// If mmdc is not available, blocks are left as codeBlocks (no error).
func renderMermaidBlocks(doc *adf.Document) (bool, error) {
	blocks := findMermaidBlocks(doc)
	if len(blocks) == 0 {
		return false, nil
	}

	if err := mermaid.EnsureAvailable(); err != nil {
		// mmdc not installed — skip rendering silently
		return false, nil
	}

	tempDir, err := os.MkdirTemp("", "md2confl-mermaid-*")
	if err != nil {
		return false, fmt.Errorf("creating temp dir for mermaid: %w", err)
	}
	// Note: caller is responsible for cleanup via the tempDir embedded in SVG paths,
	// but we rely on OS cleanup of /tmp. The files need to survive until upload.

	renderer := &mermaid.Renderer{OutputDir: tempDir}

	// Render all blocks in parallel (limit 2 — mmdc is heavy/Chromium)
	type renderResult struct {
		index   int
		svgPath string
	}
	results := make([]renderResult, len(blocks))
	g, ctx := errgroup.WithContext(context.Background())
	g.SetLimit(2)
	for i, block := range blocks {
		g.Go(func() error {
			svgPath, err := renderer.Render(ctx, []byte(block.source))
			if err != nil {
				return fmt.Errorf("rendering mermaid diagram: %w", err)
			}
			results[i] = renderResult{index: i, svgPath: svgPath}
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		return false, err
	}

	// Patch sequentially after all renders complete
	for i, block := range blocks {
		patchMermaidBlock(block, results[i].svgPath)
	}

	return true, nil
}

// countMermaidSVGs counts how many media nodes reference SVG files from mermaid rendering.
func countMermaidSVGs(doc *adf.Document) int {
	count := 0
	for _, node := range doc.Content {
		if node.Type == "mediaSingle" {
			for _, child := range node.Content {
				if child.Type == "media" {
					if url, ok := child.Attrs["url"].(string); ok && strings.Contains(url, "mermaid-") {
						count++
					}
				}
			}
		}
	}
	return count
}

// resolveInterDocLinksFromResults performs a second pass over all docResults
// to replace relative Markdown links with Confluence page URLs.
func (app *appEnv) resolveInterDocLinksFromResults() error {
	linkMap := make(map[string]string, len(app.docResults))
	for absPath, res := range app.docResults {
		linkMap[absPath] = res.pageURL
	}

	client, err := confluence.NewClient(confluence.Config{
		BaseURL:  app.url,
		SpaceKey: app.space,
		Email:    app.email,
		Token:    app.token,
	})
	if err != nil {
		return err
	}
	client.SetLogger(app.logger)

	g, ctx := errgroup.WithContext(context.Background())
	g.SetLimit(4)
	for absPath, res := range app.docResults {
		if res.finalADF == nil {
			continue
		}

		baseDir := filepath.Dir(absPath)
		count := patchDocLinks(res.finalADF, baseDir, linkMap, app.repoURL, app.repoRoot)
		if count == 0 {
			continue
		}

		patchedJSON, err := json.MarshalIndent(res.finalADF, "", "  ")
		if err != nil {
			return fmt.Errorf("marshaling patched ADF for %q: %w", absPath, err)
		}

		g.Go(func() error {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if err := updatePageWithRetry(client, res.pageID, res.title, string(patchedJSON)); err != nil {
				return fmt.Errorf("updating page %s with resolved links: %w", res.pageID, err)
			}
			app.logger.Info("Resolved inter-document links", "count", count, "file", filepath.Base(absPath))
			return nil
		})
	}

	return g.Wait()
}

// previewInterDocLinksFromResults performs a read-only scan of all docResults
// and reports how many inter-document links would be resolved (dry-run mode).
// Unlike previewInterDocLinks, this iterates over all results including files
// from directory inputs.
func (app *appEnv) previewInterDocLinksFromResults() {
	linkMap := make(map[string]string, len(app.docResults))
	for absPath, res := range app.docResults {
		url := res.pageURL
		if url == "" {
			url = fmt.Sprintf("(page %q)", res.title)
		}
		linkMap[absPath] = url
	}

	for absPath, res := range app.docResults {
		if res.finalADF == nil {
			continue
		}

		baseDir := filepath.Dir(absPath)
		count := countResolvableLinks(res.finalADF, baseDir, linkMap, app.repoURL, app.repoRoot)
		if count > 0 {
			app.logger.Info("Dry-run: would resolve inter-document links", "count", count, "file", filepath.Base(absPath))
		}
	}
}

// countResolvableLinks counts how many relative links in the ADF document
// could be resolved against the linkMap or repo URL, without modifying the tree.
func countResolvableLinks(doc *adf.Document, baseDir string, linkMap map[string]string, repoURL, repoRoot string) int {
	count := 0
	for i := range doc.Content {
		count += countNodeResolvableLinks(&doc.Content[i], baseDir, linkMap, repoURL, repoRoot)
	}
	return count
}

func countNodeResolvableLinks(node *adf.Node, baseDir string, linkMap map[string]string, repoURL, repoRoot string) int {
	count := 0

	for _, mark := range node.Marks {
		if mark.Type != "link" {
			continue
		}
		href, ok := mark.Attrs["href"].(string)
		if !ok || href == "" {
			continue
		}
		if strings.HasPrefix(href, "http://") || strings.HasPrefix(href, "https://") || strings.HasPrefix(href, "//") {
			continue
		}

		cleanHref := href
		if idx := strings.Index(cleanHref, "#"); idx >= 0 {
			cleanHref = cleanHref[:idx]
		}
		if cleanHref == "" {
			continue
		}

		resolved := filepath.Join(baseDir, cleanHref)
		absResolved, err := filepath.Abs(resolved)
		if err != nil {
			continue
		}

		if _, found := linkMap[absResolved]; found {
			count++
		} else if repoURL != "" && repoRoot != "" {
			relPath, err := filepath.Rel(repoRoot, absResolved)
			if err == nil && !strings.HasPrefix(relPath, "..") {
				count++
			}
		}
	}

	for i := range node.Content {
		count += countNodeResolvableLinks(&node.Content[i], baseDir, linkMap, repoURL, repoRoot)
	}

	return count
}

// updatePageWithRetry fetches the current page version and updates it,
// retrying once on a version conflict (409).
func updatePageWithRetry(client *confluence.Client, pageID, title, adfJSON string) error {
	for attempt := 0; attempt < 2; attempt++ {
		page, err := client.GetPage(pageID)
		if err != nil {
			return err
		}
		_, err = client.UpdatePage(pageID, title, adfJSON, page.Version.Number)
		if err == nil {
			return nil
		}
		var apiErr *confluence.APIError
		if errors.As(err, &apiErr) && apiErr.Category == confluence.ErrCategoryConflict && attempt == 0 {
			continue // retry with fresh version
		}
		return err
	}
	return nil
}

// patchDocLinks walks the ADF tree and replaces relative Markdown link hrefs
// with Confluence page URLs using the provided linkMap. Links not found in
// linkMap are resolved to the repository URL if repoURL and repoRoot are set.
// Returns the number of links patched.
func patchDocLinks(doc *adf.Document, baseDir string, linkMap map[string]string, repoURL, repoRoot string) int {
	count := 0
	for i := range doc.Content {
		count += patchNodeLinks(&doc.Content[i], baseDir, linkMap, repoURL, repoRoot)
	}
	return count
}

func patchNodeLinks(node *adf.Node, baseDir string, linkMap map[string]string, repoURL, repoRoot string) int {
	count := 0

	// Check marks for link references
	for i := range node.Marks {
		if node.Marks[i].Type != "link" {
			continue
		}
		href, ok := node.Marks[i].Attrs["href"].(string)
		if !ok || href == "" {
			continue
		}
		// Skip absolute URLs
		if strings.HasPrefix(href, "http://") || strings.HasPrefix(href, "https://") || strings.HasPrefix(href, "//") {
			continue
		}

		// Extract fragment before stripping
		cleanHref := href
		fragment := ""
		if idx := strings.Index(cleanHref, "#"); idx >= 0 {
			fragment = cleanHref[idx:] // includes the '#'
			cleanHref = cleanHref[:idx]
		}
		if cleanHref == "" {
			continue
		}

		// Resolve relative to baseDir
		resolved := filepath.Join(baseDir, cleanHref)
		absResolved, err := filepath.Abs(resolved)
		if err != nil {
			continue
		}

		if pageURL, found := linkMap[absResolved]; found {
			node.Marks[i].Attrs["href"] = pageURL + fragment
			count++
		} else if repoURL != "" && repoRoot != "" {
			relPath, err := filepath.Rel(repoRoot, absResolved)
			if err == nil && !strings.HasPrefix(relPath, "..") {
				node.Marks[i].Attrs["href"] = repoURL + relPath + fragment
				count++
			}
		}
	}

	// Recurse into children
	for i := range node.Content {
		count += patchNodeLinks(&node.Content[i], baseDir, linkMap, repoURL, repoRoot)
	}

	return count
}

// adfUnchanged compares two ADF JSON strings for equivalence.
// It normalizes by unmarshaling and re-marshaling to ignore formatting differences.
func adfUnchanged(existing, newADF string) bool {
	if existing == "" {
		return false
	}
	var existingDoc, newDoc any
	if err := json.Unmarshal([]byte(existing), &existingDoc); err != nil {
		return false
	}
	if err := json.Unmarshal([]byte(newADF), &newDoc); err != nil {
		return false
	}
	normalizeJSON(existingDoc)
	normalizeJSON(newDoc)
	a, _ := json.Marshal(existingDoc)
	b, _ := json.Marshal(newDoc)
	return string(a) == string(b)
}

// normalizeJSON recursively normalizes a decoded JSON value so that
// null and empty arrays/objects are treated equivalently.
func normalizeJSON(v any) {
	switch val := v.(type) {
	case map[string]any:
		for k, child := range val {
			switch c := child.(type) {
			case []any:
				if len(c) == 0 {
					val[k] = nil
				} else {
					normalizeJSON(c)
				}
			case map[string]any:
				normalizeJSON(c)
			}
		}
	case []any:
		for i, child := range val {
			switch c := child.(type) {
			case []any:
				if len(c) == 0 {
					val[i] = nil
				} else {
					normalizeJSON(c)
				}
			case map[string]any:
				normalizeJSON(c)
			}
		}
	}
}

// lockOutput acquires the output mutex and returns an unlock function.
func (app *appEnv) lockOutput() func() {
	if app.outputMu != nil {
		app.outputMu.Lock()
		return app.outputMu.Unlock
	}
	return func() {}
}

// printWarningSummary prints a consolidated warning summary at the end of a run.
func (app *appEnv) printWarningSummary() {
	if app.warnings == nil || len(*app.warnings) == 0 {
		return
	}
	w := *app.warnings
	fmt.Fprintf(app.stderr, "\n%d warning(s):\n", len(w))
	for _, msg := range w {
		fmt.Fprintf(app.stderr, "  - %s\n", msg)
	}
}

// addWarning appends a warning message for the end-of-run summary (thread-safe).
func (app *appEnv) addWarning(msg string) {
	if app.warningsMu != nil {
		app.warningsMu.Lock()
		defer app.warningsMu.Unlock()
	}
	*app.warnings = append(*app.warnings, msg)
}

// addDocResult records a publish result for inter-document link resolution (thread-safe).
func (app *appEnv) addDocResult(absPath string, result *docPublishResult) {
	if app.docResultsMu != nil {
		app.docResultsMu.Lock()
		defer app.docResultsMu.Unlock()
	}
	app.docResults[absPath] = result
}
