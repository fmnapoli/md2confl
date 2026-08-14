// Copyright 2026 md2confl contributors
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/fmnapoli/md2confl/confluence"
	"golang.org/x/sync/errgroup"
	"gopkg.in/yaml.v3"
)

// Config represents the .md2confl.yml configuration file.
type Config struct {
	URL         string      `yaml:"url"`
	Space       string      `yaml:"space"`
	Email       string      `yaml:"email"`
	ParentID    string      `yaml:"parent-id"`
	Force       *bool       `yaml:"force"`
	WriteMarker *bool       `yaml:"write-marker"`
	RepoURL     string      `yaml:"repo-url"`
	UserAgent   string      `yaml:"user-agent"`
	Server      *bool       `yaml:"server"`
	Approve     *bool       `yaml:"approve"`
	Documents   []DocConfig `yaml:"documents"`
}

// DocConfig represents a single document entry in the config file.
type DocConfig struct {
	Input    string `yaml:"input"`
	Output   string `yaml:"output"`
	Title    string `yaml:"title"`
	Space    string `yaml:"space"`
	ParentID string `yaml:"parent-id"`
}

// loadConfig reads and parses a .md2confl.yml file.
// It rejects configs containing a token field and validates that each document has an input.
// Paths are resolved relative to the config file's directory.
func loadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading config %q: %w", path, err)
	}

	// Reject token in config for security
	var raw map[string]any
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("parsing config %q: %w", path, err)
	}
	if _, ok := raw["token"]; ok {
		return nil, fmt.Errorf("config %q must not contain 'token' — use CONFLUENCE_TOKEN env var or --token flag", path)
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parsing config %q: %w", path, err)
	}

	configDir := filepath.Dir(path)

	for i, doc := range cfg.Documents {
		if doc.Input == "" {
			return nil, fmt.Errorf("config %q: document[%d] missing required 'input' field", path, i)
		}
		// Resolve relative paths against config dir
		if !filepath.IsAbs(doc.Input) {
			cfg.Documents[i].Input = filepath.Join(configDir, doc.Input)
		}
		if doc.Output != "" && !filepath.IsAbs(doc.Output) {
			cfg.Documents[i].Output = filepath.Join(configDir, doc.Output)
		}
	}

	return &cfg, nil
}

// findConfig looks for .md2confl.yml in the current working directory.
// Returns the path if found, or empty string if not.
func findConfig() string {
	candidates := []string{".md2confl.yml", ".md2confl.yaml"}
	for _, name := range candidates {
		if info, err := os.Stat(name); err == nil && !info.IsDir() {
			abs, err := filepath.Abs(name)
			if err != nil {
				return name
			}
			return abs
		}
	}
	return ""
}

// applyConfig fills appEnv fields from config (global level) for fields not already set by flags.
func (app *appEnv) applyConfig(explicitFlags map[string]bool) {
	if app.config == nil {
		return
	}
	cfg := app.config

	if !explicitFlags["url"] && cfg.URL != "" {
		app.url = cfg.URL
	}
	if !explicitFlags["space"] && cfg.Space != "" {
		app.space = cfg.Space
	}
	if !explicitFlags["email"] && cfg.Email != "" {
		app.email = cfg.Email
	}
	if !explicitFlags["parent-id"] && cfg.ParentID != "" {
		app.parentID = cfg.ParentID
	}
	if !explicitFlags["force"] && cfg.Force != nil {
		app.force = *cfg.Force
	}
	if !explicitFlags["write-marker"] && cfg.WriteMarker != nil {
		app.writeMarker = *cfg.WriteMarker
	}
	if !explicitFlags["repo-url"] && cfg.RepoURL != "" {
		app.repoURL = cfg.RepoURL
	}
	if !explicitFlags["user-agent"] && cfg.UserAgent != "" {
		app.userAgent = cfg.UserAgent
	}
	if !explicitFlags["server"] && cfg.Server != nil {
		app.serverMode = *cfg.Server
	}
	if !explicitFlags["approve"] && cfg.Approve != nil {
		app.approve = *cfg.Approve
	}
}

// withDocumentConfig creates a shallow copy of appEnv with document-level overrides applied.
func (app *appEnv) withDocumentConfig(doc DocConfig) *appEnv {
	clone := *app
	// Clear config to prevent re-entering runDocuments from run()
	clone.config = nil
	clone.input = doc.Input
	if doc.Output != "" {
		clone.output = doc.Output
		clone.publish = false
	} else {
		clone.output = ""
		clone.publish = true
	}
	if doc.Title != "" {
		clone.title = doc.Title
	}
	if doc.Space != "" {
		clone.space = doc.Space
	}
	if doc.ParentID != "" {
		clone.parentID = doc.ParentID
	}
	return &clone
}

// runDocuments iterates over config documents and processes each one.
// If inputFilter is non-empty, only matching documents are processed.
// After publishing all documents, a second pass resolves inter-document links.
func (app *appEnv) runDocuments(inputFilter string) error {
	if app.config == nil || len(app.config.Documents) == 0 {
		return fmt.Errorf("no documents defined in config")
	}

	filtered := app.config.Documents
	if inputFilter != "" {
		filtered = nil
		// Resolve filter to absolute path for comparison
		absFilter, _ := filepath.Abs(inputFilter)
		for _, doc := range app.config.Documents {
			absInput, _ := filepath.Abs(doc.Input)
			if absInput == absFilter || doc.Input == inputFilter ||
				strings.HasSuffix(absInput, inputFilter) {
				filtered = append(filtered, doc)
			}
		}
		if len(filtered) == 0 {
			return fmt.Errorf("no documents matching %q in config", inputFilter)
		}
	}

	// Initialize shared map for collecting publish results
	app.docResults = make(map[string]*docPublishResult)
	var mu sync.Mutex

	// First pass: publish all documents (parallel).
	// A document that fails is recorded and skipped instead of aborting the
	// run: the other documents still get published, and the second pass below
	// still resolves the links of everything that succeeded. The run fails as
	// a whole at the end, in Run, via the aggregated failure error.
	app.docResultsMu = &mu
	var g errgroup.Group
	g.SetLimit(app.concurrency)
	for _, doc := range filtered {
		g.Go(func() error {
			clone := app.withDocumentConfig(doc)
			if err := clone.run(); err != nil {
				app.recordDocFailure(doc.Input, err)
			}
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		return err
	}

	// Publicar um subconjunto filtrado não pode empurrar links relativos crus
	// para o Confluence: registra, sem publicar, as páginas dos documentos que
	// ficaram de fora, para que o segundo pass tenha para onde apontar.
	if app.serverMode && !app.dryRun && inputFilter != "" {
		client, err := confluence.NewServerClient(confluence.Config{
			BaseURL:   app.url,
			SpaceKey:  app.space,
			Email:     app.email,
			Token:     app.token,
			UserAgent: app.userAgent,
		})
		if err == nil {
			client.SetLogger(app.logger)
			app.registerLinkTargetsFromConfig(client, filtered)
		}
	}

	// Second pass: resolve inter-document links across all published docs
	// (including files from directory inputs, not just the config entries).
	switch {
	case len(app.docResults) > 1 && !app.dryRun:
		if app.serverMode {
			client, err := confluence.NewServerClient(confluence.Config{
				BaseURL:   app.url,
				SpaceKey:  app.space,
				Email:     app.email,
				Token:     app.token,
				UserAgent: app.userAgent,
			})
			if err == nil {
				client.SetLogger(app.logger)
				if err := app.resolveInterDocLinksServer(client); err != nil {
					return fmt.Errorf("resolving inter-document links: %w", err)
				}
			}
		} else {
			if err := app.resolveInterDocLinksFromResults(); err != nil {
				return fmt.Errorf("resolving inter-document links: %w", err)
			}
		}
	case len(app.docResults) > 1 && app.dryRun:
		app.previewInterDocLinksFromResults()
	case !app.dryRun:
		// Sem segundo pass não há quem resolva os links: se sobrou algum
		// relativo, o corpo publicado ficou com ele.
		app.warnUnresolvedMarkdownLinks()
	}

	// Approve all published pages via Comala Workflows (after all updates are done)
	if app.approve && app.serverMode && !app.dryRun {
		client, err := confluence.NewServerClient(confluence.Config{
			BaseURL:   app.url,
			SpaceKey:  app.space,
			Email:     app.email,
			Token:     app.token,
			UserAgent: app.userAgent,
		})
		if err == nil {
			client.SetLogger(app.logger)
			for _, res := range app.docResults {
				if res.pageID != "" && !res.linkOnly {
					if err := client.ApproveWorkflow(res.pageID, "Review"); err != nil {
						app.logger.Warn("Could not approve page", "pageID", res.pageID, "error", err)
					}
				}
			}
		}
	}

	return nil
}
