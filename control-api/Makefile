.PHONY: run build test lint tidy

# Local dev environment variables
export MONGO_URI       ?= mongodb://localhost:27017
export MONGO_DB        ?= devops_platform
export SERVER_PORT     ?= 8080
export K8S_IN_CLUSTER  ?= false
export LOG_LEVEL       ?= debug

run:
	-lsof -ti :$(SERVER_PORT) | xargs kill 2>/dev/null; true
	go run ./cmd/server/main.go

build:
	go build -o bin/server ./cmd/server/main.go

test:
	go test ./...

lint:
	golangci-lint run

tidy:
	go mod tidy
