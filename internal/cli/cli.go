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
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/fmnapoli/md2confl/adf"
	"github.com/fmnapoli/md2confl/confluence"
	"github.com/fmnapoli/md2confl/mermaid"
	"github.com/fmnapoli/md2confl/parser"
)

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
	configPath  string
	config      *Config

	version string
	stdout  io.Writer
	stderr  io.Writer
}

// Run parses CLI arguments and executes the requested operation.
// Returns an exit code: 0 for success, 1 for user error, 2 for API error.
func Run(args []string, version string, stdout, stderr io.Writer) int {
	app := &appEnv{
		version: version,
		stdout:  stdout,
		stderr:  stderr,
	}

	if err := app.fromArgs(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		printError(stderr, err.Error(), "", 1, false)
		return 1
	}

	if err := app.run(); err != nil {
		code := 1
		var apiErr *apiError
		if errors.As(err, &apiErr) {
			code = apiErr.exitCode
			printError(stderr, apiErr.Error(), apiErr.hint, code, app.jsonOutput)
		} else {
			printError(stderr, err.Error(), "", code, app.jsonOutput)
		}
		return code
	}
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
	fs.StringVar(&app.configPath, "config", "", "Path to config file (default: auto-detect .md2confl.yml)")

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
		fmt.Fprintf(app.stderr, "Using config: %s\n", found)
	}

	// Apply config globals (fills gaps not set by flags)
	app.applyConfig(explicitFlags)

	// --input is optional when config has documents
	if app.input == "" && (app.config == nil || len(app.config.Documents) == 0) {
		return fmt.Errorf("--input is required (or define documents in config file)")
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
			fmt.Fprintf(app.stderr, "Rendered %d mermaid diagram(s) to SVG\n", countMermaidSVGs(doc))
		}
	}

	adfJSON, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling ADF: %w", err)
	}

	if app.dryRun {
		return app.handleDryRun(path, adfJSON)
	}

	if app.output != "" {
		return app.handleFileOutput(path, adfJSON)
	}

	if app.publish {
		return app.handlePublish(path, source, adfJSON, doc)
	}

	// Default: dry-run behavior when no output/publish specified
	return app.handleDryRun(path, adfJSON)
}

func (app *appEnv) handleDryRun(path string, adfJSON []byte) error {
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
	fmt.Fprintln(app.stdout, string(adfJSON))
	return nil
}

func (app *appEnv) handleFileOutput(path string, adfJSON []byte) error {
	if err := os.WriteFile(app.output, append(adfJSON, '\n'), 0644); err != nil {
		return fmt.Errorf("writing output: %w", err)
	}
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
		result, err = client.UpdatePage(pageID, title, adfStr, page.Version.Number)
		if err != nil {
			return app.wrapConfluenceError(err)
		}
	} else if app.force {
		// Find by title and update, or create
		existing, err := client.FindByTitle(spaceID, title)
		if err != nil {
			return app.wrapConfluenceError(err)
		}
		if existing != nil {
			result, err = client.UpdatePage(existing.ID, title, adfStr, existing.Version.Number)
			if err != nil {
				return app.wrapConfluenceError(err)
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
	localImages := findLocalImages(doc)
	if len(localImages) > 0 {
		baseDir := filepath.Dir(path)
		attachmentMap := map[string]string{} // url -> attachment ID
		for _, imgURL := range localImages {
			imgPath := imgURL
			if !filepath.IsAbs(imgURL) {
				imgPath = filepath.Join(baseDir, imgURL)
			}
			if _, err := os.Stat(imgPath); err != nil {
				fmt.Fprintf(app.stderr, "Warning: local image not found: %s\n", imgPath)
				continue
			}
			attID, err := client.UploadAttachment(result.PageID, imgPath)
			if err != nil {
				fmt.Fprintf(app.stderr, "Warning: failed to upload %s: %v\n", imgURL, err)
				continue
			}
			attachmentMap[imgURL] = attID
		}
		if len(attachmentMap) > 0 {
			patchLocalImages(doc, attachmentMap, result.PageID)
			patchedJSON, err := json.MarshalIndent(doc, "", "  ")
			if err == nil {
				// Get current version for update
				currentPage, err := client.GetPage(result.PageID)
				if err == nil {
					updated, err := client.UpdatePage(result.PageID, result.Title, string(patchedJSON), currentPage.Version.Number)
					if err == nil {
						result = updated
					}
				}
			}
		}
	}

	// Write page-id marker back to file if requested
	if app.writeMarker && result.PageID != "" {
		if err := app.writePageIDMarker(path, source, result.PageID); err != nil {
			fmt.Fprintf(app.stderr, "Warning: could not write page-id marker: %v\n", err)
		}
	}

	printResult(app.stdout, Result{
		Status:   "success",
		PageID:   result.PageID,
		PageURL:  result.PageURL,
		Title:    result.Title,
		SpaceKey: result.SpaceKey,
		Action:   result.Action,
		Version:  result.Version,
	}, app.jsonOutput)

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

	// If marker already exists, replace it
	if pageIDRegex.Match(source) {
		updated := pageIDRegex.ReplaceAll(source, []byte(marker))
		return os.WriteFile(path, updated, 0644)
	}

	// Prepend marker to file
	updated := append([]byte(marker+"\n"), source...)
	return os.WriteFile(path, updated, 0644)
}

// DirEntry represents a directory in the hierarchy.
type DirEntry struct {
	Path     string
	Name     string
	Readme   *mdFile
	Files    []mdFile
	Children []DirEntry
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

	if !app.publish {
		// In non-publish mode, convert all files and output ADF
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

	spaceID, err := client.ResolveSpaceID(app.space)
	if err != nil {
		return app.wrapConfluenceError(err)
	}

	return app.publishDirTree(client, spaceID, app.parentID, tree, true)
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
		fullPath := filepath.Join(root, e.Name())
		if e.IsDir() {
			child, err := buildDirTree(fullPath)
			if err != nil {
				return nil, err
			}
			entry.Children = append(entry.Children, *child)
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

	if tree.Readme != nil {
		pagePath = tree.Readme.Path
		pageSource = tree.Readme.Content
		doc, err := parser.ConvertToADF(tree.Readme.Content)
		if err != nil {
			return fmt.Errorf("converting %q: %w", pagePath, err)
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
	var dirPageID string

	if existingPageID != "" {
		page, err := client.GetPage(existingPageID)
		if err != nil {
			return app.wrapConfluenceError(err)
		}
		result, err := client.UpdatePage(existingPageID, title, adfStr, page.Version.Number)
		if err != nil {
			return app.wrapConfluenceError(err)
		}
		dirPageID = result.PageID
		printResult(app.stdout, Result{
			Status:   "success",
			PageID:   result.PageID,
			PageURL:  result.PageURL,
			Title:    result.Title,
			SpaceKey: result.SpaceKey,
			Action:   result.Action,
			Version:  result.Version,
		}, app.jsonOutput)
	} else if app.force {
		existing, err := client.FindByTitle(spaceID, title)
		if err != nil {
			return app.wrapConfluenceError(err)
		}
		if existing != nil {
			result, err := client.UpdatePage(existing.ID, title, adfStr, existing.Version.Number)
			if err != nil {
				return app.wrapConfluenceError(err)
			}
			dirPageID = result.PageID
			printResult(app.stdout, Result{
				Status: "success", PageID: result.PageID, PageURL: result.PageURL,
				Title: result.Title, SpaceKey: result.SpaceKey, Action: result.Action, Version: result.Version,
			}, app.jsonOutput)
		} else {
			result, err := client.CreatePage(spaceID, title, parentID, adfStr)
			if err != nil {
				return app.wrapConfluenceError(err)
			}
			dirPageID = result.PageID
			printResult(app.stdout, Result{
				Status: "success", PageID: result.PageID, PageURL: result.PageURL,
				Title: result.Title, SpaceKey: result.SpaceKey, Action: result.Action, Version: result.Version,
			}, app.jsonOutput)
		}
	} else {
		result, err := client.CreatePage(spaceID, title, parentID, adfStr)
		if err != nil {
			return app.wrapConfluenceError(err)
		}
		dirPageID = result.PageID
		printResult(app.stdout, Result{
			Status: "success", PageID: result.PageID, PageURL: result.PageURL,
			Title: result.Title, SpaceKey: result.SpaceKey, Action: result.Action, Version: result.Version,
		}, app.jsonOutput)
	}

	// Write marker for README if requested
	if app.writeMarker && tree.Readme != nil && dirPageID != "" {
		if err := app.writePageIDMarker(tree.Readme.Path, tree.Readme.Content, dirPageID); err != nil {
			fmt.Fprintf(app.stderr, "Warning: could not write page-id marker: %v\n", err)
		}
	}

	// Publish other markdown files as children
	for _, f := range tree.Files {
		doc, err := parser.ConvertToADF(f.Content)
		if err != nil {
			return fmt.Errorf("converting %q: %w", f.Path, err)
		}
		adfJSON, err := json.MarshalIndent(doc, "", "  ")
		if err != nil {
			return fmt.Errorf("marshaling ADF: %w", err)
		}
		childTitle := app.deriveTitleFromSource(f.Content, strings.TrimSuffix(filepath.Base(f.Path), ".md"))
		childPageID := extractPageID(f.Content)
		childADF := string(adfJSON)

		if childPageID != "" {
			page, err := client.GetPage(childPageID)
			if err != nil {
				return app.wrapConfluenceError(err)
			}
			result, err := client.UpdatePage(childPageID, childTitle, childADF, page.Version.Number)
			if err != nil {
				return app.wrapConfluenceError(err)
			}
			printResult(app.stdout, Result{
				Status: "success", PageID: result.PageID, PageURL: result.PageURL,
				Title: result.Title, SpaceKey: result.SpaceKey, Action: result.Action, Version: result.Version,
			}, app.jsonOutput)
		} else {
			result, err := client.CreatePage(spaceID, childTitle, dirPageID, childADF)
			if err != nil {
				return app.wrapConfluenceError(err)
			}
			printResult(app.stdout, Result{
				Status: "success", PageID: result.PageID, PageURL: result.PageURL,
				Title: result.Title, SpaceKey: result.SpaceKey, Action: result.Action, Version: result.Version,
			}, app.jsonOutput)
			if app.writeMarker {
				if err := app.writePageIDMarker(f.Path, f.Content, result.PageID); err != nil {
					fmt.Fprintf(app.stderr, "Warning: could not write page-id marker: %v\n", err)
				}
			}
		}
	}

	// Recurse into subdirectories
	for _, child := range tree.Children {
		if err := app.publishDirTree(client, spaceID, dirPageID, &child, false); err != nil {
			return err
		}
	}

	return nil
}

func (app *appEnv) deriveTitleFromSource(source []byte, fallback string) string {
	for _, line := range strings.Split(string(source), "\n") {
		if after, found := strings.CutPrefix(strings.TrimSpace(line), "# "); found {
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
		for _, line := range strings.Split(string(source), "\n") {
			if after, found := strings.CutPrefix(strings.TrimSpace(line), "# "); found {
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
		!strings.HasPrefix(path, "//")
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
	for _, block := range blocks {
		svgPath, err := renderer.Render(context.Background(), []byte(block.source))
		if err != nil {
			return false, fmt.Errorf("rendering mermaid diagram: %w", err)
		}
		patchMermaidBlock(block, svgPath)
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
