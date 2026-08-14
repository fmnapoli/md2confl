// Copyright 2026 md2confl contributors
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"html"
	"sync"

	"github.com/fmnapoli/md2confl/confluence"
)

// A publicação em Server/DC acontece em duas fases: a primeira publica cada
// documento e a segunda reescreve os links inter-documento para URLs do
// Confluence e republica as páginas afetadas. Comparar o HTML recém-gerado
// (links relativos crus) com o corpo publicado (links já resolvidos) nunca
// casa, então toda execução republicava toda página.
//
// A comparação passa a ser fonte contra fonte: o digest do Storage Format
// pré-resolução fica gravado numa content property da página. O corpo não serve
// como lugar para esse metadado — o Confluence Server reescreve o Storage
// Format que recebe (verificado contra o TDN em ago/2026):
//
//   - comentários HTML são descartados por inteiro;
//   - macros ganham ac:schema-version e um ac:macro-id (UUID gerado no
//     servidor, impossível de prever localmente);
//   - caracteres não-ASCII viram entidades ("Operações" → "Opera&ccedil;...").
//
// Isto é também o que condena comparar corpo com corpo, mesmo normalizando os
// links: as três reescritas acima teriam de ser desfeitas, e o ac:macro-id não
// tem como ser reproduzido.

// digestPropertyKey é a chave da content property que guarda o digest.
// Precisa ser alfanumérica: com hífen, o Confluence Server responde HTTP 500 ao
// expandir metadata.properties.<chave> no GET da página.
const digestPropertyKey = "md2conflSource"

// pageDigest é o valor da content property: o digest do Storage Format
// pré-resolução de links que gerou a página.
type pageDigest struct {
	Digest string `json:"digest"`
}

// retryableOnce roda uma tarefa uma única vez por execução, mas só a dá por
// feita quando ela conseguiu chegar ao fim. sync.Once não serve: uma falha
// transitória na primeira tentativa desligaria a tarefa para sempre.
type retryableOnce struct {
	mu    sync.Mutex
	taken bool
}

// begin reserva a execução da tarefa. Devolve false quando outra já a fez ou a
// está fazendo.
func (o *retryableOnce) begin() bool {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.taken {
		return false
	}
	o.taken = true
	return true
}

// abort devolve a reserva: a tarefa não chegou ao fim e outra chamada pode
// tentar de novo.
func (o *retryableOnce) abort() {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.taken = false
}

// sourceDigest resume o que define o conteúdo de uma página: o título e o
// Storage Format gerado a partir do Markdown, antes da resolução de links.
// Qualquer mudança no Markdown, no título ou no conversor muda o digest.
//
// parentID de propósito fica de fora: o move de página não é feito pelo
// UpdatePage do Server (ele não envia ancestors), então mover não é um efeito
// que o digest precise disparar. Se um dia moveIfNeededServer passar a mover de
// verdade, parentID tem de entrar aqui — senão uma página com o digest em dia
// seria pulada antes de chegar ao move.
func sourceDigest(title, storageHTML string) string {
	sum := sha256.Sum256([]byte(title + "\n" + storageHTML))
	return hex.EncodeToString(sum[:])
}

// decodePageDigest lê o valor da content property. Devolve o valor zerado
// quando a página não tem a property ou o conteúdo é ilegível — nos dois casos
// a publicação segue como se a página fosse desconhecida.
func decodePageDigest(prop confluence.ContentProperty) pageDigest {
	if !prop.Exists() {
		return pageDigest{}
	}
	var v pageDigest
	if err := json.Unmarshal(prop.Value, &v); err != nil {
		return pageDigest{}
	}
	return v
}

// storedDigest devolve o digest da fonte gravado na página.
func storedDigest(prop confluence.ContentProperty) string {
	return decodePageDigest(prop).Digest
}

// serverPageUnchanged decide se a página publicada já corresponde à fonte local.
//
// Com o digest gravado, a comparação é fonte contra fonte, imune à reescrita de
// links da segunda fase. Sem digest (página publicada por uma versão anterior,
// ou instância que não guardou a property) sobra a comparação byte a byte do
// corpo: ela quase nunca casa no Confluence real, por causa das reescritas do
// Storage Format, então o efeito prático é republicar — nunca pular uma página
// que mudou.
func serverPageUnchanged(prop confluence.ContentProperty, currentDigest, publishedBody, storageHTML string) bool {
	if stored := storedDigest(prop); stored != "" {
		return stored == currentDigest
	}
	return publishedBody != "" && publishedBody == storageHTML
}

// sameHrefs compara só os valores de href de dois documentos em Storage Format.
//
// É o que a segunda fase precisa saber ("a página já está com estes links?") e
// a única comparação de corpo que sobrevive às reescritas do Confluence:
// comentário, ac:macro-id e entidades de acento ficam todos fora de um href. As
// entidades são desfeitas dos dois lados porque uma URL com caractere não-ASCII
// volta escapada do servidor.
func sameHrefs(publishedBody, patchedHTML string) bool {
	published := extractHrefs(publishedBody)
	patched := extractHrefs(patchedHTML)
	if len(published) != len(patched) {
		return false
	}
	for i := range published {
		if published[i] != patched[i] {
			return false
		}
	}
	return true
}

// extractHrefs devolve, em ordem, os hrefs do documento já desescapados.
func extractHrefs(storageHTML string) []string {
	matches := storageHrefRegex.FindAllStringSubmatch(storageHTML, -1)
	hrefs := make([]string, 0, len(matches))
	for _, m := range matches {
		hrefs = append(hrefs, html.UnescapeString(m[2]))
	}
	return hrefs
}
