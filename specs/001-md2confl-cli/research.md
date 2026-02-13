# Pesquisa Técnica: md2confl CLI

**Branch**: `001-md2confl-cli` | **Data**: 2026-02-12

## 1. Formato ADF (Atlassian Document Format)

### Decisão: Usar ADF v1 com `atlas_doc_format` via REST API v2

**Racional**: O Confluence Cloud usa ADF como formato nativo para o novo editor. A API v2 aceita e retorna ADF via representação `atlas_doc_format`.

**Envelope do documento ADF**:
```json
{
  "version": 1,
  "type": "doc",
  "content": [ /* block nodes */ ]
}
```

**Alternativas consideradas**:
- Storage format (XHTML) via API v1 — descartado pois é legado e não suporta macros nativas do novo editor.

### Mapeamento Markdown → ADF Nodes

| Elemento Markdown | ADF Node | Tipo |
|---|---|---|
| Heading (h1-h6) | `heading` (attrs.level: 1-6) | block |
| Paragraph | `paragraph` | block |
| Bullet list | `bulletList` > `listItem` | block |
| Ordered list | `orderedList` (attrs.order) > `listItem` | block |
| Code block | `codeBlock` (attrs.language) | block |
| Blockquote | `blockquote` | block |
| Table | `table` > `tableRow` > `tableHeader`/`tableCell` | block |
| Horizontal rule | `rule` | block |
| Image | `mediaSingle` > `media` ou `mediaInline` | block/inline |
| Mermaid | `extension` (ver seção dedicada) | block |
| Text | `text` | inline |
| Bold | mark `strong` | inline mark |
| Italic | mark `em` | inline mark |
| Strikethrough | mark `strike` | inline mark |
| Code span | mark `code` | inline mark |
| Link | mark `link` (attrs.href) | inline mark |

### Representação Mermaid no ADF

**Não existe macro Mermaid nativa** no Confluence Cloud. Mermaid é suportado exclusivamente via apps third-party do Marketplace (ex: "Mermaid Charts & Diagrams for Confluence" da weweave, "Mermaid Chart" oficial). Cada app tem seus próprios `extensionType` e `extensionKey`.

**Abordagem padrão (Connect app)**:
```json
{
  "type": "bodiedExtension",
  "attrs": {
    "extensionType": "com.atlassian.confluence.macro.core",
    "extensionKey": "mermaid-diagram",
    "parameters": {
      "macroParams": {},
      "macroMetadata": {
        "macroId": { "value": "auto-generated-uuid" },
        "schemaVersion": { "value": "1" },
        "title": "Mermaid diagram"
      }
    }
  },
  "content": [
    {
      "type": "paragraph",
      "content": [
        {
          "type": "text",
          "text": "graph TD;\n    A-->B;"
        }
      ]
    }
  ]
}
```

**Abordagem Forge app**:
```json
{
  "type": "bodiedExtension",
  "attrs": {
    "extensionType": "com.atlassian.ecosystem",
    "extensionKey": "ari:cloud:ecosystem::extension/<app-id>/<env-id>/static/<macro-key>",
    "parameters": { "extensionTitle": "Mermaid diagram" }
  },
  "content": [
    {
      "type": "paragraph",
      "content": [{ "type": "text", "text": "graph TD;\n    A-->B;" }]
    }
  ]
}
```

**Fallback seguro (sem app)**:
```json
{
  "type": "codeBlock",
  "attrs": { "language": "mermaid" },
  "content": [
    { "type": "text", "text": "graph TD;\n    A-->B;" }
  ]
}
```

**Decisão para md2confl**: O fallback padrão será `codeBlock` com `language: "mermaid"` (funciona sem nenhum app instalado). Futuramente, flags como `--mermaid-extension-type` e `--mermaid-extension-key` podem habilitar a representação como `bodiedExtension` para workspaces com app Mermaid instalado.

### Regras de Validação ADF

- `content` arrays requerem `minItems: 1` na maioria dos nodes
- `extensionKey` e `extensionType` requerem `minLength: 1`
- `codeBlock` não aceita marks (maxItems: 0 em marks)
- `listItem`: primeiro filho deve ser `paragraph`, demais podem ser `bulletList`/`orderedList`
- `tableCell`/`tableHeader`: não aceitam tabelas aninhadas nem `bodiedExtension`
- Limite de conteúdo Jira: 32.767 caracteres (aplica ao JSON ADF inteiro) — Confluence Pages não tem limite documentado equivalente

## 2. Confluence Cloud REST API v2

### Decisão: Usar REST API v2 com representação `atlas_doc_format`

**Racional**: A API v2 é a API atual e recomendada para Confluence Cloud, suporta ADF nativamente.

### Endpoints necessários

| Operação | Método | Endpoint | Notas |
|---|---|---|---|
| Criar página | POST | `/wiki/api/v2/pages` | Body: `{spaceId, title, parentId, body: {representation: "atlas_doc_format", value: "<ADF JSON>"}}` |
| Atualizar página | PUT | `/wiki/api/v2/pages/{id}` | Requer `version.number` incrementado em +1 |
| Obter página | GET | `/wiki/api/v2/pages/{id}?body-format=atlas_doc_format` | Retorna body em ADF |
| Buscar por título | GET | `/wiki/api/v2/pages?title={title}&space-id={spaceId}` | Para implementar `--force` |
| Obter space por key | GET | `/wiki/api/v2/spaces?keys={KEY}` | Retorna space ID necessário para criar páginas |
| Upload attachment | POST | `/wiki/rest/api/content/{id}/child/attachment` | **API v1** — multipart form data, header `X-Atlassian-Token: no-check` |
| Listar attachments | GET | `/wiki/api/v2/pages/{id}/attachments` | Para verificar se attachment já existe |

### Autenticação

- **Método**: Basic Auth — `email:api_token` codificado em Base64
- **Header**: `Authorization: Basic <base64(email:token)>`
- **HTTPS obrigatório** (conforme Princípio IV da Constituição)

### Versionamento de páginas

Para atualizar uma página existente:
1. GET da página para obter `version.number` atual
2. PUT com `version.number` incrementado em +1
3. Não é possível pular versões — o incremento deve ser exatamente +1

### Códigos de erro relevantes

| Código | Significado | Ação no CLI |
|---|---|---|
| 401 | Token inválido ou expirado | Exit code 2 + mensagem sobre credenciais |
| 403 | Sem permissão no space | Exit code 2 + mensagem sobre permissões |
| 404 | Página/space não encontrado | Exit code 2 + mensagem sobre ID/key |
| 409 | Conflito de versão | Exit code 2 + sugerir retry |
| 422 | Conteúdo ADF inválido | Exit code 2 + mostrar detalhes do erro |

### Fluxo space-key → space-id

A API v2 usa `spaceId` (inteiro) internamente. O CLI aceita `--space` (space key string). Fluxo:
1. `GET /wiki/api/v2/spaces?keys=DEVOPS` → retorna `spaceId`
2. Usar `spaceId` em todas as chamadas subsequentes

## 3. Parser Markdown: goldmark

### Decisão: Usar goldmark v1.7+ com extensão GFM

**Racional**: goldmark é o parser Markdown mais popular em Go, CommonMark compliant, altamente extensível, e listado como dependência permitida na Constituição (Princípio II).

**Módulo**: `github.com/yuin/goldmark`

### Elementos GFM suportados pelo goldmark

Com a extensão GFM (`extension.GFM`):
- Tables (Table, TableHeader, TableRow, TableCell com Alignment)
- Strikethrough
- Autolinks (via Linkify)
- TaskList (TaskCheckBox)

**Não implementado pelo goldmark GFM**: tagfilter (GFM spec 6.11) — irrelevante para md2confl pois não geramos HTML.

### Abordagem para renderização ADF

**Decisão**: Implementar um renderer customizado usando `ast.Walk` ao invés de implementar a interface `renderer.Renderer`.

**Racional**: A interface `Renderer` do goldmark é orientada a streaming de bytes (HTML). Para ADF, precisamos construir uma árvore JSON. Usar `ast.Walk` para percorrer a AST e construir a árvore ADF em memória é mais natural.

```go
// Pseudocódigo da abordagem
func ConvertToADF(source []byte) (*ADFDocument, error) {
    md := goldmark.New(goldmark.WithExtensions(extension.GFM))
    reader := text.NewReader(source)
    doc := md.Parser().Parse(reader)

    adf := &ADFDocument{Version: 1, Type: "doc"}
    err := ast.Walk(doc, func(node ast.Node, entering bool) (ast.WalkStatus, error) {
        // mapear cada node para ADF
    })
    return adf, err
}
```

### Detecção de codeblocks Mermaid

Goldmark representa fenced code blocks como `ast.FencedCodeBlock`. O atributo `Language()` retorna a linguagem. Verificar `string(node.Language(source)) == "mermaid"` para desviar para o handler de extensão Mermaid.

### AST nodes relevantes do goldmark

**Core (github.com/yuin/goldmark/ast)**:
- Document, Heading, Paragraph, TextBlock
- List, ListItem (IsOrdered, Start)
- Blockquote, FencedCodeBlock, CodeBlock
- ThematicBreak, HTMLBlock
- Text, CodeSpan, Emphasis, Link, Image, AutoLink, RawHTML

**Extension (github.com/yuin/goldmark/extension/ast)**:
- Table, TableHeader, TableRow, TableCell (com Alignment)
- Strikethrough, TaskCheckBox

## 4. Estrutura do Projeto Go

### Decisão: Layout com `cmd/` e pacotes internos, sem subcommands

**Racional**: O md2confl é uma ferramenta single-purpose sem subcommands. A Constituição (Princípio III) exige separação em `parser`, `confluence` e `cmd`. O Go standard project layout recomenda `cmd/` para binários e pacotes de nível raiz para bibliotecas importáveis.

### Layout definido

```
cmd/md2confl/
  main.go            # Entry point: func main() { os.Exit(cli.Run(os.Args[1:])) }

parser/              # Pacote importável: Markdown → ADF (zero knowledge de Confluence)
  parser.go          # ConvertToADF(source []byte) (*adf.Document, error)
  parser_test.go
  testdata/          # Golden files: *.md input + *.json expected ADF

adf/                 # Tipos ADF (structs Go para serialização JSON)
  types.go           # Document, Node, Mark, etc.

confluence/          # Cliente API Confluence (zero knowledge de Markdown)
  client.go          # Client struct + métodos CRUD de páginas
  client_test.go     # Testes com httptest.Server (mock HTTP)

cli/                 # Wiring: flags, I/O orchestration
  cli.go             # Run(args []string) int — parsing de flags, orquestração
  cli_test.go        # Testes end-to-end de flags e output
  output.go          # Formatação de output (human-readable e JSON)

go.mod
go.sum
Makefile             # Targets: build, test, lint, cross-compile
```

### Padrão CLI: `appEnv` struct com `Run()` retornando int

```go
// cli/cli.go
type appEnv struct {
    input      string
    output     string
    dryRun     bool
    publish    bool
    url        string
    space      string
    parentID   string
    title      string
    email      string
    token      string
    force      bool
    writeMarker bool
    jsonOutput bool
    stdout     io.Writer
    stderr     io.Writer
}

func Run(args []string) int {
    var app appEnv
    if err := app.fromArgs(args); err != nil {
        return 1  // erro de usuário
    }
    if err := app.run(); err != nil {
        // distinguir entre erro de usuário (1) e erro de API (2)
        return exitCodeForError(err)
    }
    return 0
}
```

### Flag parsing: `flag.NewFlagSet` (stdlib)

**Decisão**: Usar `flag.NewFlagSet` com `flag.ContinueOnError` para controle de exit codes.

**Racional**: Constituição exige stdlib-first. O `flag` package suporta todos os flags necessários. Sem subcommands, não há necessidade de cobra/pflag.

**Nota**: O `flag` package não suporta flags POSIX long (`--flag`), apenas `-flag`. Porém, em Go, `-flag` e `--flag` são ambos aceitos pelo flag package. Flags como `--input`, `--output`, `--dry-run` funcionam nativamente.

### Testes: golden files com flag `-update`

```go
var update = flag.Bool("update", false, "update golden files")

func TestConvertToADF(t *testing.T) {
    input, _ := os.ReadFile("testdata/basic.md")
    got, _ := parser.ConvertToADF(input)
    gotJSON, _ := json.MarshalIndent(got, "", "  ")

    golden := "testdata/basic.json"
    if *update {
        os.WriteFile(golden, gotJSON, 0644)
    }

    expected, _ := os.ReadFile(golden)
    if diff := cmp.Diff(string(expected), string(gotJSON)); diff != "" {
        t.Errorf("mismatch (-want +got):\n%s", diff)
    }
}
```

### Cross-compilation

```makefile
PLATFORMS := linux/amd64 linux/arm64 darwin/amd64 darwin/arm64 windows/amd64

build-all:
	@for platform in $(PLATFORMS); do \
		GOOS=$${platform%/*} GOARCH=$${platform#*/} \
		go build -ldflags "-X main.Version=$(VERSION)" \
		-o bin/md2confl-$${platform%/*}-$${platform#*/} ./cmd/md2confl; \
	done
```

### Injeção de versão via ldflags

```go
// cmd/md2confl/main.go
var Version = "dev"

func main() {
    os.Exit(cli.Run(os.Args[1:], Version))
}
```

Build: `go build -ldflags "-X main.Version=1.0.0" -o md2confl ./cmd/md2confl`
