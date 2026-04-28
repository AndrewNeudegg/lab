GO_PACKAGES := ./cmd/... ./pkg/... ./constraints
GO_CACHE ?= /tmp/homelab-go-build-cache
GO_MOD_CACHE ?= /tmp/homelab-go-mod-cache
GO_ENV := GOCACHE=$(GO_CACHE) GOMODCACHE=$(GO_MOD_CACHE)

.PHONY: build test fmt run

build:
	mkdir -p $(GO_CACHE) $(GO_MOD_CACHE)
	$(GO_ENV) go build $(GO_PACKAGES)

test:
	mkdir -p $(GO_CACHE) $(GO_MOD_CACHE)
	$(GO_ENV) go test $(GO_PACKAGES)

fmt:
	mkdir -p $(GO_CACHE) $(GO_MOD_CACHE)
	$(GO_ENV) go fmt $(GO_PACKAGES)

run:
	go run ./cmd/homelabd -mode stdio
