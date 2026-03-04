# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

Personal learning platform for studying Go, AWS, GitHub integration, Harness CI/CD, Kubernetes, Terraform, and MongoDB. Simulates a real internal DevOps platform with full CI/CD workflows.

The project is being built incrementally. Full architecture: `docs/architecture.md`. Development plan: `docs/dev-plan.md`.

## Directory Structure

- **`control-api/`** — Go REST API (central control plane) + embedded Web UI
- **`frontend/`** — React web UI (built output goes to `control-api/web/dist/`)
- **`microservice-projects/`** — Sample microservice projects (one dir per service, each mirrors a GitHub repo)
- **`lambda-projects/`** — AWS Lambda function implementations (one dir per function)
- **`infrastructure/k8s/`** — Kubernetes manifests (`base/` + `overlays/local/`)
- **`infrastructure/terraform/`** — Terraform modules for AWS resource provisioning
- **`scripts/`** — Automation scripts (bootstrap, install, build)

## Technology Stack

| Layer | Technology |
|---|---|
| API Service | Go + Gin |
| Web UI | React 18 + TypeScript + Vite |
| Serverless | AWS Lambda |
| Container Orchestration | OrbStack Kubernetes (local) |
| Container Platform (Cloud) | AWS ECS Fargate |
| CI/CD | Harness pipelines + Delegate |
| Database | MongoDB |
| VCS Integration | GitHub (go-github + PAT) |
| IaC | Terraform (AWS resources) |

## Development Conventions

### Go (control-api)

- Module path: `github.com/Aaron-QL/PersonalDevopsPlatform/control-api`
- Router: `github.com/gin-gonic/gin`
- Layout: `cmd/server/main.go` entry point; `internal/` for all business code; `pkg/` for shared utilities
- Domain pattern: each domain has four files — `entity.go`, `repository.go`, `service.go`, `handler.go`
- Cross-domain deps: domains must NOT import each other directly; use interfaces defined at the consumer; share only primitive types via `internal/types/`
- Config: `github.com/kelseyhightower/envconfig` reads all config from env vars
- Logging: `go.uber.org/zap` structured logging
- Tests: `go test ./...` from `control-api/`
- Lint: `golangci-lint run`

**Key dependencies:**
- `github.com/gin-gonic/gin` — HTTP router
- `go.mongodb.org/mongo-driver/v2` — MongoDB driver
- `github.com/google/go-github/v68` — GitHub API client
- `k8s.io/client-go` — Kubernetes client (InClusterConfig preferred)
- `github.com/aws/aws-sdk-go-v2` — AWS SDK (ECS, ECR, Lambda, S3, CloudWatch)
- `go.uber.org/zap` — structured logging
- `github.com/kelseyhightower/envconfig` — env var config

### Frontend (frontend/)

- Framework: React 18 + TypeScript + Vite
- Styling: Tailwind CSS + shadcn/ui
- Data fetching: TanStack Query (React Query)
- Routing: React Router v6
- Auth: API Key in `localStorage` key `dpk_token`; injected as `Authorization: Bearer <key>`; redirect to `/login` on 401
- Dev: `npm run dev` (port 5173), proxies `/api` to `http://localhost:8080`
- Build: `npm run build` → output to `control-api/web/dist/` (embedded into Go binary via `//go:embed`)
- Package manager: npm

### AWS / Lambda

- Lambda functions under `lambda-projects/`, one directory per function
- Build: `GOOS=linux GOARCH=amd64 go build` → zip → upload to S3
- AWS credentials via env vars: `AWS_ACCESS_KEY_ID`, `AWS_SECRET_ACCESS_KEY`, `AWS_REGION`
- IAM User: `PerpetualSwingBot` (region: `ap-northeast-2`)

### Infrastructure

- K8s manifests: `infrastructure/k8s/base/` (production) + `infrastructure/k8s/overlays/local/` (local tuning)
- Namespaces: `devops-platform` (control-api + MongoDB), `harness-delegate`, `workloads` (project deployments)
- Terraform: `infrastructure/terraform/modules/` contains reusable modules; `bootstrap/` for one-time setup
- K8s access: InClusterConfig in-cluster; fallback to `KUBECONFIG` for local dev

### Auth & Security

- API Key auth only (no JWT/session); prefix stored in plain text, full key hashed (bcrypt) in DB
- Bootstrap: set `BOOTSTRAP_ADMIN_KEY` env var on first startup to auto-create initial platform_admin
- Secrets: AES-256-GCM encryption; DEK stored in K8s Secret, never in MongoDB

## Documentation Maintenance

Whenever architecture design, logic, or functionality changes during development or discussion, update the relevant documentation if necessary:

- **`CLAUDE.md`** — update when conventions, tech stack, directory structure, or project-level rules change
- **`docs/architecture.md`** — update when data models, API design, domain concepts, infrastructure layout, or architectural decisions change

## Git Conventions

- Commit messages must be written in **English**
- Format: `<type>: <short description>` (e.g. `feat: add deployment rollback endpoint`)
