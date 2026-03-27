// Copyright 2026 md2confl contributors
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/fmnapoli/md2confl/adf"
	"github.com/fmnapoli/md2confl/confluence"
	"github.com/fmnapoli/md2confl/parser"
)

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
		return app.convertDirTree(tree)
	}

	// Server/DC mode: usa Storage Format (XHTML) em vez de ADF
	if app.serverMode {
		return app.runDirServer(tree)
	}

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

// runDirServer publica uma árvore de diretórios usando Confluence Server/DC (REST API v1 + Storage Format).
func (app *appEnv) runDirServer(tree *DirEntry) error {
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

	standaloneDir := app.docResults == nil
	if standaloneDir {
		app.docResults = make(map[string]*docPublishResult)
		app.docResultsMu = &sync.Mutex{}
	}

	if err := app.publishDirTreeServer(client, app.parentID, tree, true); err != nil {
		return err
	}

	// Segundo pass: resolver links inter-documento (*.md → pageId)
	if standaloneDir && len(app.docResults) > 1 {
		if err := app.resolveInterDocLinksServer(client); err != nil {
			return fmt.Errorf("resolving inter-document links: %w", err)
		}
	}

	return nil
}

// publishDirTreeServer percorre a árvore e publica cada página via Server/DC API.
func (app *appEnv) publishDirTreeServer(client *confluence.ServerClient, parentID string, tree *DirEntry, isRoot bool) error {
	var pagePath string
	var pageSource []byte
	var storageHTML string

	if tree.Readme != nil {
		pagePath = tree.Readme.Path
		pageSource = tree.Readme.Content

		modifiedSource, svgPaths, err := renderMermaidInMarkdown(tree.Readme.Content)
		if err != nil {
			return fmt.Errorf("rendering mermaid in %q: %w", pagePath, err)
		}
		if len(svgPaths) > 0 {
			app.logger.Info("Rendered mermaid diagrams", "count", len(svgPaths), "file", pagePath)
		}

		html, err := parser.ConvertToStorageFormat(modifiedSource)
		if err != nil {
			return fmt.Errorf("converting %q to storage format: %w", pagePath, err)
		}
		storageHTML = html
		_ = svgPaths // upload de SVGs tratado após publicação
	}

	var title string
	if isRoot && app.title != "" {
		title = app.title
	} else if tree.Readme != nil {
		title = titleFromSource(tree.Readme.Content, tree.Name)
	} else {
		title = tree.Name
	}

	existingPageID := ""
	if pageSource != nil {
		existingPageID = extractPageID(pageSource)
	}

	dirResult, err := app.publishOrSkipServer(client, publishServerInput{
		parentID:  parentID,
		title:     title,
		pageID:    existingPageID,
		html:      storageHTML,
		inputPath: pagePath,
	})
	if err != nil {
		return err
	}

	func() {
		unlock := app.lockOutput()
		defer unlock()
		printResult(app.stdout, Result{
			Status: "success", PageID: dirResult.PageID, PageURL: dirResult.PageURL,
			Title: dirResult.Title, SpaceKey: dirResult.SpaceKey, Action: dirResult.Action, Version: dirResult.Version,
		}, app.jsonOutput)
	}()

	if app.writeMarker && tree.Readme != nil && dirResult.PageID != "" {
		if err := app.writePageIDMarker(tree.Readme.Path, tree.Readme.Content, dirResult.PageID); err != nil {
			app.logger.Warn("Could not write page-id marker", "error", err)
		}
	}

	// Salvar resultado para resolução de links inter-documento
	if app.docResults != nil && pagePath != "" {
		absPath, _ := filepath.Abs(pagePath)
		app.addDocResult(absPath, &docPublishResult{
			pageID:    dirResult.PageID,
			pageURL:   dirResult.PageURL,
			title:     dirResult.Title,
			finalHTML: storageHTML,
		})
	}

	// Publicar demais arquivos markdown como subpáginas
	for _, f := range tree.Files {
		modifiedSource, svgPaths, err := renderMermaidInMarkdown(f.Content)
		if err != nil {
			return fmt.Errorf("rendering mermaid in %q: %w", f.Path, err)
		}
		if len(svgPaths) > 0 {
			app.logger.Info("Rendered mermaid diagrams", "count", len(svgPaths), "file", f.Path)
		}

		html, err := parser.ConvertToStorageFormat(modifiedSource)
		if err != nil {
			return fmt.Errorf("converting %q to storage format: %w", f.Path, err)
		}
		childTitle := titleFromSource(f.Content, strings.TrimSuffix(filepath.Base(f.Path), ".md"))

		childResult, err := app.publishOrSkipServer(client, publishServerInput{
			parentID:  dirResult.PageID,
			title:     childTitle,
			pageID:    extractPageID(f.Content),
			html:      html,
			inputPath: f.Path,
		})
		if err != nil {
			return err
		}

		// Upload de SVGs mermaid
		if len(svgPaths) > 0 {
			patchedHTML, err := uploadAndPatchImagesServer(client, childResult.PageID, html, svgPaths, app.logger)
			if err != nil {
				app.logger.Warn("Failed to upload mermaid SVGs", "error", err)
			} else if patchedHTML != html {
				page, err := client.GetPage(childResult.PageID)
				if err == nil {
					if updated, err := client.UpdatePage(childResult.PageID, childResult.Title, patchedHTML, page.Version.Number); err != nil {
						app.logger.Warn("Failed to update page with mermaid attachments", "error", err)
					} else {
						*childResult = *updated
					}
				}
			}
		}

		if app.writeMarker && childResult.PageID != "" {
			if err := app.writePageIDMarker(f.Path, f.Content, childResult.PageID); err != nil {
				app.logger.Warn("Could not write page-id marker", "error", err)
			}
		}

		func() {
			unlock := app.lockOutput()
			defer unlock()
			printResult(app.stdout, Result{
				Status: "success", PageID: childResult.PageID, PageURL: childResult.PageURL,
				Title: childResult.Title, SpaceKey: childResult.SpaceKey, Action: childResult.Action, Version: childResult.Version,
			}, app.jsonOutput)
		}()

		// Salvar resultado para resolução de links inter-documento
		if app.docResults != nil {
			absPath, _ := filepath.Abs(f.Path)
			app.addDocResult(absPath, &docPublishResult{
				pageID:    childResult.PageID,
				pageURL:   childResult.PageURL,
				title:     childTitle,
				finalHTML: html,
			})
		}
	}

	// Recursão nos subdiretórios
	for _, child := range tree.Children {
		if err := app.publishDirTreeServer(client, dirResult.PageID, &child, false); err != nil {
			return err
		}
	}

	return nil
}

// publishServerInput agrupa parâmetros para publicação via Server/DC.
type publishServerInput struct {
	parentID  string
	title     string
	pageID    string
	html      string
	inputPath string
}

// publishOrSkipServer publica ou atualiza uma página via Server/DC API.
func (app *appEnv) publishOrSkipServer(client *confluence.ServerClient, in publishServerInput) (*confluence.PublishResult, error) {
	switch {
	case in.pageID != "":
		page, err := client.GetPage(in.pageID)
		if err != nil {
			return nil, app.wrapConfluenceError(err)
		}
		if page.Body.AtlasDocFormat.Value == in.html {
			app.logger.Info("Skipped (unchanged)", "title", in.title, "pageID", in.pageID)
			return &confluence.PublishResult{
				PageID:   in.pageID,
				Title:    in.title,
				SpaceKey: app.space,
				Action:   "skipped",
				Version:  page.Version.Number,
			}, nil
		}
		result, err := client.UpdatePage(in.pageID, in.title, in.html, page.Version.Number)
		if err != nil {
			return nil, app.wrapConfluenceError(err)
		}
		return result, nil

	case app.force:
		page, err := client.FindByTitle(app.space, in.title)
		if err != nil {
			return nil, app.wrapConfluenceError(err)
		}
		if page != nil {
			if err := app.moveIfNeededServer(client, page.ID, page.ParentID, in.parentID); err != nil {
				return nil, err
			}
			result, err := client.UpdatePage(page.ID, in.title, in.html, page.Version.Number)
			if err != nil {
				return nil, app.wrapConfluenceError(err)
			}
			return result, nil
		}
		result, err := client.CreatePage(app.space, in.title, in.parentID, in.html)
		if err != nil {
			return nil, app.wrapConfluenceError(err)
		}
		return result, nil

	default:
		result, err := client.CreatePage(app.space, in.title, in.parentID, in.html)
		if err != nil {
			return nil, app.wrapConfluenceError(err)
		}
		return result, nil
	}
}

// moveIfNeededServer move uma página para o parent correto via Server/DC API.
func (app *appEnv) moveIfNeededServer(client *confluence.ServerClient, pageID, currentParentID, desiredParentID string) error {
	if desiredParentID == "" || currentParentID == desiredParentID {
		return nil
	}
	page, err := client.GetPage(pageID)
	if err != nil {
		return app.wrapConfluenceError(err)
	}
	// Atualiza ancestors para mover a página
	_, err = client.UpdatePage(pageID, page.Title, page.Body.AtlasDocFormat.Value, page.Version.Number)
	if err != nil {
		app.logger.Warn("Could not move page", "pageID", pageID, "error", err)
	}
	return nil
}

// resolveInterDocLinksServer substitui links relativos *.md no Storage Format HTML
// por URLs absolutas do Confluence usando os page-ids coletados no primeiro pass.
func (app *appEnv) resolveInterDocLinksServer(client *confluence.ServerClient) error {
	// Construir mapa: abs path do .md → URL da página Confluence
	linkMap := make(map[string]string, len(app.docResults))
	for absPath, res := range app.docResults {
		linkMap[absPath] = res.pageURL
	}

	baseURL := strings.TrimRight(app.url, "/")

	for absPath, res := range app.docResults {
		if res.finalHTML == "" {
			continue
		}

		baseDir := filepath.Dir(absPath)
		patchedHTML := res.finalHTML
		count := 0

		// Substituir href="*.md" e href="subdir/*.md" por links Confluence
		// Regex: href="([^"]*\.md)"
		for targetPath, targetRes := range app.docResults {
			// Compute relative path from current doc to target
			relPath, err := filepath.Rel(baseDir, targetPath)
			if err != nil {
				continue
			}

			// Procurar href="relPath" no HTML
			oldHref := fmt.Sprintf("href=\"%s\"", relPath)
			if !strings.Contains(patchedHTML, oldHref) {
				continue
			}

			// Gerar URL Confluence para o target
			var newURL string
			if targetRes.pageURL != "" {
				newURL = targetRes.pageURL
			} else {
				newURL = fmt.Sprintf("%s/pages/viewpage.action?pageId=%s", baseURL, targetRes.pageID)
			}

			newHref := fmt.Sprintf("href=\"%s\"", newURL)
			patchedHTML = strings.ReplaceAll(patchedHTML, oldHref, newHref)
			count++
		}

		if count == 0 {
			continue
		}

		// Re-publicar a página com links resolvidos
		page, err := client.GetPage(res.pageID)
		if err != nil {
			app.logger.Warn("Could not fetch page for link resolution", "pageID", res.pageID, "error", err)
			continue
		}

		if _, err := client.UpdatePage(res.pageID, res.title, patchedHTML, page.Version.Number); err != nil {
			app.logger.Warn("Could not update page with resolved links", "pageID", res.pageID, "error", err)
			continue
		}

		app.logger.Info("Resolved inter-document links", "count", count, "file", filepath.Base(absPath))
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
	if tree.Readme != nil {
		if err := app.runFile(tree.Readme.Path); err != nil {
			return err
		}
	}
	for _, f := range tree.Files {
		if err := app.runFile(f.Path); err != nil {
			return err
		}
	}
	for _, child := range tree.Children {
		if err := app.convertDirTree(&child); err != nil {
			return err
		}
	}
	return nil
}

func (app *appEnv) publishDirTree(client *confluence.Client, spaceID, parentID string, tree *DirEntry, isRoot bool) error {
	var pagePath string
	var pageSource []byte
	var pageDoc *adf.Document
	var pageContent []byte

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
		emptyDoc := adf.NewDocument()
		adfJSON, _ := json.MarshalIndent(emptyDoc, "", "  ")
		pageContent = adfJSON
	}

	var title string
	if isRoot && app.title != "" {
		title = app.title
	} else if tree.Readme != nil {
		title = titleFromSource(tree.Readme.Content, tree.Name)
	} else {
		title = tree.Name
	}

	existingPageID := ""
	if pageSource != nil {
		existingPageID = extractPageID(pageSource)
	}

	dirResult, err := app.publishOrSkip(publishInput{
		client:    client,
		spaceID:   spaceID,
		parentID:  parentID,
		title:     title,
		pageID:    existingPageID,
		adfStr:    string(pageContent),
		force:     app.force,
		inputPath: pagePath,
	})
	if err != nil {
		return err
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
		childTitle := titleFromSource(f.Content, strings.TrimSuffix(filepath.Base(f.Path), ".md"))

		childResult, err := app.publishOrSkip(publishInput{
			client:    client,
			spaceID:   spaceID,
			parentID:  dirResult.PageID,
			title:     childTitle,
			pageID:    extractPageID(f.Content),
			adfStr:    string(adfJSON),
			force:     app.force,
			inputPath: f.Path,
		})
		if err != nil {
			return err
		}

		if app.writeMarker && childResult.PageID != "" {
			if err := app.writePageIDMarker(f.Path, f.Content, childResult.PageID); err != nil {
				app.logger.Warn("Could not write page-id marker", "error", err)
			}
		}

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
