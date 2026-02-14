# Uso e Referência

## Uso rápido

### 1. Converter para ADF JSON (stdout)

```bash
md2confl --input doc.md
```

Sem `--output` nem `--publish`, o ADF JSON é impresso no stdout. Útil para inspecionar o output ou redirecionar com pipe:

```bash
md2confl --input doc.md | jq '.content[0]'
```

**Exemplo de output** para um Markdown com heading e parágrafo:

```json
{
  "version": 1,
  "type": "doc",
  "content": [
    {
      "type": "heading",
      "attrs": { "level": 1 },
      "content": [
        { "type": "text", "text": "Título" }
      ]
    },
    {
      "type": "paragraph",
      "content": [
        { "type": "text", "text": "Texto do parágrafo com " },
        {
          "type": "text",
          "marks": [{ "type": "strong" }],
          "text": "negrito"
        },
        { "type": "text", "text": "." }
      ]
    }
  ]
}
```

### 2. Converter para arquivo

```bash
md2confl --input doc.md --output doc.json
```

Output:

```
✓ Converted doc.md → doc.json
```

### 3. Preview (dry-run com flags de publicação)

```bash
md2confl --input doc.md --dry-run --publish \
  --url https://site.atlassian.net \
  --space DEVOPS --title "Minha Página" \
  --email user@example.com
```

Imprime uma simulação no stderr mostrando o que seria publicado (título, espaço, URL) e o ADF JSON no stdout — sem fazer nenhuma chamada à API:

```
Dry-run: would publish to Confluence
  Title: Minha Página
  Space: DEVOPS
  URL: https://site.atlassian.net

{ ... ADF JSON ... }
```

Com `--config`, o dry-run também mostra um preview dos links inter-documento que seriam resolvidos:

```
Dry-run: would resolve 7 inter-document link(s) in "README.md"
Dry-run: would resolve 2 inter-document link(s) in "ci-cd.md"
```

### 4. Publicar no Confluence

```bash
export CONFLUENCE_TOKEN="seu-api-token"

md2confl --input doc.md --publish \
  --url https://site.atlassian.net \
  --space DEVOPS \
  --title "Minha Página" \
  --email user@example.com
```

Output:

```
✓ Published "Minha Página" → https://site.atlassian.net/wiki/spaces/DEVOPS/pages/12345/Minha+Página
  Page ID: 12345
  Action: created
  Version: 1
```

Para salvar o page ID no arquivo Markdown (permitindo atualizações futuras):

```bash
md2confl --input doc.md --publish --write-marker \
  --url https://site.atlassian.net \
  --space DEVOPS --title "Minha Página"
```

Isso prepende `<!-- confluence-page-id: 1212417 -->` ao topo do `doc.md`.

### 5. Atualizar página existente (--force)

```bash
md2confl --input doc.md --publish --force \
  --url https://site.atlassian.net \
  --space DEVOPS --title "Minha Página"
```

Com `--force`, o md2confl busca uma página com o mesmo título no espaço. Se encontrar, atualiza. Se não encontrar, cria nova. Sem `--force` e sem marcador `confluence-page-id`, sempre cria uma nova página.

### 6. Publicar hierarquia de diretórios

```bash
md2confl --input docs/ --publish \
  --url https://site.atlassian.net \
  --space DEVOPS --parent-id 12345
```

Output (uma linha por página):

```
✓ Published "Documentação" → https://site.atlassian.net/wiki/...
  Page ID: 100
  Action: created
  Version: 1
✓ Published "Guia de Instalação" → https://site.atlassian.net/wiki/...
  Page ID: 101
  Action: created
  Version: 1
```

### 7. Output JSON estruturado

Para integração com CI/CD ou scripts, use `--json` para obter output em JSON em vez de texto:

```bash
md2confl --input doc.md --publish --json \
  --url https://site.atlassian.net \
  --space DEVOPS --title "Minha Página"
```

Output de sucesso:

```json
{
  "status": "success",
  "pageId": "12345",
  "pageUrl": "https://site.atlassian.net/wiki/spaces/DEVOPS/pages/12345/Minha+Página",
  "title": "Minha Página",
  "spaceKey": "DEVOPS",
  "action": "created",
  "version": 1
}
```

Output de erro:

```json
{
  "status": "error",
  "code": 2,
  "message": "authentication failed — invalid or expired API token",
  "hint": "verify your --token or CONFLUENCE_TOKEN environment variable"
}
```

## Referência de flags

| Flag | Tipo | Padrão | Descrição |
|------|------|--------|-----------|
| `--input` | `string` | — | Caminho para arquivo `.md` ou diretório. Obrigatória, exceto quando o config file define `documents`. Se diretório, processa recursivamente todos os `.md` respeitando a hierarquia de pastas. Também funciona como `input` em entries do config. |
| `--config` | `string` | — | Caminho para arquivo de configuração YAML. Se omitido, auto-detecta `.md2confl.yml` no diretório corrente. |
| `--output` | `string` | — | Caminho do arquivo ADF JSON de saída. Mutuamente exclusivo com `--publish` e `--dry-run`. |
| `--dry-run` | `bool` | `false` | Imprime o ADF JSON no stdout sem publicar. Se combinado com `--publish`, mostra simulação (título, espaço, URL) no stderr. |
| `--publish` | `bool` | `false` | Publica no Confluence Cloud. Requer `--url`, `--space`, `--email` e `--token` (via flags ou env vars). |
| `--url` | `string` | — | URL base do Confluence Cloud (ex: `https://site.atlassian.net`). Deve usar HTTPS. Fallback: `CONFLUENCE_URL`. |
| `--space` | `string` | — | Chave do espaço no Confluence (ex: `DEVOPS`, `ENG`). O md2confl resolve a chave para o space ID via API. |
| `--parent-id` | `string` | — | ID numérico da página pai. Se omitido, a página é criada na raiz do espaço. No modo diretório, aplica-se apenas à página raiz. |
| `--title` | `string` | — | Título da página. Se omitido, usa o primeiro `# Heading` do Markdown; se não houver heading, usa o nome do arquivo sem extensão. No modo diretório, aplica-se apenas à página raiz. |
| `--email` | `string` | — | E-mail da conta Atlassian para autenticação Basic. Fallback: `CONFLUENCE_EMAIL`. |
| `--token` | `string` | — | API token da Atlassian. Fallback: `CONFLUENCE_TOKEN`. **Aviso:** passar via flag expõe o token no histórico do shell. |
| `--force` | `bool` | `false` | Busca página existente com o mesmo título no espaço e atualiza em vez de criar nova. Requer `--publish`. |
| `--write-marker` | `bool` | `false` | Após publicação bem-sucedida, escreve `<!-- confluence-page-id: XXXXX -->` no topo do arquivo Markdown fonte. Permite atualizações idempotentes futuras. Requer `--publish`. |
| `--mermaid` | `bool` | `false` | Renderiza blocos mermaid para SVG via `mmdc`. Sempre ativo com `--publish`. Use com `--dry-run` ou `--output` para preview dos diagramas renderizados. |
| `--json` | `bool` | `false` | Formata output (sucesso e erro) como JSON em vez de texto. Útil para integração com CI/CD. |
| `--verbose` | `bool` | `false` | Ativa logging detalhado no stderr. Mostra requisições HTTP (URL, status, tempo), decisões de resolução de links, retry de API e timing. |
| `--concurrency` | `int` | `4` | Número máximo de operações paralelas (documentos, uploads, mermaid). Intervalo: 1–16. |
| `--version` | `bool` | `false` | Imprime a versão e sai com exit code 0. |

## Restrições entre flags

| Combinação | Resultado |
|-----------|-----------|
| `--output` + `--publish` | Erro: mutuamente exclusivos |
| `--output` + `--dry-run` | Erro: mutuamente exclusivos |
| `--force` sem `--publish` | Erro: `--force` requer `--publish` |
| `--write-marker` sem `--publish` | Erro: `--write-marker` requer `--publish` |
| `--publish` sem `--url` | Erro: URL obrigatória (flag ou `CONFLUENCE_URL`) |
| `--publish` sem `--space` | Erro: espaço obrigatório |
| `--publish` sem `--email` | Erro: email obrigatório (flag ou `CONFLUENCE_EMAIL`) |
| `--publish` sem `--token` | Erro: token obrigatório (flag ou `CONFLUENCE_TOKEN`) |
| `--concurrency 0` ou `> 16` | Erro: concurrency deve estar entre 1 e 16 |

## Exit codes

| Código | Categoria | Quando ocorre |
|--------|-----------|---------------|
| `0` | Sucesso | Conversão ou publicação concluída sem erros |
| `1` | Erro do usuário | Flags inválidas ou conflitantes, arquivo/diretório não encontrado, combinação inválida de opções |
| `2` | Erro da API | Autenticação falhou (401/403), recurso não encontrado (404), conflito de versão (409), conteúdo inválido (400/422), erro de rede |

No modo `--json`, erros retornam um JSON com `status`, `code`, `message` e `hint`:

```json
{
  "status": "error",
  "code": 2,
  "message": "space not found: INVALID",
  "hint": "verify the space ID or key is correct"
}
```

### Categorias de erro da API

| Categoria | HTTP Status | Mensagem | Hint |
|-----------|-------------|----------|------|
| `auth` | 401, 403 | `authentication failed — invalid or expired API token` | Verificar `--token` ou `CONFLUENCE_TOKEN` |
| `not_found` | 404 | `<resource> not found: <id>` | Verificar ID ou chave do recurso |
| `conflict` | 409 | `version conflict — page was updated concurrently` | Retentar a operação |
| `validation` | 400, 422 | `invalid ADF content: <detalhes>` | Verificar Markdown por elementos não suportados |
| `network` | Outros | `unexpected API response <code>: <body>` | Verificar conectividade e URL |

## Mapeamento Markdown → ADF

A tabela abaixo mostra como cada elemento Markdown é convertido para o ADF correspondente:

| Markdown | ADF Node | Atributos | Notas |
|----------|----------|-----------|-------|
| `# Heading` … `###### H6` | `heading` | `level: 1..6` | Nível extraído da quantidade de `#` |
| Parágrafo | `paragraph` | — | Texto entre linhas em branco |
| `**bold**` | text com mark `strong` | — | Inline |
| `*italic*` | text com mark `em` | — | Inline |
| `` `code` `` | text com mark `code` | — | Inline |
| `~~strike~~` | text com mark `strike` | — | Extensão GFM |
| `[text](url)` | text com mark `link` | `href: url` | `title` adicionado se presente no Markdown |
| `<https://auto.link>` | text com mark `link` | `href: url` | Autolink — texto = URL |
| `![alt](url)` | `mediaSingle` > `media` | `type: external, url: url, layout: center` | Imagens locais (sem `http`) são uploadadas como attachments e convertidas para `type: file` |
| `` ```lang `` | `codeBlock` | `language: lang` | Sem linguagem → atributo omitido |
| `` ``` `` (sem lang) | `codeBlock` | — | Code block sem highlight |
| `> quote` | `blockquote` | — | Pode conter parágrafos e outros blocos |
| `- item` / `* item` | `bulletList` > `listItem` > `paragraph` | — | Suporta aninhamento |
| `1. item` | `orderedList` > `listItem` > `paragraph` | `order: N` se start ≠ 1 | Numeração customizada preservada |
| `---` / `***` / `___` | `rule` | — | Separador horizontal |
| Tabela GFM | `table` > `tableRow` > `tableHeader` / `tableCell` | — | Primeira linha = `tableHeader`, demais = `tableCell`. Conteúdo inline é wrapeado em `paragraph`. |
| `- [ ]` / `- [x]` | `taskList` > `taskItem` | `state: TODO/DONE, localId` | Task lists GFM — checkboxes interativos no Confluence |
| `> [!NOTE]` / `[!WARNING]` etc. | `panel` | `panelType: info/warning/...` | GitHub alerts → ADF panels. Tipos: NOTE→info, TIP→success, IMPORTANT→note, WARNING→warning, CAUTION→error |
| `:emoji:` | `emoji` | `shortName, text` | Shortcodes GitHub (`:tada:`, `:rocket:`, etc.) → emoji nativo do Confluence |
| `<details><summary>` | `expand` | `title` | Bloco colapsável. O `<details>` inteiro deve estar sem linhas em branco internas |
| `^text^` | text com mark `subsup` | `type: sup` | Superscript — ex: x^2^ renderiza como x² |
| Soft line break | text `" "` | — | Espaço simples entre linhas |
| Hard line break (`  \n` ou `\`) | `hardBreak` | — | Quebra de linha forçada |
| HTML block / inline | — | — | **Ignorado** silenciosamente (exceto `<details>`) |
