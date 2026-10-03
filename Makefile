.PHONY: fmt test build web run run-agent

fmt:
	gofmt -w cmd internal

test:
	go test ./...

web:
	npm --prefix web ci
	npm --prefix web run build

build:
	go build ./cmd/dbops-server
	go build ./cmd/dbops-agent
	go build ./cmd/dbops-restore

run:
	go run ./cmd/dbops-server --config config/server.example.yaml

run-agent:
	go run ./cmd/dbops-agent --config config/agent.example.yaml
