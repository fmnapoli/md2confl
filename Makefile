VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
BINARY := md2confl
PLATFORMS := linux/amd64 linux/arm64 darwin/amd64 darwin/arm64 windows/amd64
LDFLAGS := -X main.Version=$(VERSION)
LICENSE_HEADER := // Copyright 2026 md2confl contributors\n// SPDX-License-Identifier: Apache-2.0

.PHONY: build test lint cross-compile license-check docker clean

build:
	go build -ldflags "$(LDFLAGS)" -o bin/$(BINARY) ./cmd/md2confl

test:
	go test -race ./...

lint:
	go vet ./...

cross-compile:
	@for platform in $(PLATFORMS); do \
		GOOS=$${platform%/*} GOARCH=$${platform#*/} \
		go build -ldflags "$(LDFLAGS)" \
		-o bin/$(BINARY)-$${platform%/*}-$${platform#*/}$$([ "$${platform%/*}" = "windows" ] && echo ".exe") \
		./cmd/md2confl; \
		echo "Built $$platform"; \
	done

license-check:
	@fail=0; \
	for f in $$(find . -name '*.go' -not -path './vendor/*'); do \
		if ! head -2 "$$f" | grep -q 'SPDX-License-Identifier: Apache-2.0'; then \
			echo "Missing license header: $$f"; \
			fail=1; \
		fi; \
	done; \
	if [ "$$fail" -eq 1 ]; then exit 1; fi; \
	echo "All Go files have license headers"

docker:
	docker build -t md2confl:$(VERSION) .

clean:
	rm -rf bin/
