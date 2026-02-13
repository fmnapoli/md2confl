# Quick Start

Este guia mostra como instalar o `md2confl`, converter um Markdown para ADF e publicar sua primeira página no Confluence Cloud em menos de 5 minutos.

## 1. Instalar

Escolha uma das opções:

```bash
# Via go install
go install github.com/fmnapoli/md2confl/cmd/md2confl@latest

# Ou via Docker (recomendado — inclui suporte a Mermaid)
docker pull fmnapoli/md2confl:latest
```

## 2. Criar um Markdown de teste

```bash
cat > minha-pagina.md << 'EOF'
# Minha Primeira Página

Publicada com **md2confl**!

## Seção

- Item 1
- Item 2
- Item 3

> Citação de exemplo.
EOF
```

## 3. Converter para ADF (preview local)

```bash
md2confl --input minha-pagina.md
```

O ADF JSON é impresso no stdout. Útil para inspecionar antes de publicar.

## 4. Configurar credenciais

Crie um API token em [id.atlassian.com](https://id.atlassian.com/manage-profile/security/api-tokens) e configure as variáveis de ambiente:

```bash
export CONFLUENCE_URL="https://seu-site.atlassian.net"
export CONFLUENCE_EMAIL="seu-email@example.com"
export CONFLUENCE_TOKEN="seu-api-token"
```

Para mais detalhes, veja a página [Instalação](instalacao.md).

## 5. Publicar no Confluence

```bash
md2confl --input minha-pagina.md --publish \
  --space SUA_SPACE --title "Minha Primeira Página"
```

Output esperado:

```
✓ Published "Minha Primeira Página" → https://seu-site.atlassian.net/wiki/spaces/SUA_SPACE/pages/12345/...
  Page ID: 12345
  Action: created
  Version: 1
```

## 6. Habilitar atualizações idempotentes

Adicione `--write-marker` para salvar o page ID no arquivo Markdown:

```bash
md2confl --input minha-pagina.md --publish --write-marker \
  --space SUA_SPACE --title "Minha Primeira Página"
```

Isso adiciona `<!-- confluence-page-id: 1179649 -->` no topo do arquivo. Nas próximas execuções, a mesma página será atualizada em vez de criar uma nova.

## 7. Via Docker

Se preferir usar Docker (necessário para diagramas Mermaid):

```bash
docker run --rm -v "$(pwd):/workspace" \
  -e CONFLUENCE_URL="$CONFLUENCE_URL" \
  -e CONFLUENCE_EMAIL="$CONFLUENCE_EMAIL" \
  -e CONFLUENCE_TOKEN="$CONFLUENCE_TOKEN" \
  fmnapoli/md2confl --input minha-pagina.md --publish \
  --space SUA_SPACE --title "Minha Primeira Página"
```

## Próximos passos

- [Instalação](instalacao.md) — todas as formas de instalar e configurar
- [Uso e Referência](referencia.md) — exemplos detalhados e flags
- [Configuração](configuracao.md) — arquivo `.md2confl.yml` para múltiplos documentos
- [Publicação](publicacao.md) — fluxo de publicação, Mermaid e imagens
- [CI/CD](ci-cd.md) — automatizar publicação com GitHub Actions
