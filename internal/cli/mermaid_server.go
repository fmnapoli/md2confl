// Copyright 2026 md2confl contributors
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"golang.org/x/sync/errgroup"

	"github.com/fmnapoli/md2confl/confluence"
	"github.com/fmnapoli/md2confl/mermaid"
)

// mermaidCodeBlockPattern identifica blocos ```mermaid ... ``` no markdown.
var mermaidCodeBlockPattern = regexp.MustCompile("(?s)```mermaid\n(.*?)```")

// renderMermaidInMarkdown renderiza blocos mermaid no markdown para SVG e substitui
// por referências de imagem local. Retorna o markdown modificado e os paths dos SVGs.
func renderMermaidInMarkdown(source []byte) ([]byte, []string, error) {
	matches := mermaidCodeBlockPattern.FindAllSubmatchIndex(source, -1)
	if len(matches) == 0 {
		return source, nil, nil
	}

	if err := mermaid.EnsureAvailable(); err != nil {
		return source, nil, nil
	}

	tempDir, err := os.MkdirTemp("", "md2confl-mermaid-*")
	if err != nil {
		return nil, nil, fmt.Errorf("creating temp dir for mermaid: %w", err)
	}

	renderer := &mermaid.Renderer{OutputDir: tempDir}

	type renderResult struct {
		svgPath string
	}
	results := make([]renderResult, len(matches))

	g, ctx := errgroup.WithContext(context.Background())
	g.SetLimit(2)
	for i, match := range matches {
		diagramSource := source[match[2]:match[3]]
		g.Go(func() error {
			svgPath, err := renderer.Render(ctx, diagramSource)
			if err != nil {
				return fmt.Errorf("rendering mermaid diagram: %w", err)
			}
			results[i] = renderResult{svgPath: svgPath}
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		return nil, nil, err
	}

	// Substituir blocos mermaid por referências de imagem (de trás para frente para preservar offsets)
	modified := make([]byte, len(source))
	copy(modified, source)
	var svgPaths []string

	for i := len(matches) - 1; i >= 0; i-- {
		svgPath := results[i].svgPath
		svgPaths = append(svgPaths, svgPath)
		imgRef := fmt.Sprintf("![mermaid diagram](%s)", svgPath)
		modified = append(modified[:matches[i][0]], append([]byte(imgRef), modified[matches[i][1]:]...)...)
	}

	return modified, svgPaths, nil
}

// uploadAndPatchImagesServer faz upload de imagens locais e substitui referências no HTML.
// Retorna o HTML atualizado.
func uploadAndPatchImagesServer(client *confluence.ServerClient, pageID, storageHTML string, svgPaths []string, logger interface{ Info(string, ...any) }) (string, error) {
	if len(svgPaths) == 0 {
		return storageHTML, nil
	}

	// Upload SVGs como attachments
	for _, svgPath := range svgPaths {
		if _, err := os.Stat(svgPath); err != nil {
			continue
		}
		_, err := client.UploadAttachment(pageID, svgPath)
		if err != nil {
			logger.Info("Warning: failed to upload mermaid SVG", "path", svgPath, "error", err)
			continue
		}
	}

	// Substituir <img src="local-path"> por <ac:image><ri:attachment ri:filename="..."/></ac:image>
	patched := storageHTML
	for _, svgPath := range svgPaths {
		filename := filepath.Base(svgPath)
		// goldmark emite: <img src="path" alt="...">
		// Precisamos encontrar e substituir
		oldImgPattern := fmt.Sprintf(`<img src="%s" alt="mermaid diagram"`, svgPath)
		newImg := fmt.Sprintf(`<ac:image ac:width="800"><ri:attachment ri:filename="%s" /></ac:image`, filename)

		if strings.Contains(patched, oldImgPattern) {
			// Substituir a tag img completa
			patched = strings.Replace(patched, oldImgPattern+">", newImg+">", 1)
			patched = strings.Replace(patched, oldImgPattern+" />", newImg+">", 1)
		}
	}

	return patched, nil
}
