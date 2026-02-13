# Tasks: md2confl CLI

**Input**: Documentos de design em `/specs/001-md2confl-cli/`
**Pré-requisitos**: plan.md (obrigatório), spec.md (obrigatório), research.md, data-model.md, contracts/

**Testes**: Incluídos conforme Princípio VI da Constituição (Test Discipline) — golden files para parser, httptest.Server para API Confluence, testes end-to-end para CLI.

**Organização**: Tarefas agrupadas por user story para permitir implementação e teste independentes de cada história.

## Formato: `[ID] [P?] [Story] Descrição`

- **[P]**: Pode executar em paralelo (arquivos diferentes, sem dependências)
- **[Story]**: Qual user story (US1, US2, ..., US6)
- Caminhos exatos de arquivo incluídos nas descrições

---

## Phase 1: Setup (Infraestrutura Compartilhada)

**Objetivo**: Inicialização do projeto e estrutura básica

- [x] T001 Criar estrutura de diretórios do projeto (cmd/md2confl/, adf/, parser/, parser/testdata/, confluence/, cli/)
- [x] T002 Inicializar go.mod com dependência github.com/yuin/goldmark e extensão GFM
- [x] T003 [P] Criar Makefile com targets build, test, lint, cross-compile, e license-check (verificar headers Apache 2.0 em todos os .go) em Makefile
- [x] T004 [P] Criar LICENSE (Apache 2.0), arquivo NOTICE, e template de header Apache 2.0 para source files (.go) na raiz do repositório. Todos os arquivos .go criados nas tasks subsequentes DEVEM incluir o header de licença

---

## Phase 2: Foundational (Pré-requisitos Bloqueantes)

**Objetivo**: Infraestrutura core que DEVE estar completa antes de QUALQUER user story

**⚠️ CRÍTICO**: Nenhum trabalho de user story pode começar até esta fase estar completa

- [x] T005 Implementar tipos ADF (Document, Node, Mark) com tags JSON conforme data-model.md em adf/types.go
- [x] T006 [P] Criar entry point com injeção de versão via ldflags em cmd/md2confl/main.go
- [x] T007 [P] Implementar struct appEnv com parsing de todas as flags via flag.NewFlagSet(ContinueOnError) em cli/cli.go
- [x] T008 [P] Implementar formatação de output (modo texto e modo JSON) com struct de resultado em cli/output.go

**Checkpoint**: Fundação pronta — implementação de user stories pode começar

---

## Phase 3: User Story 1 — Conversão Local de Markdown para ADF (Prioridade: P1) 🎯 MVP

**Objetivo**: Converter um arquivo Markdown GFM com diagramas Mermaid em ADF JSON válido, salvando em arquivo

**Teste Independente**: Executar `md2confl --input doc.md --output doc.json` com um Markdown contendo headings, tabelas, codeblocks e Mermaid, e validar que o JSON gerado é ADF correto

### Testes para User Story 1

> **NOTA: Escrever golden files PRIMEIRO como referência de output esperado**

- [x] T009 [P] [US1] Criar golden files de teste para elementos básicos (headings, parágrafos, listas, links, formatação inline) em parser/testdata/basic.md + parser/testdata/basic.json
- [x] T010 [P] [US1] Criar golden files de teste para diagramas Mermaid em parser/testdata/mermaid.md + parser/testdata/mermaid.json
- [x] T011 [P] [US1] Criar golden files de teste para tabelas GFM em parser/testdata/table.md + parser/testdata/table.json
- [x] T012 [P] [US1] Criar golden files de teste para codeblocks com syntax highlighting em parser/testdata/codeblock.md + parser/testdata/codeblock.json

### Implementação da User Story 1

- [x] T013 [US1] Implementar ConvertToADF() com setup goldmark+GFM e infraestrutura do AST walker (entering/leaving com stack de nodes) em parser/parser.go
- [x] T014 [US1] Implementar handlers de nodes block-level: heading (h1-h6), paragraph, bulletList, orderedList, listItem, codeBlock (com language), blockquote, table, tableRow, tableHeader, tableCell, rule em parser/parser.go
- [x] T015 [US1] Implementar handlers de nodes inline: text com marks (strong, em, strike, code, link) e imagens externas (mediaSingle com type external) em parser/parser.go
- [x] T016 [US1] Implementar detecção de codeblock Mermaid (language == "mermaid") → codeBlock com language "mermaid" como fallback seguro conforme research.md em parser/parser.go
- [x] T017 [US1] Implementar test runner com golden files e flag -update para regeneração em parser/parser_test.go
- [x] T018 [US1] Integrar fluxo --input (leitura de arquivo MD) e --output (escrita de ADF JSON formatado) em cli/cli.go
- [x] T019 [US1] Implementar testes end-to-end para --input/--output em cli/cli_test.go

**Checkpoint**: User Story 1 funcional e testável independentemente — conversão local de MD para ADF completa

---

## Phase 4: User Story 2 — Preview Dry-Run (Prioridade: P1)

**Objetivo**: Exibir ADF gerado no terminal sem publicar, com simulação de publicação quando flags de publish estão presentes

**Teste Independente**: Executar `md2confl --input doc.md --dry-run` e verificar que ADF JSON pretty-printed aparece no stdout

### Implementação da User Story 2

- [x] T020 [US2] Implementar modo --dry-run (pretty-print ADF JSON no stdout sem efetuar publicação) em cli/cli.go
- [x] T021 [US2] Implementar exibição de simulação no dry-run quando flags de publicação presentes (mostrar título, space, parent page que seriam usados) em cli/cli.go
- [x] T022 [US2] Adicionar testes CLI para --dry-run (output ADF e simulação) em cli/cli_test.go

**Checkpoint**: User Stories 1 e 2 funcionais — ciclo editar-converter-revisar completo

---

## Phase 5: User Story 6 — Ajuda e Documentação do CLI (Prioridade: P1)

**Objetivo**: Exibir ajuda abrangente no terminal para todos os comandos e opções

**Teste Independente**: Executar `md2confl --help` e verificar que todos os flags, descrições e exemplos estão presentes

### Implementação da User Story 6

- [x] T023 [US6] Implementar --help com descrições de todos os flags, exemplos de uso e combinações válidas conforme contracts/cli-interface.md em cli/cli.go
- [x] T024 [US6] Implementar mensagem de uso resumida quando executado sem argumentos (exit code 1) em cli/cli.go
- [x] T025 [US6] Implementar flag --version (exibir versão injetada via ldflags) em cli/cli.go
- [x] T026 [US6] Adicionar testes CLI para --help, --version e execução sem argumentos em cli/cli_test.go

**Checkpoint**: Todas as User Stories P1 completas (US1 + US2 + US6) — MVP funcional

---

## Phase 6: User Story 3 — Publicação no Confluence Cloud (Prioridade: P2)

**Objetivo**: Publicar arquivo Markdown como página no Confluence Cloud via API v2, com suporte a create, update, force e write-marker

**Teste Independente**: Configurar credenciais e executar `--publish`, verificar que página é criada/atualizada corretamente

### Testes para User Story 3

- [x] T027 [P] [US3] Implementar testes com httptest.Server para todos os endpoints da API Confluence (create, update, get, findByTitle, resolveSpace) em confluence/client_test.go

### Implementação da User Story 3

- [x] T028 [P] [US3] Implementar struct Client com autenticação Basic Auth (base64 email:token), HTTPS obrigatório e headers padrão em confluence/client.go
- [x] T029 [P] [US3] Implementar tipos de erro categorizados (ErrAuth, ErrNotFound, ErrConflict, ErrValidation, ErrNetwork) com mapeamento para exit codes em confluence/errors.go
- [x] T030 [US3] Implementar ResolveSpaceID (GET /spaces?keys={KEY} → spaceId) em confluence/client.go
- [x] T031 [US3] Implementar CreatePage (POST /pages com body.value como string JSON-encoded do ADF) em confluence/client.go
- [x] T032 [US3] Implementar GetPage (GET /pages/{id}?body-format=atlas_doc_format) em confluence/client.go
- [x] T033 [US3] Implementar UpdatePage (GET versão atual, PUT com version.number+1) em confluence/client.go
- [x] T034 [US3] Implementar FindByTitle (GET /pages?title={title}&space-id={spaceId}) para --force em confluence/client.go
- [x] T035 [US3] Implementar extração de metadados do Markdown: page-id marker (regex `<!--\s*confluence-page-id:\s*(\d+)\s*-->`), derivação de título (--title > H1 > filename) em cli/cli.go
- [x] T036 [US3] Implementar leitura de credenciais via env vars (CONFLUENCE_EMAIL, CONFLUENCE_TOKEN, CONFLUENCE_URL) com precedência flag > env var, e warning em stderr se token passado via flag em cli/cli.go
- [x] T037 [US3] Implementar orquestração --publish: se page-id presente → update, senão → create; exibir URL e page-id no stdout após sucesso em cli/cli.go
- [x] T038 [US3] Implementar --force: busca por título exato no space → se encontrar, sobrescreve (update); se não, cria nova em cli/cli.go
- [x] T039 [US3] Implementar --write-marker: após publish bem-sucedido, escrever `<!-- confluence-page-id: ID -->` no início do arquivo MD em cli/cli.go
- [x] T040 [US3] Implementar --json output para resultados de publicação (status, pageId, pageUrl, title, action, version) e erros (status, code, message, hint) em cli/output.go
- [x] T041 [US3] Adicionar testes CLI end-to-end para --publish, --force, --write-marker, --json e tratamento de erros em cli/cli_test.go

**Checkpoint**: User Story 3 completa — publicação no Confluence funcional

---

## Phase 7: User Story 4 — Upload de Imagens Locais (Prioridade: P2)

**Objetivo**: Upload automático de imagens locais como attachments ao publicar, mantendo referências corretas no ADF

**Teste Independente**: Publicar Markdown com imagens locais (`![alt](./img/diagram.png)`) e verificar que aparecem na página Confluence

### Implementação da User Story 4

- [x] T042 [US4] Implementar detecção de caminhos de imagens locais no Markdown (diferenciar local vs URL externa) e popular MarkdownSource.LocalImages em cli/cli.go
- [x] T043 [US4] Implementar UploadAttachment (POST multipart/form-data via API v1 com header X-Atlassian-Token: no-check) em confluence/client.go
- [x] T044 [US4] Implementar orquestração de upload: iterar imagens locais → upload como attachment → patching de nodes media no ADF com attachment ID em cli/cli.go
- [x] T045 [US4] Implementar warning quando imagem local referenciada não existe no filesystem (continuar processamento) em cli/cli.go
- [x] T046 [US4] Adicionar testes com httptest mock para upload de attachment e testes CLI para fluxo de imagens em confluence/client_test.go e cli/cli_test.go

**Checkpoint**: User Stories P2 completas (US3 + US4) — publicação com imagens funcional

---

## Phase 8: User Story 5 — Sincronização de Pasta com Hierarquia (Prioridade: P3)

**Objetivo**: Converter pasta inteira de Markdown em hierarquia de páginas no Confluence, espelhando estrutura de diretórios

**Teste Independente**: Executar publicação de uma pasta com subpastas e verificar que hierarquia de subpáginas reflete estrutura de diretórios

### Implementação da User Story 5

- [x] T047 [US5] Implementar walker de diretórios com construção de DirTree (recursivo, filtrando apenas *.md) em cli/cli.go
- [x] T048 [US5] Implementar lógica README.md como conteúdo da página pai do diretório; se ausente, criar página pai vazia com nome do diretório em cli/cli.go
- [x] T049 [US5] Implementar publicação recursiva de hierarquia (DirTree → árvore de páginas Confluence com parent-id correto) em cli/cli.go
- [x] T050 [US5] Implementar --title em modo pasta (aplica apenas à página raiz; subpáginas usam H1 > filename) e suporte a page-id markers em cada arquivo em cli/cli.go
- [x] T051 [US5] Adicionar testes CLI para modo pasta (hierarquia, README como parent, sync com markers existentes) em cli/cli_test.go

**Checkpoint**: Todas as user stories implementadas

---

## Phase 9: Polish & Preocupações Transversais

**Objetivo**: Melhorias que afetam múltiplas user stories

- [x] T052 [P] Validar cross-compilation para todas as plataformas (linux/amd64, linux/arm64, darwin/amd64, darwin/arm64, windows/amd64) via Makefile
- [x] T053 Implementar edge cases: MD vazio → ADF vazio + warning, HTML inline → texto raw ou warning, múltiplos Mermaid independentes em parser/parser.go
- [x] T054 [P] Auditoria de segurança: mascaramento de credenciais em todos os logs/outputs, warning em stderr se token passado via flag CLI em cli/cli.go e confluence/client.go
- [x] T055 Validar quickstart.md end-to-end (build, conversão, dry-run, publicação)

---

## Dependências & Ordem de Execução

### Dependências entre Phases

- **Setup (Phase 1)**: Sem dependências — pode iniciar imediatamente
- **Foundational (Phase 2)**: Depende de Setup — BLOQUEIA todas as user stories
- **US1 (Phase 3)**: Depende de Foundational — 🎯 MVP core
- **US2 (Phase 4)**: Depende de US1 (precisa do parser funcional)
- **US6 (Phase 5)**: Depende de Foundational (apenas flags), pode paralelizar com US1
- **US3 (Phase 6)**: Depende de US1 (precisa de ADF válido para publicar)
- **US4 (Phase 7)**: Depende de US3 (precisa do fluxo de publicação)
- **US5 (Phase 8)**: Depende de US3 e US4 (combina todas as capacidades)
- **Polish (Phase 9)**: Depende de todas as user stories desejadas estarem completas

### Dependências entre User Stories

```
US1 (Conversão MD→ADF) ──────┬──→ US2 (Dry-Run)
                              ├──→ US3 (Publicação) ──→ US4 (Imagens) ──→ US5 (Pastas)
                              │
US6 (Help/Docs) ──────────────┘ (independente, pode paralelizar com US1)
```

### Dentro de Cada User Story

- Golden files/testes de referência PRIMEIRO (quando aplicável)
- Models antes de services
- Services antes de endpoints/orquestração
- Implementação core antes de integração
- Story completa antes de mover para próxima prioridade

### Oportunidades de Paralelismo

- **Phase 1**: T003, T004 podem rodar em paralelo (após T001, T002)
- **Phase 2**: T006, T007, T008 podem rodar em paralelo (após T005)
- **Phase 3 (US1)**: T009-T012 (golden files) podem rodar em paralelo entre si e com T013
- **Phase 6 (US3)**: T027, T028, T029 podem rodar em paralelo no início
- **Phase 5 + US1**: US6 (help/docs) pode rodar em paralelo com US1

---

## Exemplo de Paralelismo: User Story 1

```bash
# Lançar todos os golden files em paralelo:
Task: "Criar golden file basic.md+json em parser/testdata/"
Task: "Criar golden file mermaid.md+json em parser/testdata/"
Task: "Criar golden file table.md+json em parser/testdata/"
Task: "Criar golden file codeblock.md+json em parser/testdata/"

# Após walker implementado, handlers block e inline em sequência:
Task: "Implementar handlers block-level em parser/parser.go"
Task: "Implementar handlers inline em parser/parser.go"
```

---

## Exemplo de Paralelismo: User Story 3

```bash
# Lançar infraestrutura Confluence em paralelo:
Task: "Implementar struct Client com auth em confluence/client.go"
Task: "Implementar tipos de erro em confluence/errors.go"
Task: "Implementar testes httptest em confluence/client_test.go"

# Após client pronto, endpoints em sequência:
Task: "Implementar ResolveSpaceID em confluence/client.go"
Task: "Implementar CreatePage em confluence/client.go"
Task: "Implementar GetPage em confluence/client.go"
Task: "Implementar UpdatePage em confluence/client.go"
```

---

## Estratégia de Implementação

### MVP Primeiro (User Story 1 Apenas)

1. Completar Phase 1: Setup
2. Completar Phase 2: Foundational (CRÍTICO — bloqueia todas as stories)
3. Completar Phase 3: User Story 1 (Conversão MD → ADF)
4. **PARAR E VALIDAR**: Testar US1 independentemente com golden files
5. Deploy/demo se pronto — ferramenta já útil para conversão local

### Entrega Incremental

1. Setup + Foundational → Fundação pronta
2. Adicionar US1 (Conversão) → Testar → **MVP! Conversão local funcional**
3. Adicionar US2 (Dry-Run) + US6 (Help) → Testar → **P1 completo!**
4. Adicionar US3 (Publicação) → Testar → **Publicação funcional**
5. Adicionar US4 (Imagens) → Testar → **Publicação com imagens**
6. Adicionar US5 (Pastas) → Testar → **Feature completa**
7. Polish → Validação final

### Estratégia de Time Paralelo

Com múltiplos desenvolvedores:

1. Time completa Setup + Foundational juntos
2. Após Foundational:
   - Dev A: User Story 1 (Conversão)
   - Dev B: User Story 6 (Help/Docs) — independente
3. Após US1:
   - Dev A: User Story 2 (Dry-Run) + User Story 3 (Publicação)
   - Dev B: Testes e golden files adicionais
4. Após US3:
   - Dev A: User Story 4 (Imagens)
   - Dev B: User Story 5 (Pastas) — após US4

---

## Notas

- Tarefas [P] = arquivos diferentes, sem dependências
- Label [Story] mapeia tarefa para user story específica (rastreabilidade)
- Cada user story deve ser completável e testável independentemente
- Commit após cada tarefa ou grupo lógico
- Pare em qualquer checkpoint para validar story independentemente
- Evitar: tarefas vagas, conflitos no mesmo arquivo, dependências cross-story que quebrem independência
