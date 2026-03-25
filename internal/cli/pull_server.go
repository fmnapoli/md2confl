// Copyright 2026 md2confl contributors
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"fmt"
	"strings"

	"github.com/fmnapoli/md2confl/confluence"
	"github.com/fmnapoli/md2confl/storagetomd"
)

// serverPullAdapter adapta ServerClient para a interface pullClient.
// Para operações não suportadas (GetChildren, GetAttachments, DownloadAttachment),
// retorna valores vazios em vez de erro.
type serverPullAdapter struct {
	client *confluence.ServerClient
	space  string
}

func (a *serverPullAdapter) GetPage(pageID string) (*confluence.PageResponse, error) {
	return a.client.GetPage(pageID)
}

func (a *serverPullAdapter) FindByTitle(spaceKeyOrID, title string) (*confluence.PageResponse, error) {
	// Server/DC usa spaceKey diretamente (não precisa de spaceID)
	return a.client.FindByTitle(a.space, title)
}

func (a *serverPullAdapter) ResolveSpaceID(spaceKey string) (string, error) {
	// Server/DC não precisa resolver spaceID — retorna o spaceKey como ID
	return spaceKey, nil
}

func (a *serverPullAdapter) GetChildren(pageID string) ([]confluence.ChildPage, error) {
	// TODO: implementar quando recursive pull for necessário para Server/DC
	return nil, nil
}

func (a *serverPullAdapter) GetAttachments(pageID string) ([]confluence.Attachment, error) {
	return a.client.GetAttachments(pageID)
}

func (a *serverPullAdapter) DownloadAttachment(downloadLink string) ([]byte, error) {
	return a.client.DownloadAttachment(downloadLink)
}

// convertStorageToMarkdown converte uma página Server/DC (Storage Format) para Markdown.
func convertStorageToMarkdown(pageID, title, storageBody string) ([]byte, error) {
	var buf strings.Builder

	// Page-id marker
	fmt.Fprintf(&buf, "<!-- confluence-page-id: %s -->\n", pageID)

	if storageBody != "" {
		md, err := storagetomd.Convert(storageBody)
		if err != nil {
			// Fallback: apenas título
			fmt.Fprintf(&buf, "# %s\n\n", title)
			fmt.Fprintf(&buf, "<!-- Error converting storage format: %v -->\n", err)
			return []byte(buf.String()), nil
		}

		// Verificar se o markdown já começa com H1 do título
		if !strings.HasPrefix(md, "# "+title) && !strings.HasPrefix(md, "\n# "+title) {
			fmt.Fprintf(&buf, "# %s\n\n", title)
		}
		buf.WriteString(md)
	} else {
		fmt.Fprintf(&buf, "# %s\n\n", title)
	}

	return []byte(buf.String()), nil
}
