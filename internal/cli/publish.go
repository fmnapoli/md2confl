// Copyright 2026 md2confl contributors
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sync"

	"golang.org/x/sync/errgroup"

	"github.com/fmnapoli/md2confl/adf"
	"github.com/fmnapoli/md2confl/confluence"
)

type apiError struct {
	message  string
	hint     string
	exitCode int
}

func (e *apiError) Error() string { return e.message }

var pageIDRegex = regexp.MustCompile(`<!--\s*confluence-page-id:\s*(\d+)\s*-->`)

func extractPageID(source []byte) string {
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

// publishInput groups parameters for the unified publish-or-skip flow.
type publishInput struct {
	client    *confluence.Client
	spaceID   string
	parentID  string
	title     string
	pageID    string // from marker, or ""
	adfStr    string
	force     bool
	inputPath string // for write-marker
}

// moveIfNeeded moves a page to the desired parent if it differs from the current one.
func (app *appEnv) moveIfNeeded(client *confluence.Client, pageID, currentParentID, desiredParentID string) error {
	if desiredParentID == "" || currentParentID == desiredParentID {
		return nil
	}
	if app.dryRun {
		app.logger.Info("Would move page", "pageID", pageID, "from", currentParentID, "to", desiredParentID)
		return nil
	}
	app.logger.Info("Moving page", "pageID", pageID, "from", currentParentID, "to", desiredParentID)
	if err := client.MovePage(pageID, desiredParentID); err != nil {
		return app.wrapConfluenceError(err)
	}
	return nil
}

// publishOrSkip encapsulates the create/update/skip logic that was previously
// duplicated in handlePublish and publishDirTree.
func (app *appEnv) publishOrSkip(in publishInput) (*confluence.PublishResult, error) {
	if in.pageID != "" {
		page, err := in.client.GetPage(in.pageID)
		if err != nil {
			return nil, app.wrapConfluenceError(err)
		}

		var result *confluence.PublishResult
		if adf.IsUnchanged(page.Body.AtlasDocFormat.Value, in.adfStr) {
			app.logger.Info("Skipped (unchanged)", "title", in.title)
			result = &confluence.PublishResult{
				PageID:   page.ID,
				PageURL:  page.Links.Base + page.Links.WebUI,
				Title:    in.title,
				SpaceKey: app.space,
				Version:  page.Version.Number,
				Action:   "skipped",
			}
		} else {
			result, err = in.client.UpdatePage(in.pageID, in.title, in.adfStr, page.Version.Number)
			if err != nil {
				return nil, app.wrapConfluenceError(err)
			}
		}

		if err := app.moveIfNeeded(in.client, in.pageID, page.ParentID, in.parentID); err != nil {
			return nil, err
		}
		return result, nil
	}

	if in.force {
		existing, err := in.client.FindByTitle(in.spaceID, in.title)
		if err != nil {
			return nil, app.wrapConfluenceError(err)
		}
		if existing != nil {
			page, err := in.client.GetPage(existing.ID)
			if err != nil {
				return nil, app.wrapConfluenceError(err)
			}

			var result *confluence.PublishResult
			if adf.IsUnchanged(page.Body.AtlasDocFormat.Value, in.adfStr) {
				app.logger.Info("Skipped (unchanged)", "title", in.title)
				result = &confluence.PublishResult{
					PageID:   page.ID,
					PageURL:  page.Links.Base + page.Links.WebUI,
					Title:    in.title,
					SpaceKey: app.space,
					Version:  page.Version.Number,
					Action:   "skipped",
				}
			} else {
				result, err = in.client.UpdatePage(existing.ID, in.title, in.adfStr, existing.Version.Number)
				if err != nil {
					return nil, app.wrapConfluenceError(err)
				}
			}

			if err := app.moveIfNeeded(in.client, existing.ID, page.ParentID, in.parentID); err != nil {
				return nil, err
			}
			return result, nil
		}
	}

	result, err := in.client.CreatePage(in.spaceID, in.title, in.parentID, in.adfStr)
	if err != nil {
		return nil, app.wrapConfluenceError(err)
	}
	return result, nil
}

func (app *appEnv) handlePublish(path string, source, adfJSON []byte, doc *adf.Document) error {
	client, err := confluence.NewClient(confluence.Config{
		BaseURL:   app.url,
		SpaceKey:  app.space,
		Email:     app.email,
		Token:     app.token,
		UserAgent: app.userAgent,
	})
	if err != nil {
		return &apiError{message: err.Error(), exitCode: 1}
	}
	client.SetLogger(app.logger)

	spaceID, err := client.ResolveSpaceID(app.space)
	if err != nil {
		return app.wrapConfluenceError(err)
	}

	title := deriveTitle(app.title, path, source)

	result, err := app.publishOrSkip(publishInput{
		client:    client,
		spaceID:   spaceID,
		parentID:  app.parentID,
		title:     title,
		pageID:    extractPageID(source),
		adfStr:    string(adfJSON),
		force:     app.force,
		inputPath: path,
	})
	if err != nil {
		return err
	}

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
	localImages := adf.FindLocalImages(doc)
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
		adf.PatchLocalImages(doc, attachmentMap, result.PageID)
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

	perm := os.FileMode(0644)
	if info, err := os.Stat(path); err == nil {
		perm = info.Mode().Perm()
	}

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

	updated := append([]byte(marker+"\n"), source...)
	return os.WriteFile(path, updated, perm)
}

// updatePageWithRetry fetches the current page version and updates it,
// retrying once on a version conflict (409). A page whose body already matches
// is left alone: the second pass runs on every publish, and rewriting an
// identical body would add a page version per run.
func updatePageWithRetry(client *confluence.Client, pageID, title, adfJSON string) error {
	for attempt := 0; attempt < 2; attempt++ {
		page, err := client.GetPage(pageID)
		if err != nil {
			return err
		}
		if adf.IsUnchanged(page.Body.AtlasDocFormat.Value, adfJSON) {
			return nil
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

// handlePublishServer publica uma página usando Confluence Server/DC (REST API v1 + Storage Format).
func (app *appEnv) handlePublishServer(path string, source []byte, storageHTML string, svgPaths []string) error {
	client, err := confluence.NewServerClient(confluence.Config{
		BaseURL:   app.url,
		SpaceKey:  app.space,
		Email:     app.email,
		Token:     app.token,
		UserAgent: app.userAgent,
	})
	if err != nil {
		return &apiError{message: err.Error(), exitCode: 1}
	}
	client.SetLogger(app.logger)

	title := deriveTitle(app.title, path, source)

	// O corpo publicado já sai com as referências de attachment dos SVGs — é o
	// que a comparação de idempotência e o segundo pass de links usam como base.
	storageHTML = patchMermaidRefs(storageHTML, svgPaths)

	result, err := app.publishOrSkipServer(client, publishServerInput{
		parentID:  app.parentID,
		title:     title,
		pageID:    extractPageID(source),
		html:      storageHTML,
		inputPath: path,
	})
	if err != nil {
		return err
	}

	uploadMermaidAttachments(client, result.PageID, svgPaths, app.logger)

	// Auto-approve via Comala Workflows (se configurado)
	if app.approve {
		if err := client.ApproveWorkflow(result.PageID, "Review"); err != nil {
			app.logger.Warn("Could not approve page", "error", err)
		} else {
			app.logger.Info("Approved", "pageID", result.PageID)
		}
	}

	if app.writeMarker {
		if err := app.writePageIDMarker(path, source, result.PageID); err != nil {
			app.logger.Warn("Could not write page-id marker", "error", err)
		}
	}

	// O caso "skipped" já é logado por publishOrSkipServer.
	if result.Action != "skipped" {
		app.logger.Info("Published", "title", result.Title, "action", result.Action, "pageID", result.PageID)
	}

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

	// Salvar resultado para resolução de links inter-documento (Server mode)
	app.recordDocResultServer(path, result.PageID, result.PageURL, result.Title, storageHTML)

	return nil
}

// recordDocResultServer guarda o resultado de publicação de um arquivo único em
// Server/DC mode para o segundo pass de resolução de links inter-documento.
func (app *appEnv) recordDocResultServer(path, pageID, pageURL, title, storageHTML string) {
	if app.docResults == nil {
		return
	}
	absPath, _ := filepath.Abs(path)
	app.addDocResult(absPath, &docPublishResult{
		pageID:    pageID,
		pageURL:   pageURL,
		title:     title,
		finalHTML: storageHTML,
	})
}
