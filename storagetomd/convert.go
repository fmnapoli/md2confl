// Copyright 2026 md2confl contributors
// SPDX-License-Identifier: Apache-2.0

// Package storagetomd converte Confluence Storage Format (XHTML) para Markdown.
// Suporta macros Confluence (ac:structured-macro, ac:link, ac:image) e
// elementos HTML padrão.
package storagetomd

import (
	"fmt"
	"html"
	"strings"

	xhtml "golang.org/x/net/html"
)

// Convert converte Confluence Storage Format (XHTML) para Markdown.
func Convert(storageFormat string) (string, error) {
	// Pré-processar: converter CDATA sections em texto (HTML parser ignora CDATA)
	processed := strings.ReplaceAll(storageFormat, "<![CDATA[", "")
	processed = strings.ReplaceAll(processed, "]]>", "")

	// Wrap em elemento raiz para garantir parsing válido
	wrapped := "<div>" + processed + "</div>"
	doc, err := xhtml.Parse(strings.NewReader(wrapped))
	if err != nil {
		return "", fmt.Errorf("parsing storage format: %w", err)
	}

	var sb strings.Builder
	c := &converter{w: &sb}

	// Navegar até o <div> raiz (html > head > body > div)
	root := findElement(doc, "div")
	if root == nil {
		return "", fmt.Errorf("could not find root element")
	}

	c.walkChildren(root)
	return strings.TrimRight(sb.String(), "\n") + "\n", nil
}

type converter struct {
	w           *strings.Builder
	listDepth   int
	orderedList bool
	listCounter int
	inTableHead bool
}

func (c *converter) walkChildren(n *xhtml.Node) {
	for child := n.FirstChild; child != nil; child = child.NextSibling {
		c.walk(child)
	}
}

func (c *converter) walk(n *xhtml.Node) {
	if n.Type == xhtml.TextNode {
		text := n.Data
		// Não emitir whitespace puro entre blocos
		if strings.TrimSpace(text) != "" {
			c.w.WriteString(html.UnescapeString(text))
		}
		return
	}

	if n.Type != xhtml.ElementNode {
		c.walkChildren(n)
		return
	}

	switch n.Data {
	case "h1", "h2", "h3", "h4", "h5", "h6":
		c.renderHeading(n)
	case "p":
		c.renderParagraph(n)
	case "br":
		c.w.WriteString("  \n")
	case "hr":
		c.w.WriteString("\n---\n\n")
	case "strong", "b":
		c.w.WriteString("**")
		c.walkChildren(n)
		c.w.WriteString("**")
	case "em", "i":
		c.w.WriteString("*")
		c.walkChildren(n)
		c.w.WriteString("*")
	case "del", "s":
		c.w.WriteString("~~")
		c.walkChildren(n)
		c.w.WriteString("~~")
	case "code":
		c.w.WriteString("`")
		c.walkChildren(n)
		c.w.WriteString("`")
	case "pre":
		c.renderPre(n)
	case "a":
		c.renderLink(n)
	case "img":
		c.renderImage(n)
	case "ul":
		c.renderList(n, false)
	case "ol":
		c.renderList(n, true)
	case "li":
		c.renderListItem(n)
	case "blockquote":
		c.renderBlockquote(n)
	case "table":
		c.renderTable(n)
	case "thead", "tbody":
		c.walkChildren(n)
	case "tr":
		c.renderTableRow(n)
	case "th":
		c.inTableHead = true
		c.w.WriteString("| ")
		c.walkChildren(n)
		c.w.WriteString(" ")
		c.inTableHead = false
	case "td":
		c.w.WriteString("| ")
		c.walkChildren(n)
		c.w.WriteString(" ")
	case "sup":
		c.w.WriteString("^")
		c.walkChildren(n)
		c.w.WriteString("^")
	case "span", "div", "section":
		c.walkChildren(n)
	case "ac:structured-macro":
		c.renderMacro(n)
	case "ac:link":
		c.renderAcLink(n)
	case "ac:image":
		c.renderAcImage(n)
	case "ac:emoticon":
		// Ignorar emoticons Confluence
	default:
		// Fallback: renderizar conteúdo sem a tag
		c.walkChildren(n)
	}
}

func (c *converter) renderHeading(n *xhtml.Node) {
	level := int(n.Data[1] - '0')
	c.w.WriteString("\n")
	c.w.WriteString(strings.Repeat("#", level))
	c.w.WriteString(" ")
	c.walkChildren(n)
	c.w.WriteString("\n\n")
}

func (c *converter) renderParagraph(n *xhtml.Node) {
	c.walkChildren(n)
	c.w.WriteString("\n\n")
}

func (c *converter) renderPre(n *xhtml.Node) {
	// <pre> pode conter <code> com classe de linguagem
	codeNode := findElement(n, "code")
	if codeNode != nil {
		lang := ""
		for _, a := range codeNode.Attr {
			if a.Key == "class" && strings.HasPrefix(a.Val, "language-") {
				lang = strings.TrimPrefix(a.Val, "language-")
			}
		}

		// Confluence pode substituir código mermaid por ac:image dentro do <code>.
		// Nesse caso, renderizar a imagem em vez do code block.
		if img := findElement(codeNode, "ac:image"); img != nil {
			c.renderAcImage(img)
			c.w.WriteString("\n\n")
			return
		}

		c.w.WriteString("```")
		c.w.WriteString(lang)
		c.w.WriteString("\n")
		c.w.WriteString(textContent(codeNode))
		c.w.WriteString("```\n\n")
		return
	}
	c.w.WriteString("```\n")
	c.w.WriteString(textContent(n))
	c.w.WriteString("```\n\n")
}

func (c *converter) renderLink(n *xhtml.Node) {
	href := attr(n, "href")
	c.w.WriteString("[")
	c.walkChildren(n)
	c.w.WriteString("](")
	c.w.WriteString(href)
	c.w.WriteString(")")
}

func (c *converter) renderImage(n *xhtml.Node) {
	src := attr(n, "src")
	alt := attr(n, "alt")
	c.w.WriteString("![")
	c.w.WriteString(alt)
	c.w.WriteString("](")
	c.w.WriteString(src)
	c.w.WriteString(")")
}

func (c *converter) renderList(n *xhtml.Node, ordered bool) {
	prevOrdered := c.orderedList
	prevCounter := c.listCounter
	c.orderedList = ordered
	c.listCounter = 0
	c.listDepth++
	c.walkChildren(n)
	c.listDepth--
	c.orderedList = prevOrdered
	c.listCounter = prevCounter
	if c.listDepth == 0 {
		c.w.WriteString("\n")
	}
}

func (c *converter) renderListItem(n *xhtml.Node) {
	indent := strings.Repeat("  ", c.listDepth-1)
	c.listCounter++
	if c.orderedList {
		fmt.Fprintf(c.w, "%s%d. ", indent, c.listCounter)
	} else {
		c.w.WriteString(indent + "- ")
	}
	c.walkChildren(n)
	c.w.WriteString("\n")
}

func (c *converter) renderBlockquote(n *xhtml.Node) {
	var inner strings.Builder
	innerC := &converter{w: &inner}
	innerC.walkChildren(n)

	for _, line := range strings.Split(strings.TrimRight(inner.String(), "\n"), "\n") {
		c.w.WriteString("> ")
		c.w.WriteString(line)
		c.w.WriteString("\n")
	}
	c.w.WriteString("\n")
}

func (c *converter) renderTable(n *xhtml.Node) {
	c.w.WriteString("\n")
	c.walkChildren(n)
	c.w.WriteString("\n")
}

func (c *converter) renderTableRow(n *xhtml.Node) {
	c.walkChildren(n)
	c.w.WriteString("|\n")

	// Após a primeira linha de headers, emitir separador
	if c.inTableHead || hasChildElement(n, "th") {
		// Contar colunas
		cols := countChildElements(n, "th")
		if cols == 0 {
			cols = countChildElements(n, "td")
		}
		for i := 0; i < cols; i++ {
			c.w.WriteString("|---")
		}
		c.w.WriteString("|\n")
	}
}

// renderMacro trata macros Confluence (ac:structured-macro).
func (c *converter) renderMacro(n *xhtml.Node) {
	macroName := attr(n, "ac:name")

	switch macroName {
	case "code":
		c.renderCodeMacro(n)
	case "info", "note", "tip", "warning":
		c.renderPanelMacro(n, macroName)
	case "expand":
		c.renderExpandMacro(n)
	case "toc":
		// Ignorar table of contents
	case "anchor":
		// Ignorar anchors
	case "status":
		c.renderStatusMacro(n)
	default:
		// Tentar extrair conteúdo do body
		body := findElement(n, "ac:rich-text-body")
		if body != nil {
			c.walkChildren(body)
		}
	}
}

func (c *converter) renderCodeMacro(n *xhtml.Node) {
	lang := ""
	for child := n.FirstChild; child != nil; child = child.NextSibling {
		if child.Type == xhtml.ElementNode && child.Data == "ac:parameter" {
			if attr(child, "ac:name") == "language" {
				lang = textContent(child)
			}
		}
	}

	body := findElement(n, "ac:plain-text-body")
	code := ""
	if body != nil {
		code = textContent(body)
	}

	c.w.WriteString("```")
	c.w.WriteString(lang)
	c.w.WriteString("\n")
	c.w.WriteString(code)
	if !strings.HasSuffix(code, "\n") {
		c.w.WriteString("\n")
	}
	c.w.WriteString("```\n\n")
}

func (c *converter) renderPanelMacro(n *xhtml.Node, panelType string) {
	alertType := map[string]string{
		"info":    "NOTE",
		"note":    "IMPORTANT",
		"tip":     "TIP",
		"warning": "WARNING",
	}[panelType]

	body := findElement(n, "ac:rich-text-body")
	if body == nil {
		return
	}

	var inner strings.Builder
	innerC := &converter{w: &inner}
	innerC.walkChildren(body)
	content := strings.TrimSpace(inner.String())

	fmt.Fprintf(c.w, "> [!%s]\n", alertType)
	for _, line := range strings.Split(content, "\n") {
		c.w.WriteString("> ")
		c.w.WriteString(line)
		c.w.WriteString("\n")
	}
	c.w.WriteString("\n")
}

func (c *converter) renderExpandMacro(n *xhtml.Node) {
	title := ""
	for child := n.FirstChild; child != nil; child = child.NextSibling {
		if child.Type == xhtml.ElementNode && child.Data == "ac:parameter" {
			if attr(child, "ac:name") == "title" {
				title = textContent(child)
			}
		}
	}

	body := findElement(n, "ac:rich-text-body")
	if body == nil {
		return
	}

	c.w.WriteString("<details>\n")
	if title != "" {
		fmt.Fprintf(c.w, "<summary>%s</summary>\n\n", title)
	}
	c.walkChildren(body)
	c.w.WriteString("\n</details>\n\n")
}

func (c *converter) renderStatusMacro(n *xhtml.Node) {
	title := ""
	for child := n.FirstChild; child != nil; child = child.NextSibling {
		if child.Type == xhtml.ElementNode && child.Data == "ac:parameter" {
			if attr(child, "ac:name") == "title" {
				title = textContent(child)
			}
		}
	}
	if title != "" {
		fmt.Fprintf(c.w, "**%s**", title)
	}
}

func (c *converter) renderAcLink(n *xhtml.Node) {
	// ac:link com anchor ou page reference
	anchor := attr(n, "ac:anchor")
	linkBody := findElement(n, "ac:plain-text-link-body")
	linkText := ""
	if linkBody != nil {
		linkText = textContent(linkBody)
	}

	if anchor != "" && linkText != "" {
		fmt.Fprintf(c.w, "[%s](#%s)", linkText, strings.ToLower(strings.ReplaceAll(anchor, " ", "-")))
		return
	}

	// Tentar extrair link para outra página
	pageRef := findElement(n, "ri:page")
	if pageRef != nil {
		pageTitle := attr(pageRef, "ri:content-title")
		if linkText == "" {
			linkText = pageTitle
		}
		fmt.Fprintf(c.w, "[%s](%s)", linkText, pageTitle)
		return
	}

	if linkText != "" {
		c.w.WriteString(linkText)
	}
}

func (c *converter) renderAcImage(n *xhtml.Node) {
	// ac:image com ri:attachment ou ri:url
	attachment := findElement(n, "ri:attachment")
	if attachment != nil {
		filename := attr(attachment, "ri:filename")
		fmt.Fprintf(c.w, "![%s](attachments/%s)", filename, filename)
		return
	}

	urlRef := findElement(n, "ri:url")
	if urlRef != nil {
		url := attr(urlRef, "ri:value")
		fmt.Fprintf(c.w, "![](%s)", url)
		return
	}
}

// Helpers

func findElement(n *xhtml.Node, tag string) *xhtml.Node {
	if n.Type == xhtml.ElementNode && n.Data == tag {
		return n
	}
	for child := n.FirstChild; child != nil; child = child.NextSibling {
		if found := findElement(child, tag); found != nil {
			return found
		}
	}
	return nil
}

func attr(n *xhtml.Node, key string) string {
	for _, a := range n.Attr {
		// Match exact key or namespace:key (HTML parser stores namespace separately)
		if a.Key == key {
			return a.Val
		}
		// Handle namespaced attrs like "ac:name" where Namespace="" and Key="ac:name"
		if a.Namespace+":"+a.Key == key || a.Namespace+a.Key == key {
			return a.Val
		}
	}
	return ""
}

func textContent(n *xhtml.Node) string {
	if n.Type == xhtml.TextNode {
		return n.Data
	}
	var sb strings.Builder
	for child := n.FirstChild; child != nil; child = child.NextSibling {
		sb.WriteString(textContent(child))
	}
	return sb.String()
}

func hasChildElement(n *xhtml.Node, tag string) bool {
	for child := n.FirstChild; child != nil; child = child.NextSibling {
		if child.Type == xhtml.ElementNode && child.Data == tag {
			return true
		}
	}
	return false
}

func countChildElements(n *xhtml.Node, tag string) int {
	count := 0
	for child := n.FirstChild; child != nil; child = child.NextSibling {
		if child.Type == xhtml.ElementNode && child.Data == tag {
			count++
		}
	}
	return count
}
