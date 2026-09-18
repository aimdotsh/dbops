.PHONY: fmt test build run run-agent

fmt:
	gofmt -w cmd internal

test:
	go test ./...

build:
	go build ./cmd/dbops-server
	go build ./cmd/dbops-agent

run:
	go run ./cmd/dbops-server --config config/server.example.yaml

run-agent:
	go run ./cmd/dbops-agent --config config/agent.example.yaml
