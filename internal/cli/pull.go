// Copyright 2026 md2confl contributors
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"regexp"
	"strings"
	"unicode"

	"github.com/fmnapoli/md2confl/confluence"
)

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

	version  string
	stdout   io.Writer
	stderr   io.Writer
	logger   *slog.Logger
	warnings []string
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
			printError(stderr, ae.Error(), ae.hint, code, env.jsonOutput)
		} else {
			printError(stderr, err.Error(), "", code, env.jsonOutput)
		}
		return code
	}
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
	return fmt.Errorf("pull command not yet implemented")
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
	// Replace invalid filename characters with hyphen
	invalid := regexp.MustCompile(`[/\\:*?"<>|]`)
	result := invalid.ReplaceAllString(title, "-")

	// Replace non-ASCII control characters and whitespace with hyphens
	var buf strings.Builder
	for _, r := range result {
		if unicode.IsControl(r) || unicode.IsSpace(r) {
			buf.WriteRune('-')
		} else {
			buf.WriteRune(r)
		}
	}
	result = buf.String()

	// Lowercase
	result = strings.ToLower(result)

	// Collapse consecutive hyphens
	multi := regexp.MustCompile(`-{2,}`)
	result = multi.ReplaceAllString(result, "-")

	// Trim hyphens from edges
	result = strings.Trim(result, "-")

	// Truncate to 200 characters
	if len(result) > 200 {
		result = result[:200]
		result = strings.TrimRight(result, "-")
	}

	// Fallback for empty result
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
