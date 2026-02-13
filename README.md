<!-- confluence-page-id: 589826 -->
# md2confl

[![CI](https://github.com/fmnapoli/md2confl/actions/workflows/ci.yml/badge.svg)](https://github.com/fmnapoli/md2confl/actions/workflows/ci.yml)
[![codecov](https://codecov.io/gh/fmnapoli/md2confl/branch/main/graph/badge.svg)](https://codecov.io/gh/fmnapoli/md2confl)
[![Go Report Card](https://goreportcard.com/badge/github.com/fmnapoli/md2confl)](https://goreportcard.com/report/github.com/fmnapoli/md2confl)
[![Go Reference](https://pkg.go.dev/badge/github.com/fmnapoli/md2confl.svg)](https://pkg.go.dev/github.com/fmnapoli/md2confl)
[![License](https://img.shields.io/github/license/fmnapoli/md2confl)](LICENSE)
[![Release](https://img.shields.io/github/v/release/fmnapoli/md2confl)](https://github.com/fmnapoli/md2confl/releases/latest)
[![Docker](https://img.shields.io/docker/v/fmnapoli/md2confl?label=docker&sort=semver)](https://hub.docker.com/r/fmnapoli/md2confl)

Converte arquivos Markdown para o formato [Atlassian Document Format (ADF)](https://developer.atlassian.com/cloud/jira/platform/apis/document/structure/) e publica diretamente no Confluence Cloud.

> **Documentação publicada no Confluence:** [fmnapoli.atlassian.net/wiki/spaces/DDS/pages/589826/md2confl](https://fmnapoli.atlassian.net/wiki/spaces/DDS/pages/589826/md2confl) — este próprio README e os guias em `docs/` são publicados automaticamente via CI usando o md2confl.

## Visão geral

O Confluence Cloud armazena conteúdo de páginas no formato ADF (Atlassian Document Format) — um JSON hierárquico de nós (headings, parágrafos, tabelas, etc.) e marcas (bold, italic, link, etc.). Escrever ADF manualmente é verboso e propenso a erros.

O `md2confl` resolve isso:

1. **Converte** Markdown (com extensões GFM — tabelas, strikethrough, autolinks) para ADF JSON válido, usando o parser [goldmark](https://github.com/yuin/goldmark) para construir a AST e um walker próprio para gerar a árvore ADF.

2. **Publica** páginas no Confluence Cloud via REST API v2, com autenticação Basic (email + API token).

3. **Sincroniza** hierarquias de diretórios como árvores de páginas — cada pasta vira uma página pai, cada `.md` vira uma página filha.

4. **Atualiza de forma idempotente** — marcadores `<!-- confluence-page-id: XXXXX -->` escritos no topo do Markdown permitem re-publicações que atualizam a mesma página sem duplicar.

5. **Lida com imagens locais** — referências a imagens locais (`![](./img/foto.png)`) são automaticamente enviadas como attachments e vinculadas à página.

6. **Renderiza diagramas Mermaid** — blocos `mermaid` são pré-renderizados para SVG via `mmdc` (mermaid-cli) durante o publish e enviados como imagens. A imagem Docker inclui tudo necessário (`md2confl` + Node.js + Chromium + mmdc).

## Arquitetura

```mermaid
graph LR
    main["cmd/md2confl<br/><small>entrypoint</small>"]
    cli["internal/cli<br/><small>flags, I/O, orquestração</small>"]
    parser["parser<br/><small>Markdown → ADF</small>"]
    mermaid["mermaid<br/><small>mmdc → SVG</small>"]
    confluence["confluence<br/><small>REST API v2</small>"]
    adf["adf<br/><small>tipos ADF</small>"]

    main --> cli
    cli --> parser
    cli --> mermaid
    cli --> confluence
    parser --> adf
    confluence --> adf
```

| Pacote | Visibilidade | Responsabilidade |
|--------|-------------|-----------------|
| `adf` | Público | Tipos de dados ADF — `Document`, `Node`, `Mark`. Representa o envelope JSON `{"version":1, "type":"doc", "content":[...]}` e a árvore de nós com atributos e marcas inline. |
| `parser` | Público | Converte `[]byte` Markdown para `*adf.Document`. Usa goldmark com extensão GFM para fazer parse, e um AST walker com stack para construir a árvore ADF. Entrada: bytes Markdown. Saída: documento ADF pronto para serializar. |
| `mermaid` | Público | Renderiza diagramas Mermaid para SVG via `mmdc` (mermaid-cli). Verifica disponibilidade do binário, gera nomes de arquivo determinísticos via SHA256 e configura puppeteer para ambientes Docker/CI. |
| `confluence` | Público | Cliente REST API v2 do Confluence Cloud. Resolve space key → ID, cria/atualiza páginas, busca por título, faz upload de attachments. Todos os erros são `*APIError` com categoria, status HTTP e hint acionável. |
| `internal/cli` | Interno | Orquestração CLI: parsing de flags, resolução de credenciais (flags > env vars), modos de operação (dry-run, output file, publish), processamento de diretórios, renderização de mermaid, upload de imagens locais, escrita de marcadores page-id. Não é API pública — só `main.go` importa. |

### Fluxo de dados

```mermaid
graph LR
    MD["Arquivo .md<br/><small>Markdown + GFM</small>"]
    Parse["goldmark.Parse()"]
    AST["Markdown AST"]
    Walker["walker.walk()<br/><small>stack push/pop</small>"]
    Doc["adf.Document"]
    JSON["JSON<br/><small>stdout ou arquivo</small>"]
    Mermaid["mmdc<br/><small>mermaid → SVG</small>"]
    API["Confluence API<br/><small>criar/atualizar página</small>"]
    Attach["Upload attachments<br/><small>imagens + SVGs</small>"]

    MD --> Parse --> AST --> Walker --> Doc
    Doc --> JSON
    Doc --> Mermaid --> API
    API --> Attach
```

**Detalhes do walker:** O walker visita cada nó da AST duas vezes (`entering=true` e `entering=false`). Na entrada, faz `push()` na stack para abrir um escopo de filhos. Na saída, faz `pop()` para coletar os filhos e `append()` para adicionar o nó completo ao pai. Para nós inline com formatação (bold, italic, etc.), aplica marks em vez de criar nós wrapper. Nós leaf (imagens, breaks) usam `WalkSkipChildren` para evitar travessia desnecessária.

## Documentação

| Página | Descrição |
|--------|-----------|
| [Quick Start](docs/quickstart.md) | Tutorial: instalar → converter → publicar primeira página |
| [Instalação](docs/instalacao.md) | go install, build manual, Docker, variáveis de ambiente, API token |
| [Uso e Referência](docs/referencia.md) | Exemplos de uso, flags, restrições entre flags, exit codes |
| [Configuração](docs/configuracao.md) | Arquivo `.md2confl.yml`, auto-discovery, precedência |
| [Publicação](docs/publicacao.md) | Fluxo de publicação, marcadores, modo pasta, Mermaid, imagens |
| [Desenvolvimento](docs/desenvolvimento.md) | Pré-requisitos, Makefile, testes, golden files, estrutura |
| [CI/CD](docs/ci-cd.md) | Automatizar publicação com GitHub Actions |

## Licença

[Apache License 2.0](LICENSE)
