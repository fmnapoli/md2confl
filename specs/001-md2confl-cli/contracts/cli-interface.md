# Contrato: Interface CLI md2confl

## Sinopse

```
md2confl --input <path> [options]
```

## Flags

| Flag | Tipo | Obrigatório | Default | Descrição |
|------|------|-------------|---------|-----------|
| `--input` | string | sim | — | Caminho para arquivo .md ou diretório |
| `--output` | string | não | — | Caminho para arquivo JSON de saída (ADF) |
| `--dry-run` | bool | não | false | Exibe ADF no stdout sem publicar |
| `--publish` | bool | não | false | Publica no Confluence Cloud |
| `--url` | string | com --publish | — | URL base do Confluence (ex: https://site.atlassian.net) |
| `--space` | string | com --publish | — | Space key do Confluence |
| `--parent-id` | string | não | — | ID da página pai |
| `--title` | string | não | derivado | Título da página (ver regras de derivação) |
| `--email` | string | com --publish | env | Email do usuário Atlassian |
| `--token` | string | com --publish | env | API token do Atlassian |
| `--force` | bool | não | false | Sobrescreve página existente com mesmo título |
| `--write-marker` | bool | não | false | Escreve page-id de volta no arquivo MD |
| `--json` | bool | não | false | Output JSON estruturado |
| `--version` | bool | não | false | Exibe versão do CLI |
| `--help` | bool | não | false | Exibe ajuda |

## Variáveis de Ambiente

| Variável | Equivale a |
|----------|-----------|
| `CONFLUENCE_EMAIL` | `--email` |
| `CONFLUENCE_TOKEN` | `--token` |
| `CONFLUENCE_URL` | `--url` |

**Precedência**: Flags CLI > Variáveis de ambiente

## Exit Codes

| Código | Significado | Exemplos |
|--------|------------|---------|
| 0 | Sucesso | Conversão OK, publicação OK |
| 1 | Erro do usuário | Input inválido, flags faltando, arquivo não encontrado |
| 2 | Erro de API | Auth inválida, rede, conflito no Confluence |

## Formatos de Output

### Modo texto (default)

**Conversão para arquivo**:
```
✓ Converted doc.md → doc.json
```

**Dry-run**:
```json
{
  "version": 1,
  "type": "doc",
  "content": [...]
}
```

**Publicação**:
```
✓ Published "Meu Doc" → https://site.atlassian.net/wiki/spaces/DEVOPS/pages/111222
  Page ID: 111222
  Action: created
  Version: 1
```

**Erro**:
```
Error: authentication failed — invalid API token
  Hint: verify your --token or CONFLUENCE_TOKEN environment variable
```

### Modo JSON (`--json`)

**Sucesso**:
```json
{
  "status": "success",
  "pageId": "111222",
  "pageUrl": "https://site.atlassian.net/wiki/spaces/DEVOPS/pages/111222",
  "title": "Meu Doc",
  "action": "created",
  "version": 1
}
```

**Erro**:
```json
{
  "status": "error",
  "code": 2,
  "message": "authentication failed — invalid API token",
  "hint": "verify your --token or CONFLUENCE_TOKEN environment variable"
}
```

## Combinações de Flags Válidas

| Cenário | Flags |
|---------|-------|
| Converter para arquivo | `--input doc.md --output doc.json` |
| Preview (dry-run) | `--input doc.md --dry-run` |
| Publicar nova página | `--input doc.md --publish --url ... --space ... --email ... --token ...` |
| Atualizar página (via marker) | `--input doc.md --publish --url ... --space ... --email ... --token ...` |
| Sobrescrever por título | `--input doc.md --publish --force --url ... --space ... --email ... --token ...` |
| Publicar pasta | `--input docs/ --publish --url ... --space ... --email ... --token ...` |
| Publicar + escrever marker | `--input doc.md --publish --write-marker --url ... --space ...` |

## Validações

1. `--input` é obrigatório (exit 1 se ausente)
2. `--publish` requer `--url` e `--space` (exit 1 se faltando)
3. `--publish` requer `--email` e `--token` (via flag ou env var) (exit 1 se faltando)
4. `--output` e `--publish` são mutuamente exclusivos (exit 1 se ambos)
5. `--output` e `--dry-run` são mutuamente exclusivos (exit 1 se ambos)
6. `--write-marker` requer `--publish` (exit 1 se sem --publish)
7. `--force` requer `--publish` (exit 1 se sem --publish)
8. Input path deve existir (exit 1 se não encontrado)
