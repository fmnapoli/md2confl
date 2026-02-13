<!-- confluence-page-id: 1245185 -->
# Configuração

O `md2confl` suporta um arquivo de configuração YAML (`.md2confl.yml`) que centraliza defaults globais e mapeia documentos para publicação ou conversão. Isso elimina a repetição de flags como `--url`, `--space`, `--email`, etc.

## Formato do config

```yaml
# .md2confl.yml

# Defaults globais — aplicados a todos os documentos
url: https://site.atlassian.net
space: DEVOPS
email: user@example.com
parent-id: "12345"
force: true
write-marker: true

# Documentos — cada entry mapeia input → destino
documents:
  # Publish: input → Confluence
  - input: docs/architecture.md
    title: "Architecture Overview"
    # herda url, space, email, parent-id do global

  - input: docs/runbook.md
    title: "Operations Runbook"
    space: OPS                 # override do global
    parent-id: "67890"

  # Convert: input → arquivo JSON
  - input: docs/spec.md
    output: dist/spec.json     # sem publish — apenas converte

  - input: docs/onboarding.md
    # title derivado do H1 (comportamento default)
```

**Regras:**
- `token` **não pode** estar no config (segurança) — sempre via `CONFLUENCE_TOKEN` ou `--token`
- Se entry tem `output:` → modo convert (gera JSON); se não → modo publish
- `documents` é uma lista ordenada (processados em sequência)
- Cada entry precisa apenas de `input` — tudo mais é opcional e herda do global
- Caminhos relativos são resolvidos a partir do diretório do config

## Uso com config

```bash
# Processar TODOS os documents do config
md2confl --config .md2confl.yml

# Filtrar por um input específico
md2confl --config .md2confl.yml --input docs/runbook.md

# Override via flag (flag > config)
md2confl --config .md2confl.yml --space STAGING

# Preview sem publicar
md2confl --config .md2confl.yml --dry-run

# Sem config — comportamento atual inalterado
md2confl --input doc.md --publish --url https://... --space DEVOPS
```

## Auto-discovery

Sem `--config` explícito, o md2confl procura `.md2confl.yml` (ou `.md2confl.yaml`) no diretório corrente. Se encontrar, carrega silenciosamente e emite uma mensagem no stderr:

```
Using config: /path/to/.md2confl.yml
```

Se não encontrar, segue o fluxo normal baseado em flags.

## Precedência

```
flag > config (document-level) > config (global-level) > env var
```

## Exemplo: publicar múltiplos documentos

Um caso comum é manter a documentação no repositório e publicar todas as páginas com um único comando:

```yaml
# .md2confl.yml
url: https://site.atlassian.net
space: DEVOPS
email: user@example.com
force: true
write-marker: true

documents:
  - input: README.md
    title: "Meu Projeto"

  - input: docs/quickstart.md
    parent-id: "12345"    # página filha do README

  - input: docs/api.md
    parent-id: "12345"
```

```bash
# Publica tudo de uma vez
md2confl --config .md2confl.yml
```
