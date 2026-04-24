.PHONY: build test fmt run

build:
	go build ./...

test:
	go test ./...

fmt:
	go fmt ./...

run:
	go run ./cmd/homelabd -mode stdio
