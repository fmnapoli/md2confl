// Copyright 2026 md2confl contributors
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"regexp"
)

// A publicação em Server/DC acontece em duas fases: a primeira publica cada
// documento e a segunda reescreve os links inter-documento para URLs do
// Confluence e republica as páginas afetadas. Comparar o HTML recém-gerado
// (links relativos crus) com o corpo publicado (links já resolvidos) nunca
// casa, então toda execução republicava toda página.
//
// Para comparar fonte com fonte, o corpo publicado carrega um marcador com o
// digest da fonte que o gerou. O marcador é um comentário HTML no começo do
// Storage Format: sobrevive à reescrita de hrefs da segunda fase, não é
// renderizado e não custa chamada de API extra. Se o Confluence remover o
// comentário, o digest some e a comparação cai no caminho antigo (byte a byte)
// — degrada para republicar à toa, nunca para pular uma página que mudou.

// sourceMarkerRegex captura o digest gravado no corpo publicado.
var sourceMarkerRegex = regexp.MustCompile(`<!--\s*md2confl-source:\s*([0-9a-f]{64})\s*-->\n?`)

// sourceDigest resume o que define o conteúdo de uma página: o título e o
// Storage Format gerado a partir do Markdown, antes da resolução de links.
// Qualquer mudança no Markdown, no título ou no conversor muda o digest.
func sourceDigest(title, storageHTML string) string {
	sum := sha256.Sum256([]byte(title + "\n" + stripSourceMarker(storageHTML)))
	return hex.EncodeToString(sum[:])
}

// stampSourceMarker devolve o Storage Format prefixado com o marcador do
// digest da fonte. É o corpo que vai para o Confluence e o que a segunda fase
// usa como base — o marcador precisa acompanhar todas as republicações, senão
// a execução seguinte não reconhece a página como já publicada.
func stampSourceMarker(title, storageHTML string) string {
	clean := stripSourceMarker(storageHTML)
	return fmt.Sprintf("<!-- md2confl-source: %s -->\n", sourceDigest(title, clean)) + clean
}

// extractSourceMarker devolve o digest gravado no corpo, ou "" se não houver.
func extractSourceMarker(body string) string {
	m := sourceMarkerRegex.FindStringSubmatch(body)
	if m == nil {
		return ""
	}
	return m[1]
}

// stripSourceMarker remove o marcador do corpo.
func stripSourceMarker(body string) string {
	return sourceMarkerRegex.ReplaceAllString(body, "")
}

// serverBodyUnchanged decide se o corpo publicado já corresponde ao conteúdo
// local. stampedHTML é o candidato já marcado (saída de stampSourceMarker).
//
// Com marcador dos dois lados a comparação é fonte contra fonte, imune à
// reescrita de links da segunda fase. Sem marcador no corpo publicado (página
// criada por uma versão anterior, ou Confluence que descarta comentários) a
// comparação volta a ser byte a byte: pode republicar à toa, mas não pula uma
// página que mudou.
func serverBodyUnchanged(publishedBody, stampedHTML string) bool {
	if publishedBody == "" {
		return false
	}
	if digest := extractSourceMarker(publishedBody); digest != "" {
		return digest == extractSourceMarker(stampedHTML)
	}
	return stripSourceMarker(publishedBody) == stripSourceMarker(stampedHTML)
}
