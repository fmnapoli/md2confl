<!-- confluence-page-id: 1278002 -->
# Componentes ADF

Esta página demonstra todos os elementos Markdown suportados pelo md2confl e como são renderizados no Confluence.

## Formatação inline

Texto com **negrito**, *itálico*, `código inline`, ~~riscado~~ e [link](https://example.com).

Combinações: ***negrito e itálico***, **negrito com `código`**, ~~riscado com **negrito**~~.

## Headings

### Heading 3

#### Heading 4

##### Heading 5

###### Heading 6

## Listas

### Bullet list

- Primeiro item
- Segundo item com **formatação**
- Terceiro item
  - Sub-item aninhado
  - Outro sub-item

### Ordered list

1. Passo um
2. Passo dois
3. Passo três

Ordered list com início customizado:

5. Item cinco
6. Item seis
7. Item sete

### Task list (checkboxes)

- [ ] Tarefa pendente
- [x] Tarefa concluída
- [ ] Tarefa com **formatação** e `código`
- [x] Outra tarefa feita com [link](https://example.com)

## Blockquote

> Uma citação simples com **negrito** e *itálico*.

## GitHub Alerts (panels)

> [!NOTE]
> Informação útil que o usuário deveria saber, mesmo lendo casualmente.

> [!TIP]
> Conselho opcional para ajudar o usuário a ter mais sucesso.

> [!IMPORTANT]
> Informação crucial necessária para que o usuário tenha sucesso.

> [!WARNING]
> Conteúdo crítico que exige atenção imediata por conta de riscos potenciais.

> [!CAUTION]
> Consequências negativas potenciais de uma ação.

## Code blocks

Inline: use `go test ./...` para rodar testes.

Fenced com linguagem:

```go
func main() {
    fmt.Println("Hello, Confluence!")
}
```

Fenced sem linguagem:

```
plain text code block
sem syntax highlighting
```

## Tabelas

| Feature | Status | Notas |
|---------|--------|-------|
| Headings | :white_check_mark: Suportado | H1 a H6 |
| Task lists | :white_check_mark: Suportado | `- [ ]` e `- [x]` |
| Alerts | :white_check_mark: Suportado | 5 tipos de panel |
| Emoji | :white_check_mark: Suportado | Shortcodes GitHub |
| Superscript | :white_check_mark: Suportado | `^text^` |

## Emoji :tada:

Emojis inline: :wave: Olá! Tudo :+1: por aqui :rocket:

Emojis em diferentes contextos:

- Item com emoji :white_check_mark:
- Alerta :warning: importante

## Superscript

Fórmulas: x^2^ + y^3^ = z^n^

Referências: 1^st^, 2^nd^, 3^rd^

Texto com ^superscript^ no meio da frase.

## Expand (collapse)

<details><summary>Clique para expandir</summary>
Este conteúdo fica escondido por padrão.
Pode conter **formatação** e `código`.
</details>

<details><summary>Outro bloco colapsável</summary>
Mais conteúdo escondido aqui.
</details>

## Imagens

Badge inline: [![CI](https://github.com/fmnapoli/md2confl/actions/workflows/ci.yml/badge.svg)](https://github.com/fmnapoli/md2confl/actions/workflows/ci.yml)

## Separadores

Conteúdo acima.

---

Conteúdo abaixo.

## Hard break

Linha um
Linha dois (com hard break).
