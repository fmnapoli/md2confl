# Contrato: Confluence Cloud REST API v2

**Base URL**: `https://{domain}.atlassian.net/wiki/api/v2`
**Auth**: `Authorization: Basic base64(email:token)`
**Content-Type**: `application/json`

## 1. Resolver Space Key → Space ID

```
GET /spaces?keys={SPACE_KEY}
```

**Response 200**:
```json
{
  "results": [
    {
      "id": "123456",
      "key": "DEVOPS",
      "name": "DevOps Space",
      "type": "global",
      "status": "current"
    }
  ]
}
```

**Erros**: 401 (auth), 403 (sem permissão)

---

## 2. Criar Página

```
POST /pages
Content-Type: application/json
```

**Request Body**:
```json
{
  "spaceId": "123456",
  "status": "current",
  "title": "Meu Documento",
  "parentId": "789012",
  "body": {
    "representation": "atlas_doc_format",
    "value": "{\"version\":1,\"type\":\"doc\",\"content\":[...]}"
  }
}
```

**Notas**:
- `parentId` é opcional — se omitido, cria na raiz do space
- **CRÍTICO**: `body.value` é uma **string JSON-encoded** (resultado de `json.Marshal` do ADF), **NÃO** um objeto JSON aninhado. Passar objeto raw causa 400 Bad Request
- `spaceId` é um inteiro numérico (string), não o space key

**Response 200**:
```json
{
  "id": "111222",
  "title": "Meu Documento",
  "status": "current",
  "version": {
    "number": 1,
    "createdAt": "2026-02-12T10:00:00Z"
  },
  "_links": {
    "webui": "/spaces/DEVOPS/pages/111222/Meu+Documento",
    "base": "https://mysite.atlassian.net/wiki"
  }
}
```

**URL completa da página**: `{_links.base}{_links.webui}`

**Erros**: 400 (ADF inválido), 401, 403, 404 (space/parent não existe)

---

## 3. Atualizar Página

```
PUT /pages/{id}
Content-Type: application/json
```

**Request Body**:
```json
{
  "id": "111222",
  "status": "current",
  "title": "Meu Documento Atualizado",
  "body": {
    "representation": "atlas_doc_format",
    "value": "{\"version\":1,\"type\":\"doc\",\"content\":[...]}"
  },
  "version": {
    "number": 2,
    "message": "Updated by md2confl"
  }
}
```

**Notas**:
- `version.number` DEVE ser `versão_atual + 1`
- Primeiro: GET da página para obter versão atual
- `version.message` é opcional mas recomendado

**Erros**: 409 (conflito de versão — outro update concorrente)

---

## 4. Obter Página por ID

```
GET /pages/{id}?body-format=atlas_doc_format
```

**Response 200**:
```json
{
  "id": "111222",
  "title": "Meu Documento",
  "version": {
    "number": 2
  },
  "body": {
    "atlas_doc_format": {
      "value": "{...}",
      "representation": "atlas_doc_format"
    }
  },
  "_links": {
    "webui": "/spaces/DEVOPS/pages/111222",
    "base": "https://mysite.atlassian.net/wiki"
  }
}
```

**Nota**: Sem `body-format`, o campo `body` vem vazio.

---

## 5. Buscar Página por Título no Space

```
GET /pages?space-id={spaceId}&title={title}&status=current
```

**Response 200**:
```json
{
  "results": [
    {
      "id": "111222",
      "title": "Meu Documento",
      "version": { "number": 2 }
    }
  ]
}
```

**Usado por**: `--force` (busca por título exato para sobrescrever)

---

## 6. Upload Attachment (API v1)

**Nota**: Não existe endpoint v2 para upload de attachments. Usar a API v1.

```
POST /wiki/rest/api/content/{pageId}/child/attachment
Content-Type: multipart/form-data
X-Atlassian-Token: no-check
```

**Form fields**:
- `file`: binário da imagem
- `comment` (opcional): descrição

**Response 200**:
```json
{
  "results": [
    {
      "id": "att123",
      "title": "arch.png",
      "mediaType": "image/png",
      "fileSize": 45678,
      "_links": {
        "download": "/download/attachments/111222/arch.png"
      }
    }
  ]
}
```

**Nota**: Para referenciar o attachment no ADF, usar node `media` com `type: "file"` e `id` correspondente ao attachment.

---

## Referência de imagem no ADF

### Imagem local (upload como attachment)

```json
{
  "type": "mediaSingle",
  "attrs": { "layout": "center" },
  "content": [
    {
      "type": "media",
      "attrs": {
        "type": "file",
        "id": "att123",
        "collection": ""
      }
    }
  ]
}
```

### Imagem externa (URL direta)

```json
{
  "type": "mediaSingle",
  "attrs": { "layout": "center" },
  "content": [
    {
      "type": "media",
      "attrs": {
        "type": "external",
        "url": "https://example.com/logo.png"
      }
    }
  ]
}
```
