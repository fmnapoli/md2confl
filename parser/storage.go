// Copyright 2026 md2confl contributors
// SPDX-License-Identifier: Apache-2.0

package parser

import (
	"bytes"
	"fmt"
	"html"
	"net/url"
	"regexp"
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

	// Pós-processamento: substituir listas de anchor links por macro TOC
	result = convertAnchorListToTOC(result)

	return result, nil
}

// storageRenderer renderiza nós específicos para Confluence Storage Format.
type storageRenderer struct{}

func (r *storageRenderer) RegisterFuncs(reg renderer.NodeRendererFuncRegisterer) {
	// Headings → anchor macro para links internos (#secao)
	reg.Register(ast.KindHeading, r.renderHeading)

	// Links → normalizar anchors internos (#secao) para ASCII
	reg.Register(ast.KindLink, r.renderLink)

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

func (r *storageRenderer) renderHeading(w util.BufWriter, source []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
	n := node.(*ast.Heading)
	if entering {
		// Gerar anchor slug a partir do texto do heading (estilo GitHub)
		text := extractHeadingText(node, source)
		slug := slugify(text)
		if slug != "" {
			_, _ = fmt.Fprintf(w, "<ac:structured-macro ac:name=\"anchor\"><ac:parameter ac:name=\"\">%s</ac:parameter></ac:structured-macro>\n", html.EscapeString(slug))
		}
		_, _ = fmt.Fprintf(w, "<h%d>", n.Level)
	} else {
		_, _ = fmt.Fprintf(w, "</h%d>\n", n.Level)
	}
	return ast.WalkContinue, nil
}

// extractHeadingText extrai o texto puro de um heading node.
func extractHeadingText(node ast.Node, source []byte) string {
	var buf strings.Builder
	for child := node.FirstChild(); child != nil; child = child.NextSibling() {
		if child.Kind() == ast.KindText {
			buf.Write(child.(*ast.Text).Segment.Value(source))
		} else if child.Kind() == ast.KindCodeSpan {
			// Extrair texto do code span
			for grandchild := child.FirstChild(); grandchild != nil; grandchild = grandchild.NextSibling() {
				if grandchild.Kind() == ast.KindText {
					buf.Write(grandchild.(*ast.Text).Segment.Value(source))
				}
			}
		}
	}
	return buf.String()
}

// slugify converte texto em slug ASCII para uso como anchor no Confluence.
// Remove acentos para evitar mismatch entre URL-encoding e HTML entities.
func slugify(s string) string {
	s = strings.ToLower(s)
	s = strings.TrimSpace(s)
	s = removeAccents(s)
	var buf strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			buf.WriteRune(r)
		case r == ' ', r == '-':
			buf.WriteRune('-')
		}
	}
	return buf.String()
}

// removeAccents substitui caracteres acentuados por seus equivalentes ASCII.
func removeAccents(s string) string {
	replacements := []struct{ from, to string }{
		{"á", "a"}, {"à", "a"}, {"ã", "a"}, {"â", "a"}, {"ä", "a"},
		{"é", "e"}, {"è", "e"}, {"ê", "e"}, {"ë", "e"},
		{"í", "i"}, {"ì", "i"}, {"î", "i"}, {"ï", "i"},
		{"ó", "o"}, {"ò", "o"}, {"õ", "o"}, {"ô", "o"}, {"ö", "o"},
		{"ú", "u"}, {"ù", "u"}, {"û", "u"}, {"ü", "u"},
		{"ç", "c"}, {"ñ", "n"},
	}
	for _, r := range replacements {
		s = strings.ReplaceAll(s, r.from, r.to)
	}
	return s
}

func (r *storageRenderer) renderLink(w util.BufWriter, source []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
	n := node.(*ast.Link)
	dest := string(n.Destination)

	if entering {
		if strings.HasPrefix(dest, "#") {
			// Anchor interno: normalizar para ASCII slug
			anchor := dest[1:]
			// Decodificar URL-encoding se presente
			if strings.Contains(anchor, "%") {
				if decoded, err := url.PathUnescape(anchor); err == nil {
					anchor = decoded
				}
			}
			anchor = slugify(anchor)
			_, _ = fmt.Fprintf(w, "<a href=\"#%s\">", html.EscapeString(anchor))
		} else {
			_, _ = fmt.Fprintf(w, "<a href=\"%s\">", html.EscapeString(dest))
		}
	} else {
		_, _ = w.WriteString("</a>")
	}
	return ast.WalkContinue, nil
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
// Blockquotes normais (sem marker [!TYPE]) são preservados intactos.
func convertAlertsToPanels(s string) string {
	for alertType, panelType := range alertTypeToPanel {
		marker := fmt.Sprintf("[!%s]", alertType)
		if !strings.Contains(s, marker) {
			continue
		}

		// Substituir abertura: <blockquote>\n<p>[!TYPE]\n → macro abertura
		// e marcar o </blockquote> correspondente usando heurística posicional
		openOld := fmt.Sprintf("<blockquote>\n<p>[!%s]\n", alertType)
		openNew := fmt.Sprintf("<ac:structured-macro ac:name=\"%s\"><ac:rich-text-body>\n<p>", panelType)
		s = strings.ReplaceAll(s, openOld, openNew)

		// Variação sem newline após marker
		openOld2 := fmt.Sprintf("<blockquote>\n<p>[!%s]", alertType)
		openNew2 := fmt.Sprintf("<ac:structured-macro ac:name=\"%s\"><ac:rich-text-body>\n<p>", panelType)
		s = strings.ReplaceAll(s, openOld2, openNew2)
	}

	// Substituir apenas </blockquote> que pertencem a macros convertidas.
	// Estratégia: percorrer o texto e fechar macros abertas que encontrem </blockquote>.
	var result strings.Builder
	result.Grow(len(s))
	openMacros := 0
	remaining := s
	for len(remaining) > 0 {
		// Procurar próxima tag relevante
		macroIdx := strings.Index(remaining, "<ac:rich-text-body>")
		closeIdx := strings.Index(remaining, "</blockquote>")

		if macroIdx >= 0 && (closeIdx < 0 || macroIdx < closeIdx) {
			// Encontrou abertura de macro antes de um </blockquote>
			end := macroIdx + len("<ac:rich-text-body>")
			result.WriteString(remaining[:end])
			remaining = remaining[end:]
			openMacros++
		} else if closeIdx >= 0 && openMacros > 0 {
			// Encontrou </blockquote> com macro aberta — substituir
			result.WriteString(remaining[:closeIdx])
			result.WriteString("</ac:rich-text-body></ac:structured-macro>")
			remaining = remaining[closeIdx+len("</blockquote>"):]
			openMacros--
		} else {
			// Sem mais macros abertas ou sem tags — copiar tudo
			result.WriteString(remaining)
			break
		}
	}

	return result.String()
}

// convertAnchorListToTOC substitui listas de anchor links (#secao) por macro TOC do Confluence.
// Detecta a primeira lista <ul> top-level onde a maioria dos <li> contém links href="#...".
func convertAnchorListToTOC(s string) string {
	// Encontrar blocos <ul>...</ul> top-level (incluindo nested)
	// Usa contagem de tags para encontrar o </ul> de fechamento correto
	idx := 0
	for {
		start := strings.Index(s[idx:], "<ul>")
		if start < 0 {
			break
		}
		start += idx

		// Encontrar o </ul> de fechamento correto (contando nesting)
		depth := 1
		pos := start + 4
		for pos < len(s) && depth > 0 {
			nextOpen := strings.Index(s[pos:], "<ul>")
			nextClose := strings.Index(s[pos:], "</ul>")
			if nextClose < 0 {
				break
			}
			if nextOpen >= 0 && nextOpen < nextClose {
				depth++
				pos += nextOpen + 4
			} else {
				depth--
				if depth == 0 {
					pos += nextClose + 5
				} else {
					pos += nextClose + 5
				}
			}
		}
		if depth != 0 {
			idx = start + 4
			continue
		}

		block := s[start:pos]

		// Contar <li> com anchor links vs total
		liCount := strings.Count(block, "<li>")
		anchorLiRe := regexp.MustCompile(`<li>\s*<a href="#[^"]+">`)
		anchorCount := len(anchorLiRe.FindAllString(block, -1))

		if liCount >= 3 && float64(anchorCount)/float64(liCount) >= 0.7 {
			toc := "<ac:structured-macro ac:name=\"toc\">" +
				"<ac:parameter ac:name=\"minLevel\">2</ac:parameter>" +
				"<ac:parameter ac:name=\"maxLevel\">4</ac:parameter>" +
				"</ac:structured-macro>\n"
			s = s[:start] + toc + s[pos:]
			// Só substituir a primeira ocorrência (o TOC real da página)
			break
		}

		idx = pos
	}
	return s
}
