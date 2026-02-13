# Modelo de Dados: md2confl CLI

**Branch**: `001-md2confl-cli` | **Data**: 2026-02-12

## Entidades

### 1. ADF Document (`adf.Document`)

Representação Go do envelope ADF.

```go
type Document struct {
    Version int    `json:"version"`          // sempre 1
    Type    string `json:"type"`             // sempre "doc"
    Content []Node `json:"content"`          // block nodes
}
```

### 2. ADF Node (`adf.Node`)

Representação genérica de um node ADF. Usa composição para suportar todos os tipos.

```go
type Node struct {
    Type    string          `json:"type"`
    Attrs   map[string]any  `json:"attrs,omitempty"`
    Content []Node          `json:"content,omitempty"`
    Marks   []Mark          `json:"marks,omitempty"`
    Text    string          `json:"text,omitempty"`
}
```

**Tipos de node block**:
- `heading` — attrs: `{level: 1-6}`
- `paragraph`
- `bulletList`
- `orderedList` — attrs: `{order: int}`
- `listItem`
- `codeBlock` — attrs: `{language: string}`
- `blockquote`
- `table`
- `tableRow`
- `tableHeader` — attrs: `{colspan: int, rowspan: int}`
- `tableCell` — attrs: `{colspan: int, rowspan: int}`
- `rule` — sem content/attrs
- `mediaSingle` — attrs: `{layout: "center"|"wide"|"full-width"}`
- `media` — attrs: `{type: "file"|"external", url: string, id: string, collection: string}`
- `bodiedExtension` — attrs: `{extensionType: string, extensionKey: string, parameters: map}` (macros com body, ex: Mermaid)
- `extension` — attrs: `{extensionType: string, extensionKey: string, parameters: map}` (macros sem body)

**Tipos de node inline**:
- `text` — campo `text` com `marks` opcionais

### 3. ADF Mark (`adf.Mark`)

```go
type Mark struct {
    Type  string         `json:"type"`
    Attrs map[string]any `json:"attrs,omitempty"`
}
```

**Tipos de mark**:
- `strong` — bold
- `em` — italic
- `strike` — strikethrough
- `code` — inline code
- `link` — attrs: `{href: string, title: string}`
- `underline`

### 4. Markdown Source (`cli.MarkdownSource`)

Representação de um arquivo Markdown de entrada com metadados extraídos.

```go
type MarkdownSource struct {
    Path       string   // caminho absoluto no filesystem
    Content    []byte   // conteúdo raw do arquivo
    PageID     string   // extraído de <!-- confluence-page-id: ID --> (vazio se ausente)
    Title      string   // derivado: flag --title > primeiro H1 > filename sem extensão
    LocalImages []string // caminhos relativos de imagens locais referenciadas
}
```

**Regras de derivação do título**:
1. Flag `--title` (se fornecido) — aplica apenas à página raiz em modo pasta
2. Primeiro heading H1 do conteúdo
3. Nome do arquivo sem extensão (ex: `setup.md` → `setup`)

**Extração do page-id**:
- Regex: `<!--\s*confluence-page-id:\s*(\d+)\s*-->`
- Posição: qualquer lugar no documento (tipicamente primeira ou última linha)

### 5. Publish Config (`confluence.Config`)

Configuração para publicação no Confluence.

```go
type Config struct {
    BaseURL  string // ex: "https://mysite.atlassian.net"
    SpaceKey string // ex: "DEVOPS"
    SpaceID  string // resolvido via API a partir de SpaceKey
    ParentID string // ID da página pai (opcional)
    Email    string // email do usuário Atlassian
    Token    string // API token — NUNCA logar/exibir
}
```

**Precedência de credenciais**:
1. Flags CLI: `--email`, `--token`
2. Variáveis de ambiente: `CONFLUENCE_EMAIL`, `CONFLUENCE_TOKEN`

### 6. Publish Result (`confluence.PublishResult`)

Resultado de uma operação de publicação.

```go
type PublishResult struct {
    PageID    string `json:"pageId"`
    PageURL   string `json:"pageUrl"`
    Title     string `json:"title"`
    SpaceKey  string `json:"spaceKey"`
    Version   int    `json:"version"`
    Action    string `json:"action"` // "created" | "updated"
}
```

### 7. Directory Tree (`cli.DirTree`)

Representação da hierarquia de diretórios para modo pasta.

```go
type DirEntry struct {
    Path     string      // caminho do diretório
    Name     string      // nome do diretório (título da página)
    Readme   *MarkdownSource // README.md do diretório (nil se ausente)
    Files    []MarkdownSource // outros .md no diretório
    Children []DirEntry  // subdiretórios
}
```

**Regras de hierarquia**:
- `README.md` de um diretório → conteúdo da página pai
- Outros `.md` no mesmo diretório → subpáginas da página pai
- Subdiretórios → subpáginas recursivas
- Diretório sem `README.md` → página pai criada vazia com nome do diretório

## Diagrama de Relacionamentos

```
MarkdownSource ──parse──> adf.Document
     │                         │
     │ (PageID)                │ (serializar JSON)
     │                         ▼
     │                    JSON string
     │                         │
     ▼                         ▼
confluence.Config ──publish──> Confluence API
     │                         │
     │                         ▼
     │                    PublishResult
     │                         │
     │ (--write-marker)        │ (stdout)
     ▼                         ▼
  Atualiza MD source     Exibe PageID/URL
```

## Transições de Estado

### Fluxo de publicação de uma página

```
[Novo] ─────────────────────────> [Criado no Confluence]
  │                                       │
  │ (page-id no MD ou --force match)      │ (versão incrementada)
  ▼                                       ▼
[Existente] ───────────────────> [Atualizado no Confluence]
```

1. **Sem page-id e sem --force**: Tenta criar. Se título já existe, retorna erro com sugestão de `--force`.
2. **Com page-id**: Atualiza a página existente (GET versão atual, PUT com versão+1).
3. **Com --force**: Busca por título exato no space. Se encontrar, sobrescreve. Se não, cria nova.

### Fluxo de resolução de conflitos

```
POST /pages → 409 Conflict
  ├── Sem --force: EXIT 2 + mensagem sugerindo --force ou page-id
  └── Com --force: GET por título → PUT para atualizar
```
