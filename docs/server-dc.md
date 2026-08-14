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
5. Patch do HTML: `<img>` local → `<ac:image><ri:attachment ri:filename="..."/></ac:image>`
6. Publica a página já com as referências de attachment
7. Upload dos SVGs como attachments

O patch acontece **antes** de publicar porque o path local do SVG vive num diretório temporário de nome aleatório, enquanto o nome do arquivo é derivado do conteúdo do diagrama. Publicar o HTML já com a referência de attachment mantém o corpo estável entre execuções (ver [skip de páginas inalteradas](publicacao.md#skip-de-páginas-inalteradas)) e dispensa a segunda publicação que existia só para trocar as referências depois do upload.

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

### WAF bloqueando a busca por título (HTTP 403)

Alguns proxies liberam o acesso por page ID mas bloqueiam o endpoint de busca
(`GET /rest/api/content?spaceKey=...&title=...`). O sintoma é um 403 só nos
documentos **sem** o marcador `<!-- confluence-page-id: N -->`, que são
justamente os que dependem da busca quando `--force` está ativo:

```
1 document(s) failed:
  - docs/guia.md: title search rejected with HTTP 403 (space "DEVOPS", title "Guia") — ...
      Hint: not transient — retrying will not help; add a <!-- confluence-page-id: N --> marker ...
```

Não é transiente e não adianta repetir: o 403 não é retentado. O md2confl
também **não** trata o bloqueio como "página não encontrada" — isso faria o
`--force` criar uma página nova a cada execução, duplicando o documento.

Saídas possíveis:

- Adicionar o marcador `<!-- confluence-page-id: N -->` ao documento (via
  `--write-marker` numa execução em que a busca funcione, ou copiando o ID da
  URL da página), que passa a ser publicado por ID.
- Ajustar o User-Agent (ver acima) ou a regra do WAF.

Os demais documentos seguem sendo publicados normalmente; só os bloqueados são
pulados, e o processo termina com exit code 2.

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
approve: true
user-agent: meu-bot
force: true
write-marker: true

documents:
  - input: README.md
    title: "Meu Projeto"
  - input: docs/
    parent-id: "12345"
```

## Auto-approve com Comala Workflows

Ambientes com [Comala Document Management](https://marketplace.atlassian.com/apps/142/comala-document-management) (plugin de workflows para Confluence Server/DC) podem exigir aprovação após a publicação de uma página.

A flag `--approve` automatiza essa etapa — após publicar ou atualizar uma página, o md2confl envia um `PATCH` para a Comala API aprovando o estado "Review":

```bash
md2confl --input doc.md --publish --server --force --approve \
  --url https://confluence.empresa.com \
  --space DEVOPS \
  --title "Minha Página"
```

### Fluxo

1. Publica/atualiza a página normalmente
2. Envia `PATCH /rest/cw/1/content/{pageID}/approvals/approve` com `{"name": "Review"}`
3. Se o workflow não estiver configurado na página (HTTP 404), é **silenciosamente ignorado** — não gera erro
4. Se a aprovação falhar por outro motivo, emite um warning no stderr (não interrompe a execução)

### Configuração via YAML

```yaml
url: https://confluence.empresa.com
space: DEVOPS
server: true
approve: true
```

> **Nota:** `--approve` requer `--server`. Não se aplica ao Confluence Cloud.

## Flags adicionais

| Flag | Tipo | Descrição |
|------|------|-----------|
| `--server` | `bool` | Usar Confluence Server/Data Center (REST API v1 + Storage Format) |
| `--approve` | `bool` | Auto-approve após publish via Comala Document Management API. Se o workflow não estiver configurado (404), é silenciosamente ignorado. |
| `--user-agent` | `string` | Custom User-Agent header para requisições HTTP |
