# Plano de Implementação: md2confl CLI

**Branch**: `001-md2confl-cli` | **Data**: 2026-02-12 | **Spec**: [spec.md](spec.md)
**Input**: Especificação da feature em `/specs/001-md2confl-cli/spec.md`

## Sumário

Ferramenta CLI em Go que converte arquivos Markdown (GFM) com diagramas Mermaid para o formato ADF (Atlassian Document Format) e opcionalmente publica como páginas no Confluence Cloud via REST API v2. Distribuída como binário único estático, sem dependências de runtime. O parser Markdown usa goldmark com extensão GFM, e a conversão para ADF é feita via walker customizado da AST. A publicação usa a API v2 do Confluence com representação `atlas_doc_format`.

## Contexto Técnico

**Linguagem/Versão**: Go 1.22+
**Dependências Principais**: `github.com/yuin/goldmark` (+ extensão GFM) — única dependência externa permitida pela Constituição
**Storage**: N/A (filesystem para I/O, sem banco de dados)
**Testing**: `go test` com golden files (`testdata/`), `httptest.Server` para mocks da API Confluence
**Plataforma Alvo**: Linux (amd64, arm64), macOS (amd64, arm64), Windows (amd64) — cross-compilation via GOOS/GOARCH, zero CGO
**Tipo de Projeto**: Single (CLI tool com pacotes importáveis)
**Metas de Performance**: Conversão de arquivo MD de 10.000 linhas com 5 diagramas Mermaid em < 1 segundo
**Restrições**: Binário estático único, zero dependências runtime, < 10 dependências transitivas, HTTPS exclusivo para API
**Escopo**: ~5 pacotes Go, ~15-20 arquivos fonte, ~2000-3000 LOC estimado

## Constitution Check

*GATE: Deve passar antes da Phase 0. Re-verificado após Phase 1.*

| Princípio | Status | Evidência |
|-----------|--------|-----------|
| I. CLI Mínimo | PASS | Single-purpose CLI, binário único, convenções Unix (args/stdin/stdout/stderr), --help auto-documentável, sem prompts interativos |
| II. Stdlib-First | PASS | Única dependência: goldmark (permitida explicitamente). net/http, encoding/json, flag — tudo stdlib. Transitivas estimadas < 5 |
| III. Arquitetura Modular | PASS | Pacotes: `parser/` (zero knowledge de Confluence), `confluence/` (zero knowledge de Markdown), `cli/` (wiring), `adf/` (tipos compartilhados). `parser` importável como lib standalone |
| IV. Segurança por Default | PASS | Token aceito via env var (preferencial), flag (com warning). --token-file planejado para v2 (SHOULD). Nunca logado/exibido. HTTPS exclusivo |
| V. Performance | PASS | goldmark é streaming parser em Go puro. Construção de árvore ADF em memória proporcional ao input. < 1s para 10k linhas validado pelo design |
| VI. Disciplina de Testes | PASS | Golden files para parser (MD→ADF), httptest.Server para confluence, testes CLI end-to-end de flags. Meta: ≥ 70% cobertura |
| VII. ADF Mapping Extensível | PASS | Walker pattern com handlers por tipo de node. Novo elemento = novo handler, sem alterar infraestrutura do walker |
| DevOps & GitOps | PASS | MD-first (nunca modifica source sem --write-marker), exit codes semânticos (0/1/2), --json para pipelines, idempotente, cross-platform |
| Licenciamento | PASS | Apache 2.0. goldmark é MIT. Todas dependências compatíveis |

**Resultado**: Todos os gates PASS. Nenhuma violação.

## Estrutura do Projeto

### Documentação (esta feature)

```text
specs/001-md2confl-cli/
├── plan.md              # Este arquivo
├── research.md          # Phase 0: pesquisa técnica consolidada
├── data-model.md        # Phase 1: modelo de dados (entidades Go)
├── quickstart.md        # Phase 1: guia de início rápido
├── contracts/
│   ├── confluence-api.md  # Phase 1: contrato REST API Confluence v2
│   └── cli-interface.md   # Phase 1: contrato da interface CLI
└── tasks.md             # Phase 2: tarefas ordenadas (/speckit.tasks)
```

### Código Fonte (raiz do repositório)

```text
cmd/md2confl/
  main.go              # Entry point: func main() { os.Exit(cli.Run(os.Args[1:], Version)) }

adf/
  types.go             # Structs ADF: Document, Node, Mark

parser/
  parser.go            # ConvertToADF(source []byte) (*adf.Document, error)
  parser_test.go       # Golden file tests
  testdata/            # *.md (input) + *.json (expected ADF output)
    basic.md
    basic.json
    mermaid.md
    mermaid.json
    table.md
    table.json
    ...

confluence/
  client.go            # Client struct: CreatePage, UpdatePage, GetPage, FindByTitle, UploadAttachment
  client_test.go       # httptest.Server mocks
  errors.go            # Tipos de erro categorizados (user error vs API error)

cli/
  cli.go               # Run(args []string, version string) int
  cli_test.go          # Testes end-to-end de flags/output
  output.go            # Formatação: human-readable e JSON

go.mod
go.sum
Makefile               # build, test, lint, cross-compile
LICENSE                # Apache 2.0
NOTICE                 # Third-party licenses
```

**Decisão de Estrutura**: Layout Go idiomático com `cmd/` para o binário e pacotes de nível raiz para bibliotecas importáveis. O pacote `parser` é importável como `go get github.com/user/md2confl/parser` conforme Princípio III. O pacote `adf` é separado do `parser` para evitar dependência circular — tanto `parser` quanto `confluence` precisam dos tipos ADF.

## Constitution Check Pós-Design

| Princípio | Status | Notas Pós-Design |
|-----------|--------|-------------------|
| I. CLI Mínimo | PASS | Sem mudanças. `flag.NewFlagSet` com `ContinueOnError` |
| II. Stdlib-First | PASS | Confirmado: goldmark + GFM extension = ~3 dependências transitivas |
| III. Arquitetura Modular | PASS | 4 pacotes: `adf`, `parser`, `confluence`, `cli`. Sem dependências circulares |
| IV. Segurança por Default | PASS | Warning em stderr se token passado via CLI flag. Credenciais mascaradas em logs |
| V. Performance | PASS | goldmark AST walk é O(n). Construção ADF é O(n). Sem buffering ilimitado |
| VI. Disciplina de Testes | PASS | Golden files definidos. httptest para API. Flag `-update` para regenerar |
| VII. ADF Mapping Extensível | PASS | Handler registry por `ast.NodeKind`. Adicionar handler = 1 função + 1 registro |

## Complexity Tracking

> Nenhuma violação de constituição identificada. Tabela vazia.

| Violação | Por que Necessária | Alternativa Simples Rejeitada Porque |
|----------|-------------------|-------------------------------------|
| — | — | — |
