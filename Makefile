.PHONY: fmt test build run

fmt:
	gofmt -w cmd internal

test:
	go test ./...

build:
	go build ./cmd/dbops-server

run:
	go run ./cmd/dbops-server --config config/server.example.yaml
