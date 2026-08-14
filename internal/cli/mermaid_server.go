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

// patchMermaidRefs substitui <img src="local-path"> pelas referências de
// attachment do Confluence. É uma transformação pura: só depende do HTML e dos
// paths dos SVGs, então pode rodar antes de publicar.
//
// Rodar antes importa para a idempotência: o path local vive num diretório
// temporário com nome aleatório, enquanto o nome do arquivo é derivado do
// conteúdo do diagrama. Publicar o HTML já com a referência de attachment
// mantém o corpo estável entre execuções — e dispensa a segunda publicação que
// existia só para trocar as referências depois do upload.
func patchMermaidRefs(storageHTML string, svgPaths []string) string {
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
	return patched
}

// uploadMermaidAttachments faz upload dos SVGs renderizados como attachments da
// página. O upload roda mesmo quando o corpo é pulado por já estar publicado:
// o attachment pode ter falhado numa execução anterior e o corpo, idêntico,
// nunca mais seria republicado para consertá-lo.
func uploadMermaidAttachments(client *confluence.ServerClient, pageID string, svgPaths []string, logger interface{ Info(string, ...any) }) {
	for _, svgPath := range svgPaths {
		if _, err := os.Stat(svgPath); err != nil {
			continue
		}
		if _, err := client.UploadAttachment(pageID, svgPath); err != nil {
			logger.Info("Warning: failed to upload mermaid SVG", "path", svgPath, "error", err)
		}
	}
}
