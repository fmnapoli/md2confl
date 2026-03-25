// Copyright 2026 md2confl contributors
// SPDX-License-Identifier: Apache-2.0

package parser

import (
	"bytes"
	"fmt"
	"html"
	"strings"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	east "github.com/yuin/goldmark/extension/ast"
	"github.com/yuin/goldmark/renderer"
	gmhtml "github.com/yuin/goldmark/renderer/html"
	"github.com/yuin/goldmark/util"

	emoji "github.com/yuin/goldmark-emoji"
)

// ConvertToStorageFormat converte Markdown para Confluence Storage Format (XHTML).
// Storage Format é o formato nativo do Confluence Server/Data Center.
func ConvertToStorageFormat(source []byte) (string, error) {
	md := goldmark.New(
		goldmark.WithExtensions(
			extension.GFM,
			emoji.Emoji,
			&superscriptExtension{},
		),
		goldmark.WithRenderer(
			renderer.NewRenderer(
				renderer.WithNodeRenderers(
					// Usa o renderer HTML padrão como base (XHTML mode para Confluence)
					util.Prioritized(gmhtml.NewRenderer(
						gmhtml.WithUnsafe(),
						gmhtml.WithXHTML(),
					), 1000),
					// Renderer customizado para macros Confluence (prioridade maior)
					util.Prioritized(&storageRenderer{}, 100),
				),
			),
		),
	)

	var buf bytes.Buffer
	if err := md.Convert(source, &buf); err != nil {
		return "", fmt.Errorf("converting markdown to storage format: %w", err)
	}

	result := buf.String()

	// Pós-processamento: converter GitHub alerts em painéis Confluence
	result = convertAlertsToPanels(result)

	return result, nil
}

// storageRenderer renderiza nós específicos para Confluence Storage Format.
type storageRenderer struct{}

func (r *storageRenderer) RegisterFuncs(reg renderer.NodeRendererFuncRegisterer) {
	// Code blocks → ac:structured-macro (para syntax highlighting)
	reg.Register(ast.KindFencedCodeBlock, r.renderFencedCodeBlock)

	// Tables — usar renderer HTML padrão mas com classes Confluence
	reg.Register(east.KindTable, r.renderTable)
	reg.Register(east.KindTableHeader, r.renderTableHeader)
	reg.Register(east.KindTableRow, r.renderTableRow)
	reg.Register(east.KindTableCell, r.renderTableCell)

	// Task lists (checkboxes)
	reg.Register(east.KindTaskCheckBox, r.renderTaskCheckBox)
}

func (r *storageRenderer) renderFencedCodeBlock(w util.BufWriter, source []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
	n := node.(*ast.FencedCodeBlock)
	if entering {
		lang := ""
		if n.Language(source) != nil {
			lang = string(n.Language(source))
		}

		// Mermaid blocks: manter como code block simples (sem macro)
		if lang == "mermaid" {
			_, _ = w.WriteString("<pre><code class=\"language-mermaid\">")
			writeLines(w, source, n)
			_, _ = w.WriteString("</code></pre>\n")
			return ast.WalkSkipChildren, nil
		}

		_, _ = w.WriteString("<ac:structured-macro ac:name=\"code\"")
		_, _ = w.WriteString(" ac:schema-version=\"1\">")
		if lang != "" {
			_, _ = fmt.Fprintf(w, "<ac:parameter ac:name=\"language\">%s</ac:parameter>", html.EscapeString(lang))
		}
		_, _ = w.WriteString("<ac:plain-text-body><![CDATA[")
		writeLines(w, source, n)
		_, _ = w.WriteString("]]></ac:plain-text-body>")
		_, _ = w.WriteString("</ac:structured-macro>\n")
	}
	return ast.WalkSkipChildren, nil
}

func (r *storageRenderer) renderTable(w util.BufWriter, _ []byte, _ ast.Node, entering bool) (ast.WalkStatus, error) {
	if entering {
		_, _ = w.WriteString("<table class=\"confluenceTable\"><tbody>\n")
	} else {
		_, _ = w.WriteString("</tbody></table>\n")
	}
	return ast.WalkContinue, nil
}

func (r *storageRenderer) renderTableHeader(w util.BufWriter, _ []byte, _ ast.Node, entering bool) (ast.WalkStatus, error) {
	if entering {
		_, _ = w.WriteString("<tr>")
	} else {
		_, _ = w.WriteString("</tr>\n")
	}
	return ast.WalkContinue, nil
}

func (r *storageRenderer) renderTableRow(w util.BufWriter, _ []byte, _ ast.Node, entering bool) (ast.WalkStatus, error) {
	if entering {
		_, _ = w.WriteString("<tr>")
	} else {
		_, _ = w.WriteString("</tr>\n")
	}
	return ast.WalkContinue, nil
}

func (r *storageRenderer) renderTableCell(w util.BufWriter, _ []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
	n := node.(*east.TableCell)
	tag := "td"
	class := "confluenceTd"
	if n.Parent().Kind() == east.KindTableHeader {
		tag = "th"
		class = "confluenceTh"
	}
	if entering {
		_, _ = fmt.Fprintf(w, "<%s class=\"%s\">", tag, class)
	} else {
		_, _ = fmt.Fprintf(w, "</%s>", tag)
	}
	return ast.WalkContinue, nil
}

func (r *storageRenderer) renderTaskCheckBox(w util.BufWriter, _ []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
	if !entering {
		return ast.WalkContinue, nil
	}
	n := node.(*east.TaskCheckBox)
	if n.IsChecked {
		_, _ = w.WriteString("<ac:task-status>complete</ac:task-status> ")
	} else {
		_, _ = w.WriteString("<ac:task-status>incomplete</ac:task-status> ")
	}
	return ast.WalkContinue, nil
}

// writeLines escreve as linhas de texto de um code block.
func writeLines(w util.BufWriter, source []byte, n ast.Node) {
	for i := 0; i < n.Lines().Len(); i++ {
		line := n.Lines().At(i)
		_, _ = w.Write(line.Value(source))
	}
}

// alertTypeToPanel mapeia tipos de alerta GitHub para tipos de painel Confluence.
var alertTypeToPanel = map[string]string{
	"NOTE":      "info",
	"TIP":       "tip",
	"IMPORTANT": "note",
	"WARNING":   "warning",
	"CAUTION":   "warning",
}

// convertAlertsToPanels converte blockquotes com alertas GitHub em painéis Confluence.
func convertAlertsToPanels(s string) string {
	for alertType, panelType := range alertTypeToPanel {
		marker := fmt.Sprintf("[!%s]", alertType)
		if !strings.Contains(s, marker) {
			continue
		}

		// Substituir abertura: <blockquote>\n<p>[!TYPE]\n → macro abertura
		openOld := fmt.Sprintf("<blockquote>\n<p>[!%s]\n", alertType)
		openNew := fmt.Sprintf("<ac:structured-macro ac:name=\"%s\"><ac:rich-text-body>\n<p>", panelType)
		s = strings.ReplaceAll(s, openOld, openNew)

		// Variação sem newline após marker
		openOld2 := fmt.Sprintf("<blockquote>\n<p>[!%s]", alertType)
		openNew2 := fmt.Sprintf("<ac:structured-macro ac:name=\"%s\"><ac:rich-text-body>\n<p>", panelType)
		s = strings.ReplaceAll(s, openOld2, openNew2)
	}

	// Substituir fechamento: </blockquote> que segue uma rich-text-body
	// Apenas se estamos dentro de uma macro (heurística: painel aberto antes)
	s = strings.ReplaceAll(s, "</blockquote>", "</ac:rich-text-body></ac:structured-macro>")

	return s
}
