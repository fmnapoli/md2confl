VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
BINARY := md2confl
LDFLAGS := -X main.Version=$(VERSION)
LICENSE_HEADER := // Copyright 2026 md2confl contributors\n// SPDX-License-Identifier: Apache-2.0
COVERAGE_THRESHOLD := 60

.PHONY: build test lint test-coverage verify release license-check docker clean

build:
	go build -ldflags "$(LDFLAGS)" -o bin/$(BINARY) ./cmd/md2confl

test:
	go test -race ./...

lint:
	golangci-lint run ./...

test-coverage:
	@go test -race -coverprofile=coverage.out -covermode=atomic ./...
	@total=$$(go tool cover -func=coverage.out | grep total: | awk '{print $$3}' | sed 's/%//'); \
	echo "Coverage: $${total}%"; \
	threshold=$(COVERAGE_THRESHOLD); \
	if [ "$$(echo "$$total < $$threshold" | bc -l)" -eq 1 ]; then \
		echo "FAIL: coverage $${total}% < $${threshold}%"; exit 1; \
	fi

verify: lint test-coverage

release:
	goreleaser release --snapshot --clean

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
	rm -rf bin/ dist/ coverage.out
