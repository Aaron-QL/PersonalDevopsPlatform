# 系统架构设计文档

> 基于 README.md 初步设想扩展而来，供日后开发参考和修改。

---

## 1. 整体拓扑

```
┌──────────────────────────────────────────────────────────────┐
│                     外部服务（云端）                          │
│  GitHub (Repos/Webhooks)    Harness SaaS                     │
└────────┬──────────────────────┬───────────────────────────────┘
         │ webhook push         │ delegate 轮询
         ▼                      ▼
┌─────────────────────────────────────────────────────────────┐
│  本地机器（macOS + OrbStack Kubernetes）                     │
│                                                             │
│  Browser ──── HTTP/REST ────────────────────────┐          │
│                                                 │          │
│  ┌──────────────────────────────────────────────▼──────┐   │
│  │  Kubernetes cluster (OrbStack)                       │   │
│  │  ┌──────────────────────────────┐ ┌───────────────┐  │   │
│  │  │  control-api (Go REST API)   │ │Harness Delegate│  │   │
│  │  │  + embedded Web UI (React)   │ │  (Deployment)  │  │   │
│  │  └──────┬───────────────────────┘ └───────┬───────┘  │   │
│  │         │ client-go(in-cluster)            │部署Project│   │
│  │         ▼                                 ▼           │   │
│  │  ┌──────────────┐   ┌──────────────────────────────┐  │   │
│  │  │   MongoDB    │   │  Project Workloads            │  │   │
│  │  │(devops-platf)│   │  (每个 Environment 一个 ns)   │  │   │
│  │  └──────────────┘   └──────────────────────────────┘  │   │
│  └──────────────────────────────────────────────────────┘   │
└─────────────────────────────────────────────────────────────┘
```

**组件交互矩阵：**

| From | To | 方式 |
|---|---|---|
| Browser | control-api | REST HTTP（`/api/v1/...`），`Authorization: Bearer <api-key>` |
| Browser | Web UI（静态资源） | HTTP GET（由 control-api 通过 `//go:embed` 内嵌并提供） |
| GitHub | control-api | Webhook HTTP POST（入向，需 ngrok / Cloudflare Tunnel） |
| control-api | GitHub API | REST (google/go-github + PAT) |
| control-api | Harness API | REST `x-api-key` 认证（出向） |
| control-api | Kubernetes API Server | client-go in-cluster config |
| Harness SaaS | Harness Delegate | Delegate 主动轮询 Harness SaaS |
| Harness Delegate | Kubernetes | kubectl / k8s API in-cluster |

---

## 2. 核心业务概念

### Deployment（部署）
描述某个 Project 在某个 Environment 中的部署状态。`deployments` 表记录每个 (Project, Environment) 的**当前状态**（upsert）；部署历史即 CD 类型的 `pipeline_runs`（通过 `env_id` 过滤），不单独维护历史表。

支持的部署策略：
- **rolling**：K8s 默认滚动更新
- **recreate**：先停止旧版本再启动新版本
- **blue-green**：新版本与旧版本并行运行，流量切换后下线旧版本
- **canary**：按比例逐步切流到新版本

支持回滚：指定历史 Artifact 重新触发部署即为回滚。

### Artifact（构建产物）
CI pipeline 运行后产出的可部署制品。CD pipeline 部署时必须指定一个 Artifact。

- microservice 类型：Docker 镜像（存储在镜像仓库，如 ghcr.io）
- lambda 类型：ZIP 包（存储在 S3）

Artifact 由 CI pipeline run 自动创建，不支持手动创建。

### Archetype（原型模板）
创建 Project 时使用的初始化模板，与语言绑定。包含三部分内容：
- **repo template**：GitHub 代码模板仓库地址 + 初始化脚本（在 repo 创建后执行）
- **CI pipeline YAML**：向 Harness 创建 CI pipeline 时使用的 input YAML
- **CD pipeline YAML**：向 Harness 创建 CD pipeline 时使用的 input YAML

Archetype 支持版本管理，创建 Project 时可选择使用某个 Archetype 的特定版本（默认使用 latest）。

### Cluster（集群）
一个物理计算集群，是顶层基础设施资源，与业务概念无关。支持两种类型：

- **`k8s`**：Kubernetes 集群（如本地 OrbStack），注册时需提供 kubeconfig（存储于 K8s Secret），control-api 通过 client-go 管理
- **`ecs-fargate`**：AWS ECS Fargate 集群，注册时通过 Terraform 自动 provision VPC + ALB + ECS Cluster，control-api 通过 AWS SDK 管理

一个 Cluster 上可以运行多个 Environment。

### Organization（组织）
微服务的逻辑分组单元，代表一个业务系统。包含若干 Project 和若干 Environment。

创建 Organization 时，平台会以相同名称在 GitHub 和 Harness 上创建同名 Organization；若已存在则直接关联。Organization 的 `slug` 在三端保持一致。

### Environment（环境）
部署目标环境，对应某个 Cluster 上的一个 Kubernetes namespace。属于某个 Organization，同时必须指定所属 Cluster。常见示例：`dev`、`staging`、`prod`。Project 部署时必须指定目标 Environment。

创建 Environment 时的行为取决于所属 Cluster 类型：
- **k8s 类型**：control-api 通过 client-go 在目标集群创建对应 K8s namespace；删除时同步删除 namespace（含所有资源）
- **ecs-fargate 类型**：Environment 是逻辑概念，无需在 AWS 创建对应资源；部署时在 ECS Cluster 内以 Service 名称区分不同 Environment 的工作负载

`is_protected` 标志用于标记受保护环境（如 `prod`）：向受保护 Environment 部署时必须先经过 Promotion 审批流程，禁止直接部署。

### Project（项目）
平台管理的最小部署单元。属于某个 Organization，有两种类型：
- **microservice**：运行在 Kubernetes 上的微服务
- **lambda**：AWS Lambda 函数

创建 Project 时需选择一个 Archetype（及版本），平台会依次执行：
1. 在对应 GitHub Organization 下创建同名 repo（使用 Archetype 的 repo template 和 init script 初始化）
2. 在对应 Harness Organization 下创建同名 Harness project（若已存在则关联）
3. 使用 Archetype 的 CI YAML 在 Harness 创建默认 CI pipeline
4. 使用 Archetype 的 CD YAML 在 Harness 创建默认 CD pipeline

部署时选择目标 Environment，运行在该 Environment 对应的 Cluster 和 namespace 中。

### User & RBAC（认证与权限）
平台使用 API Key 鉴权，无 session/cookie。每个 User 可持有多个 API Key（前缀明文存储，哈希后入库）。

**角色体系（双层）：**
- `is_platform_admin`：User 级别的全局管理员，可管理所有资源
- Organization 级别角色（通过 `org_members` 表分配）：
  - `admin`：该 Org 内所有资源的读写权限，包括管理成员
  - `developer`：可读写 Project/Pipeline/Deployment，不可管理 Environment 和成员
  - `viewer`：只读

**权限矩阵（典型操作）：**

| 操作 | platform_admin | org admin | org developer | org viewer |
|---|---|---|---|---|
| 创建/删除 Organization | ✓ | - | - | - |
| 创建/删除 Cluster | ✓ | - | - | - |
| 管理 Org 成员 | ✓ | ✓ | - | - |
| 创建/删除 Environment | ✓ | ✓ | - | - |
| 创建/部署 Project | ✓ | ✓ | ✓ | - |
| 查看所有资源 | ✓ | ✓（本 Org）| ✓（本 Org）| ✓（本 Org）|
| 查看/操作 Secret 明文 | ✓ | ✓（本 Org）| - | - |

**初始管理员 Bootstrap：**
`POST /api/v1/users` 本身需要鉴权，存在冷启动问题。解决方式：control-api 启动时检查 `users` 表，若为空且环境变量 `BOOTSTRAP_ADMIN_KEY` 不为空，则自动创建一个 `is_platform_admin=true` 的初始用户，并以该值注册为其第一个 API Key。Bootstrap 完成后该变量可从环境变量中移除。

### Secret（密钥管理）
`project_env_vars` 中 `is_secret=true` 的条目的 `value` 在写入 MongoDB 前使用 **AES-256-GCM** 加密。加密密钥（DEK）存储在 K8s Secret（`devops-platform` namespace）中，control-api 启动时读取，不写入 MongoDB。

- 默认查询 env-vars 时，secret 的 value 返回 `"***"` 掩码
- 需要携带 `?reveal=true` 参数（仅 org admin 及以上可调用）才返回明文，且该操作自动写入 Audit Log

### Promotion（晋级审批）
向 `is_protected=true` 的 Environment 部署时，需要先创建 Promotion 请求，由具备权限的人审批通过后，CD pipeline 才会被触发。

- Promotion 记录关联：source env、target env、artifact、申请人、审批人
- 状态流转：`pending` → `approved` / `rejected`
- `approved` 后自动触发 CD pipeline

### Notification（通知）
平台关键事件（Pipeline 成功/失败、部署完成、Promotion 等待审批等）可通过 Webhook 渠道对外通知。

- `notification_channels`：配置 Webhook URL（可设 HMAC secret 签名请求）
- 支持按 Org 或全局配置，可过滤事件类型
- 通知记录保留 30 天

### Audit Log（审计日志）
所有写操作（POST/PUT/DELETE）和 Secret 明文查看操作自动通过 Gin 中间件写入审计日志，记录：操作者、HTTP 方法、请求路径、关键字段变更摘要、时间戳、来源 IP。保留 90 天。

### 资源关系

```
Cluster   1 ──── * Environment

Organization 1 ──── * Environment
Organization 1 ──── * Project
Organization 1 ──── * OrgMember（User ←→ Organization 的多对多，含 role）

User     1 ──── * ApiKey（多个 key，哈希存储）

Environment ──── 1 Cluster
Environment ──── 1 Organization
Environment  is_protected: bool（true = 需要 Promotion 审批才能部署）
Environment  创建/删除 → 触发 client-go 在目标 Cluster 创建/删除对应 K8s namespace

Project  ──── 1 Organization
Project  ──── 1 Archetype Version（创建时指定，记录所用模板及版本）
Project  ──── 1 GitHub Repo（= org.github_org_name/project.slug，自动创建或关联）
Project  ──── 1 Harness Project（= project.slug，在 org.harness_org_id 下，自动创建或关联）
Project  1 ──── * Pipeline（ci、cd 等，每条对应一个 Harness pipeline）
Project  type: "microservice" | "lambda"
Project  部署到 ──── Environment（cluster = environment.cluster_id, namespace = environment.k8s_namespace）

# 以下两个资源均以 (Project, Environment) 为作用域
ProjectEnvVar   ──── 1 Project + 1 Environment（该 Project 在该 Environment 下的环境变量，secret 加密存储）
ConfigFile      ──── 1 Project + 1 Environment（挂载的配置文件，含多个版本）
ConfigFile  1 ──── * ConfigFileVersion（版本历史，有且仅有一个 active 版本）

CI PipelineRun  1 ──── 1 Artifact（CI 运行后产出）
Deployment      ──── 1 Project + 1 Environment（当前部署状态，唯一）
Deployment      ──── 1 Artifact（当前部署的产物版本）
CD PipelineRun  = 一次部署操作（env_id + is_rollback 字段区分用途）

Promotion  ──── 1 Project + source Environment + target Environment（受保护环境的审批请求）
Promotion  status: "pending" | "approved" | "rejected"（approved 后自动触发 CD）

NotificationChannel  ──── 1 Organization（或全局），type: "webhook"
AuditLog  记录所有写操作 + secret 明文查看，TTL 90 天
```

### 删除级联行为

| 删除对象 | 级联效果 |
|---|---|
| **Organization** | 禁止删除（若旗下仍有 Project 或 Environment）；全部移除后：删除 org_members、notification_channels |
| **Environment** | 同步删除对应 K8s namespace（含其中所有工作负载）；删除关联的 deployments、project_env_vars、config_files |
| **Project** | 删除关联的 pipelines、pipeline_runs、artifacts、deployments；**不**自动删除 GitHub repo 和 Harness project（需手动操作，防止误删） |
| **Cluster** | 禁止删除（若仍有 Environment 指向该 Cluster） |
| **Archetype** | 禁止删除（若仍有 Project 引用该 Archetype） |
| **Artifact** | 直接删除记录；不操作远端镜像仓库或 S3（制品清理由外部策略负责） |
| **Promotion** | 仅 `pending` 状态可撤销（由申请人调用），已 approved/rejected 的不可删除，仅作历史记录 |

---

## 3. 项目根目录结构

```
PersonalDevopsPlatform/
├── control-api/             # Go REST API + embedded Web UI
├── frontend/                # React Web UI 源码
├── microservice-projects/   # 示例微服务项目（每个子目录对应一个 GitHub repo）
├── lambda-projects/         # AWS Lambda 函数（每个子目录一个函数）
├── infrastructure/
│   ├── k8s/                 # Kubernetes manifests
│   ├── terraform/           # AWS 资源 IaC
│   ├── harness/pipelines/   # Harness pipeline YAML 模板
│   └── docker-compose.yml   # 本地开发 MongoDB
├── scripts/                 # 自动化脚本（bootstrap、install、build）
├── docs/                    # 架构文档、开发计划
├── .gitignore
├── CLAUDE.md
└── README.md
```

---

## 4. control-api 内部结构

### Go 目录布局

```
control-api/
├── cmd/server/main.go               # 入口：依赖注入，启动 HTTP server
├── internal/
│   ├── config/config.go             # envconfig 读取环境变量
│   ├── server/{server,router}.go    # gin router，路由注册
│   ├── middleware/{logging,auth,audit}.go # 请求日志，Bearer token 鉴权，审计日志中间件
│   ├── domain/
│   │   ├── archetype/{entity,repository,service,handler}.go
│   │   ├── artifact/{entity,repository,service,handler}.go
│   │   ├── deployment/{entity,repository,service,handler}.go
│   │   ├── cluster/{entity,repository,service,handler}.go
│   │   ├── organization/{entity,repository,service,handler}.go
│   │   ├── environment/{entity,repository,service,handler}.go
│   │   ├── project/{entity,repository,service,handler}.go
│   │   ├── pipeline/{entity,repository,service,handler}.go
│   │   ├── envvar/{entity,repository,service,handler}.go
│   │   ├── configfile/{entity,repository,service,handler}.go
│   │   ├── user/{entity,repository,service,handler}.go        # User & API Key 管理
│   │   ├── promotion/{entity,repository,service,handler}.go   # 晋级审批
│   │   ├── notification/{entity,repository,service,handler}.go # 通知渠道 & 记录
│   │   ├── audit/{entity,repository,service,handler}.go       # 审计日志查询
│   │   ├── github/{entity,repository,service,handler}.go
│   │   ├── harness/{entity,repository,service,handler}.go
│   │   └── k8s/{entity,repository,service,handler}.go
│   ├── store/mongo/
│   │   ├── client.go                # MongoDB 连接
│   │   ├── {archetype,artifact,deployment,cluster,organization,environment,project,pipeline,envvar,configfile,user,promotion,notification,audit,github,harness,k8s}_repo.go
│   │   └── migrations/indexes.go   # 启动时建索引
│   ├── types/
│   │   ├── ids.go                   # 跨 domain 共享 ID 类型（OrgID、ProjectID、EnvID、ArtifactID 等）
│   │   └── events.go                # 内部事件类型（CIPipelineCompleted 等，用于异步解耦）
│   └── clients/
│       ├── github/github_client.go  # go-github 封装
│       ├── harness/harness_client.go # net/http 封装（无官方 Go SDK）
│       ├── k8s/k8s_client.go       # client-go 封装，优先 InClusterConfig
│       └── aws/
│           ├── ecs_client.go        # ECS Service 创建/更新/状态查询
│           ├── ecr_client.go        # ECR repo 管理（推送权限 token）
│           └── terraform_client.go  # 封装 terraform CLI 调用（exec + output 解析）
├── pkg/httputil/{response,errors}.go # 统一 JSON 响应格式
├── go.mod                           # module: github.com/Aaron-QL/PersonalDevopsPlatform/control-api
├── Makefile                         # build / test / docker-build / docker-push
└── Dockerfile                       # 多阶段构建，distroless 基础镜像
```

**跨 domain 依赖规则：**
- `internal/types/` — 只放**原始共享类型**：ID 类型别名（`type ArtifactID string`）和内部事件结构体。所有 domain 均可 import，但 `types` 本身不 import 任何 domain
- **Interface 定义在消费方**：`deployment` 需要查询 artifact，就在 `deployment/service.go` 里定义 `ArtifactReader` 接口，不建立共享 interface 目录。`main.go` 负责将具体实现注入给消费方
- **不允许 domain 之间直接 import 对方的包**，只通过接口通信

**核心依赖：**
- `github.com/gin-gonic/gin` — 路由
- `go.mongodb.org/mongo-driver/v2` — MongoDB 驱动
- `github.com/google/go-github/v68` — GitHub API
- `k8s.io/client-go` + `k8s.io/api` + `k8s.io/apimachinery` — k8s 客户端
- `github.com/kelseyhightower/envconfig` — 配置
- `go.uber.org/zap` — 结构化日志

### 环境变量清单（config.go）

| 变量名 | 类型 | 说明 |
|---|---|---|
| `MONGO_URI` | string | MongoDB 连接串，如 `mongodb://mongo:27017` |
| `MONGO_DB` | string | 数据库名，默认 `devops_platform` |
| `SERVER_PORT` | int | HTTP 监听端口，默认 `8080` |
| `K8S_IN_CLUSTER` | bool | `true` 使用 InClusterConfig，`false` 使用 KUBECONFIG，默认 `true` |
| `GITHUB_PAT` | string | GitHub Personal Access Token（操作 org/repo/webhook 用） |
| `HARNESS_API_KEY` | string | Harness API Key（`x-api-key` 鉴权） |
| `HARNESS_ACCOUNT_ID` | string | Harness 账号 ID（实际值：`A_w0u4V5QHqX_GAFVmCJAQ`） |
| `HARNESS_WEBHOOK_TOKEN` | string | Harness 回调端点的预共享 token（验证回调合法性） |
| `ENCRYPTION_DEK_SECRET` | string | K8s Secret 名称，存储 AES-256-GCM 的数据加密密钥，默认 `control-api-dek` |
| `BOOTSTRAP_ADMIN_KEY` | string | 仅首次启动时生效：若 users 表为空，自动创建初始 platform_admin 并以此值作为其 API Key |
| `AWS_ACCESS_KEY_ID` | string | AWS IAM User `PerpetualSwingBot` 的 Access Key |
| `AWS_SECRET_ACCESS_KEY` | string | AWS IAM User `PerpetualSwingBot` 的 Secret Key |
| `AWS_REGION` | string | AWS 默认区域（实际值：`ap-northeast-2` 首尔） |
| `TERRAFORM_WORKING_DIR` | string | Terraform 配置文件目录，默认 `../infrastructure/terraform` |
| `LOG_LEVEL` | string | 日志级别，默认 `info` |

### REST API 资源设计

```
# 探针（免鉴权）
GET  /healthz
GET  /readyz

# Archetype（顶层资源，与语言绑定的项目模板）
GET    /api/v1/archetypes                                             # 列表（可按 language 过滤）
POST   /api/v1/archetypes                                             # 创建
GET    /api/v1/archetypes/:archetypeId                                # 详情
PUT    /api/v1/archetypes/:archetypeId                                # 更新元信息
DELETE /api/v1/archetypes/:archetypeId                                # 删除
GET    /api/v1/archetypes/:archetypeId/versions                       # 版本列表
POST   /api/v1/archetypes/:archetypeId/versions                       # 发布新版本
GET    /api/v1/archetypes/:archetypeId/versions/:version              # 查看某版本详情

# Cluster
GET    /api/v1/clusters                                               # 列表
POST   /api/v1/clusters                                               # 创建
GET    /api/v1/clusters/:clusterId                                    # 详情
PUT    /api/v1/clusters/:clusterId                                    # 更新
DELETE /api/v1/clusters/:clusterId                                    # 删除

# Organization
GET    /api/v1/organizations                                          # 列表
POST   /api/v1/organizations                                          # 创建
GET    /api/v1/organizations/:orgId                                   # 详情
PUT    /api/v1/organizations/:orgId                                   # 更新
DELETE /api/v1/organizations/:orgId                                   # 删除

# Environment（嵌套在 Organization 下）
GET    /api/v1/organizations/:orgId/environments                      # 列表
POST   /api/v1/organizations/:orgId/environments                      # 创建
GET    /api/v1/organizations/:orgId/environments/:envId               # 详情
PUT    /api/v1/organizations/:orgId/environments/:envId               # 更新
DELETE /api/v1/organizations/:orgId/environments/:envId               # 删除

# Project（嵌套在 Organization 下）
GET    /api/v1/organizations/:orgId/projects                          # 列表
POST   /api/v1/organizations/:orgId/projects                          # 创建
GET    /api/v1/organizations/:orgId/projects/:projectId               # 详情
PUT    /api/v1/organizations/:orgId/projects/:projectId               # 更新
DELETE /api/v1/organizations/:orgId/projects/:projectId               # 删除

# Pipeline（嵌套在 Project 下）
GET    /api/v1/organizations/:orgId/projects/:projectId/pipelines                    # 列表
POST   /api/v1/organizations/:orgId/projects/:projectId/pipelines                    # 创建
GET    /api/v1/organizations/:orgId/projects/:projectId/pipelines/:pipelineId        # 详情
PUT    /api/v1/organizations/:orgId/projects/:projectId/pipelines/:pipelineId        # 更新
DELETE /api/v1/organizations/:orgId/projects/:projectId/pipelines/:pipelineId        # 删除
POST   /api/v1/organizations/:orgId/projects/:projectId/pipelines/:pipelineId/trigger # 手动触发

# 当前用户（免传 userId，根据 API Key 自动识别）
GET    /api/v1/me                                                       # 当前用户信息及所属 Org 列表

# GitHub
GET    /api/v1/github/repos                                           # 列表已注册仓库
POST   /api/v1/github/repos                                           # 注册仓库（创建 Webhook）
DELETE /api/v1/github/repos/:owner/:repo                              # 注销（删除 Webhook）
GET    /api/v1/github/repos/:owner/:repo/prs                          # PR 列表（实时查 GitHub API）
POST   /api/v1/github/webhooks/receive                                # Webhook 接收端点
GET    /api/v1/github/events                                          # 已存储事件列表

# 环境变量（以 Project + Environment 为作用域）
GET    /api/v1/organizations/:orgId/projects/:projectId/environments/:envId/env-vars            # 查看全部
PUT    /api/v1/organizations/:orgId/projects/:projectId/environments/:envId/env-vars            # 整体覆盖更新
POST   /api/v1/organizations/:orgId/projects/:projectId/environments/:envId/env-vars            # 新增单个
DELETE /api/v1/organizations/:orgId/projects/:projectId/environments/:envId/env-vars/:key       # 删除单个

# 配置文件（以 Project + Environment 为作用域，含版本控制）
GET    /api/v1/organizations/:orgId/projects/:projectId/environments/:envId/config-files                           # 列表
POST   /api/v1/organizations/:orgId/projects/:projectId/environments/:envId/config-files                           # 创建
GET    /api/v1/organizations/:orgId/projects/:projectId/environments/:envId/config-files/:fileId                   # 详情（当前 active 版本）
PUT    /api/v1/organizations/:orgId/projects/:projectId/environments/:envId/config-files/:fileId                   # 更新元信息（名称/挂载路径）
DELETE /api/v1/organizations/:orgId/projects/:projectId/environments/:envId/config-files/:fileId                   # 删除（含所有版本）
GET    /api/v1/organizations/:orgId/projects/:projectId/environments/:envId/config-files/:fileId/versions          # 版本列表
POST   /api/v1/organizations/:orgId/projects/:projectId/environments/:envId/config-files/:fileId/versions          # 创建新版本
GET    /api/v1/organizations/:orgId/projects/:projectId/environments/:envId/config-files/:fileId/versions/:ver     # 查看某版本内容
POST   /api/v1/organizations/:orgId/projects/:projectId/environments/:envId/config-files/:fileId/versions/:ver/activate  # 激活某版本

# Artifact（CI 产物，只读——由 CI pipeline run 自动写入）
GET    /api/v1/organizations/:orgId/projects/:projectId/artifacts                    # 列表（按创建时间倒序）
GET    /api/v1/organizations/:orgId/projects/:projectId/artifacts/:artifactId        # 详情
DELETE /api/v1/organizations/:orgId/projects/:projectId/artifacts/:artifactId        # 删除旧产物

# Deployment（部署管理）
GET    /api/v1/organizations/:orgId/projects/:projectId/deployments                              # 各环境当前部署概览（每个 env 一条）
GET    /api/v1/organizations/:orgId/projects/:projectId/environments/:envId/deployment           # 某环境当前部署状态（含 K8s 实时状态）
POST   /api/v1/organizations/:orgId/projects/:projectId/environments/:envId/deployments          # 触发新部署（body: artifact_id, strategy）
GET    /api/v1/organizations/:orgId/projects/:projectId/environments/:envId/deployments          # 部署历史列表
POST   /api/v1/organizations/:orgId/projects/:projectId/environments/:envId/deployments/rollback # 回滚（body: artifact_id 或留空回滚到上一版本）

# Logs（服务日志）
GET    /api/v1/organizations/:orgId/projects/:projectId/environments/:envId/logs                 # 实时日志（K8s pod logs）

# Pipeline Runs（按 project 查询——最常用）
GET    /api/v1/organizations/:orgId/projects/:projectId/pipeline-runs              # 该 project 的所有 run（可按 type=ci|cd、status 过滤）
GET    /api/v1/organizations/:orgId/projects/:projectId/pipeline-runs/:runId       # 单条 run 详情

# Pipeline Runs（跨 project 全局查询）
GET    /api/v1/pipeline-runs                                          # 全部运行记录（可按 pipeline_id 过滤）
GET    /api/v1/pipeline-runs/:runId                                   # 单条运行记录

# Harness 回调（Harness pipeline 完成后主动回调，用 HARNESS_WEBHOOK_TOKEN 验证）
POST   /api/v1/harness/webhooks/receive                               # 接收 pipeline 完成事件，创建 Artifact / 更新 deployment 状态

# User & API Key（平台用户管理，需 platform_admin 权限）
GET    /api/v1/users                                                   # 用户列表
POST   /api/v1/users                                                   # 创建用户
GET    /api/v1/users/:userId                                           # 用户详情
PUT    /api/v1/users/:userId                                           # 更新用户信息
DELETE /api/v1/users/:userId                                           # 删除用户
POST   /api/v1/users/:userId/api-keys                                  # 为用户生成新 API Key（明文仅返回一次）
GET    /api/v1/users/:userId/api-keys                                  # 列出用户所有 API Key（仅返回 key_prefix，不返回哈希）
DELETE /api/v1/users/:userId/api-keys/:keyId                           # 撤销 API Key

# Org 成员管理（需 org admin 权限）
GET    /api/v1/organizations/:orgId/members                            # 成员列表
POST   /api/v1/organizations/:orgId/members                            # 添加成员（body: user_id, role）
PUT    /api/v1/organizations/:orgId/members/:userId                    # 修改成员角色
DELETE /api/v1/organizations/:orgId/members/:userId                    # 移除成员

# Secret 查看（env-vars 已有端点，此处仅补充明文解密端点）
GET    /api/v1/organizations/:orgId/projects/:projectId/environments/:envId/env-vars?reveal=true   # 带明文值（org admin+）

# Promotion（晋级审批）
GET    /api/v1/organizations/:orgId/promotions                         # Org 下所有 Promotion 列表
POST   /api/v1/organizations/:orgId/promotions                         # 创建 Promotion 请求（body: project_id, source_env_id, target_env_id, artifact_id）
GET    /api/v1/organizations/:orgId/promotions/:promotionId            # 详情
POST   /api/v1/organizations/:orgId/promotions/:promotionId/approve    # 审批通过（自动触发 CD）
POST   /api/v1/organizations/:orgId/promotions/:promotionId/reject     # 拒绝（body: reason）

# Notification（通知渠道）
GET    /api/v1/organizations/:orgId/notification-channels              # 渠道列表
POST   /api/v1/organizations/:orgId/notification-channels              # 创建渠道（body: name, type=webhook, url, secret, event_types）
PUT    /api/v1/organizations/:orgId/notification-channels/:channelId   # 更新渠道
DELETE /api/v1/organizations/:orgId/notification-channels/:channelId   # 删除渠道
POST   /api/v1/organizations/:orgId/notification-channels/:channelId/test  # 发送测试通知

# Audit Log（审计日志查询，需 org admin+ 权限）
GET    /api/v1/organizations/:orgId/audit-logs                         # 本 Org 审计日志（支持时间范围、user 过滤）
GET    /api/v1/audit-logs                                              # 全局审计日志（需 platform_admin）

# Kubernetes
GET    /api/v1/k8s/namespaces                                         # 命名空间列表
GET    /api/v1/k8s/deployments                                        # 全部 Deployment（可按 ns 过滤）
GET    /api/v1/k8s/deployments/:ns/:name                              # 单个 Deployment 实时状态
DELETE /api/v1/k8s/deployments/:ns/:name                              # 删除 Deployment
GET    /api/v1/k8s/pods/:ns/:name/logs                                # Pod 日志
```

---

## 4. MongoDB 数据模型

数据库名：`devops_platform`

| Collection | 用途 | TTL |
|---|---|---|
| `users` | 平台用户记录 | 永久 |
| `api_keys` | 用户 API Key（哈希存储） | 永久 |
| `org_members` | User ↔ Organization 成员关系及角色 | 永久 |
| `archetypes` | Archetype 元信息 | 永久 |
| `archetype_versions` | Archetype 版本内容 | 永久 |
| `clusters` | Cluster 记录（含 kubeconfig 引用） | 永久 |
| `organizations` | Organization 记录 | 永久 |
| `environments` | Environment 记录（含 is_protected） | 永久 |
| `projects` | Project 记录 | 永久 |
| `github_repos` | 已注册仓库（含 webhook_id） | 永久 |
| `webhook_events` | 原始 Webhook payload（BSON） | 7天 |
| `pipelines` | Pipeline 记录（每个 Project 的 CI/CD 等流水线） | 永久 |
| `pipeline_runs` | 流水线运行记录 | 永久 |
| `artifacts` | CI 构建产物记录 | 永久 |
| `deployments` | 各 (Project, Environment) 的当前部署状态（反范式缓存，唯一） | 永久 |
| `project_env_vars` | Project 在某 Environment 下的环境变量（secret 字段 AES-256-GCM 加密） | 永久 |
| `config_files` | 配置文件元信息（名称、挂载路径等） | 永久 |
| `config_file_versions` | 配置文件版本内容 | 永久 |
| `promotions` | 晋级审批请求（向受保护 Environment 部署的审批流） | 永久 |
| `notification_channels` | 通知渠道配置（Webhook URL 等） | 永久 |
| `notification_records` | 已发送通知记录 | 30天 |
| `audit_logs` | 所有写操作及 secret 明文查看的审计记录 | 90天 |
| `k8s_deployments` | K8s 部署操作记录 | 永久 |
| `app_config` | 流水线触发规则等配置 K-V | 永久 |

**核心 Schema：**

```
users:
  _id, name, email(唯一), is_platform_admin,
  created_at, updated_at
  索引: { email: 1 } unique

api_keys:
  _id, user_id(ref), name,                  # name 用于区分多个 key（如 "CLI Key"、"CI Key"）
  key_prefix,                               # 明文前缀（如 "dpk_abc12"），用于列表展示识别
  key_hash,                                 # SHA-256 哈希，鉴权时比对
  last_used_at, created_at
  索引: { user_id: 1 }, { key_hash: 1 } unique

org_members:
  _id, org_id(ref), user_id(ref),
  role,                                     # "admin" | "developer" | "viewer"
  created_at, updated_at
  索引: { org_id: 1, user_id: 1 } unique

archetypes:
  _id, name, slug(唯一), language,      # language 与 Project.language 对应
  description, latest_version,          # latest_version 指向最新发布版本号
  created_at, updated_at
  索引: { slug: 1 } unique, { language: 1 }

archetype_versions:
  _id, archetype_id(ref), version,       # semver，如 "1.0.0"
  repo_template_url,                     # GitHub template repo URL，用于初始化代码仓库
  init_script,                           # 在 repo 创建后执行的初始化脚本（shell）
  ci_pipeline_yaml,                      # 向 Harness 创建 CI pipeline 时的 input YAML
  cd_pipeline_yaml,                      # 向 Harness 创建 CD pipeline 时的 input YAML
  changelog,                             # 本版本变更说明
  created_at
  索引: { archetype_id: 1, version: -1 }

clusters:
  _id, name, description,
  type,                                     # "k8s" | "ecs-fargate"
  # k8s 类型专用：
  kubeconfig_secret_name,                   # K8s Secret 名称（存储 kubeconfig，在 devops-platform ns 下）
  # ecs-fargate 类型专用（由 Terraform output 写入）：
  aws_region,                               # 如 "ap-northeast-1"
  ecs_cluster_arn,                          # ECS Cluster ARN
  alb_dns_name,                             # ALB 对外 DNS 地址（服务入口）
  vpc_id,                                   # VPC ID
  private_subnet_ids,                       # ECS Task 运行的私有子网 ID 列表
  created_at, updated_at
  索引: { name: 1 } unique

organizations:
  _id, name, slug(唯一), description,
  github_org_name,    # GitHub org name（= slug，创建/关联后写入）
  harness_org_id,     # Harness org identifier（创建/关联后写入）
  created_at, updated_at
  索引: { slug: 1 } unique

environments:
  _id, org_id(ref), cluster_id(ref), name, slug, k8s_namespace, description,
  is_protected,                             # true = 部署需先经 Promotion 审批
  created_at, updated_at
  索引: { org_id: 1 }, { cluster_id: 1, k8s_namespace: 1 } unique  # 同一 cluster 内 namespace 唯一

projects:
  _id, org_id(ref), name, slug(唯一), description,
  type,                      # "microservice" | "lambda"
  language,                  # "go" | "python" | "java" | "nodejs" | 其他
  archetype_id(ref),         # 创建时选用的 Archetype
  archetype_version,         # 创建时选用的 Archetype 版本（semver）
  github_repo,               # "org-slug/project-slug"（创建/关联后写入）
  harness_project_id,        # Harness project identifier（创建/关联后写入）
  k8s_deployment_name,       # K8s deployment name（仅 microservice + k8s cluster 有效）
  ecr_repo_url,              # ECR 镜像仓库地址（Terraform provision 后写入，ecs-fargate 专用）
  aws_s3_bucket,             # S3 artifact bucket 名称（Terraform provision 后写入）
  created_at, updated_at
  索引: { org_id: 1 }, { slug: 1 } unique

pipelines:
  _id, project_id(ref), name, type,              # type: "ci" | "cd" | "ci-cd" 等
  harness_pipeline_id,                           # 对应 Harness 中的 pipeline identifier
  description, created_at, updated_at
  索引: { project_id: 1 }

pipeline_runs:
  _id, pipeline_id(ref), harness_run_id, status,
  triggered_by, trigger_context,                 # 触发来源（webhook/manual）和上下文（commit sha 等）
  artifact_id,                                   # CI run：产出的 Artifact；CD run：使用的 Artifact
  env_id,                                        # CD run 专用：部署目标 Environment
  is_rollback,                                   # CD run 专用：是否为回滚操作
  started_at, finished_at, updated_at
  索引: { pipeline_id: 1, started_at: -1 }, { env_id: 1, started_at: -1 }

artifacts:
  _id, project_id(ref), ci_run_id(ref),          # 由哪次 CI pipeline run 产出
  type,                                          # "docker-image" | "zip"
  name,                                          # 产物名称，如 "ghcr.io/org/project"
  version,                                       # 版本标识，通常为 git commit SHA 或 semver tag
  location,                                      # 完整引用：镜像 tag 或 S3 URI
  status,                                        # "building" | "ready" | "failed"
  created_at
  索引: { project_id: 1, created_at: -1 }

project_env_vars:
  _id, project_id(ref), env_id(ref),
  vars: [{ key, value, is_secret }],   # is_secret=true 时 value 字段 AES-256-GCM 加密；部署为 K8s Secret，否则 ConfigMap
  created_at, updated_at
  索引: { project_id: 1, env_id: 1 } unique

config_files:
  _id, project_id(ref), env_id(ref),
  name,                  # 文件名，如 "application.yaml"
  mount_path,            # 容器内挂载路径，如 "/app/config/application.yaml"
  active_version,        # 当前生效的版本号
  description, created_at, updated_at
  索引: { project_id: 1, env_id: 1 }

config_file_versions:
  _id, config_file_id(ref), version,   # 递增整数，从 1 开始
  content,               # 文件内容（文本）
  comment,               # 变更说明
  created_at
  索引: { config_file_id: 1, version: -1 }

deployments:                             # 当前状态表，每个 (project, env) 唯一，upsert
  _id, project_id(ref), env_id(ref),
  artifact_id(ref),                      # 当前部署的 artifact
  strategy,                              # "rolling" | "recreate" | "blue-green" | "canary"
  status,                                # "deploying" | "running" | "degraded" | "failed" | "stopped"
  replicas_desired, replicas_ready,      # 来自 K8s（实时查询时覆盖）
  deployed_at, deployed_by
  索引: { project_id: 1, env_id: 1 } unique

promotions:
  _id, org_id(ref), project_id(ref),
  source_env_id(ref),                        # 来源 Environment（通常为 staging）
  target_env_id(ref),                        # 目标 Environment（is_protected=true）
  artifact_id(ref),                          # 要部署的产物版本
  strategy,                                  # 部署策略
  status,                                    # "pending" | "approved" | "rejected"
  requested_by,                              # 申请人 user_id
  reviewed_by,                               # 审批人 user_id（approved/rejected 后写入）
  reject_reason,                             # 拒绝原因（可选）
  triggered_run_id,                          # approved 后触发的 pipeline_run_id
  created_at, reviewed_at
  索引: { org_id: 1, status: 1 }, { target_env_id: 1 }

notification_channels:
  _id, org_id(ref),                          # null 表示全局渠道（仅 platform_admin 可创建）
  name,
  type,                                      # 目前仅 "webhook"
  url,                                       # Webhook URL
  hmac_secret,                               # 可选，请求签名密钥（加密存储）
  event_types: [],                           # 订阅的事件类型，空数组 = 全部事件
  is_enabled,
  created_at, updated_at
  索引: { org_id: 1 }

notification_records:
  _id, channel_id(ref), event_type,
  payload,                                   # 发送的 payload（BSON）
  status,                                    # "sent" | "failed"
  http_status,                               # 目标端返回的 HTTP 状态码
  error,                                     # 失败时的错误信息
  created_at
  TTL 索引: { created_at: 1 } expire 30天
  索引: { channel_id: 1, created_at: -1 }

audit_logs:
  _id, actor_user_id(ref), actor_email,       # 快照 email，防用户删除后丢失
  method,                                     # HTTP 方法
  path,                                       # 请求路径
  resource_type,                              # 如 "environment"、"project"、"secret"
  resource_id,                                # 操作的资源 ID
  org_id,                                     # 关联 Organization（可为 null）
  summary,                                    # 关键变更摘要（如 {"action":"deploy","artifact_id":"..."}）
  source_ip,
  created_at
  TTL 索引: { created_at: 1 } expire 90天
  索引: { org_id: 1, created_at: -1 }, { actor_user_id: 1, created_at: -1 }

github_repos:
  _id, project_id(ref),
  owner,                                     # GitHub org/user name
  repo,                                      # repo name
  webhook_id,                                # GitHub Webhook ID（用于注销时删除）
  webhook_secret,                            # Webhook HMAC 验签密钥（加密存储）
  created_at, updated_at
  索引: { owner: 1, repo: 1 } unique, { project_id: 1 }

webhook_events:
  _id, repo_owner, repo_name,
  event_type,                                # "push" | "pull_request" | "release" 等
  delivery_id,                               # GitHub X-GitHub-Delivery header（去重用）
  payload,                                   # 原始 BSON payload
  processed,                                 # 是否已被处理
  created_at
  TTL 索引: { created_at: 1 } expire 7天
  索引: { repo_owner: 1, repo_name: 1, created_at: -1 }, { delivery_id: 1 } unique

k8s_deployments:
  _id, project_id(ref), env_id(ref),
  action,                                    # "deploy" | "rollback" | "scale" | "delete"
  artifact_id(ref),
  strategy,
  initiated_by,                              # user_id
  k8s_response,                              # K8s API 返回的原始结果（BSON）
  created_at
  索引: { project_id: 1, env_id: 1, created_at: -1 }

app_config:
  _id, key(唯一), value, description, updated_at
  # 示例 key："default_deploy_strategy"、"ci_trigger_branches"
  索引: { key: 1 } unique

```

> 部署历史直接查询 `pipeline_runs`（`pipeline.type='cd'` + `env_id`），不单独维护 deployment_history。

**实时查询 vs 持久化原则：**
- **持久化**：Organization/Environment/Project 元数据、Webhook 原始事件、Pipeline 运行记录、K8s 部署操作记录
- **实时查询（不持久化）**：GitHub PR 列表、K8s 当前 Pod/Deployment 实时状态（K8s API 为权威）、Harness 流水线当前进度

---

## 5. 基础设施布局（OrbStack Kubernetes）

### 命名空间规划

```
devops-platform          # 控制面：control-api、MongoDB 等平台服务（与业务 namespace 隔离）
harness-delegate         # Harness Delegate
<environment.k8s_namespace>  # 业务面：每个 Environment 对应所属 Cluster 上的一个 namespace
```

示例：Cluster `local` 上，Org `my-app` 的两个 Environment：
```
my-app-dev     # dev 环境的 namespace
my-app-prod    # prod 环境的 namespace
```

### infrastructure/ 目录结构

```
infrastructure/
├── k8s/
│   ├── base/
│   │   ├── namespaces.yaml                          # devops-platform, harness-delegate
│   │   ├── mongodb/{statefulset,service,pvc,secret}.yaml   # namespace: devops-platform
│   │   ├── control-api/{deployment,service,configmap,secret}.yaml  # namespace: devops-platform
│   │   └── harness-delegate/values.yaml
│   └── overlays/local/kustomization.yaml   # 本地资源限制调优
├── terraform/
│   ├── backend.tf                           # S3 remote state 配置
│   ├── modules/
│   │   ├── project-resources/               # 每个 Project 的 AWS 资源：ECR + S3 + IAM Role
│   │   │   ├── main.tf
│   │   │   ├── variables.tf
│   │   │   └── outputs.tf
│   │   └── fargate-cluster/                 # ECS Fargate 集群基础设施：VPC + ALB + ECS Cluster
│   │       ├── main.tf
│   │       ├── variables.tf
│   │       └── outputs.tf
│   └── bootstrap/                           # 一次性创建 Terraform state 存储（S3 + DynamoDB）
│       └── main.tf
├── harness/pipelines/build-and-deploy.yaml  # 流水线 YAML（as-code）
└── docker-compose.yml                       # 本地开发：MongoDB
```

### control-api 对外暴露

本地 OrbStack 集群使用 **NodePort** Service 暴露 control-api，端口固定为 `30080`，通过 `localhost:30080` 访问（OrbStack 自动将 NodePort 映射到宿主机）：

```yaml
# infrastructure/k8s/base/control-api/service.yaml
apiVersion: v1
kind: Service
spec:
  type: NodePort
  ports:
    - port: 8080
      nodePort: 30080
```

GitHub Webhook 需要公网地址，使用隧道转发到 `localhost:30080`（见 Section 6）。

### control-api 访问 K8s 的方式

```go
// 优先 InClusterConfig（生产路径：读 ServiceAccount token，自动注入）
cfg, err := rest.InClusterConfig()

// 本地开发 fallback：
// export KUBECONFIG=~/.kube/config   (OrbStack 自动写入)
// export K8S_IN_CLUSTER=false
```

control-api 使用专用 ServiceAccount，绑定有限 ClusterRole（**不使用 cluster-admin**）：
- 权限：deployments/services/pods/namespaces/replicasets 的 get/list/watch/create/update/delete

### Harness Delegate 安装（Helm）

```bash
helm install harness-delegate harness-delegate/harness-delegate \
  --namespace harness-delegate \
  --set delegateName=local-k8s-delegate \
  --set accountId=<ACCOUNT_ID> \
  --set delegateToken=<DELEGATE_TOKEN>
```

> Delegate 仅需**出向**连接 `app.harness.io`，无需开放入向端口。

### 本地开发工作流（不部署到 K8s）

日常开发时无需每次重新打包镜像部署到集群，使用以下流程：

```bash
# 1. 用 docker-compose 启动 MongoDB（仅需数据库）
docker-compose up -d mongo

# 2. 设置环境变量，绕过 K8s 依赖
export K8S_IN_CLUSTER=false          # 使用 ~/.kube/config（OrbStack 自动写入）
export MONGO_URI=mongodb://localhost:27017
export BOOTSTRAP_ADMIN_KEY=dev-admin-key-local
export GITHUB_PAT=ghp_xxx
export HARNESS_API_KEY=xxx
export LOG_LEVEL=debug

# 3. 直接运行
go run ./cmd/server/main.go
```

`infrastructure/docker-compose.yml` 提供本地 MongoDB（无鉴权，仅开发用）：

```yaml
services:
  mongo:
    image: mongo:7
    ports:
      - "27017:27017"
    volumes:
      - mongo-data:/data/db
volumes:
  mongo-data:
```

**K8s 操作在本地开发时的处理：** `K8S_IN_CLUSTER=false` 时 control-api 使用 `~/.kube/config`（指向 OrbStack 本地集群），namespace 创建/删除等操作仍然真实执行。若需要完全跳过 K8s 操作，可在 `k8s_client.go` 中检测 `DRY_RUN=true` 环境变量，记录 log 但不实际调用。

---

## 6. CI/CD 完整流程

### GitHub push → 自动部署某 Project 到指定 Environment

```
git push → <project-repo> main 分支
  └─ GitHub fires Webhook
       └─ POST /api/v1/github/webhooks/receive
            ├─ 验证 HMAC-SHA256 签名
            ├─ 持久化 webhook_event
            ├─ 查找关联该 repo 的 Project，读取触发规则
            └─ 异步触发 CI pipeline
                 └─ Harness Delegate 执行 CI：
                      docker build → push ghcr.io/perpetualswing/<project>:sha-<commit>
                      ↓ CI 完成后 Harness 回调 control-api（HTTP Webhook Step）
                        POST /api/v1/harness/webhooks/receive
                        Header: Authorization: Bearer <HARNESS_WEBHOOK_TOKEN>
                        Body: { pipeline_id, run_id, status, outputs: { image_tag, ... } }
                        ├─ 创建 Artifact 记录（type=docker-image, version=sha-<commit>,
                        │                      location=ghcr.io/perpetualswing/<project>:sha-<commit>）
                        └─ （可选）自动触发 CD pipeline，携带 artifact_id + env_id

  └─ CD pipeline 触发（body: artifact_id + strategy + env_id）
       └─ Harness Delegate 按策略执行部署：
            rolling/recreate → kubectl set image / rollout
            blue-green       → 部署新 Deployment，切换 Service selector，下线旧版
            canary           → 部署小比例新版 Pod，逐步调整权重
            ↓ 部署完成后 Harness 同样回调 /api/v1/harness/webhooks/receive
              └─ upsert deployments（当前状态缓存）
                 # pipeline_runs 本身即为完整部署历史，无需额外写入

# 回滚
POST .../environments/:envId/deployments/rollback  { artifact_id: "<旧版本>" }
  → 以 is_rollback=true 触发新一轮 CD，使用指定历史 artifact 重走部署流程

# 查询
GET .../projects/:projectId/deployments                     # 各环境已部署哪个版本
GET .../projects/:projectId/environments/:envId/deployment  # 当前版本 + K8s 实时状态（replicas/ready）
GET .../projects/:projectId/artifacts                       # 所有可用 artifact 列表
GET .../projects/:projectId/environments/:envId/logs        # 实时 Pod 日志
GET /api/v1/pipeline-runs/:runId                            # 流水线执行状态
```

### GitHub Webhook 本地接收

本地集群无公网 IP，需隧道将 GitHub Webhook 转发到 control-api：
- **开发阶段**：`ngrok http <NodePort>` 获取临时公网地址
- **更持久方案**：Cloudflare Tunnel（免费，稳定）

---

## 7. 分阶段实施路线图

### Phase 1 — 核心资源 CRUD + Auth（P0）
**目标**：control-api 运行在 K8s，MongoDB 正常，能对所有核心资源进行完整 CRUD；API Key 鉴权可用

- `go mod init` 初始化 control-api 模块
- `cmd/server/main.go` + gin + healthz/readyz
- MongoDB 连接 + config.go（envconfig）
- **[P0-2]** user domain + api_keys + org_members；API Key 鉴权中间件；RBAC 权限检查
- archetype domain：entity/repository/service/handler（含版本管理）
- cluster domain：entity/repository/service/handler（含 kubeconfig_secret_name 字段）
- organization domain：含成员管理 API
- environment domain：含 org_id、cluster_id 外键，`is_protected` 字段
- **[P0-1]** 创建/删除 Environment 时自动通过 client-go 创建/删除 K8s namespace
- project domain：含 org_id、archetype_id 外键
- k8s manifests：namespace、MongoDB StatefulSet（含 PVC）、control-api Deployment（含 ServiceAccount + ClusterRole）
- Makefile：build、docker-build、docker-push

**学到**：Go 模块、gin 路由、MongoDB driver、API Key 鉴权、K8s manifest、OrbStack K8s 基本操作

**测试**：service 层用 mock repository（接口注入）做单元测试；MongoDB repository 层用 `testcontainers-go` 启动真实 MongoDB 做集成测试；`go test ./...` 覆盖所有 domain

---

### Phase 2 — GitHub 集成
**目标**：Project 与 GitHub 仓库关联，control-api 能接收 push/PR 事件

- `internal/clients/github/github_client.go`（go-github + PAT）
- 注册仓库 API（调用 GitHub API 创建 Webhook）
- Webhook 接收端点（HMAC-SHA256 验证 + 事件持久化）
- 创建 Project 时自动关联 GitHub 仓库并注册 Webhook

**学到**：GitHub Webhooks API、HMAC 验证、go-github 库、事件驱动模式

**测试**：用 `httptest` 测试 Webhook 接收端点的 HMAC 验证逻辑；mock go-github client 测试仓库注册流程

---

### Phase 3 — Harness CI/CD 集成 + Secret + Promotion（P1 前置）
**目标**：push 代码 → 自动触发对应 Project 的流水线 → 部署到指定 Environment；敏感配置加密存储

- Harness Delegate 安装到 K8s（Helm）
- `internal/clients/harness/harness_client.go`（net/http 封装）
- harness domain 端点：trigger/poll
- Webhook 事件处理器 → 根据 Project + 触发规则确定目标 Environment，触发流水线
- **[P1-1]** Secret 加密：AES-256-GCM，DEK 存储于 K8s Secret；env-vars `?reveal=true` 端点
- **[P1-2]** Promotion 审批：promotion domain；Environment `is_protected` 阻断直接部署；approve/reject API
- **[Terraform]** 创建 Project 时调用 Terraform 自动 provision ECR repo + S3 bucket + IAM Role
- control-api Dockerfile（多阶段构建，distroless 基础镜像）
- `infrastructure/harness/pipelines/` 流水线 YAML 模板
- `infrastructure/terraform/modules/project-resources/` Terraform 模块

**学到**：Harness SaaS 配置、Delegate 架构、pipeline YAML as-code、加密最佳实践、审批流设计、Terraform 基础（HCL、provider、state、module）

**测试**：加密/解密逻辑单元测试；Harness 回调端点用 `httptest` + mock service 测试；Promotion 状态流转的 service 层单元测试

---

### Phase 4 — Kubernetes 管理域 + 运营完善（P1 补全）
**目标**：通过 API 查看和管理 Project 在各 Environment 中的实际运行状态；平台可观测性完善

- `internal/clients/k8s/k8s_client.go`（InClusterConfig + ServiceAccount RBAC）
- K8s domain 全部端点（Deployment 状态、Pod 日志）
- **[P1-5]** Health 端点：从 K8s `availableReplicas` vs `replicas` 派生服务健康状态，无额外轮询
- **[P1-3]** Notification：notification_channels + notification_records；Gin middleware 在关键事件后异步触发
- **[P1-4]** Audit Log：audit_logs；Gin middleware 自动记录所有写操作 + Secret 明文查看
- Project 详情 API 聚合返回：元数据 + 各 Environment 的 K8s 实时状态 + 最近一次流水线结果
- `scripts/bootstrap.sh`：一键初始化本地环境
- zap 结构化日志、统一 API 错误类型、request-id 中间件

**学到**：client-go、RBAC 最小权限原则、Webhook 通知、审计中间件、生产级可观测性

**测试**：mock k8s client-go 测试 namespace 生命周期逻辑；端到端集成测试（testcontainers-go 起 MongoDB + fake K8s）覆盖核心 deploy/rollback 流程

---

### Phase 5 — Terraform + AWS Fargate
**目标**：Cluster 支持 `ecs-fargate` 类型；Terraform 管理 AWS 基础设施；Project 在 Fargate 上可完整部署

- Terraform 入门：HCL 语法、provider、state、module、remote state（S3 + DynamoDB）
- `infrastructure/terraform/modules/fargate-cluster/`：VPC + ALB + ECS Cluster（Terraform 创建 ecs-fargate 类型 Cluster 时触发）
- `infrastructure/terraform/modules/project-resources/`：ECR + S3 + IAM Role（创建 Project 时已在 Phase 3 触发，此阶段补全 Fargate 相关输出）
- `internal/clients/aws/ecs_client.go`：ECS Service 创建/更新/状态查询
- `internal/clients/aws/terraform_client.go`：封装 `terraform apply/output` 调用
- Cluster domain 扩展：支持 `type` 字段，k8s / ecs-fargate 分支逻辑
- deployment/service.go：Strategy Pattern，根据 cluster type 调用不同客户端
- Fargate 日志：通过 CloudWatch Logs API 获取（替代 K8s pod logs）
- S3 remote state 配置，Terraform 状态文件不进 Git

**学到**：Terraform IaC、AWS ECS/Fargate/ALB/VPC/IAM、AWS SDK for Go、Strategy Pattern、多云部署抽象

---

### Phase 6 — Frontend Web UI
**目标**：React SPA 覆盖所有核心操作，最终内嵌到 control-api 二进制

- Vite + React + TypeScript + Tailwind CSS + shadcn/ui + TanStack Query 项目搭建
- 登录页（API Key 认证）+ 全局 Layout（Sidebar + 路由）
- Organization / Environment / Project / Member 管理页
- Project 详情：部署状态总览、Pipeline 触发、Artifact 列表、Env Vars（secret 掩码）、Config Files、Logs
- Promotions 审批页
- Audit Logs 查询页
- Admin 页：Users / Clusters / Archetypes
- Dockerfile 多阶段构建（node → go → distroless），前端产物内嵌到 Go 二进制

**学到**：React + TypeScript、TanStack Query 数据请求、shadcn/ui、Go `//go:embed`、多阶段 Docker 构建

**测试**：核心组件（Login、部署触发、Promotion 审批）用 Vitest + React Testing Library 做单元测试

---

## 8. 关键开发文件（起点）

| 文件路径 | 说明 |
|---|---|
| `control-api/cmd/server/main.go` | Phase 1 第一个文件，负责依赖注入和服务启动 |
| `control-api/internal/config/config.go` | 所有外部服务凭证的配置结构体 |
| `control-api/internal/domain/archetype/` | 第一个 domain，确立四文件模式；含版本子资源 |
| `control-api/internal/domain/cluster/` | 顶层基础设施 domain |
| `control-api/internal/domain/organization/` | 含 slug 唯一索引 |
| `control-api/internal/domain/environment/` | 含 org_id、cluster_id 外键和 k8s_namespace 字段 |
| `control-api/internal/domain/project/` | 核心业务 domain，含 org_id 外键 |
| `infrastructure/k8s/base/control-api/deployment.yaml` | 含 ServiceAccount 和 Secret 挂载的核心 manifest |
| `infrastructure/k8s/base/mongodb/statefulset.yaml` | Phase 1 必须，学习 StatefulSet + PVC |

---

## 9. 架构决策说明

| 决策 | 原因 |
|---|---|
| **Cluster 作为顶层基础设施资源** | Cluster 是物理概念，与业务 Organization 无关，多个 Org 可共用同一 Cluster |
| **Environment 持有 cluster_id 和 k8s_namespace** | Environment 是"在哪个集群的哪个 namespace 跑"的完整描述，解耦业务与基础设施 |
| **同一 Cluster 内 namespace 唯一**（复合唯一索引） | 防止两个 Environment 意外映射到同一 Cluster 的同一 namespace 造成资源冲突 |
| **Environment 作为独立资源**（而非 Project 字段） | Environment 可复用于多个 Project，且生命周期独立；删除 Project 不应影响 Environment |
| **Organization / Environment / Project 三层模型** | Organization 管业务边界，Environment 管部署目标，Project 管服务定义，职责清晰 |
| **Project 嵌套在 Organization 路由下** | 强制 URL 语义，避免跨 Organization 误操作 |
| **Gin** 作为路由框架 | 生态成熟，内置 JSON binding/validation，中间件丰富，社区资料多 |
| **InClusterConfig 优先** | 生产级模式，ServiceAccount token 由 k8s 自动注入，避免凭证泄露 |
| **轮询 Harness**（而非 Webhook 回调） | Phase 3 先用轮询（更简单），后期可升级为 Harness 主动回调 |
| **MongoDB** 而非 PostgreSQL | Webhook payload schema 可变，BSON 天然适配；StatefulSet+PVC 是高价值 K8s 实践 |
| **API Key 而非 JWT** | 个人学习项目无需 SSO，API Key 更简单；前缀明文方便识别，哈希存储防泄露 |
| **Namespace lifecycle 由 control-api 管理** | Environment CRUD 操作同步创建/删除 K8s namespace，保证平台状态与集群状态一致，避免孤儿 namespace |
| **AES-256-GCM 加密 Secret，DEK 存 K8s Secret** | 加密密钥不进 MongoDB，职责分离；K8s Secret 由 RBAC 保护，比明文更安全且比 Vault 更简单 |
| **Promotion 审批阻断直接部署** | `is_protected` 环境（如 prod）强制走审批流，防止未经审查的代码直接上线 |
| **Health 状态从 K8s `availableReplicas` 派生** | 无需额外健康检查轮询任务，K8s 本身已有 liveness/readiness probe，直接读取 Deployment 状态即可 |
| **Audit Log 用 Gin middleware 自动记录** | 业务代码无需手动调用，覆盖面广且不遗漏；TTL 索引控制存储大小 |
| **Bootstrap 用环境变量而非独立接口** | 避免开放无鉴权的 admin 创建接口；仅首次启动生效，之后可从环境变量移除，安全且简单 |
| **禁止删除仍被引用的 Cluster/Archetype** | 防止悬空引用；删除 Org 要求先清空下属资源，强制用户显式决策，避免级联误删 |
| **Project 删除不同步删除 GitHub repo 和 Harness project** | 远端资源删除不可逆，防止误操作；由用户手动在 GitHub/Harness 侧清理 |
| **Harness 回调用预共享 token 认证** | Harness Webhook Step 支持自定义 Header，预共享 token 实现简单；比 IP 白名单更灵活，比 HMAC 实现更简单 |
| **NodePort 暴露 control-api（本地）** | OrbStack 自动映射 NodePort 到宿主机，无需额外配置 Ingress；本地开发场景下足够用 |
| **`internal/types` 存放跨 domain 共享类型** | 防止 domain 间循环 import；ID 类型用 `type OrgID string` 而非裸 string，增加类型安全 |
| **service 层用接口注入，main.go 统一组装** | domain 间只通过接口通信，具体实现由 main.go 注入；方便单元测试（mock repository）且不产生循环依赖 |
| **testcontainers-go 做 repository 层集成测试** | 比 mock MongoDB 更真实，能测到真实索引和查询行为；不依赖外部 MongoDB 服务，CI 可直接运行 |
| **React SPA 内嵌到 control-api 二进制** | 单一部署单元，无需独立 nginx Pod；Go `//go:embed` 将构建产物嵌入，`/api/v1/` 路由走 API，其余路由返回 `index.html` |
| **Vite dev server 代理 API** | 开发时前端跑 `:5173`，`/api` 请求代理到 `:8080`，无需改 CORS 配置；生产构建后由 Go 内嵌提供 |
| **Terraform 管理 AWS 基础设施，不管 K8s 资源** | K8s 资源由 control-api 通过 client-go 直接管理（更实时）；AWS 资源（VPC/ECS/ECR/IAM）生命周期长、变更少，适合 Terraform 声明式管理 |
| **Terraform state 存 S3 + DynamoDB 锁** | state 文件含敏感信息不进 Git；S3 保证持久化，DynamoDB 防并发 apply 导致 state 损坏；是 Terraform 生产环境的标准实践 |
| **Fargate 作为第二种 Cluster 类型（非替代）** | K8s（OrbStack）用于本地 dev 环境，Fargate 用于云上 staging/prod；两者并存学习两套技术，也贴近真实公司的混合架构 |
| **Strategy Pattern 隔离 K8s 和 Fargate 部署逻辑** | deployment/service.go 通过接口调用，不感知底层差异；新增 Cluster 类型只需实现新 Strategy，不改已有代码（开闭原则） |

---

## 10. Frontend Web UI

### 技术栈

| 层 | 选型 |
|---|---|
| 框架 | React 18 + TypeScript |
| 构建工具 | Vite |
| 样式 | Tailwind CSS |
| UI 组件库 | shadcn/ui（基于 Radix UI + Tailwind，免安装 CSS-in-JS） |
| 数据请求 | TanStack Query（React Query）——处理 loading/error/cache |
| 路由 | React Router v6 |
| 包管理 | npm |

### 目录结构

```
frontend/
├── src/
│   ├── main.tsx
│   ├── App.tsx                  # 路由配置
│   ├── api/
│   │   ├── client.ts            # fetch 封装，自动注入 Authorization header
│   │   ├── types.ts             # TypeScript 类型（与 Go entity 对应）
│   │   ├── organizations.ts
│   │   ├── projects.ts
│   │   ├── environments.ts
│   │   ├── deployments.ts
│   │   ├── pipelines.ts
│   │   ├── artifacts.ts
│   │   ├── promotions.ts
│   │   ├── users.ts
│   │   └── ...
│   ├── pages/
│   │   ├── Login.tsx
│   │   ├── Dashboard.tsx
│   │   ├── orgs/
│   │   │   ├── OrgList.tsx
│   │   │   └── OrgDetail.tsx    # Tabs: Environments / Projects / Members
│   │   ├── projects/
│   │   │   └── ProjectDetail.tsx # Tabs: Overview / Pipelines / Artifacts / Env Vars / Config / Logs
│   │   ├── Promotions.tsx
│   │   ├── AuditLogs.tsx
│   │   └── admin/
│   │       ├── Users.tsx
│   │       ├── Clusters.tsx
│   │       └── Archetypes.tsx
│   ├── components/
│   │   ├── Layout.tsx            # 侧边栏 + 顶栏 wrapper
│   │   ├── Sidebar.tsx
│   │   ├── StatusBadge.tsx       # pipeline / deployment 状态标签
│   │   └── ConfirmDialog.tsx     # 删除 / 审批确认弹窗
│   └── hooks/
│       └── useAuth.ts            # API Key 读写 localStorage，登录/登出逻辑
├── index.html
├── package.json
├── vite.config.ts               # 开发时代理 /api → http://localhost:8080
└── tsconfig.json
```

### 页面规划

| 页面 | 路径 | 主要功能 |
|---|---|---|
| 登录 | `/login` | 输入 API Key，调 `GET /api/v1/me` 验证后写入 localStorage |
| 仪表盘 | `/` | 各 Org 概览卡片、近期部署状态、待审批 Promotion 数量 |
| 组织列表 | `/orgs` | 所有 Organization 列表，快捷进入详情 |
| 组织详情 | `/orgs/:orgId` | Tab — Environments（含 is_protected 标记）/ Projects / Members |
| 项目详情 | `/orgs/:orgId/projects/:projectId` | Tab — Overview（各 env 部署状态）/ Pipelines（触发、查看历史）/ Artifacts / Env Vars（secret 掩码）/ Config Files（版本管理）/ Logs（轮询 K8s pod logs） |
| 晋级审批 | `/promotions` | 待审批列表（pending），审批通过 / 拒绝操作 |
| 审计日志 | `/audit-logs` | 时间范围 + 操作人筛选，只读表格 |
| 用户管理 | `/admin/users` | platform_admin 专属：用户 CRUD、API Key 管理 |
| 集群管理 | `/admin/clusters` | Cluster CRUD |
| 模板管理 | `/admin/archetypes` | Archetype CRUD 含版本管理 |

### 认证方式

- API Key 存储在 `localStorage`，key 名 `dpk_token`
- `api/client.ts` 拦截所有请求，自动添加 `Authorization: Bearer <key>` header
- 收到 `401` 响应时自动跳转 `/login`，清除 localStorage

### 生产构建与内嵌

```
# 1. 构建前端，输出到 control-api/web/dist/
cd frontend && npm run build
# vite.config.ts 中配置 build.outDir: "../control-api/web/dist"

# 2. Go 内嵌（control-api/internal/server/embed.go）
//go:embed ../web/dist
var WebDist embed.FS

# 3. router.go 中注册静态文件路由
# /api/v1/* → API handler
# /* → 返回 index.html（SPA fallback）
```

Dockerfile 多阶段构建同时处理前后端：
```dockerfile
FROM node:20-alpine AS frontend-builder
WORKDIR /frontend
COPY frontend/package*.json ./
RUN npm ci
COPY frontend/ .
RUN npm run build   # 输出到 control-api/web/dist/

FROM golang:1.22-alpine AS backend-builder
WORKDIR /app
COPY control-api/ .
COPY --from=frontend-builder /frontend/../control-api/web/dist ./web/dist
RUN CGO_ENABLED=0 go build -o server ./cmd/server/main.go

FROM gcr.io/distroless/static-debian12
COPY --from=backend-builder /app/server /server
ENTRYPOINT ["/server"]
```

### 开发模式

```bash
# 终端 1：启动 control-api
cd control-api
export K8S_IN_CLUSTER=false && go run ./cmd/server/main.go

# 终端 2：启动前端开发服务器
cd frontend
npm run dev   # http://localhost:5173
# vite.config.ts 代理：/api → http://localhost:8080
```
