# Copyright 2026 md2confl contributors
# SPDX-License-Identifier: Apache-2.0

# Stage 1: Build Go binary
FROM golang:1.26-bookworm AS builder
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -ldflags "-X main.Version=$(git describe --tags --always --dirty 2>/dev/null || echo docker)" \
    -o /md2confl ./cmd/md2confl

# Stage 2: Runtime with Node.js + Chromium + mmdc
FROM node:22-bookworm-slim
RUN apt-get update && apt-get install -y --no-install-recommends \
    chromium fonts-liberation ca-certificates \
    && rm -rf /var/lib/apt/lists/*
ENV PUPPETEER_SKIP_CHROMIUM_DOWNLOAD=true
ENV PUPPETEER_EXECUTABLE_PATH=/usr/bin/chromium
ENV XDG_CONFIG_HOME=/tmp/.chromium
ENV XDG_CACHE_HOME=/tmp/.chromium
RUN npm install -g @mermaid-js/mermaid-cli \
    && mkdir -p /tmp/.chromium /tmp/crashdumps && chmod 777 /tmp/.chromium /tmp/crashdumps
COPY --from=builder /md2confl /usr/local/bin/md2confl
RUN useradd -m md2confl
USER md2confl
WORKDIR /workspace
ENTRYPOINT ["md2confl"]
