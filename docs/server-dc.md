# Confluence Server/Data Center

O md2confl suporta Confluence Server e Data Center (on-premise) via a flag `--server`. Neste modo, utiliza a REST API v1 com Storage Format (XHTML) em vez da API v2 com ADF (JSON) usada no Cloud.

## Diferenças entre Cloud e Server/DC

| Aspecto | Cloud (padrão) | Server/DC (`--server`) |
|---------|---------------|----------------------|
| API | REST API v2 (`/wiki/api/v2`) | REST API v1 (`/rest/api`) |
| Formato | ADF (JSON) | Storage Format (XHTML) |
| Auth | Basic (email + API token) | Basic (username + password) |
| Space | Resolve space key → space ID | Usa space key diretamente |
| Busca | `GET /pages?space-id=...&title=...` | `GET /content?spaceKey=...&title=...` |
| Attachments | UUID (`fileId`) | ID numérico |
| Pull | ADF → Markdown (`adftomd`) | Storage Format → Markdown (`storagetomd`) |

## Publicar no Server/DC

### Credenciais

```bash
export CONFLUENCE_URL="https://confluence.empresa.com"
export CONFLUENCE_EMAIL="seu-usuario"     # username, não email
export CONFLUENCE_TOKEN="sua-senha"       # password ou PAT
```

### Publicar uma página

```bash
md2confl --input doc.md --publish --server --force \
  --url https://confluence.empresa.com \
  --space DEVOPS \
  --title "Minha Página"
```

### Publicar com Mermaid

Blocos mermaid são pré-renderizados para SVG, uploaded como attachments, e referenciados via macro `<ac:image>`:

```bash
md2confl --input doc.md --publish --server --force \
  --url https://confluence.empresa.com \
  --space DEVOPS \
  --title "Minha Página"
```

O fluxo para Mermaid em Server/DC:

1. Detecta blocos ` ```mermaid ` no Markdown
2. Renderiza cada bloco para SVG via `mmdc`
3. Substitui o bloco por referência de imagem no Markdown
4. Converte Markdown → Storage Format (XHTML)
5. Publica a página
6. Upload dos SVGs como attachments
7. Patch do HTML: `<img>` local → `<ac:image><ri:attachment ri:filename="..."/></ac:image>`
8. Re-publica com referências de attachment

### Custom User-Agent

Ambientes com Cloudflare ou outros WAFs podem exigir um User-Agent específico:

```bash
md2confl --input doc.md --publish --server --force \
  --user-agent "meu-bot" \
  --url https://confluence.empresa.com \
  --space DEVOPS
```

Também configurável via `.md2confl.yml`:

```yaml
url: https://confluence.empresa.com
space: DEVOPS
user-agent: meu-bot
server: true
```

## Importar do Server/DC (pull)

O subcomando `pull` com `--server` busca uma página do Confluence Server/DC e converte Storage Format (XHTML) para Markdown.

### Pull por título

```bash
md2confl pull --server \
  --url https://confluence.empresa.com \
  --space DEVOPS \
  --title "Minha Página" \
  --output-dir ./docs
```

### Pull por page ID

```bash
md2confl pull --server \
  --url https://confluence.empresa.com \
  --page-id 12345 \
  --output-dir ./docs
```

### Pull com attachments

Por padrão, attachments (imagens) são baixados para `attachments/` dentro do diretório de saída. Para pular:

```bash
md2confl pull --server --skip-attachments \
  --url https://confluence.empresa.com \
  --space DEVOPS \
  --title "Minha Página"
```

### Conversão Storage Format → Markdown

O conversor `storagetomd` suporta:

| Storage Format (XHTML) | Markdown |
|------------------------|----------|
| `<h1>` ... `<h6>` | `#` ... `######` |
| `<strong>`, `<em>`, `<del>`, `<code>` | `**bold**`, `*italic*`, `~~strike~~`, `` `code` `` |
| `<a href="...">` | `[text](url)` |
| `<ul>`, `<ol>`, `<li>` | `- item`, `1. item` |
| `<table>` | Tabela GFM |
| `<blockquote>` | `> quote` |
| `<hr>` | `---` |
| `ac:structured-macro ac:name="code"` | ` ```language ` |
| `ac:structured-macro ac:name="info"` | `> [!NOTE]` |
| `ac:structured-macro ac:name="warning"` | `> [!WARNING]` |
| `ac:structured-macro ac:name="tip"` | `> [!TIP]` |
| `ac:structured-macro ac:name="expand"` | `<details><summary>` |
| `ac:structured-macro ac:name="status"` | `**STATUS**` |
| `ac:image` + `ri:attachment` | `![name](attachments/name.png)` |
| `ac:link` + `ac:anchor` | `[text](#anchor)` |
| `ac:link` + `ri:page` | `[text](Page Title)` |

### Limitações do pull

- **Recursive pull** (`--recursive`) ainda não implementado para Server/DC
- Imagens inline em blocos mermaid com fallback são convertidas para referências de imagem
- Diagramas mermaid que falharam no Confluence aparecem como imagem de fallback (não como código mermaid)

## Configuração via YAML

Campos específicos de Server/DC no `.md2confl.yml`:

```yaml
url: https://confluence.empresa.com
space: DEVOPS
email: meu-usuario
server: true
user-agent: meu-bot
force: true
write-marker: true

documents:
  - input: README.md
    title: "Meu Projeto"
  - input: docs/
    parent-id: "12345"
```

## Flags adicionais

| Flag | Tipo | Descrição |
|------|------|-----------|
| `--server` | `bool` | Usar Confluence Server/Data Center (REST API v1 + Storage Format) |
| `--user-agent` | `string` | Custom User-Agent header para requisições HTTP |
