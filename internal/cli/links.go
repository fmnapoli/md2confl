// Copyright 2026 md2confl contributors
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"path/filepath"
	"regexp"
	"strings"

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
		BaseURL:   app.url,
		SpaceKey:  app.space,
		Email:     app.email,
		Token:     app.token,
		UserAgent: app.userAgent,
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

// storageHrefRegex captures the value of an href attribute in Storage Format.
// The leading \s keeps it from matching macro attributes such as ac:href.
var storageHrefRegex = regexp.MustCompile(`(\s)href="([^"]*)"`)

// serverLinkMap builds the map of absolute .md path → Confluence page URL.
// Pages whose publish result carried no URL fall back to viewpage.action?pageId=.
func (app *appEnv) serverLinkMap() map[string]string {
	baseURL := strings.TrimRight(app.url, "/")

	linkMap := make(map[string]string, len(app.docResults))
	for absPath, res := range app.docResults {
		switch {
		case res.pageURL != "":
			linkMap[absPath] = res.pageURL
		case res.pageID != "":
			linkMap[absPath] = fmt.Sprintf("%s/pages/viewpage.action?pageId=%s", baseURL, res.pageID)
		}
	}
	return linkMap
}

// patchStorageLinks rewrites the relative hrefs of a Storage Format document
// using the same resolution as the ADF path (adf.ResolveHref): fragments are
// preserved, "./" and "../" are normalized, and targets outside the published
// set fall back to repoURL. Returns the new HTML and how many hrefs changed.
func patchStorageLinks(storageHTML, baseDir string, linkMap map[string]string, repoURL, repoRoot string) (string, int) {
	count := 0
	patched := storageHrefRegex.ReplaceAllStringFunc(storageHTML, func(match string) string {
		m := storageHrefRegex.FindStringSubmatch(match)
		href := html.UnescapeString(m[2])

		resolved, found := adf.ResolveHref(href, baseDir, linkMap, repoURL, repoRoot)
		if !found {
			return match
		}
		count++
		return m[1] + `href="` + html.EscapeString(resolved) + `"`
	})
	return patched, count
}
