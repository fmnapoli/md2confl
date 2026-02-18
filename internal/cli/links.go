// Copyright 2026 md2confl contributors
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"

	"golang.org/x/sync/errgroup"

	"github.com/fmnapoli/md2confl/adf"
	"github.com/fmnapoli/md2confl/confluence"
)

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
		count := adf.PatchDocLinks(res.finalADF, baseDir, linkMap, app.repoURL, app.repoRoot)
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
		count := adf.CountResolvableLinks(res.finalADF, baseDir, linkMap, app.repoURL, app.repoRoot)
		if count > 0 {
			app.logger.Info("Dry-run: would resolve inter-document links", "count", count, "file", filepath.Base(absPath))
		}
	}
}
