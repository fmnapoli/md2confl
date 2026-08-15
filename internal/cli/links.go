// Copyright 2026 md2confl contributors
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"os"
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

// hrefSchemeRegex detecta um href absoluto (http:, mailto:, …). Espelha o
// schemeRegex do pacote adf, que não é exportado.
var hrefSchemeRegex = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9+.-]*:`)

// markdownHrefPath resolves a relative Markdown href to an absolute path.
// Returns false for anything that is not a relative link to a .md file
// (absolute URLs, anchors, images, links to other file types).
func markdownHrefPath(href, baseDir string) (string, bool) {
	if href == "" || strings.HasPrefix(href, "//") || strings.HasPrefix(href, "#") || hrefSchemeRegex.MatchString(href) {
		return "", false
	}
	if idx := strings.Index(href, "#"); idx >= 0 {
		href = href[:idx]
	}
	if !strings.HasSuffix(strings.ToLower(href), ".md") {
		return "", false
	}
	abs, err := filepath.Abs(filepath.Join(baseDir, href))
	if err != nil {
		return "", false
	}
	return abs, true
}

// registerLinkTargetsFromConfig anota, sem publicar, as páginas dos documentos
// do config que ficaram fora do filtro desta execução.
//
// Publicar um documento isolado (--config X --input doc.md) não roda o segundo
// pass, porque só há um documento em docResults; sem isso o corpo publicado sai
// com os links relativos crus — o estado do incidente que originou este
// mecanismo. Os page-ids vêm dos marcadores confluence-page-id dos outros
// documentos, e só as páginas de fato referenciadas são consultadas na API.
func (app *appEnv) registerLinkTargetsFromConfig(client *confluence.ServerClient, published []DocConfig) {
	if app.config == nil || app.docResults == nil {
		return
	}

	// Alvos referenciados pelos documentos que esta execução publicou.
	referenced := map[string]bool{}
	for absPath, res := range app.docResults {
		if res.finalHTML == "" {
			continue
		}
		baseDir := filepath.Dir(absPath)
		for _, href := range extractHrefs(res.finalHTML) {
			if target, ok := markdownHrefPath(href, baseDir); ok {
				referenced[target] = true
			}
		}
	}
	if len(referenced) == 0 {
		return
	}

	publishedInputs := map[string]bool{}
	for _, doc := range published {
		abs, _ := filepath.Abs(doc.Input)
		publishedInputs[abs] = true
	}

	for _, doc := range app.config.Documents {
		absInput, _ := filepath.Abs(doc.Input)
		if publishedInputs[absInput] {
			continue
		}
		for _, file := range collectMarkdownFiles(doc.Input) {
			abs, _ := filepath.Abs(file)
			if !referenced[abs] {
				continue
			}
			app.docResultsMu.Lock()
			_, known := app.docResults[abs]
			app.docResultsMu.Unlock()
			if known {
				continue
			}
			app.registerLinkTarget(client, abs)
		}
	}
}

// registerLinkTarget lê o marcador de page-id do arquivo e guarda a URL da
// página como destino de link, sem publicar nada.
func (app *appEnv) registerLinkTarget(client *confluence.ServerClient, absPath string) {
	source, err := os.ReadFile(absPath)
	if err != nil {
		return
	}
	pageID := extractPageID(source)
	if pageID == "" {
		app.logger.Debug("Link target has no page-id marker", "file", filepath.Base(absPath))
		return
	}
	// A URL precisa ser a mesma que uma execução completa gravaria, senão a
	// execução completa seguinte reescreveria a página só para trocá-la.
	page, err := client.GetPage(pageID)
	if err != nil {
		app.logger.Warn("Could not resolve a link target outside this run",
			"file", filepath.Base(absPath), "pageID", pageID, "error", err)
		return
	}
	app.addDocResult(absPath, &docPublishResult{
		pageID:   pageID,
		pageURL:  page.Links.Base + page.Links.WebUI,
		title:    page.Title,
		linkOnly: true,
	})
	app.logger.Debug("Registered a link target outside this run", "file", filepath.Base(absPath), "pageID", pageID)
}

// collectMarkdownFiles devolve os arquivos .md de um input do config, seja ele
// um arquivo ou um diretório.
func collectMarkdownFiles(input string) []string {
	info, err := os.Stat(input)
	if err != nil {
		return nil
	}
	if !info.IsDir() {
		if strings.HasSuffix(strings.ToLower(input), ".md") {
			return []string{input}
		}
		return nil
	}
	tree, err := buildDirTree(input)
	if err != nil {
		return nil
	}
	return treeMarkdownFiles(tree)
}

func treeMarkdownFiles(tree *DirEntry) []string {
	var files []string
	if tree.Readme != nil {
		files = append(files, tree.Readme.Path)
	}
	for _, f := range tree.Files {
		files = append(files, f.Path)
	}
	for i := range tree.Children {
		files = append(files, treeMarkdownFiles(&tree.Children[i])...)
	}
	return files
}

// warnUnresolvedMarkdownLinks avisa quando um corpo publicado ficou com link
// relativo para .md — o leitor da página cai num 404 do Confluence.
func (app *appEnv) warnUnresolvedMarkdownLinks() {
	for absPath, res := range app.docResults {
		if res.finalHTML == "" {
			continue
		}
		baseDir := filepath.Dir(absPath)
		var unresolved []string
		for _, href := range extractHrefs(res.finalHTML) {
			if _, ok := markdownHrefPath(href, baseDir); ok {
				unresolved = append(unresolved, href)
			}
		}
		if len(unresolved) == 0 {
			continue
		}
		app.logger.Warn("Published with unresolved relative links — no second pass ran for this document",
			"file", filepath.Base(absPath), "links", strings.Join(unresolved, ", "))
		app.addWarning(fmt.Sprintf("%s was published with relative Markdown links (%s); publish the whole config so they resolve to Confluence URLs",
			filepath.Base(absPath), strings.Join(unresolved, ", ")))
	}
}

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
