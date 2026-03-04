# PersonalDevopsPlatform

A personal learning platform for studying and practicing modern DevOps technologies. The goal is to build a fully functional internal developer platform that simulates real-world CI/CD workflows.

## What I'm Learning

- **Go** — REST API development with Gin
- **Kubernetes** — container orchestration with OrbStack (local) and AWS ECS Fargate (cloud)
- **Harness** — CI/CD pipelines and delegate management
- **GitHub** — VCS integration, webhooks, and repo automation
- **AWS** — Lambda, S3, ECR, ECS Fargate, IAM, Terraform
- **MongoDB** — data modeling and persistence
- **Terraform** — infrastructure as code for AWS resources

## What This Platform Does

- Manages the full lifecycle of microservice and Lambda projects
- Automatically creates GitHub repos and Harness pipelines when a project is registered
- Triggers CI on git push and CD on pipeline completion
- Supports multiple deployment strategies: rolling, blue-green, canary, recreate
- Manages environments and Kubernetes namespaces via client-go
- Provides secret management with AES-256-GCM encryption
- Includes a React web UI embedded into the Go binary

## Docs

- [Architecture](docs/architecture.md)
- [Development Plan](docs/dev-plan.md)
