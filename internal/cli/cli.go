// Copyright 2026 md2confl contributors
// SPDX-License-Identifier: Apache-2.0

// Package cli implements the md2confl command-line interface.
// It wires together flag parsing, Markdown→ADF conversion, and Confluence
// publishing. This package is internal — only cmd/md2confl/main.go imports it.
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
	"strings"
	"sync"

	"github.com/fmnapoli/md2confl/adf"
	"github.com/fmnapoli/md2confl/parser"
)

// docPublishResult holds the publish outcome for a single document,
// used to resolve inter-document links in the second pass.
type docPublishResult struct {
	pageID    string
	pageURL   string
	title     string
	finalADF  *adf.Document
	finalHTML string // Storage Format HTML (Server/DC mode only)
}

// docFailure records a document that could not be published. Failures are
// collected instead of aborting the run, so that the remaining documents — and
// the second pass that resolves inter-document links, which only runs after
// every publish — still go through.
type docFailure struct {
	path string
	err  error
}

type appEnv struct {
	input         string
	output        string
	dryRun        bool
	publish       bool
	url           string
	space         string
	parentID      string
	title         string
	email         string
	token         string
	force         bool
	writeMarker   bool
	jsonOutput    bool
	renderMermaid bool
	showVersion   bool
	verbose       bool
	concurrency   int
	configPath    string
	repoURL       string
	repoRoot      string
	userAgent     string
	serverMode    bool
	approve       bool
	config        *Config
	docResults    map[string]*docPublishResult // abs input path → result
	docResultsMu  *sync.Mutex
	warnings      *[]string
	warningsMu    *sync.Mutex
	failures      *[]docFailure
	failuresMu    *sync.Mutex
	// digestCheck garante uma única verificação por execução de que o servidor
	// realmente guardou o digest da fonte. É ponteiro porque os clones por
	// documento (withDocumentConfig) copiam o appEnv por valor.
	digestCheck *retryableOnce

	version  string
	stdout   io.Writer
	stderr   io.Writer
	outputMu *sync.Mutex
	logger   *slog.Logger
}

// Run parses CLI arguments and executes the requested operation.
// Returns an exit code: 0 for success, 1 for user error, 2 for API error.
func Run(args []string, version string, stdout, stderr io.Writer) int {
	// Detect "pull" subcommand and delegate
	if len(args) > 0 && args[0] == "pull" {
		return runPullCommand(args[1:], version, stdout, stderr)
	}

	warnings := make([]string, 0)
	failures := make([]docFailure, 0)
	outputMu := &sync.Mutex{}
	app := &appEnv{
		version:     version,
		stdout:      stdout,
		stderr:      stderr,
		outputMu:    outputMu,
		warnings:    &warnings,
		warningsMu:  &sync.Mutex{},
		failures:    &failures,
		failuresMu:  &sync.Mutex{},
		digestCheck: &retryableOnce{},
	}

	if err := app.fromArgs(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		printError(stderr, err.Error(), "", 1, false)
		return 1
	}

	logLevel := slog.LevelInfo
	if app.verbose {
		logLevel = slog.LevelDebug
	}
	app.logger = slog.New(slog.NewTextHandler(stderr, &slog.HandlerOptions{Level: logLevel}))

	err := app.run()
	// Documents skipped by per-document isolation must still fail the run:
	// reporting them only as text would leave the caller (a CI step) green.
	if err == nil {
		err = app.failureError()
	}
	app.printFailureSummary()

	if err != nil {
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

func (app *appEnv) fromArgs(args []string) error {
	fs := flag.NewFlagSet("md2confl", flag.ContinueOnError)
	fs.SetOutput(app.stderr)

	app.registerFlags(fs)

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

	explicitFlags := map[string]bool{}
	fs.Visit(func(f *flag.Flag) {
		explicitFlags[f.Name] = true
	})

	if err := app.loadAndApplyConfig(explicitFlags); err != nil {
		return err
	}

	app.resolveCredentials(explicitFlags)

	return app.validateFlags()
}

// registerFlags defines all CLI flags on the given FlagSet.
func (app *appEnv) registerFlags(fs *flag.FlagSet) {
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
	fs.StringVar(&app.userAgent, "user-agent", "", "Custom User-Agent header for HTTP requests (e.g., for Cloudflare bypass)")
	fs.BoolVar(&app.serverMode, "server", false, "Use Confluence Server/Data Center API (REST API v1 + Storage Format)")
	fs.BoolVar(&app.approve, "approve", false, "Auto-approve page after publish (Comala Workflows)")
}

// loadAndApplyConfig loads config from explicit path or auto-discovery,
// then applies global config values for fields not set by flags.
func (app *appEnv) loadAndApplyConfig(explicitFlags map[string]bool) error {
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

	app.applyConfig(explicitFlags)
	return nil
}

// resolveCredentials fills credentials from environment variables if not set by flags or config.
func (app *appEnv) resolveCredentials(explicitFlags map[string]bool) {
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
}

// validateFlags checks flag combinations and required fields.
func (app *appEnv) validateFlags() error {
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
		if app.config == nil {
			return fmt.Errorf("--write-marker requires --publish")
		}
	}
	if app.force && !app.publish {
		if app.config == nil {
			return fmt.Errorf("--force requires --publish")
		}
	}
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
	if app.repoURL == "" {
		app.repoURL, app.repoRoot = detectRepoURL()
	}
	if app.repoURL != "" && !strings.HasSuffix(app.repoURL, "/") {
		app.repoURL += "/"
	}
	if app.repoURL != "" && app.repoRoot == "" {
		app.repoRoot, _ = gitOutput("rev-parse", "--show-toplevel")
		if app.repoRoot == "" {
			app.repoRoot = findRepoRoot()
		}
	}

	if app.input == "" && app.config != nil && len(app.config.Documents) > 0 {
		return app.runDocuments("")
	}

	if app.input != "" && app.config != nil && len(app.config.Documents) > 0 {
		for _, doc := range app.config.Documents {
			absInput, _ := filepath.Abs(doc.Input)
			absFlag, _ := filepath.Abs(app.input)
			if absInput == absFlag {
				return app.runDocuments(app.input)
			}
		}
	}

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

	// Server/DC mode: usa Storage Format (XHTML) em vez de ADF
	if app.serverMode && app.publish {
		return app.runFileServer(path, source)
	}

	doc, err := parser.ConvertToADF(source)
	if err != nil {
		return fmt.Errorf("converting %q: %w", path, err)
	}

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
	return app.handleDryRun(path, adfJSON, doc)
}

// runFileServer processa um arquivo Markdown para Confluence Server/DC.
func (app *appEnv) runFileServer(path string, source []byte) error {
	// Renderizar mermaid para SVG e substituir no markdown
	modifiedSource, svgPaths, err := renderMermaidInMarkdown(source)
	if err != nil {
		return fmt.Errorf("rendering mermaid in %q: %w", path, err)
	}
	if len(svgPaths) > 0 {
		app.logger.Info("Rendered mermaid diagrams", "count", len(svgPaths))
	}

	storageHTML, err := parser.ConvertToStorageFormat(modifiedSource)
	if err != nil {
		return fmt.Errorf("converting %q to storage format: %w", path, err)
	}

	return app.handlePublishServer(path, source, storageHTML, svgPaths)
}

func (app *appEnv) handleDryRun(path string, adfJSON []byte, doc *adf.Document) error {
	unlock := app.lockOutput()
	defer unlock()

	if app.publish {
		title := deriveTitle(app.title, path, nil)
		fmt.Fprintf(app.stderr, "Dry-run: would publish to Confluence\n")
		fmt.Fprintf(app.stderr, "  Title: %s\n", title)
		fmt.Fprintf(app.stderr, "  Space: %s\n", app.space)
		if app.parentID != "" {
			fmt.Fprintf(app.stderr, "  Parent: %s\n", app.parentID)
		}
		fmt.Fprintf(app.stderr, "  URL: %s\n", app.url)
		fmt.Fprintf(app.stderr, "\n")
	}

	if app.docResults != nil && app.output == "" {
		absPath, _ := filepath.Abs(path)
		title := deriveTitle(app.title, path, nil)
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

// deriveTitle returns a page title using flag, source H1, or filename fallback.
func deriveTitle(flagTitle, filePath string, source []byte) string {
	if flagTitle != "" {
		return flagTitle
	}
	if t := titleFromSource(source, ""); t != "" {
		return t
	}
	base := filepath.Base(filePath)
	return strings.TrimSuffix(base, filepath.Ext(base))
}

// titleFromSource scans source for the first H1 heading (outside code fences).
// Returns fallback if no H1 is found.
func titleFromSource(source []byte, fallback string) string {
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

// recordDocFailure registers a document that failed and must be skipped
// (thread-safe). The caller keeps going with the remaining documents; the run
// as a whole fails at the end via failureError.
func (app *appEnv) recordDocFailure(path string, err error) {
	if app.logger != nil {
		app.logger.Error("Document failed — skipping", "path", path, "error", err)
	}
	if app.failures == nil {
		return
	}
	if app.failuresMu != nil {
		app.failuresMu.Lock()
		defer app.failuresMu.Unlock()
	}
	*app.failures = append(*app.failures, docFailure{path: path, err: err})
}

// docFailures returns a copy of the failures collected so far (thread-safe).
func (app *appEnv) docFailures() []docFailure {
	if app.failures == nil {
		return nil
	}
	if app.failuresMu != nil {
		app.failuresMu.Lock()
		defer app.failuresMu.Unlock()
	}
	return append([]docFailure(nil), *app.failures...)
}

// failureError aggregates the collected per-document failures into a single
// error, or returns nil when every document went through. Its exit code is the
// highest one among the failures, so a run that only hit API errors still
// exits 2 and a CI step can tell the difference from a usage error.
func (app *appEnv) failureError() error {
	failures := app.docFailures()
	if len(failures) == 0 {
		return nil
	}
	exitCode := 1
	for _, f := range failures {
		var apiErr *apiError
		if errors.As(f.err, &apiErr) && apiErr.exitCode > exitCode {
			exitCode = apiErr.exitCode
		}
	}
	return &apiError{
		message:  fmt.Sprintf("%d document(s) failed and were skipped", len(failures)),
		hint:     "see the failure summary above; the remaining documents were published",
		exitCode: exitCode,
	}
}

// printFailureSummary prints the documents skipped during the run.
func (app *appEnv) printFailureSummary() {
	failures := app.docFailures()
	if len(failures) == 0 {
		return
	}
	fmt.Fprintf(app.stderr, "\n%d document(s) failed:\n", len(failures))
	for _, f := range failures {
		fmt.Fprintf(app.stderr, "  - %s: %v\n", f.path, f.err)
		// The hint carries the way out (e.g. add a page-id marker); the
		// aggregated error that follows only reports the count.
		var apiErr *apiError
		if errors.As(f.err, &apiErr) && apiErr.hint != "" {
			fmt.Fprintf(app.stderr, "      Hint: %s\n", apiErr.hint)
		}
	}
}

// addDocResult records a publish result for inter-document link resolution (thread-safe).
func (app *appEnv) addDocResult(absPath string, result *docPublishResult) {
	if app.docResultsMu != nil {
		app.docResultsMu.Lock()
		defer app.docResultsMu.Unlock()
	}
	app.docResults[absPath] = result
}
