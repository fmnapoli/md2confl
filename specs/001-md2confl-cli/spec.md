# Especificação de Feature: md2confl CLI

**Feature Branch**: `001-md2confl-cli`
**Criado**: 2026-02-12
**Status**: Draft
**Input**: Ferramenta CLI para converter arquivos Markdown com diagramas Mermaid em páginas do Confluence Cloud (formato ADF)

## Clarificações

### Sessão 2026-02-12

- Q: Qual a regra de precedência para derivação do título da página? → A: `--title` flag (se fornecido) > primeiro H1 do arquivo > nome do arquivo sem extensão. Em modo pasta, `--title` aplica apenas à página raiz; cada subpágina usa H1 > filename.
- Q: Como README.md deve ser tratado na hierarquia de pastas? → A: README.md se torna o conteúdo da página pai do diretório; os demais arquivos `.md` no mesmo diretório viram subpáginas. Se o diretório não contiver README.md, a página pai é criada vazia com o nome do diretório.
- Q: Qual o mecanismo de busca/match do `--force`? → A: `--force` busca por título exato no space alvo; se encontrar página com mesmo título, sobrescreve. Sem match, cria nova página.
- Q: Após primeiro publish, o sistema deve escrever o marcador page-id de volta no arquivo MD? → A: Somente com flag explícito `--write-marker`. Por padrão, o sistema não modifica o arquivo Markdown original (princípio MD-first). O page-id é sempre exibido no stdout após publish.
- Q: O CLI deve suportar output JSON estruturado e exit codes diferenciados para CI/CD? → A: Sim. Flag `--json` para output estruturado em todas as operações. Exit codes: 0=sucesso, 1=erro do usuário (input inválido, flags faltando), 2=erro de API (auth, rede, conflito).

## Cenários de Usuário & Testes *(obrigatório)*

### User Story 1 - Conversão Local de Markdown para ADF (Prioridade: P1)

Como DevOps engineer, quero converter um arquivo Markdown local (contendo headings, tabelas, codeblocks e diagramas Mermaid) em um arquivo ADF JSON válido, para que eu possa revisar a saída antes de publicar no Confluence.

**Por que esta prioridade**: Esta é a funcionalidade core — sem conversão correta de Markdown para ADF, nenhuma outra feature funciona. Entrega valor imediato mesmo sem integração com Confluence.

**Teste Independente**: Pode ser testada fornecendo um arquivo Markdown com elementos GFM variados e diagramas Mermaid, executando o CLI com `--output resultado.json`, e validando que o JSON gerado é ADF válido com todos os elementos preservados.

**Cenários de Aceitação**:

1. **Dado** um arquivo Markdown com headings (h1-h6), listas (ordenadas e não-ordenadas), links e texto formatado, **Quando** o usuário executa `md2confl --input doc.md --output doc.json`, **Então** o sistema gera um arquivo ADF JSON válido com todos os elementos corretamente mapeados.
2. **Dado** um arquivo Markdown com um codeblock ` ```mermaid ` contendo `graph TD; A-->B;`, **Quando** o sistema converte para ADF, **Então** o codeblock Mermaid é representado como `codeBlock` ADF com `language: "mermaid"`, preservando o código-fonte do diagrama (não como imagem renderizada).
3. **Dado** um arquivo Markdown com tabelas GFM (incluindo headers e múltiplas linhas), **Quando** o sistema converte para ADF, **Então** as tabelas são representadas como table nodes ADF preservando estrutura de headers e células.
4. **Dado** um arquivo Markdown com codeblocks de linguagens de programação (ex: ```go, ```yaml), **Quando** o sistema converte para ADF, **Então** os codeblocks são representados com a linguagem de syntax highlighting preservada.

---

### User Story 2 - Preview Dry-Run (Prioridade: P1)

Como DevOps engineer, quero visualizar o ADF gerado no terminal sem publicar, para ter feedback rápido durante a edição do documento.

**Por que esta prioridade**: Complementa a conversão local como ciclo de feedback essencial — editar, converter, revisar, repetir.

**Teste Independente**: Pode ser testada executando o CLI com `--dry-run` e verificando que o ADF JSON formatado é exibido no stdout junto com uma simulação do que seria publicado.

**Cenários de Aceitação**:

1. **Dado** um arquivo Markdown válido, **Quando** o usuário executa `md2confl --input doc.md --dry-run`, **Então** o ADF JSON formatado (pretty-printed) é exibido no stdout.
2. **Dado** flags de publicação fornecidas junto com `--dry-run`, **Quando** o usuário executa o comando, **Então** o sistema mostra uma simulação da publicação (título, space, parent page) sem efetuar nenhuma chamada à API do Confluence.

---

### User Story 3 - Publicação no Confluence Cloud (Prioridade: P2)

Como arquiteto de software, quero publicar um arquivo Markdown diretamente como página no Confluence Cloud, para compartilhar documentação técnica com o time sem copy/paste manual.

**Por que esta prioridade**: É o objetivo final do fluxo, mas depende da conversão P1 estar funcionando. Entrega o valor completo da ferramenta.

**Teste Independente**: Pode ser testada configurando credenciais de API Confluence e executando `--publish`, verificando que a página é criada com título, conteúdo e hierarquia corretos.

**Cenários de Aceitação**:

1. **Dado** um arquivo Markdown e credenciais Confluence válidas (URL, email, token, space key), **Quando** o usuário executa `md2confl --input doc.md --publish --url https://site.atlassian.net --space DEVOPS --email user@ex.com --token TOKEN --title "Meu Doc"`, **Então** o sistema cria uma nova página no Confluence com o conteúdo ADF convertido e retorna a URL e ID da página criada.
2. **Dado** um arquivo Markdown contendo o marcador `<!-- confluence-page-id: 12345 -->`, **Quando** o usuário executa o comando de publicação, **Então** o sistema atualiza a página existente (ID 12345) ao invés de criar uma nova.
3. **Dado** um parent page ID fornecido via `--parent-id`, **Quando** o sistema publica a página, **Então** a nova página é criada como subpágina da parent page especificada.
4. **Dado** credenciais inválidas (token expirado/incorreto), **Quando** o usuário tenta publicar, **Então** o sistema exibe uma mensagem de erro clara indicando que o token de autenticação é inválido.

---

### User Story 4 - Upload de Imagens Locais (Prioridade: P2)

Como autor de documentação, quero que imagens referenciadas localmente no Markdown sejam automaticamente enviadas como attachments ao publicar no Confluence, para que as imagens não sejam perdidas.

**Por que esta prioridade**: Imagens são parte essencial de documentação técnica, mas a feature depende do fluxo de publicação (P2) estar funcionando.

**Teste Independente**: Pode ser testada com um Markdown que referencia imagens locais (`![alt](./img/diagram.png)`), publicando no Confluence e verificando que as imagens aparecem corretamente na página.

**Cenários de Aceitação**:

1. **Dado** um Markdown com referência a uma imagem local (`![arch](./images/arch.png)`), **Quando** o sistema publica no Confluence, **Então** a imagem é uploadada como attachment da página e o ADF referencia o attachment corretamente.
2. **Dado** um Markdown com referência a uma imagem via URL externa (`![logo](https://example.com/logo.png)`), **Quando** o sistema converte para ADF, **Então** a imagem é referenciada diretamente pela URL (sem download/upload).
3. **Dado** um Markdown com referência a uma imagem local que não existe no caminho especificado, **Quando** o sistema tenta converter, **Então** exibe um aviso indicando o arquivo não encontrado e continua o processamento.

---

### User Story 5 - Sincronização de Pasta com Hierarquia (Prioridade: P3)

Como arquiteto, quero converter uma pasta inteira de documentação Markdown em uma hierarquia de páginas no Confluence, para manter a estrutura organizacional dos documentos.

**Por que esta prioridade**: Feature avançada que combina todas as capacidades anteriores. Útil para sync automatizado, mas não bloqueia uso básico da ferramenta.

**Teste Independente**: Pode ser testada com uma pasta contendo subpastas com arquivos Markdown, executando a publicação e verificando que a hierarquia de subpáginas reflete a estrutura de diretórios.

**Cenários de Aceitação**:

1. **Dado** uma pasta `docs/` com a estrutura `docs/README.md`, `docs/setup/install.md`, `docs/setup/config.md`, **Quando** o usuário executa `md2confl --input docs/ --publish --space DEVOPS`, **Então** o sistema cria páginas no Confluence espelhando a hierarquia de diretórios (page raiz + subpáginas para cada subpasta/arquivo).
2. **Dado** uma pasta com arquivos já publicados (contendo marcadores `<!-- confluence-page-id: ... -->`), **Quando** o usuário executa a sincronização, **Então** o sistema atualiza as páginas existentes e cria apenas as novas.

---

### User Story 6 - Ajuda e Documentação do CLI (Prioridade: P1)

Como novo usuário da ferramenta, quero ver ajuda abrangente no terminal, para entender todos os comandos e opções disponíveis sem consultar documentação externa.

**Por que esta prioridade**: Essencial para adoção — sem help claro, usuários não conseguem descobrir como usar a ferramenta.

**Teste Independente**: Pode ser testada executando `md2confl --help` e verificando que todos os flags, opções e exemplos de uso estão documentados.

**Cenários de Aceitação**:

1. **Dado** que o usuário não conhece a ferramenta, **Quando** executa `md2confl --help`, **Então** o sistema exibe todas as flags disponíveis (input, output, dry-run, publish, url, space, parent-id, title, email, token, force, write-marker, json) com descrições e exemplos de uso.
2. **Dado** que o usuário executa o comando sem argumentos, **Quando** nenhum input é fornecido, **Então** o sistema exibe uma mensagem de uso resumida com os comandos mais comuns.

---

### Casos de Borda

- O que acontece quando o arquivo Markdown está vazio? O sistema deve gerar um ADF válido com documento vazio e exibir um aviso.
- O que acontece quando o Markdown contém HTML inline? O sistema deve preservar o HTML como texto raw no ADF ou ignorá-lo com aviso.
- O que acontece quando há múltiplos codeblocks Mermaid no mesmo documento? Todos devem ser convertidos independentemente para extensões Mermaid no ADF.
- Como o sistema lida com conflito ao tentar criar uma página que já existe (sem marcador de page-id no MD)? O sistema deve retornar erro informativo sugerindo uso de `--force` (que busca por título exato no space e sobrescreve) ou inclusão do marcador de page-id.
- O que acontece quando a API do Confluence retorna erro 422? O sistema deve exibir mensagem clara sobre o conflito e sugerir ações corretivas.
- Como o sistema lida com arquivos Markdown maiores que 10.000 linhas? O sistema deve processá-los normalmente dentro do critério de performance (<1 segundo).
- O que acontece com links relativos entre Markdown files em uma pasta? Em v1, links relativos são preservados como-estão no ADF (podem quebrar no Confluence). Mapeamento automático para páginas Confluence é planejado para v2.

## Requisitos *(obrigatório)*

### Requisitos Funcionais

- **FR-001**: O sistema DEVE aceitar como input um arquivo `.md` único via flag `--input caminho/arquivo.md`.
- **FR-002**: O sistema DEVE aceitar como input uma pasta (processamento recursivo de todos os `.md`) via flag `--input caminho/pasta/`.
- **FR-003**: O sistema DEVE fazer parsing completo de Markdown GFM incluindo: headings (h1-h6), listas ordenadas e não-ordenadas, tabelas, codeblocks com syntax highlighting, links, texto em negrito/itálico/strikethrough, e blockquotes.
- **FR-004**: O sistema DEVE detectar codeblocks com linguagem `mermaid` e convertê-los para `codeBlock` ADF com `language: "mermaid"` por padrão (portável, funciona sem apps third-party). A arquitetura DEVE suportar output como `bodiedExtension` via flags opcionais futuros. Não renderizar como imagem SVG/PNG.
- **FR-005**: O sistema DEVE suportar output para arquivo JSON via flag `--output caminho/saida.json`.
- **FR-006**: O sistema DEVE suportar modo dry-run via flag `--dry-run`, exibindo o ADF JSON formatado no stdout sem efetuar publicação.
- **FR-007**: O sistema DEVE suportar publicação direta no Confluence Cloud via flag `--publish`, criando ou atualizando páginas.
- **FR-008**: O sistema DEVE aceitar credenciais de publicação via flags: `--url` (URL base do Confluence), `--space` (space key), `--parent-id` (ID da página pai), `--title` (título da página), `--email` (email do usuário), `--token` (API token).
- **FR-009**: O sistema DEVE aceitar credenciais (email, token e URL base) via variáveis de ambiente (`CONFLUENCE_EMAIL`, `CONFLUENCE_TOKEN`, `CONFLUENCE_URL`) como alternativa às flags de linha de comando. Precedência: flags CLI > variáveis de ambiente.
- **FR-010**: O sistema DEVE detectar o marcador `<!-- confluence-page-id: ID -->` no arquivo Markdown e, quando presente, atualizar a página existente ao invés de criar uma nova.
- **FR-011**: O sistema DEVE, ao processar uma pasta como input, criar subpáginas no Confluence espelhando a hierarquia de diretórios do filesystem. Se um diretório contiver `README.md`, este arquivo DEVE ser usado como conteúdo da página pai daquele diretório (demais `.md` viram subpáginas). Se não houver `README.md`, a página pai DEVE ser criada vazia com o nome do diretório como título.
- **FR-012**: O sistema DEVE fazer upload de imagens referenciadas localmente no Markdown como attachments da página Confluence e referenciá-las corretamente no ADF.
- **FR-013**: O sistema DEVE preservar imagens referenciadas por URL externa como links diretos no ADF (sem download).
- **FR-014**: O sistema DEVE exibir mensagens de erro claras e acionáveis para: credenciais inválidas, conflitos de página, arquivos não encontrados, e erros de rede.
- **FR-015**: O sistema DEVE retornar a URL e o ID da página criada/atualizada após publicação bem-sucedida.
- **FR-016**: O sistema DEVE suportar flag `--force` que busca por título exato no space alvo; se encontrar página com mesmo título, sobrescreve o conteúdo. Sem match de título, cria nova página normalmente.
- **FR-017**: O sistema DEVE exibir ajuda completa via `--help` cobrindo todos os flags, descrições e exemplos de uso.
- **FR-018**: O sistema DEVE nunca registrar em logs ou exibir em output o token de API ou credenciais sensíveis.
- **FR-019**: O sistema DEVE ser distribuído como binário único, instalável sem dependências externas de runtime.
- **FR-020**: O sistema DEVE suportar flag `--write-marker` que, após publicação bem-sucedida, escreve o marcador `<!-- confluence-page-id: ID -->` de volta no arquivo Markdown original. Por padrão (sem o flag), o sistema NÃO DEVE modificar arquivos de entrada.
- **FR-021**: O sistema DEVE sempre exibir o page-id no stdout após publicação bem-sucedida, independentemente do flag `--write-marker`.
- **FR-022**: O sistema DEVE suportar flag `--json` que formata todo output (sucesso, erros, dry-run, resultados de publish) como JSON estruturado, adequado para parsing por pipelines CI/CD.
- **FR-023**: O sistema DEVE utilizar exit codes diferenciados: 0 para sucesso, 1 para erro do usuário (input inválido, flags obrigatórios ausentes, arquivo não encontrado), 2 para erro de API (autenticação, rede, conflito no Confluence).

### Entidades-Chave

- **Documento Markdown**: Arquivo de entrada contendo texto em formato GFM, possivelmente com codeblocks Mermaid, imagens locais/remotas, e opcionalmente o marcador de page-id do Confluence. Atributos: caminho no filesystem, conteúdo, page-id (opcional), título (derivado por precedência: flag `--title` > primeiro H1 > nome do arquivo sem extensão; em modo pasta, `--title` aplica apenas à página raiz).
- **Documento ADF**: Representação intermediária no formato Atlassian Document Format. Contém a árvore de nodes correspondente ao Markdown convertido, incluindo extensões Mermaid e referências a attachments. Atributos: versão, tipo, conteúdo (árvore de nodes).
- **Página Confluence**: Página publicada no Confluence Cloud. Atributos: ID, título, space key, parent page ID, body (ADF), URL, versão.
- **Attachment**: Arquivo binário (imagem) uploadado como anexo de uma página Confluence. Atributos: nome do arquivo, media type, tamanho, página associada.
- **Configuração de Publicação**: Conjunto de parâmetros necessários para publicar no Confluence. Atributos: URL base, space key, parent page ID, email, API token, título.

## Critérios de Sucesso *(obrigatório)*

### Resultados Mensuráveis

- **SC-001**: Usuário consegue converter um arquivo Markdown de 10.000 linhas com 5 diagramas Mermaid para ADF em menos de 1 segundo.
- **SC-002**: 100% dos elementos GFM suportados (headings, listas, tabelas, codeblocks, links, imagens, formatação inline) são preservados na conversão para ADF sem perda de informação.
- **SC-003**: Diagramas Mermaid são preservados como codeBlock com language "mermaid" no ADF, mantendo o código-fonte editável. Workspaces com app Mermaid instalado renderizam automaticamente; sem app, o código é exibido como bloco de código.
- **SC-004**: Usuário consegue publicar um documento no Confluence em um único comando, sem etapas manuais intermediárias (copy/paste, upload de imagens separado, etc.).
- **SC-005**: Atualizações de documentos já publicados (via marcador page-id) preservam o histórico de versões da página no Confluence.
- **SC-006**: Usuário novo consegue instalar a ferramenta e publicar seu primeiro documento em menos de 5 minutos, usando apenas a ajuda embutida (`--help`).
- **SC-007**: A ferramenta funciona em Linux, macOS e Windows sem modificações ou dependências adicionais.
- **SC-008**: Credenciais de API nunca são expostas em logs, output do terminal ou mensagens de erro.

## Premissas

- O Confluence Cloud NÃO possui macro Mermaid nativa. O suporte a Mermaid depende de apps third-party (ex: "Mermaid Charts & Diagrams" da weweave). O md2confl usa `codeBlock` como formato portável por padrão.
- O formato ADF do Confluence Cloud é estável e documentado pela Atlassian.
- Usuários possuem API tokens válidos do Atlassian com permissões de criação/edição de páginas no space alvo.
- A ferramenta será distribuída como binário único compilado (Go), sem necessidade de runtime ou interpretador instalado.
- Para v1, apenas Confluence Cloud é suportado (não Confluence Server/Data Center).
- Elementos Markdown não suportados pelo ADF (se houver) serão convertidos para a representação mais próxima possível, com aviso ao usuário.

## Fora de Escopo (v1)

- Suporte a Confluence Server ou Data Center (apenas Cloud com ADF).
- Embed de vídeos ou conteúdo multimídia avançado.
- Operações multi-space (bulk publish em múltiplos spaces simultaneamente).
- Interface gráfica (GUI) ou interface web.
- Renderização local de diagramas Mermaid (SVG/PNG) — a conversão usa exclusivamente codeBlock com language "mermaid".
- Mapeamento automático de links relativos entre Markdown files para páginas Confluence correspondentes (v2).
