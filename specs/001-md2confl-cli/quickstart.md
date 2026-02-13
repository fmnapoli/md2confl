# Quickstart: md2confl

## Pré-requisitos

- Go 1.22+ instalado
- (Para publicação) Conta Atlassian Cloud com API token

## Build

```bash
git clone https://github.com/user/md2confl.git
cd md2confl
go build -o md2confl ./cmd/md2confl
```

## Uso Básico

### 1. Converter Markdown para ADF (arquivo)

```bash
./md2confl --input README.md --output readme.json
```

### 2. Preview no terminal (dry-run)

```bash
./md2confl --input README.md --dry-run
```

### 3. Publicar no Confluence

```bash
# Via flags
./md2confl --input README.md --publish \
  --url https://mysite.atlassian.net \
  --space DEVOPS \
  --email user@example.com \
  --token YOUR_API_TOKEN \
  --title "Getting Started"

# Via variáveis de ambiente
export CONFLUENCE_URL=https://mysite.atlassian.net
export CONFLUENCE_EMAIL=user@example.com
export CONFLUENCE_TOKEN=YOUR_API_TOKEN

./md2confl --input README.md --publish --space DEVOPS --title "Getting Started"
```

### 4. Atualizar página existente

Adicione o marcador no seu arquivo Markdown:
```markdown
<!-- confluence-page-id: 111222 -->
# Meu Documento
...
```

```bash
./md2confl --input doc.md --publish --space DEVOPS
```

### 5. Publicar pasta inteira

```bash
./md2confl --input docs/ --publish --space DEVOPS
```

### 6. Output JSON para CI/CD

```bash
./md2confl --input doc.md --publish --space DEVOPS --json
```

## Estrutura do Projeto

```
cmd/md2confl/main.go  — entry point
parser/               — Markdown → ADF (biblioteca importável)
adf/                  — tipos ADF (structs Go)
confluence/           — cliente REST API Confluence
cli/                  — wiring CLI (flags, output)
```

## Testes

```bash
# Todos os testes
go test ./...

# Com cobertura
go test -coverprofile=coverage.out ./...
go tool cover -html=coverage.out

# Atualizar golden files
go test ./parser/... -update
```

## Cross-compilation

```bash
# Linux AMD64
GOOS=linux GOARCH=amd64 go build -o md2confl-linux-amd64 ./cmd/md2confl

# macOS ARM64 (Apple Silicon)
GOOS=darwin GOARCH=arm64 go build -o md2confl-darwin-arm64 ./cmd/md2confl

# Windows AMD64
GOOS=windows GOARCH=amd64 go build -o md2confl-windows-amd64.exe ./cmd/md2confl
```
