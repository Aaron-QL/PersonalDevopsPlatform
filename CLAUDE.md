# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

This is a personal learning/experimentation platform for studying Go, AWS Lambda, GitHub integration, Harness CI/CD, Kubernetes, and MongoDB. The goal is to build a complete DevOps learning environment that simulates real CI/CD workflows with no frontend — configuration only via API.

The project is being built incrementally from the simplest viable approach.

## Planned Architecture

- **`control-api/`** — Go-based REST API service (the central control plane)
- **`lambda-projects/`** — AWS Lambda function implementations
- **`infrastructure/`** — Kubernetes manifests and infrastructure-as-code
- **`scripts/`** — Automation and deployment utility scripts

### Technology Stack

| Layer | Technology |
|---|---|
| API Service | Go (REST) |
| Serverless | AWS Lambda |
| CI/CD | Harness pipelines |
| Orchestration | Kubernetes |
| Database | MongoDB |
| VCS Integration | GitHub |

## Development Conventions

### Go (control-api)
- When initializing: `go mod init` inside `control-api/`, use module path consistent with the repository
- Standard Go project layout: `cmd/`, `internal/`, `pkg/` subdirectories within `control-api/`
- Tests: `go test ./...` from the service root
- Linting: `golangci-lint run` if configured

### AWS / Lambda
- Use AWS CLI and SAM CLI or CDK for Lambda packaging and deployment
- Lambda functions live under `lambda-projects/`, one directory per function

### Infrastructure
- Kubernetes manifests go in `infrastructure/k8s/`
- Keep environment-specific configs separated (e.g., `infrastructure/k8s/base/` and `infrastructure/k8s/overlays/`)

> **Note:** Build commands, test commands, and CI configuration will be added here as each component is implemented.
