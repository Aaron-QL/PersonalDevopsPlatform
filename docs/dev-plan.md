# 开发计划

> 从零开始构建 PersonalDevopsPlatform 的完整步骤，每天约 2 小时工作量。
> 参考架构设计：`docs/architecture.md`

---

## 前置说明

- 每天结束时应有一个**可验证的里程碑**，不要推进到下一天再验证
- 命令行工具统一用 **Homebrew** 安装（`brew install xxx`）
- 所有密钥/token 统一记录在本地密码管理器（1Password / Keychain），不写入代码
- 项目根目录：`~/Documents/PersonalDevopsPlatform`

---

## 准备阶段：账号 + 工具（Day 1–3）

---

### Day 1 — 账号注册 + 基础工具安装

**目标**：所有账号就绪，本地 Go / OrbStack / kubectl / Helm 均可用

**核心概念**：
- **Homebrew**：macOS 的包管理器，通过 `brew install` 统一管理命令行工具，避免手动下载安装包带来的版本混乱
- **OrbStack**：macOS 上的容器和 Kubernetes 运行时，内置 Docker Engine，比 Docker Desktop 更轻量；启用 Kubernetes 后自动配置 `~/.kube/config`
- **kubectl**：Kubernetes 命令行客户端，通过读取 kubeconfig 与集群的 API Server 通信，是操作 K8s 最基础的工具
- **Helm**：Kubernetes 的包管理器，将一组相关的 K8s manifest 打包为 Chart，通过 `helm install` 一键部署复杂应用（如 Harness Delegate）

#### 1. 账号注册

**GitHub**（如已有账号可跳过注册，但需完成后续配置）
1. 注册：https://github.com/signup
2. 开启两步验证（Settings → Password and authentication → Enable 2FA）
3. 创建一个 **GitHub Organization**（将来用于存放项目 repo）：
   - https://github.com/organizations/plan → 选 Free plan
   - Organization name 建议与 `architecture.md` 中 org slug 一致
4. 生成 **PAT（Personal Access Token）**：
   - Settings → Developer settings → Personal access tokens → Fine-grained tokens → Generate new token
   - 权限勾选：`repo`（全部）、`admin:org`（全部）、`admin:repo_hook`（全部）
   - 有效期选 1 年，复制保存到密码管理器，变量名记为 `GITHUB_PAT`

**AWS**（用于 Lambda 类型 project 的 S3 存储，Phase 3 才用）
1. 注册：https://portal.aws.amazon.com/billing/signup
2. 选 **Free Tier**，需绑定信用卡（不会扣费）
3. 注册完成后进入 IAM → 创建一个用于本平台的 IAM User：
   - 权限：`AmazonS3FullAccess`（后续按需收紧）
   - 创建 Access Key，保存 `AWS_ACCESS_KEY_ID` 和 `AWS_SECRET_ACCESS_KEY`
4. 在 S3 创建一个 bucket（如 `personal-devops-artifacts`），选离自己最近的区域

**Harness**
1. 注册：https://app.harness.io/auth/#/signup → 选 Free plan
2. 进入 Account Settings → 记录 **Account ID**（右上角 → Account Settings → Overview → Account Id）
3. 生成 **API Key**：My Profile → API Keys → New API Key → 权限选 Account Admin
   - 记录 `HARNESS_API_KEY` 和 `HARNESS_ACCOUNT_ID`

#### 2. 工具安装

```bash
# Homebrew（如未安装）
/bin/bash -c "$(curl -fsSL https://raw.githubusercontent.com/Homebrew/install/HEAD/install.sh)"

# Go
brew install go
go version   # 验证：>= 1.22

# OrbStack（含 Docker + Kubernetes）
brew install --cask orbstack
# 安装完成后打开 OrbStack App → Settings → Kubernetes → Enable Kubernetes
# 等待 K8s 就绪（状态变为绿色 Running）

# kubectl（OrbStack 已自动配置 kubeconfig）
kubectl version --client
kubectl get nodes   # 应看到 orbstack 节点 Ready

# Helm
brew install helm
helm version

# golangci-lint（代码检查）
brew install golangci-lint
```

**今日里程碑验证：**
```bash
go version && kubectl get nodes && helm version
# 三条命令均有正常输出即通过
```

**✅ Day 1 完成记录：**

| 项目 | 值 |
|---|---|
| GitHub Org | https://github.com/PerpetualSwing |
| AWS IAM User | `PerpetualSwingBot` |
| AWS Region | `ap-northeast-2`（首尔） |
| S3 Bucket | `perpetualswing-artifacts-aaron` |
| Harness Account ID | `A_w0u4V5QHqX_GAFVmCJAQ` |

---

### Day 2 — K8s 命名空间 + MongoDB 部署

**目标**：MongoDB 运行在 K8s `devops-platform` namespace，可从本地连接

**核心概念**：
- **Namespace**：K8s 的逻辑隔离单元，同一集群内不同 Namespace 的资源互不干扰；本项目用它隔离控制面（`devops-platform`）和业务工作负载
- **StatefulSet**：专为有状态应用设计的 K8s 资源，保证 Pod 有固定的网络标识（`mongo-0`）和独立的存储卷；MongoDB 需要用 StatefulSet 而非 Deployment
- **PersistentVolumeClaim（PVC）**：向集群申请持久化存储的声明，Pod 重启后数据不丢失；OrbStack 会自动创建对应的本地目录作为 PV
- **Service**：为一组 Pod 提供稳定的网络入口（固定 DNS 名称和 IP），`mongo` Service 让 control-api 始终能用 `mongo:27017` 访问数据库而无需关心 Pod IP 变化

#### 1. 创建目录结构

```bash
cd ~/Documents/PersonalDevopsPlatform
mkdir -p infrastructure/k8s/base/{namespaces,mongodb,control-api}
mkdir -p infrastructure/scripts
```

#### 2. 创建 namespace

```yaml
# infrastructure/k8s/base/namespaces/namespaces.yaml
apiVersion: v1
kind: Namespace
metadata:
  name: devops-platform
---
apiVersion: v1
kind: Namespace
metadata:
  name: harness-delegate
```

```bash
kubectl apply -f infrastructure/k8s/base/namespaces/namespaces.yaml
kubectl get namespaces   # 确认两个 namespace 存在
```

#### 3. 部署 MongoDB（开发用，无认证）

```yaml
# infrastructure/k8s/base/mongodb/statefulset.yaml
apiVersion: apps/v1
kind: StatefulSet
metadata:
  name: mongo
  namespace: devops-platform
spec:
  selector:
    matchLabels:
      app: mongo
  serviceName: mongo
  replicas: 1
  template:
    metadata:
      labels:
        app: mongo
    spec:
      containers:
        - name: mongo
          image: mongo:7
          ports:
            - containerPort: 27017
          volumeMounts:
            - name: data
              mountPath: /data/db
  volumeClaimTemplates:
    - metadata:
        name: data
      spec:
        accessModes: ["ReadWriteOnce"]
        resources:
          requests:
            storage: 2Gi
---
apiVersion: v1
kind: Service
metadata:
  name: mongo
  namespace: devops-platform
spec:
  selector:
    app: mongo
  ports:
    - port: 27017
```

```bash
kubectl apply -f infrastructure/k8s/base/mongodb/
kubectl get pods -n devops-platform   # mongo-0 应变为 Running
```

#### 4. 验证 MongoDB 连接

```bash
# 从本地端口转发测试
kubectl port-forward -n devops-platform svc/mongo 27017:27017 &
# 安装 mongosh
brew install mongosh
mongosh mongodb://localhost:27017 --eval "db.runCommand({ping:1})"
# 看到 { ok: 1 } 即通过
```

**今日里程碑验证：**
```
kubectl get pods -n devops-platform → mongo-0 Running
mongosh 能 ping 通 MongoDB
```

---

### Day 3 — GitHub 仓库初始化 + ngrok 安装

**目标**：项目 repo 推送到 GitHub，ngrok 可将本地端口暴露到公网

**核心概念**：
- **Git remote**：本地仓库与远端仓库的关联，`git remote add origin` 建立映射后，`git push` 就知道往哪里推送代码
- **Personal Access Token（PAT）**：GitHub 的 API 认证凭证，比密码更安全，可以精确控制权限范围（如只允许操作 repo，不允许删除账号）
- **ngrok**：内网穿透工具，在本地服务和公网之间建立加密隧道；GitHub Webhook 需要能访问到你的 control-api，ngrok 提供临时公网 HTTPS 地址解决这个问题
- **Webhook（概念）**：一种"反向 API"模式——不是你去轮询 GitHub 有没有新事件，而是 GitHub 在事件发生时主动 POST 到你指定的 URL

#### 1. 创建 GitHub repo

```bash
cd ~/Documents/PersonalDevopsPlatform
git init   # 如尚未 init
# 在 GitHub 上创建 repo：PersonalDevopsPlatform（属于个人账号，非 Org）
git remote add origin git@github.com:<your-username>/PersonalDevopsPlatform.git
git add .
git commit -m "init: project structure and architecture docs"
git push -u origin main
```

#### 2. 安装 ngrok（本地 Webhook 接收用）

```bash
brew install ngrok

# 注册 ngrok 免费账号：https://ngrok.com/signup
# 获取 auth token 后配置：
ngrok config add-authtoken <YOUR_TOKEN>

# 测试（先不启动服务，只验证 ngrok 命令可用）
ngrok --version
```

#### 3. 安装 docker-compose（本地开发用）

```bash
# OrbStack 已内置 docker，只需 compose
brew install docker-compose

# 创建本地开发用 docker-compose
cat > infrastructure/docker-compose.yml << 'EOF'
services:
  mongo:
    image: mongo:7
    ports:
      - "27017:27017"
    volumes:
      - mongo-data:/data/db
volumes:
  mongo-data:
EOF
```

#### 4. 安装 testcontainers 依赖（测试用）

```bash
# testcontainers-go 需要 Docker，OrbStack 已提供，确认 docker 命令可用
docker ps
```

**今日里程碑验证：**
```
git log --oneline → 看到 init commit
ngrok --version → 有输出
docker ps → 正常返回（无需有容器）
```

---

## Phase 1：control-api 核心框架（Day 4–10）

---

### Day 4 — Go 项目初始化 + HTTP 框架 + MongoDB 连接

**目标**：`go run ./cmd/server/main.go` 能启动，`GET /healthz` 返回 200，能连接 MongoDB

**核心概念**：
- **Go Module**：Go 的依赖管理系统，`go.mod` 记录模块路径和所有依赖版本；`go mod tidy` 自动整理，确保 `go.sum` 中的哈希与实际依赖一致
- **Gin**：Go 的 HTTP 路由框架，核心概念是 `gin.Engine`（路由器）、`gin.Context`（请求/响应上下文）、中间件链；与 Node.js 的 Express 设计思路相似
- **依赖注入（DI）**：在 `main.go` 中手动创建所有依赖（DB连接、客户端），然后逐层传递给需要它的组件；Go 通常不用 DI 框架，在 `main.go` 里显式组装即可
- **Graceful Shutdown**：收到 SIGTERM 信号时先停止接收新请求，等已有请求处理完再退出；K8s 删除 Pod 时会发 SIGTERM，正确处理可以避免请求中断

```bash
cd ~/Documents/PersonalDevopsPlatform
mkdir -p control-api/{cmd/server,internal/{config,server,middleware,domain,store/mongo,types,clients},pkg/httputil}

cd control-api
go mod init github.com/<your-username>/PersonalDevopsPlatform/control-api

# 安装依赖
go get github.com/gin-gonic/gin
go get go.mongodb.org/mongo-driver/v2/mongo
go get github.com/kelseyhightower/envconfig
go get go.uber.org/zap
```

需要编写的文件：
- `internal/config/config.go` — 环境变量结构体（参考架构文档环境变量清单）
- `internal/store/mongo/client.go` — MongoDB 连接，`Ping` 验证
- `pkg/httputil/response.go` — 统一 JSON 响应格式 `{"code":0,"data":...}` / `{"code":1,"message":...}`
- `internal/server/server.go` — gin engine，注册中间件
- `internal/server/router.go` — 路由注册，`GET /healthz` 和 `GET /readyz`
- `cmd/server/main.go` — 依赖注入，`server.Run()`

```bash
# 本地启动验证
export MONGO_URI=mongodb://localhost:27017
export K8S_IN_CLUSTER=false
go run ./cmd/server/main.go &
curl http://localhost:8080/healthz   # 期望：{"status":"ok"}
```

**今日里程碑验证：**
```
curl /healthz → 200 {"status":"ok"}
curl /readyz  → 200（MongoDB ping 通）
```

---

### Day 5 — User domain + API Key 生成 + Bootstrap 机制

**目标**：可以通过 `BOOTSTRAP_ADMIN_KEY` 自动创建初始管理员，`POST /api/v1/users` 需鉴权可用

**核心概念**：
- **哈希 vs 加密**：哈希是单向的（SHA-256），无法还原原文，适合存储密码/API Key；加密是双向的（AES），可以解密，适合需要还原的数据。API Key 存储用哈希，Secret 变量存储用加密
- **crypto/rand**：Go 标准库的密码学安全随机数生成器，生成 API Key 等安全凭证必须用它，而不是 `math/rand`（后者可预测）
- **MongoDB 唯一索引**：在集合的某个字段上创建唯一约束，数据库层保证不会有重复值；比应用层先查再插入更安全（原子操作，无竞态条件）
- **Bootstrap 模式**：冷启动时用环境变量注入初始凭证，服务自检"首次运行"状态后执行一次性初始化；避免了"需要账号才能创建账号"的鸡蛋问题

需要编写的文件：
- `internal/types/ids.go` — `type UserID string`、`type OrgID string` 等 ID 类型别名
- `internal/domain/user/entity.go` — `User`、`ApiKey` 结构体
- `internal/domain/user/repository.go` — `UserRepository` 接口
- `internal/domain/user/service.go` — 业务逻辑：创建用户、生成 API Key（`crypto/rand` 生成随机 key，`crypto/sha256` 哈希存储，key 格式：`dpk_` + 32位随机）、Bootstrap 逻辑
- `internal/domain/user/handler.go` — HTTP handler
- `internal/store/mongo/user_repo.go` — MongoDB 实现
- `internal/store/mongo/migrations/indexes.go` — 建索引（`users.email` unique，`api_keys.key_hash` unique）

Bootstrap 逻辑在 `main.go` 启动时执行：
```go
if os.Getenv("BOOTSTRAP_ADMIN_KEY") != "" {
    userSvc.Bootstrap(ctx, os.Getenv("BOOTSTRAP_ADMIN_KEY"))
}
```

**今日里程碑验证：**
```bash
export BOOTSTRAP_ADMIN_KEY=test-admin-key
go run ./cmd/server/main.go
curl http://localhost:8080/api/v1/users \
  -H "Authorization: Bearer test-admin-key"
# 期望：返回用户列表（含刚创建的 admin）
```

---

### Day 6 — Auth 中间件 + RBAC + org_members domain

**目标**：所有 API 路由受 API Key 保护，权限检查可用

**核心概念**：
- **中间件（Middleware）**：在请求到达 handler 之前和之后执行的函数链，适合处理横切关注点（认证、日志、限流）；Gin 的中间件通过 `c.Next()` 将控制权传递给下一个处理器
- **RBAC（基于角色的访问控制）**：将权限分配给角色，再将角色分配给用户；比直接给用户分权限更易维护，改角色权限自动影响所有持有该角色的用户
- **gin.Context 的 Set/Get**：中间件通过 `c.Set("user", user)` 将数据注入上下文，后续 handler 通过 `c.Get("user")` 取出；是 Gin 在同一请求生命周期内传递数据的标准方式
- **HTTP 401 vs 403**：401 表示"未认证"（没带或带了错误的凭证），403 表示"已认证但无权限"（你是谁我知道，但你不能做这件事）；正确区分这两个状态码是 API 设计规范

需要编写的文件：
- `internal/middleware/auth.go` — 从 `Authorization: Bearer <key>` 提取 key，SHA-256 哈希后查 `api_keys` 集合，将 `User` 注入 gin context
- `internal/middleware/rbac.go` — 根据 context 中的 User 和路由参数 `:orgId` 查 `org_members`，判断角色权限；提供 `RequirePlatformAdmin()`、`RequireOrgRole(role)` 两个中间件工厂函数
- `internal/domain/user/handler.go` — 补全：`GET /api/v1/me`、`GET /api/v1/users/:userId/api-keys`
- org_members CRUD：`POST/PUT/DELETE /api/v1/organizations/:orgId/members`（在 organization domain handler 里实现）

```bash
# 验证：无 token 应返回 401
curl http://localhost:8080/api/v1/users
# 期望：{"code":401,"message":"unauthorized"}

# 有 token 可正常访问
curl http://localhost:8080/api/v1/users \
  -H "Authorization: Bearer test-admin-key"
# 期望：200
```

**今日里程碑验证：**
```
无 token → 401
错误 token → 401
正确 token → 200
```

---

### Day 7 — Cluster + Archetype domain（含版本管理）

**目标**：Cluster 和 Archetype 的完整 CRUD（含 Archetype 版本子资源）可用

**核心概念**：
- **Semver（语义化版本）**：版本号格式 `MAJOR.MINOR.PATCH`，规则：破坏性变更升 MAJOR，新增功能升 MINOR，Bug 修复升 PATCH；Archetype 版本用 semver 让使用者清晰了解升级影响
- **Go interface 的隐式实现**：Go 不需要显式声明"implements"，只要结构体有接口要求的所有方法，就自动满足该接口；这是 Go 实现依赖注入和 mock 测试的基础
- **原子性写操作**：更新 Archetype 的 `latest_version` 字段需要同时保证版本记录已写入；在 MongoDB 中可以用单文档更新保证原子性，或接受最终一致（先写版本，再更新 latest）
- **MongoDB aggregation pipeline**：类似 SQL 的 GROUP BY + JOIN，用 `$lookup`、`$match`、`$group` 等阶段组合实现复杂查询；是 MongoDB 中替代关系型 JOIN 的方式

这两个 domain 是纯 CRUD，无外部依赖，适合巩固四文件模式：
- `cluster/` — entity / repository / service / handler
- `archetype/` — entity / repository / service / handler（service 层需处理 `latest_version` 字段的维护）
- `archetype_versions/` 作为 archetype 的子资源，在 archetype handler 里注册路由

重点：`archetype_versions` 在发布新版本时需更新 `archetypes.latest_version` 字段，这是一个跨集合的写操作，需在 service 层处理（非事务，失败重试即可）。

**今日里程碑验证：**
```bash
# 创建 Archetype
curl -X POST http://localhost:8080/api/v1/archetypes \
  -H "Authorization: Bearer test-admin-key" \
  -d '{"name":"Go Microservice","slug":"go-microservice","language":"go"}'

# 发布版本
curl -X POST http://localhost:8080/api/v1/archetypes/<id>/versions \
  -H "Authorization: Bearer test-admin-key" \
  -d '{"version":"1.0.0","repo_template_url":"...","changelog":"initial"}'

# 查看 archetype，latest_version 应为 "1.0.0"
curl http://localhost:8080/api/v1/archetypes/<id> \
  -H "Authorization: Bearer test-admin-key"
```

---

### Day 8 — Organization domain

**目标**：Organization CRUD 可用，slug 唯一，成员管理 API 可用

**核心概念**：
- **Slug**：URL 友好的标识符，通常是名称的小写连字符版本（如 "My App" → "my-app"）；既人类可读又能安全用在 URL 路径中，比 UUID 更直观
- **MongoDB compound index（复合索引）**：在多个字段上联合建索引，支持多字段组合查询；如 `{org_id: 1, user_id: 1}` unique 索引同时保证同一 org 内 user 不重复
- **外键（逻辑引用）**：MongoDB 没有数据库级外键约束，`org_id` 只是个字段，删除被引用的 Org 不会自动报错；需要在应用层（service 层）手动检查和级联处理

- `organization/` — entity / repository / service / handler
- 创建 Organization 时，`github_org_name` 和 `harness_org_id` 先留空（Phase 2/3 再补充同步逻辑）
- 补全 org_members 端点（复用 Day 6 写的 RBAC 逻辑）

**今日里程碑验证：**
```bash
# 创建 Org
curl -X POST http://localhost:8080/api/v1/organizations \
  -H "Authorization: Bearer test-admin-key" \
  -d '{"name":"My App","slug":"my-app"}'

# 添加成员（需先有另一个 user）
curl -X POST http://localhost:8080/api/v1/organizations/<id>/members \
  -H "Authorization: Bearer test-admin-key" \
  -d '{"user_id":"...","role":"developer"}'
```

---

### Day 9 — Environment domain + K8s namespace 生命周期

**目标**：创建 Environment 时自动在 K8s 创建对应 namespace，删除时同步删除

**核心概念**：
- **client-go**：Kubernetes 官方 Go 客户端库，提供与 K8s API Server 通信的所有能力；`InClusterConfig()` 让运行在 Pod 内的程序自动读取注入的 ServiceAccount token，无需手动配置凭证
- **ServiceAccount**：K8s 为 Pod 提供的身份标识，类似"服务账号"；Pod 启动时自动挂载对应 ServiceAccount 的 token，用于向 API Server 证明身份
- **ClusterRole + ClusterRoleBinding**：K8s RBAC 的核心资源，ClusterRole 定义权限集合（能对哪些资源做什么操作），ClusterRoleBinding 将权限绑定给 ServiceAccount；最小权限原则要求只授予必要的权限
- **Namespace 的实际意义**：在 K8s 中，Namespace 不只是逻辑隔离，还是资源配额、网络策略、RBAC 的作用域边界；为每个 Environment 创建独立 Namespace 是生产环境的标准实践

```bash
# 安装 client-go
cd control-api
go get k8s.io/client-go@latest
go get k8s.io/api@latest
go get k8s.io/apimachinery@latest
```

需要编写的文件：
- `internal/clients/k8s/k8s_client.go` — 初始化：`K8S_IN_CLUSTER=true` 用 `rest.InClusterConfig()`，`false` 用 `clientcmd.BuildConfigFromFlags("", kubeconfig)`；提供 `CreateNamespace(name)`、`DeleteNamespace(name)` 方法
- `internal/domain/environment/` — entity / repository / service / handler
- `environment/service.go` 在 Create 后调用 `k8sClient.CreateNamespace(env.K8sNamespace)`，在 Delete 前调用 `k8sClient.DeleteNamespace(env.K8sNamespace)`

```bash
# 验证（本地 K8S_IN_CLUSTER=false，使用 OrbStack kubeconfig）
export K8S_IN_CLUSTER=false
go run ./cmd/server/main.go

curl -X POST http://localhost:8080/api/v1/organizations/<orgId>/environments \
  -H "Authorization: Bearer test-admin-key" \
  -d '{"name":"dev","slug":"dev","k8s_namespace":"my-app-dev","cluster_id":"..."}'

kubectl get namespaces | grep my-app-dev   # 应存在
```

**今日里程碑验证：**
```
创建 Environment → kubectl 能看到对应 namespace
删除 Environment → kubectl 确认 namespace 消失
```

---

### Day 10 — Project + Pipeline domain + K8s 部署 control-api

**目标**：Project CRUD 可用；control-api 以 Pod 形式运行在 OrbStack K8s 中

**核心概念**：
- **Docker 多阶段构建**：一个 Dockerfile 中定义多个 `FROM` 阶段，前一阶段编译，最后阶段只复制产出的二进制文件；最终镜像不包含编译器和源码，体积极小（Go 二进制 + distroless 基础镜像通常只有几十 MB）
- **distroless 镜像**：Google 提供的极简基础镜像，只含运行时必要的库，没有 shell、包管理器等工具；攻击面极小，安全性高，是生产 Go 服务的推荐基础镜像
- **ghcr.io（GitHub Container Registry）**：GitHub 提供的免费 Docker 镜像仓库，与 GitHub 账号权限打通；用 `$GITHUB_PAT` 作为密码登录，比 Docker Hub 更适合个人/开源项目
- **K8s Deployment 滚动更新**：更新镜像时 K8s 不会直接停掉旧 Pod，而是先启动新 Pod、确认健康后再停旧 Pod；`kubectl rollout status` 可以观察滚动更新进度

#### Project + Pipeline domain

- `project/` — entity / repository / service / handler（创建时 `github_repo` 和 `harness_project_id` 先留空）
- `pipeline/` — entity / repository / service / handler

#### K8s manifests

```yaml
# infrastructure/k8s/base/control-api/serviceaccount.yaml
apiVersion: v1
kind: ServiceAccount
metadata:
  name: control-api
  namespace: devops-platform
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: control-api
rules:
  - apiGroups: [""]
    resources: ["namespaces","pods","services","configmaps","secrets"]
    verbs: ["get","list","watch","create","update","delete"]
  - apiGroups: ["apps"]
    resources: ["deployments","replicasets"]
    verbs: ["get","list","watch","create","update","delete"]
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: control-api
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: control-api
subjects:
  - kind: ServiceAccount
    name: control-api
    namespace: devops-platform
```

```dockerfile
# control-api/Dockerfile
FROM golang:1.22-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o server ./cmd/server/main.go

FROM gcr.io/distroless/static-debian12
COPY --from=builder /app/server /server
ENTRYPOINT ["/server"]
```

```bash
# 构建并推送到 ghcr.io（GitHub Container Registry，免费）
# 先登录 ghcr.io
echo $GITHUB_PAT | docker login ghcr.io -u <github-username> --password-stdin

docker build -t ghcr.io/<github-username>/control-api:latest ./control-api
docker push ghcr.io/<github-username>/control-api:latest

# 部署到 K8s
kubectl apply -f infrastructure/k8s/base/control-api/
kubectl get pods -n devops-platform   # control-api Pod 应 Running
```

**今日里程碑验证：**
```
kubectl get pods -n devops-platform → control-api-xxx Running
curl http://localhost:30080/healthz → {"status":"ok"}
```

---

## Phase 2：GitHub 集成（Day 11–14）

---

### Day 11 — GitHub API Client + Repo 注册 API

**目标**：通过 API 能在 GitHub 上创建 Webhook，查询 PR 列表

**核心概念**：
- **REST API 客户端设计**：封装 HTTP 请求的最佳实践——统一的 base URL、认证 Header 注入、错误处理和响应解析；go-github 库已经做了这层封装，学习其设计思路
- **GitHub Fine-grained PAT**：比 Classic PAT 更精细的权限控制，可以限制只能访问特定 repo 或 org，是 GitHub 的最新安全推荐实践
- **Webhook 创建流程**：通过 GitHub API `POST /repos/{owner}/{repo}/hooks` 创建 Webhook，需要提供回调 URL、触发事件类型和 HMAC secret；secret 用于后续验证回调来源合法性

```bash
cd control-api
go get github.com/google/go-github/v68
```

需要编写的文件：
- `internal/clients/github/github_client.go` — 封装：`CreateWebhook(owner, repo, url, secret)`、`DeleteWebhook(owner, repo, webhookID)`、`ListPRs(owner, repo)`、`CreateRepo(org, name, templateURL)`
- `internal/domain/github/` — entity（`GithubRepo`）/ repository / service / handler
- `POST /api/v1/github/repos` — 注册仓库（调用 GitHub API 创建 Webhook）
- `DELETE /api/v1/github/repos/:owner/:repo` — 注销 Webhook

**今日里程碑验证：**
```bash
# 注册一个已存在的 GitHub repo
curl -X POST http://localhost:8080/api/v1/github/repos \
  -H "Authorization: Bearer test-admin-key" \
  -d '{"owner":"<org>","repo":"<repo-name>","project_id":"..."}'
# 去 GitHub repo Settings → Webhooks，确认 Webhook 已创建
```

---

### Day 12 — Webhook 接收端点 + HMAC 验证

**目标**：GitHub push 事件能被 control-api 接收并持久化，HMAC 签名验证正确

**核心概念**：
- **HMAC-SHA256**：基于密钥的消息认证码，GitHub 用它对 Webhook payload 签名（用共享 secret 对 body 计算 HMAC，放在 `X-Hub-Signature-256` header 里）；接收端用同样的 secret 重新计算并比对，证明请求来自 GitHub 而非伪造
- **事件驱动架构**：系统通过事件（而非轮询）感知外部变化；Webhook 是推送模式，比轮询更实时、更节省资源，但需要处理重试、幂等等问题
- **异步处理**：Webhook handler 应立即返回 200（告诉 GitHub"收到了"），实际业务处理（触发 CI）放到后台 goroutine；避免因处理耗时导致 GitHub 超时重试

需要编写的文件：
- `POST /api/v1/github/webhooks/receive` handler：
  1. 读取 `X-Hub-Signature-256` header
  2. 用该 repo 的 `webhook_secret` 计算 HMAC-SHA256，比对
  3. 解析 `X-GitHub-Event` 类型
  4. 持久化到 `webhook_events` 集合
  5. 若事件类型为 `push` 且分支为 `main`，异步触发 CI（目前先打 log，Phase 3 再实现）

```bash
# 启动 ngrok 将本地端口暴露出去
ngrok http 8080
# 记录 ngrok 生成的 HTTPS URL，如 https://abc123.ngrok.io

# 更新 GitHub Webhook URL 为 https://abc123.ngrok.io/api/v1/github/webhooks/receive

# 去 GitHub repo 手动触发 Webhook（Settings → Webhooks → Redeliver）
# 查看 control-api 日志，应看到事件已接收
```

**今日里程碑验证：**
```
GitHub Webhook 触发 → control-api 日志输出事件内容
webhook_events 集合中有新记录
HMAC 错误的请求返回 401
```

---

### Day 13 — 创建 Project 时自动初始化 GitHub Repo

**目标**：`POST /api/v1/organizations/:orgId/projects` 时自动在 GitHub 创建 repo 并注册 Webhook

**核心概念**：
- **副作用（Side Effects）**：一个操作触发其他系统的变更；创建 Project 时同步创建 GitHub repo 和 Harness pipeline 就是典型的多系统副作用，难点在于部分失败时的清理（补偿事务）
- **幂等性**：同一操作执行多次结果相同；API 和副作用都应设计为幂等，比如创建 GitHub repo 时先检查是否已存在，存在则关联而非报错
- **补偿事务（Saga 模式简化版）**：分布式操作没有原子性，部分失败时需要手动回滚已成功的步骤；简单做法是记录每一步结果，失败时按逆序清理

在 `project/service.go` 的 Create 方法中添加副作用（步骤顺序重要，失败需清理）：
1. 在 MongoDB 创建 Project 记录（`github_repo` 暂为空）
2. 调用 `githubClient.CreateRepo(org.GithubOrgName, project.Slug, archetype.RepoTemplateURL)`
3. 调用 `githubClient.CreateWebhook(...)` 注册 Webhook
4. 更新 Project 的 `github_repo` 字段

注意：步骤 2-3 失败时需要在 MongoDB 删除刚创建的 Project（或标记为 `provisioning_failed`）

**今日里程碑验证：**
```bash
curl -X POST http://localhost:8080/api/v1/organizations/<orgId>/projects \
  -H "Authorization: Bearer test-admin-key" \
  -d '{"name":"demo-service","slug":"demo-service","type":"microservice","language":"go","archetype_id":"..."}'
# 去 GitHub org 确认 repo 已创建、Webhook 已注册
```

---

### Day 14 — 端到端测试：git push → webhook 接收

**目标**：向 GitHub repo 推送代码，control-api 能收到并正确处理

**核心概念**：
- **端到端测试（E2E）**：从用户视角出发，测试整个系统的完整流程而非单个模块；价值在于发现集成问题，但维护成本高、运行慢
- **调试技巧**：`kubectl logs -f` 实时跟踪 Pod 日志；`kubectl describe pod` 查看 Pod 事件和状态；这两个命令是 K8s 日常调试最常用的工具

```bash
# clone 刚创建的 repo
git clone git@github.com:<org>/demo-service.git /tmp/demo-service
cd /tmp/demo-service
echo "test" >> README.md
git add . && git commit -m "test push" && git push

# 观察 control-api 日志
kubectl logs -n devops-platform -l app=control-api -f
# 应看到 push 事件被接收
```

排查常见问题：
- ngrok 会话过期 → 重启 ngrok，更新 GitHub Webhook URL
- HMAC 验证失败 → 检查 `webhook_secret` 是否与注册时一致

**今日里程碑验证：**
```
git push → control-api 日志输出 "received push event for demo-service"
webhook_events 集合有新记录，processed=false
```

---

## Phase 3：Harness CI/CD（Day 15–22）

---

### Day 15 — Harness Delegate 安装

**目标**：Harness Delegate 运行在 K8s，Harness 控制台显示 Delegate 在线

**核心概念**：
- **Harness Delegate 架构**：Delegate 是运行在你的基础设施内的代理进程，主动向 Harness SaaS 轮询任务；Harness SaaS 不需要访问你的集群，解决了内网无公网 IP 的问题，与 Jenkins Agent 的设计思路相似
- **Helm Chart**：K8s 应用的打包格式，包含一组模板化的 manifest 文件；`helm install` 时传入 `--set key=value` 覆盖默认配置，适合安装配置复杂的第三方应用
- **`helm repo add` + `helm repo update`**：类似 `apt-get update`，先注册 Chart 仓库地址，再拉取最新的 Chart 索引；之后才能 `helm install` 对应的 Chart

#### 1. 在 Harness 控制台获取 Delegate token

Account Settings → Delegates → New Delegate → Kubernetes → Helm Chart 方式
记录生成的 `delegateToken`

#### 2. 安装 Delegate

```bash
# 添加 Harness Helm repo
helm repo add harness-delegate https://app.harness.io/storage/harness-download/delegate-helm-chart/
helm repo update

helm install harness-delegate harness-delegate/harness-delegate \
  --namespace harness-delegate \
  --set delegateName=local-k8s-delegate \
  --set accountId=<HARNESS_ACCOUNT_ID> \
  --set delegateToken=<DELEGATE_TOKEN> \
  --set managerEndpoint=https://app.harness.io \
  --set delegateDockerImage=harness/delegate:latest \
  --set replicas=1 \
  --set upgrader.enabled=false

kubectl get pods -n harness-delegate -w   # 等待 Running
```

#### 3. 验证

Harness 控制台 → Account Settings → Delegates → 看到 `local-k8s-delegate` 状态为 **Connected**

**今日里程碑验证：**
```
kubectl get pods -n harness-delegate → delegate Pod Running
Harness 控制台 Delegate 状态 = Connected
```

---

### Day 16 — Harness API Client

**目标**：通过代码能调用 Harness API 创建 org、project、pipeline，触发 pipeline run

**核心概念**：
- **无官方 SDK 时的 HTTP 客户端封装**：用 `net/http` + `encoding/json` 手写客户端是 Go 的常见实践；重点是统一处理：请求构造（base URL + path）、认证 header 注入、错误响应解析、超时设置
- **Harness API 认证**：所有请求需要 `x-api-key` header（API Key）和 `Harness-Account` header（Account ID）；与 GitHub PAT 类似，是最简单的 API Key 认证模式
- **接口抽象**：Harness client 应该定义为 interface，方便在测试中替换为 mock；这是 Go 依赖注入的核心实践

Harness 无官方 Go SDK，用 `net/http` 封装：

```bash
# 无需新依赖，用标准库
```

需要编写的文件：
- `internal/clients/harness/harness_client.go`：
  - `CreateOrg(orgName string)`
  - `CreateProject(orgID, projectName string)`
  - `CreatePipeline(orgID, projectID, pipelineYAML string)`
  - `TriggerPipeline(orgID, projectID, pipelineID string, inputs map) (runID string)`
  - `GetPipelineRun(orgID, projectID, pipelineID, runID string)`

所有请求添加 Header：`x-api-key: <HARNESS_API_KEY>`，`Harness-Account: <ACCOUNT_ID>`

**今日里程碑验证：**
```bash
# 写一个简单的 main_test.go 测试 Harness 连接
go test ./internal/clients/harness/... -v -run TestPing
# 能正常调用 Harness API 返回 200
```

---

### Day 17 — Archetype 模板配置 + 创建 Project 同步 Harness

**目标**：创建 Project 时自动在 Harness 创建同名 project 和 CI/CD pipeline

**核心概念**：
- **Pipeline as Code（PaC）**：用 YAML/代码描述流水线步骤，与应用代码一起版本控制；Harness 支持 pipeline YAML，可以通过 API 动态创建和更新 pipeline
- **Harness 资源层级**：Account → Organization → Project → Pipeline；创建 Pipeline 前必须先确保 Org 和 Project 存在，API 调用有严格的顺序依赖
- **模板化 YAML**：Archetype 的 CI/CD YAML 是模板，创建 Project 时需要将占位符（如 `{{.ProjectSlug}}`）替换为实际值；Go 的 `text/template` 包可以处理这类逻辑

#### 1. 填充 Archetype CI/CD YAML 模板

为之前创建的 `go-microservice` Archetype 版本填充真实的 CI/CD YAML：
- `ci_pipeline_yaml`：Harness CI pipeline，步骤包括 `go build`、`docker build`、`docker push ghcr.io`
- `cd_pipeline_yaml`：Harness CD pipeline，步骤包括 `kubectl set image`

#### 2. 更新 project/service.go 的 Create 方法

在已有的 GitHub 步骤之后继续：
1. 调用 `harnessClient.CreateProject(org.HarnessOrgID, project.Slug)`
2. 调用 `harnessClient.CreatePipeline(...)` 创建 CI pipeline
3. 调用 `harnessClient.CreatePipeline(...)` 创建 CD pipeline
4. 更新 Project 的 `harness_project_id`，向 `pipelines` 集合写入两条记录

**今日里程碑验证：**
```bash
curl -X POST .../organizations/<orgId>/projects \
  -d '{"name":"new-service",...}'
# Harness 控制台：Projects 下出现 new-service，含 CI 和 CD 两条 pipeline
```

---

### Day 18 — Webhook 触发 CI + Harness 回调端点

**目标**：git push → CI pipeline 自动触发；CI 完成后回调 control-api 创建 Artifact 记录

**核心概念**：
- **Harness HTTP Step**：Pipeline 中的一个特殊步骤类型，执行完成后向指定 URL 发送 HTTP 请求；用来在 CI/CD 完成时通知 control-api，是 Harness 与外部系统集成的标准方式
- **预共享密钥认证（Pre-shared Key）**：两端提前约定一个密钥，每次请求携带该密钥验证身份；比 HMAC 简单，适合内部系统间通信（两端都在你控制下）
- **Pipeline 输出变量**：Harness pipeline 步骤可以输出变量（如 `<+artifact.tag>`），在后续步骤中引用；回调时将这些变量放入 body，control-api 据此创建 Artifact 记录

#### 1. Webhook 事件处理器

在 `webhook_events` 处理逻辑中（异步 goroutine）：
- 找到关联的 Project（通过 `github_repo` 字段）
- 找到该 Project 的 CI pipeline
- 调用 `harnessClient.TriggerPipeline(...)` 触发 CI
- 在 `pipeline_runs` 写入 run 记录（status=running）

#### 2. Harness 回调端点

在 Harness CI pipeline 最后一步加一个 **HTTP Step**（Webhook 类型），配置：
- URL：`https://<ngrok-url>/api/v1/harness/webhooks/receive`
- Headers：`Authorization: Bearer <HARNESS_WEBHOOK_TOKEN>`
- Body：`{"pipeline_id":"<+pipeline.identifier>","run_id":"<+pipeline.executionId>","status":"<+pipeline.status>","outputs":{"image_tag":"<+artifact.tag>"}}`

实现 `POST /api/v1/harness/webhooks/receive` handler：
- 验证 Bearer token 与 `HARNESS_WEBHOOK_TOKEN` 一致
- 若 status=Success：创建 Artifact 记录，更新 pipeline_run status=success
- 若 status=Failed：更新 pipeline_run status=failed

**今日里程碑验证：**
```
git push → Harness CI pipeline 自动开始
CI 完成 → artifacts 集合中出现新记录
```

---

### Day 19 — Artifact + Deployment domain + CD 触发

**目标**：通过 API 触发部署，Harness CD pipeline 执行，deployment 状态更新

**核心概念**：
- **Artifact（制品）**：CI 构建的可部署产物，Docker 镜像是最常见的形式；用 `image:tag` 唯一标识一个版本，tag 通常是 git commit SHA（可追溯）而非 `latest`（不可追溯）
- **不可变基础设施**：一旦构建出的 Artifact 不应被修改，部署新版本就是用新 Artifact 替换旧 Artifact；这是现代 DevOps 的核心原则，保证部署的可重复性
- **Deployment 的当前状态缓存**：`deployments` 表只存每个 (project, env) 的最新状态，历史在 `pipeline_runs` 里；这是"写时更新缓存"模式，用空间换查询速度

- `artifact/` domain — 只读（无 Create，由回调写入）
- `deployment/` domain：
  - `POST .../environments/:envId/deployments` — 触发 CD，检查 `env.is_protected`（若为 true 返回 403，需走 Promotion）
  - `GET .../environments/:envId/deployment` — 查当前状态
  - `GET .../environments/:envId/deployments` — 历史（查 pipeline_runs）

**今日里程碑验证：**
```bash
# 手动触发部署
curl -X POST .../environments/<envId>/deployments \
  -d '{"artifact_id":"...","strategy":"rolling"}'
# Harness 控制台看到 CD pipeline 运行
# deployment 状态变为 deploying → running
```

---

### Day 20 — 回滚 + Secret 加密（AES-256-GCM）

**目标**：回滚 API 可用；env-vars 中 `is_secret=true` 的值加密存储

**核心概念**：
- **AES-256-GCM**：对称加密算法，256位密钥，GCM 模式同时提供加密和完整性验证；是存储敏感数据的行业标准选择
- **DEK/KEK 密钥层级**：Data Encryption Key（DEK）加密实际数据，Key Encryption Key（KEK）加密 DEK；DEK 存 MongoDB，KEK 存 K8s Secret，两者分离，泄露任意一个都不足以解密数据
- **K8s Secret**：以 base64 编码（非加密）存储敏感数据的 K8s 资源；结合 RBAC 限制访问权限，是在 K8s 环境中管理密钥的标准方式（比明文 ConfigMap 安全，比 Vault 简单）

#### 1. 回滚

在 `deployment/service.go` 的 Rollback 方法中：用历史 Artifact 触发新一轮 CD，`is_rollback=true`

#### 2. Secret 加密

```bash
# 生成 32 字节随机 DEK，存入 K8s Secret
DEK=$(openssl rand -base64 32)
kubectl create secret generic control-api-dek \
  --from-literal=dek="$DEK" \
  -n devops-platform
```

在 `envvar/service.go` 中：
- 写入时：若 `is_secret=true`，用 AES-256-GCM 加密 value（密钥从 K8s Secret 读取，启动时缓存）
- 读取时：若 `is_secret=true` 且 `reveal=false`，返回 `"***"`；`reveal=true` 时解密返回明文，并写审计日志

**今日里程碑验证：**
```bash
# 写入 secret
curl -X POST .../env-vars \
  -d '{"key":"DB_PASSWORD","value":"secret123","is_secret":true}'
# 查 MongoDB，确认 value 是密文（非明文）
# 读取时默认返回 ***
curl .../env-vars   # DB_PASSWORD: "***"
curl ".../env-vars?reveal=true" -H "Authorization: Bearer admin-key"  # DB_PASSWORD: "secret123"
```

---

### Day 21 — Promotion 审批流

**目标**：向 `is_protected=true` 的 Environment 部署必须先经 Promotion 审批

**核心概念**：
- **状态机（State Machine）**：用有限的状态和明确的转换规则描述对象的生命周期；Promotion 的 `pending → approved/rejected` 就是一个简单状态机，状态机设计可以防止非法状态转换（如 approved 后再次 approve）
- **审批流（Approval Gate）**：在 CD pipeline 的关键节点设置人工审批；对应生产环境的变更管理流程，防止未经审查的代码直接上线

- `promotion/` domain
- 在 `deployment/service.go` 的 Deploy 方法开头检查：若 `env.is_protected=true`，返回错误"请先创建 Promotion 请求"
- Promotion `approve` 时自动调用 `deploymentSvc.Deploy()`（内部绕过 is_protected 检查，通过额外参数标识）

**今日里程碑验证：**
```bash
# 设置 prod env 为 protected
# 直接部署应失败
curl -X POST .../environments/<prod-env-id>/deployments \
  -d '{"artifact_id":"..."}'
# 期望：403 {"message":"environment is protected, create a promotion request"}

# 创建 Promotion
curl -X POST .../promotions \
  -d '{"project_id":"...","target_env_id":"<prod>","artifact_id":"..."}'

# 审批
curl -X POST .../promotions/<id>/approve
# CD pipeline 自动触发
```

---

### Day 22 — envvar + config-files domain

**目标**：Project 的环境变量和配置文件 CRUD 完整可用

**核心概念**：
- **ConfigMap vs Secret**：K8s 的两种配置注入方式，ConfigMap 存普通配置，Secret 存敏感数据（base64 编码）；部署时 control-api 将 env-vars 同步到对应的 K8s ConfigMap/Secret，Pod 通过 `envFrom` 挂载
- **版本控制原理**：config-file 版本用递增整数，每次提交新版本不覆盖旧版本而是新增记录；`active_version` 字段指向当前生效版本，类似 Git 的 HEAD 指针

- `envvar/` — entity / repository / service / handler（Secret 加密已在 Day 20 实现）
- `configfile/` — entity / repository / service / handler（含版本管理，逻辑参考 archetype_versions）

**今日里程碑验证：**
```bash
# 创建配置文件版本
curl -X POST .../config-files \
  -d '{"name":"app.yaml","mount_path":"/app/config/app.yaml"}'
curl -X POST .../config-files/<id>/versions \
  -d '{"content":"server:\n  port: 8080","comment":"initial"}'
curl -X POST .../config-files/<id>/versions/1/activate
# active_version 应为 1
```

---

## Phase 4：运营完善（Day 23–28）

---

### Day 23 — Notification 通知渠道

**目标**：配置 Webhook 渠道后，关键事件（CI 失败、部署完成、Promotion 待审批）自动发送通知

**核心概念**：
- **观察者模式（Observer Pattern）**：定义对象间的一对多依赖关系，当一个对象状态改变时，所有依赖者自动收到通知；notification channel 是"订阅者"，平台事件是"发布者"
- **出站 Webhook（Outbound Webhook）**：与入站 Webhook（GitHub 回调）方向相反，是平台主动推送事件给外部系统；HMAC 签名出站请求让接收方能验证消息来源
- **异步解耦**：通知发送不应阻塞主流程；用 goroutine 异步发送，失败记录到 `notification_records` 但不影响主业务操作

- `notification/` domain
- 定义事件类型枚举（`internal/types/events.go`）：`CIPipelineFailed`、`DeploymentCompleted`、`PromotionPending` 等
- 在各 service 的关键节点异步调用 `notificationSvc.Send(eventType, payload)`
- `notificationSvc.Send` 查询匹配该 org 和 event_type 的 channels，逐一发送 HTTP POST（携带 HMAC 签名）

**今日里程碑验证：**
```bash
# 配置一个 Webhook 渠道（可用 https://webhook.site 作为测试接收端）
curl -X POST .../notification-channels \
  -d '{"name":"test","type":"webhook","url":"https://webhook.site/<your-id>","event_types":["deployment.completed"]}'

# 触发一次部署，完成后去 webhook.site 确认收到通知
```

---

### Day 24 — Audit Log 中间件

**目标**：所有写操作和 secret 明文查看自动记录到 `audit_logs`

**核心概念**：
- **AOP（面向切面编程）**：将横切关注点（日志、认证、事务）从业务逻辑中分离出来，以中间件/拦截器的形式织入；Gin middleware 的 `c.Next()` 前后执行代码是 AOP 的典型实现
- **审计日志设计原则**：不可变（只追加，不修改删除）；包含足够上下文（谁、什么时间、对什么资源、做了什么）；使用 TTL 自动清理而非手动删除
- **快照（Snapshot）**：audit log 中存储操作时的 `actor_email` 而非只存 `user_id`，防止用户被删除后历史记录变成"幽灵操作"；是审计系统的常见设计

在 `internal/middleware/audit.go` 中实现 Gin 中间件：
- 拦截 `POST`/`PUT`/`DELETE` 方法的请求
- 从 gin context 读取当前 User
- 提取 `resource_type`（从 URL 路径解析，如 `/api/v1/organizations/:orgId/projects` → `project`）
- 提取 `org_id`（从路径参数 `:orgId`）
- 在响应写完后（`c.Next()` 之后）写入 `audit_logs`

在 `envvar/handler.go` 中：`?reveal=true` 查询额外手动写一条 audit log（resource_type=`secret`）

**今日里程碑验证：**
```bash
# 执行几次写操作后
curl http://localhost:8080/api/v1/organizations/<orgId>/audit-logs \
  -H "Authorization: Bearer admin-key"
# 应看到每次操作的记录
```

---

### Day 25 — K8s 实时状态 + Health

**目标**：查询 Project 部署状态时返回 K8s 实时的 replica 数量，`GET .../deployment` 聚合数据

**核心概念**：
- **K8s Deployment 状态字段**：`spec.replicas`（期望数量）、`status.availableReplicas`（实际可用数量）、`status.readyReplicas`（通过 readiness probe 的数量）；三者的关系反映了部署的健康状态
- **Liveness vs Readiness Probe**：Liveness 探测失败时 K8s 重启 Pod；Readiness 探测失败时 K8s 将 Pod 从 Service 端点移除（停止接受流量）；两者组合保证只有健康的 Pod 才接受流量
- **实时查询 vs 持久化**：K8s 是运行时状态的权威来源，实时查询比持久化同步更准确；`deployments` 表只缓存上次已知状态，真实健康状态每次请求时实时从 K8s 获取

在 `deployment/service.go` 的 GetCurrent 方法中：
- 从 MongoDB 读取 `deployments` 记录
- 调用 `k8sClient.GetDeployment(namespace, deploymentName)` 获取实时 K8s Deployment
- 用 K8s 的 `availableReplicas` 和 `replicas` 覆盖响应中的状态字段
- 推导健康状态：`available == desired` → `running`；`available < desired` → `degraded`；`available == 0` → `failed`

**今日里程碑验证：**
```bash
curl .../environments/<envId>/deployment \
  -H "Authorization: Bearer dev-key"
# 响应中包含实时 replicas_desired/replicas_ready 和 status
```

---

### Day 26 — Cloudflare Tunnel 配置（替换 ngrok）

**目标**：使用稳定的 Cloudflare Tunnel 替代 ngrok，Webhook 地址固定不变

**核心概念**：
- **反向代理隧道原理**：cloudflared 进程在本地运行，主动向 Cloudflare 边缘节点建立出站连接；外部请求到达 Cloudflare 域名后，通过这条已建立的隧道转发到本地服务，无需开放入站端口
- **DNS CNAME 记录**：Cloudflare Tunnel 会在你的域名下创建一条 CNAME 记录指向 Cloudflare 边缘节点；理解 DNS 解析链路（域名 → CNAME → IP）是网络基础知识

```bash
# 注册 Cloudflare 账号：https://cloudflare.com（免费）
# 安装 cloudflared
brew install cloudflare/cloudflare/cloudflared

# 登录并创建 tunnel
cloudflared tunnel login
cloudflared tunnel create devops-platform

# 创建配置文件
cat > ~/.cloudflared/config.yml << 'EOF'
tunnel: <TUNNEL_ID>
credentials-file: /Users/<you>/.cloudflared/<TUNNEL_ID>.json
ingress:
  - hostname: devops.<your-domain>.com
    service: http://localhost:30080
  - service: http_status:404
EOF

# 启动 tunnel
cloudflared tunnel run devops-platform
```

更新所有 GitHub Webhook URL 为固定的 Cloudflare Tunnel 地址。

> 如没有自定义域名，可继续用 ngrok，此 Day 改为补充之前未完成的功能或休息。

**今日里程碑验证：**
```
Cloudflare Tunnel 运行中
GitHub push → Webhook 稳定到达 control-api（不需要重启 ngrok）
```

---

### Day 27 — bootstrap.sh + 端到端全流程测试

**目标**：一条命令能从零初始化本地环境；完整走通 push → CI → CD → deployment 全链路

**核心概念**：
- **Shell 脚本 `set -e`**：遇到任何命令失败立即退出脚本，防止错误被忽略继续执行后续步骤；是编写可靠 bootstrap 脚本的基本实践
- **`kubectl rollout status`**：等待 Deployment 滚动更新完成后才返回，是脚本中等待 K8s 资源就绪的标准方式；结合 `set -e` 可以在部署失败时中断脚本

#### 1. bootstrap.sh

```bash
# infrastructure/scripts/bootstrap.sh
#!/usr/bin/env bash
set -e

echo "=== Creating namespaces ==="
kubectl apply -f k8s/base/namespaces/namespaces.yaml

echo "=== Deploying MongoDB ==="
kubectl apply -f k8s/base/mongodb/

echo "=== Waiting for MongoDB ==="
kubectl rollout status statefulset/mongo -n devops-platform

echo "=== Deploying control-api ==="
kubectl apply -f k8s/base/control-api/
kubectl rollout status deployment/control-api -n devops-platform

echo "=== Bootstrap done! ==="
echo "control-api: http://localhost:30080"
```

#### 2. 端到端测试

```bash
# 1. 创建 Org
# 2. 创建 Archetype（含 CI/CD YAML）
# 3. 创建 Cluster + Environment（dev 和 prod，prod 设 is_protected=true）
# 4. 创建 Project → 验证 GitHub repo + Harness pipeline 自动创建
# 5. git push → 验证 CI 自动触发 → Artifact 自动创建
# 6. 部署到 dev → 验证 CD 执行 → K8s Pod 运行
# 7. 创建 Promotion → 审批 → 部署到 prod
# 8. 查看审计日志确认记录完整
```

---

### Day 28 — 收尾：测试补全 + 文档整理

**目标**：核心 service 层有单元测试，architecture.md 与实现保持一致

**核心概念**：
- **Go 测试模式**：`go test ./...` 递归运行所有包的测试；`-v` 显示详细输出；`-run TestXxx` 只运行匹配的测试函数；`-cover` 生成覆盖率报告
- **Mock vs Stub**：Mock 是可验证调用行为的假对象（能断言"是否被调用了"），Stub 只是返回预设值；Go 通常用 interface 配合手写假实现来做 mock，无需框架
- **testcontainers-go**：在测试中用代码启动真实的 Docker 容器（如 MongoDB），测试完自动清理；比 mock 数据库更真实，能测到真实的索引约束和查询行为

#### 测试补全

```bash
# 为每个 domain 的 service 层补充单元测试（mock repository）
# 重点：
# - user: API Key 生成和哈希验证
# - environment: K8s namespace 创建/删除（mock k8s client）
# - envvar: AES-256-GCM 加密/解密
# - promotion: 状态流转
# - deployment: is_protected 检查逻辑

go test ./... -v
```

#### 文档整理

- 更新 `CLAUDE.md`：补充已实现的 build/test 命令
- 检查 `architecture.md` 与实际代码是否有偏差，更新不一致处
- 在 `README.md` 补充：快速启动步骤、API 示例

**今日里程碑验证：**
```
go test ./... → 全部 PASS
README.md 有完整的本地启动步骤
```

---

## Phase 5：Frontend Web UI（Day 29–36）

---

### Day 29 — 前端项目初始化 + 基础框架

**目标**：`npm run dev` 启动，能访问空白首页；API client 基础结构可用

**核心概念**：
- **ES Modules（ESM）**：浏览器原生支持的模块系统，用 `import/export` 语法；Vite 在开发时直接让浏览器加载 ESM，无需打包，所以启动极快
- **Vite 的 HMR（热模块替换）**：修改代码后只替换变更的模块，不刷新整个页面；React 组件状态在 HMR 后保留，开发体验远好于完整刷新
- **npm vs yarn vs pnpm**：三种 Node 包管理器，功能类似；本项目用 npm（内置于 Node.js，无需单独安装），`package.json` 记录依赖，`package-lock.json` 锁定精确版本

```bash
cd ~/Documents/PersonalDevopsPlatform

# 创建 Vite + React + TypeScript 项目
npm create vite@latest frontend -- --template react-ts
cd frontend
npm install

# 安装核心依赖
npm install -D tailwindcss postcss autoprefixer
npx tailwindcss init -p

npm install @tanstack/react-query react-router-dom
npm install axios    # 或用原生 fetch，选其一

# 安装 shadcn/ui
npx shadcn-ui@latest init
# 选 Default style，选 Slate 颜色，选 CSS variables
# 安装常用组件
npx shadcn-ui@latest add button input card table badge dialog tabs
```

配置 `vite.config.ts` 代理：
```ts
server: {
  proxy: {
    '/api': 'http://localhost:8080'
  }
}
```

需要编写的文件：
- `src/api/client.ts` — fetch 封装：自动读取 localStorage 中的 API Key 注入 header，401 时跳转 `/login`
- `src/api/types.ts` — TypeScript 类型定义（Organization、Project、Environment、Deployment 等）
- `src/hooks/useAuth.ts` — 读写 localStorage `dpk_token`，提供 `login(key)`、`logout()`、`isAuthenticated`

**今日里程碑验证：**
```
npm run dev → http://localhost:5173 可访问
src/api/client.ts 有完整的 fetch 封装
```

---

### Day 30 — 登录页 + 全局 Layout + 路由

**目标**：输入 API Key 可登录，侧边栏导航可用，未登录自动跳转 `/login`

**核心概念**：
- **React Router 的 `<Outlet>`**：在父路由组件中渲染子路由的位置；Layout 组件包含 Sidebar 和顶栏，中间的内容区放 `<Outlet>`，子页面在这里渲染，实现嵌套路由
- **localStorage vs sessionStorage**：前者关闭浏览器后数据保留，后者关闭后清除；API Key 存 localStorage 让用户刷新页面不需要重新登录
- **React Context**：全局状态共享机制，`useAuth` hook 通过 Context 让所有组件都能访问当前用户信息，避免 prop drilling（逐层传递 props）

需要编写的文件：
- `src/pages/Login.tsx` — 输入框 + 登录按钮，调 `GET /api/v1/me` 验证 key，成功后跳转首页
- `src/components/Layout.tsx` — 顶栏（当前用户名、登出按钮）+ 左侧 Sidebar
- `src/components/Sidebar.tsx` — 导航菜单：Dashboard / Organizations / Promotions / Audit Logs / Admin（platform_admin 才显示）
- `src/App.tsx` — React Router 路由配置，`<PrivateRoute>` 守卫未登录跳转

路由结构：
```
/login                          → Login
/                               → Dashboard（PrivateRoute）
/orgs                           → OrgList
/orgs/:orgId                    → OrgDetail
/orgs/:orgId/projects/:projectId → ProjectDetail
/promotions                     → Promotions
/audit-logs                     → AuditLogs
/admin/users                    → Admin/Users
/admin/clusters                 → Admin/Clusters
/admin/archetypes               → Admin/Archetypes
```

**今日里程碑验证：**
```
未登录访问 / → 自动跳转 /login
输入正确 API Key → 跳转首页，侧边栏可见
点击侧边栏各菜单 → 路由跳转正常（页面暂时空白可接受）
```

---

### Day 31 — Dashboard + Organization 列表/详情

**目标**：Dashboard 展示概览数据，Organization 可以查看、创建

**核心概念**：
- **TanStack Query 的核心概念**：`useQuery` 自动处理数据获取、loading 状态、错误状态和缓存；`queryKey` 是缓存的键，相同 key 的查询共享缓存；`useMutation` 处理写操作并支持 `onSuccess` 回调刷新相关查询
- **React 的声明式 UI**：不手动操作 DOM，而是描述"数据是什么状态时 UI 应该长什么样"；数据变化后 React 自动计算 diff 并更新 DOM（Virtual DOM）
- **shadcn/ui Dialog**：基于 Radix UI 的无障碍弹窗组件，自动处理键盘导航（Esc 关闭、Tab 焦点陷阱）和屏幕阅读器支持，这些细节手写很容易遗漏

需要编写的文件：
- `src/pages/Dashboard.tsx` — 用 TanStack Query 并发请求：Org 数量、最近 5 次部署状态、待审批 Promotion 数量；展示为 3 个统计卡片
- `src/pages/orgs/OrgList.tsx` — Organization 列表表格（名称/slug/成员数），右上角「创建」按钮弹出 Dialog 表单
- `src/pages/orgs/OrgDetail.tsx` — 3 个 Tab：
  - **Environments**：列表展示（名称、namespace、is_protected badge、所属 Cluster），支持创建/删除
  - **Projects**：列表展示（名称、类型、语言、状态），点击进入 ProjectDetail
  - **Members**：成员列表，显示角色，支持添加/修改/移除（org admin 及以上可操作）

**今日里程碑验证：**
```
Dashboard 显示统计数据
Organizations 列表正常加载
创建 Organization → 列表刷新
OrgDetail 3 个 Tab 均有数据
```

---

### Day 32 — Project 详情：Overview + Pipelines + Artifacts

**目标**：Project 详情页的前三个 Tab 完整可用

**核心概念**：
- **React 的 Tab 组件模式**：Tab 本质是条件渲染——根据当前激活的 tab 值显示对应内容；shadcn/ui 的 `<Tabs>` 组件封装了状态管理和 ARIA 属性
- **数据聚合（API Aggregation）**：Project Overview 需要从多个 API 获取数据（deployments、pipeline runs、artifacts），用 `Promise.all` 或 TanStack Query 的并发 `useQuery` 同时发起请求减少等待时间
- **乐观更新（Optimistic Update）**：点击"触发 Pipeline"后不等 API 响应就立刻在 UI 上显示"running"状态；若 API 失败再回滚；TanStack Query 的 `onMutate` + `onError` 实现这个模式

需要编写的文件：
- `src/pages/projects/ProjectDetail.tsx` — Tab 容器（6 个 Tab）
- **Overview Tab**：以卡片网格展示该 Project 在每个 Environment 中的部署状态（版本、replicas ready/desired、健康状态 badge），点击 Environment 卡片进入部署操作
- **Pipelines Tab**：Pipeline 列表（CI/CD），显示最近一次 run 的状态和时间；「手动触发」按钮 → 确认弹窗 → 触发后状态变为 running
- **Artifacts Tab**：Artifact 列表（版本/tag、类型、状态、创建时间），支持删除旧产物

`src/components/StatusBadge.tsx`：根据状态（running/degraded/failed/pending/success）渲染不同颜色的 Badge

**今日里程碑验证：**
```
Overview Tab 展示各 env 的部署状态卡片
Pipelines Tab 手动触发 pipeline → Harness 运行
Artifacts Tab 列表正常显示
```

---

### Day 33 — 部署操作：触发部署 + 回滚 + 实时日志

**目标**：可以通过 UI 触发部署、选策略、回滚，并查看 Pod 日志

**核心概念**：
- **轮询（Polling）**：前端每隔固定时间发一次请求获取最新数据，是实现"实时"效果最简单的方式；TanStack Query 的 `refetchInterval` 参数可以直接开启轮询，停止时设为 `false`
- **SSE vs WebSocket vs 轮询**：SSE（Server-Sent Events）是服务端向客户端单向推送的标准方式，适合日志流；WebSocket 是双向的，适合聊天；轮询最简单但有延迟。本项目日志用轮询，实现简单够用
- **`<pre>` + `overflow-auto`**：展示日志/代码的标准 HTML 方式，保留空白和换行；用 `overflow-auto` 实现滚动，`scrollIntoView` 可以自动滚动到最新一行

需要编写的文件：
- **部署操作弹窗**（在 Overview Tab 的 Environment 卡片上）：
  - 选择 Artifact（下拉列表，显示版本/时间）
  - 选择部署策略（rolling / recreate / blue-green / canary）
  - 确认后调 `POST .../deployments`
  - 若 env is_protected → 提示「请前往 Promotions 创建审批」
- **回滚**：在 Deployment 历史列表中选定历史版本 → 确认 → 调 rollback API
- **Logs Tab**：轮询 `GET .../logs` 每 3 秒刷新一次，展示为 `<pre>` 滚动文本框，「暂停/继续」按钮控制轮询

**今日里程碑验证：**
```
点击部署 → 选 Artifact + 策略 → 确认 → 部署触发
Logs Tab 展示实时 Pod 日志（3 秒自动刷新）
回滚弹窗选历史版本 → 确认 → 回滚触发
```

---

### Day 34 — Env Vars + Config Files

**目标**：环境变量和配置文件的完整管理 UI

**核心概念**：
- **受控组件（Controlled Component）**：表单元素的值由 React state 控制（`value={state}` + `onChange={setState}`）；与非受控组件（用 ref 直接读 DOM）相比，受控组件更容易做验证和联动
- **表格行内编辑模式**：点击单元格切换为 `<input>`，失焦或回车时保存；是管理界面的常见 UX 模式，避免为每行数据打开独立的编辑弹窗

需要编写的文件：
- **Env Vars Tab**：
  - 表格展示所有 key-value，`is_secret=true` 的 value 显示 `***`
  - 表格行内可编辑（非 secret）；右上角「Show Secrets」按钮（org admin 才可见）→ 调 `?reveal=true` → 展示明文
  - 「新增」/ 「删除单个」操作
- **Config Files Tab**：
  - 文件列表（名称、挂载路径、当前版本）
  - 点击文件 → 展开版本历史列表（版本号、备注、时间）
  - 「查看内容」→ 代码预览弹窗（`<pre>` + 语法高亮可选）
  - 「激活版本」按钮，激活后当前版本标记更新
  - 「提交新版本」→ 表单（textarea 输入内容 + 备注）

**今日里程碑验证：**
```
Env Vars Tab 新增/删除变量
Show Secrets 展示明文（需 org admin key）
Config Files 提交新版本 → 激活 → active_version 更新
```

---

### Day 35 — Promotions 审批页 + Audit Logs 页

**目标**：Promotion 审批流程可通过 UI 操作，审计日志可查询

**核心概念**：
- **分页加载**：API 返回 `page`、`limit`、`total` 字段，前端根据 `total/limit` 计算总页数；TanStack Query 的 `keepPreviousData: true` 可以在翻页时保持旧数据显示，避免白屏
- **时间范围选择器**：`<input type="datetime-local">` 是原生 HTML 的日期时间选择器，配合 shadcn/ui 的 `DatePicker` 组件可以有更好的 UX；前端将选择的时间转换为 ISO 8601 格式传给 API

需要编写的文件：
- `src/pages/Promotions.tsx`：
  - 默认展示 `status=pending` 列表（Project 名、目标 env、Artifact 版本、申请人、申请时间）
  - Tab 切换：Pending / All
  - 每行「审批」→ 确认弹窗（可填拒绝原因）→ 调 approve/reject API
  - 「新建 Promotion」表单（选 Project、source env、target env、Artifact）
- `src/pages/AuditLogs.tsx`：
  - 时间范围选择器 + 操作人筛选
  - 只读表格（操作人、操作类型、资源、时间、来源 IP）
  - 分页加载

**今日里程碑验证：**
```
Promotions 列表展示 pending 条目
审批操作 → CD 自动触发
AuditLogs 时间筛选正常工作
```

---

### Day 36 — Admin 页 + 内嵌到 control-api + 最终部署

**目标**：Admin 功能完整，前端内嵌到 Go 二进制，K8s 中运行完整 UI

**核心概念**：
- **Go `//go:embed`**：编译时将指定目录的文件内嵌到 Go 二进制中；`embed.FS` 类型实现了 `fs.FS` 接口，可以直接作为 HTTP 文件服务器；内嵌后单个二进制文件包含所有静态资源，无需额外文件系统
- **SPA Fallback 路由**：前端路由（如 `/orgs/123`）是客户端路由，服务端没有对应文件；需要将所有非 API、非静态文件的路由都返回 `index.html`，让前端 React Router 接管路由
- **Docker 多阶段构建（前后端联合）**：node 阶段构建前端，go 阶段将前端产物 COPY 进来再编译；最终镜像只有 go 阶段的产出，node 环境完全不进入最终镜像

#### 1. Admin 页面

- `src/pages/admin/Users.tsx`：用户列表、创建用户、生成/撤销 API Key（platform_admin 专属）
- `src/pages/admin/Clusters.tsx`：Cluster CRUD
- `src/pages/admin/Archetypes.tsx`：Archetype + 版本管理（与 API 完整对接）

#### 2. 前端内嵌到 control-api

```bash
# vite.config.ts 中设置构建输出目录
# build: { outDir: "../control-api/web/dist" }

cd frontend && npm run build
# control-api/web/dist/ 目录应生成
```

在 `control-api/internal/server/embed.go` 中：
```go
//go:embed ../../web/dist
var WebDist embed.FS
```

在 `router.go` 中注册 SPA fallback（所有非 `/api` 路由返回 `index.html`）

#### 3. 更新 Dockerfile 为多阶段构建

```bash
# 重新构建镜像
docker build -t ghcr.io/<github-username>/control-api:latest .
docker push ghcr.io/<github-username>/control-api:latest

# 滚动更新 K8s
kubectl rollout restart deployment/control-api -n devops-platform
kubectl rollout status deployment/control-api -n devops-platform
```

**今日里程碑验证：**
```
浏览器访问 http://localhost:30080 → 显示登录页
登录后完整 UI 可用
kubectl logs 中无 embed 相关报错
```

---

## Phase 6：Terraform + AWS Fargate（Day 37–44）

---

### Day 37 — Terraform 入门 + AWS 账号配置

**目标**：Terraform 本地可用，AWS 凭证配置完成，能用 Terraform 管理第一个 AWS 资源（S3 bucket）

**核心概念**：
- **Terraform 是什么**：声明式基础设施工具，你描述"想要什么资源"，Terraform 计算当前状态与目标状态的差异，自动执行创建/更新/删除；与 AWS Console 手动点击相比，代码可版本控制、可重复执行
- **HCL（HashiCorp Configuration Language）**：Terraform 的配置语言，语法简洁；`resource "aws_s3_bucket" "artifacts" {}` 声明一个 S3 bucket 资源，Terraform 负责调用 AWS API 创建它
- **Terraform State**：Terraform 在 `terraform.tfstate` 文件中记录它管理的资源的真实状态；下次 `terraform apply` 时与最新配置对比，决定需要做哪些变更；state 文件包含敏感信息，不应提交到 Git
- **Provider**：Terraform 通过 Provider 与各云平台对接，`hashicorp/aws` provider 封装了所有 AWS API 调用；`terraform init` 下载 provider 插件

```bash
brew install terraform
terraform --version

# 配置 AWS 凭证（使用 Day 1 创建的 IAM User 的 Access Key）
brew install awscli
aws configure
# 输入 AWS_ACCESS_KEY_ID、AWS_SECRET_ACCESS_KEY、region（如 ap-northeast-1）

# 验证
aws sts get-caller-identity   # 应返回 IAM User 信息
```

创建第一个 Terraform 配置：

```hcl
# infrastructure/terraform/project-resources/main.tf
terraform {
  required_providers {
    aws = { source = "hashicorp/aws", version = "~> 5.0" }
  }
}

provider "aws" {
  region = var.aws_region
}

resource "aws_s3_bucket" "artifacts" {
  bucket = "${var.org_slug}-artifacts"
}
```

```bash
cd infrastructure/terraform/project-resources
terraform init      # 下载 provider
terraform plan      # 预览将要创建的资源（不实际执行）
terraform apply     # 创建资源（需输入 yes 确认）
terraform destroy   # 删除所有资源（演示用）
```

**今日里程碑验证：**
```
terraform apply 成功
AWS Console → S3 → 看到 bucket 已创建
terraform destroy 后 bucket 消失
```

---

### Day 38 — Terraform 模块：Project 资源 Provision

**目标**：创建 Project 时自动用 Terraform 在 AWS 创建 ECR repo + S3 bucket + IAM Role

**核心概念**：
- **Terraform Module**：可复用的配置单元，类似函数；`module "project_resources" { source = "./modules/project" }` 引用模块，传入变量得到输出；本项目为每个 Project 创建一套独立的 AWS 资源
- **Terraform 变量**：`variable "project_slug" {}` 声明输入变量，`terraform apply -var="project_slug=demo-svc"` 传入；`output "ecr_repo_url" {}` 声明输出值，供调用方使用
- **ECR（Elastic Container Registry）**：AWS 的 Docker 镜像仓库；CI pipeline 构建完镜像后推送到 ECR，CD pipeline 从 ECR 拉取部署；比 ghcr.io 更适合 AWS 生态（IAM 权限集成）
- **IAM Role + Policy**：Role 是权限集合，附加到服务上（EC2、ECS Task）；Policy 定义具体权限（允许向哪个 ECR 推送镜像）；CI pipeline 用 IAM Role 而非 Access Key 是更安全的实践

```hcl
# infrastructure/terraform/modules/project/main.tf
resource "aws_ecr_repository" "this" {
  name = var.project_slug
}

resource "aws_s3_bucket" "artifacts" {
  bucket = "${var.org_slug}-${var.project_slug}-artifacts"
}

resource "aws_iam_role" "ci_role" {
  name               = "${var.project_slug}-ci-role"
  assume_role_policy = data.aws_iam_policy_document.ci_assume.json
}

output "ecr_repo_url" {
  value = aws_ecr_repository.this.repository_url
}
```

在 `project/service.go` 的 Create 方法中新增步骤：
```go
// 调用 terraform apply 并解析 output
output, err := terraformClient.Apply("project-resources", map[string]string{
    "project_slug": project.Slug,
    "org_slug":     org.Slug,
})
project.ECRRepoURL = output["ecr_repo_url"]
```

**今日里程碑验证：**
```
创建 Project → AWS Console 看到同名 ECR repo 和 S3 bucket 被创建
project 记录中 ecr_repo_url 字段有值
```

---

### Day 39 — Terraform State 远程存储 + 工作区

**目标**：Terraform state 存储到 S3（而非本地文件），支持多环境隔离

**核心概念**：
- **Remote State（远程状态）**：将 state 文件存储在 S3 + DynamoDB（锁）；多人协作时防止并发 apply 导致 state 损坏；本项目单人使用，但这是生产环境的标准实践，值得学习
- **S3 Backend**：Terraform 的 backend 配置告诉它把 state 存在哪；S3 存文件，DynamoDB 提供分布式锁（防止两个 `terraform apply` 同时运行）
- **Terraform Workspace**：在同一套配置下管理多个独立的 state，类似 Git branch；`terraform workspace new staging` 创建新工作区，适合用同一套 Terraform 代码管理 dev/staging/prod 多个环境

```hcl
# infrastructure/terraform/backend.tf
terraform {
  backend "s3" {
    bucket         = "my-terraform-state"
    key            = "devops-platform/terraform.tfstate"
    region         = "ap-northeast-1"
    dynamodb_table = "terraform-lock"
    encrypt        = true
  }
}
```

```bash
# 创建 state 存储资源（bootstrap，只需一次）
aws s3 mb s3://my-terraform-state
aws dynamodb create-table --table-name terraform-lock \
  --attribute-definitions AttributeName=LockID,AttributeType=S \
  --key-schema AttributeName=LockID,KeyType=HASH \
  --billing-mode PAY_PER_REQUEST

terraform init   # 重新初始化，迁移 state 到 S3
```

**今日里程碑验证：**
```
terraform apply 后 state 文件出现在 S3 而非本地
terraform workspace list 显示 default 工作区
```

---

### Day 40 — ECS Fargate 基础设施（用 Terraform 创建）

**目标**：用 Terraform 在 AWS 创建一套完整的 ECS Fargate 运行环境

**核心概念**：
- **ECS 核心概念三件套**：Cluster（逻辑分组）、Task Definition（容器规格定义，类似 K8s Pod spec，描述镜像、CPU、内存、环境变量）、Service（保证指定数量的 Task 持续运行，类似 K8s Deployment）
- **Fargate Launch Type**：ECS 的无服务器模式，不需要管理 EC2 Node；AWS 自动分配计算资源运行 Task；与 K8s 相比，Fargate 更简单但定制性弱
- **VPC + 子网 + 安全组**：VPC（Virtual Private Cloud）是 AWS 内的私有网络；子网（Subnet）将 VPC 划分为更小网段；安全组（Security Group）是状态化防火墙，控制入/出站流量；ECS Task 运行在子网里，安全组控制它能接受哪些端口的访问
- **ALB（Application Load Balancer）**：7层负载均衡器，将外部 HTTP/HTTPS 请求分发到 ECS Task；Target Group 注册 Task 的 IP 和端口，ALB 根据健康检查自动管理

```hcl
# infrastructure/terraform/fargate-cluster/main.tf
module "vpc" {
  source  = "terraform-aws-modules/vpc/aws"
  version = "~> 5.0"
  cidr    = "10.0.0.0/16"
  azs     = ["ap-northeast-1a", "ap-northeast-1c"]
  public_subnets  = ["10.0.1.0/24", "10.0.2.0/24"]
  private_subnets = ["10.0.11.0/24", "10.0.12.0/24"]
}

resource "aws_ecs_cluster" "this" {
  name = var.cluster_name
}

resource "aws_lb" "this" {
  name               = "${var.cluster_name}-alb"
  internal           = false
  load_balancer_type = "application"
  subnets            = module.vpc.public_subnets
}

output "ecs_cluster_arn" { value = aws_ecs_cluster.this.arn }
output "alb_dns_name"    { value = aws_lb.this.dns_name }
output "vpc_id"          { value = module.vpc.vpc_id }
output "private_subnets" { value = module.vpc.private_subnets }
```

```bash
terraform apply
# 大约 5-10 分钟，AWS 在创建 VPC、ALB 等资源
```

**今日里程碑验证：**
```
terraform apply 成功（约 5-10 分钟）
AWS Console → ECS → Clusters → 看到新建的集群
AWS Console → EC2 → Load Balancers → 看到 ALB
```

---

### Day 41 — Cluster 概念扩展：支持 ecs-fargate 类型

**目标**：control-api 的 Cluster domain 支持 `type: "ecs-fargate"`，创建 Fargate Cluster 时触发 Terraform

**核心概念**：
- **策略模式（Strategy Pattern）**：针对不同 Cluster 类型选择不同的部署实现；`DeploymentStrategy` interface 有 `Deploy()`、`GetStatus()` 两个方法，`K8sStrategy` 和 `FargateStrategy` 分别实现；`main.go` 根据 `cluster.Type` 注入对应策略
- **AWS SDK for Go（v2）**：AWS 官方 Go 客户端，与 client-go 类似但针对 AWS 服务；`aws.Config` 包含凭证和 region，通过 `aws.LoadDefaultConfig()` 从环境变量或 IAM Role 自动加载

```bash
cd control-api
go get github.com/aws/aws-sdk-go-v2/config
go get github.com/aws/aws-sdk-go-v2/service/ecs
go get github.com/aws/aws-sdk-go-v2/service/ecr
```

更新 `clusters` schema 新增字段：
```
type,                        # "k8s" | "ecs-fargate"（新增）
aws_region,                  # 如 "ap-northeast-1"（ecs-fargate 专用）
ecs_cluster_arn,             # Terraform output 写入
alb_dns_name,                # ALB 地址（服务对外入口）
vpc_id, private_subnet_ids,  # Fargate Task 运行的网络位置
```

新增 `internal/clients/aws/ecs_client.go`：
```go
type ECSClient interface {
    DeployService(ctx context.Context, req DeployRequest) error
    GetServiceStatus(ctx context.Context, cluster, service string) (*ServiceStatus, error)
}
```

**今日里程碑验证：**
```
POST /api/v1/clusters 时 type=ecs-fargate → Terraform apply 自动执行
Cluster 记录中 ecs_cluster_arn 字段有值
```

---

### Day 42 — 部署 Project 到 Fargate（ECS Service 创建/更新）

**目标**：向 Fargate 类型的 Environment 触发部署时，control-api 调用 ECS API 创建/更新 ECS Service

**核心概念**：
- **Task Definition**：ECS 中描述如何运行容器的规格，类似 K8s 的 Pod spec；包含：镜像 URL、CPU/内存、环境变量、IAM execution role、日志配置；每次部署新镜像需要注册新版本的 Task Definition
- **ECS Service 更新流程**：注册新 Task Definition → 更新 Service 指向新 Task Definition → ECS 滚动替换旧 Task（类似 K8s rolling update）
- **CloudWatch Logs**：AWS 的日志服务；ECS Task 的标准输出自动发送到 CloudWatch Log Group；control-api 的"查看日志"功能对 Fargate 类型需要调用 CloudWatch API 而非 K8s API

```go
// internal/clients/aws/ecs_client.go
func (c *ecsClient) DeployService(ctx context.Context, req DeployRequest) error {
    // 1. 注册新 Task Definition（新镜像 tag）
    taskDefArn, err := c.registerTaskDefinition(ctx, req)
    // 2. 检查 ECS Service 是否存在，不存在则创建
    _, err = c.ecsClient.DescribeServices(...)
    if serviceNotExists {
        _, err = c.ecsClient.CreateService(...)
    } else {
        // 3. 更新 Service 指向新 Task Definition
        _, err = c.ecsClient.UpdateService(...)
    }
    return err
}
```

在 `deployment/service.go` 中根据 cluster type 分支：
```go
switch cluster.Type {
case "k8s":
    return s.k8sClient.Deploy(ctx, req)
case "ecs-fargate":
    return s.ecsClient.DeployService(ctx, req)
}
```

**今日里程碑验证：**
```
向 Fargate Environment 触发部署
AWS Console → ECS → Clusters → Services → 看到新建的 Service
Service 状态变为 RUNNING，Task 健康
```

---

### Day 43 — Fargate 服务状态查询 + 日志

**目标**：`GET .../deployment` 对 Fargate 类型返回真实 ECS 状态；Logs Tab 对 Fargate 查 CloudWatch

**核心概念**：
- **ECS Service 状态字段**：`runningCount`（运行中 Task 数）、`desiredCount`（期望 Task 数）、`pendingCount`（启动中 Task 数）；与 K8s 的 `availableReplicas/replicas` 对应，可以用同样的逻辑推导健康状态
- **CloudWatch Logs Insights**：可以用 SQL-like 语法查询日志；简单场景用 `GetLogEvents` API 按时间范围获取日志流；ECS Task 的日志 group 格式为 `/ecs/{task-definition-family}`
- **接口统一（Adapter Pattern）**：K8s 和 Fargate 的状态查询返回不同的数据结构，在 client 层将两者都转换为统一的 `ServiceStatus` 结构体；上层 deployment service 无需关心底层差异

更新 `deployment/service.go` 的 GetCurrent 方法：
```go
switch cluster.Type {
case "k8s":
    status, _ = s.k8sClient.GetDeploymentStatus(ns, name)
case "ecs-fargate":
    status, _ = s.ecsClient.GetServiceStatus(cluster.ECSClusterARN, serviceName)
}
// 统一转换为 ServiceStatus{Desired, Available, Status}
```

**今日里程碑验证：**
```
GET .../deployment（Fargate env）→ 返回 ECS 实时 running/desired 状态
GET .../logs（Fargate env）→ 返回 CloudWatch 最近 100 行日志
```

---

### Day 44 — 端到端：Fargate 完整部署流程 + 收尾

**目标**：完整走通 git push → CI 构建推送 ECR → 部署到 Fargate → 通过 ALB 访问服务

**核心概念**：
- **ECR 镜像推送认证**：与 ghcr.io 不同，ECR 使用临时 token 认证（通过 `aws ecr get-login-password` 获取，有效期 12 小时）；CI pipeline 需要配置 IAM Role 或 Access Key 以获取推送权限
- **ALB Target Group 健康检查**：ALB 定期向注册的 Target（ECS Task IP）发送健康检查请求；只有通过健康检查的 Target 才会接收流量；新部署的 Task 需要通过健康检查后才会被 ALB 纳入
- **Fargate vs K8s 对比**：Fargate 更简单（无需管 Node、无需配 Ingress），AWS 托管程度高；K8s 更灵活（可以用所有 K8s 生态工具），但运维复杂度高；两者并存是很多公司的选择（本地/dev 用 K8s，生产用 Fargate 或 EKS）

完整流程验证：
```bash
# 1. 创建 Fargate 类型 Cluster（触发 Terraform）
# 2. 创建 Organization + Fargate Environment
# 3. 创建 Project（触发 ECR repo 创建）
# 4. git push → CI 构建 → 推送到 ECR
# 5. 触发部署到 Fargate → ECS Service 创建
# 6. 等待 Task Running → 通过 ALB DNS 访问服务
curl http://<alb-dns-name>/healthz   # 访问部署在 Fargate 上的服务
```

**今日里程碑验证：**
```
curl http://<alb-dns-name>/healthz → 返回 {"status":"ok"}
AWS Console ECS Service 状态 ACTIVE，runningCount == desiredCount
全平台两种 Cluster 类型（K8s + Fargate）同时运行
```

---

## 里程碑总览

| Day | 里程碑 |
|---|---|
| 1 | GitHub / AWS / Harness 账号就绪，Go / OrbStack / Helm 可用 |
| 2 | MongoDB 运行在 K8s，可连接 |
| 3 | 项目 repo 在 GitHub，ngrok 可用 |
| 4 | `GET /healthz` 返回 200，MongoDB 连接正常 |
| 5 | API Key 鉴权可用，Bootstrap 管理员自动创建 |
| 6 | 401 / 403 权限控制生效 |
| 7 | Archetype 含版本管理的完整 CRUD |
| 8 | Organization + 成员管理 CRUD |
| 9 | 创建 / 删除 Environment 同步 K8s namespace |
| 10 | control-api 以 Pod 运行在 OrbStack K8s |
| 11 | GitHub Webhook 自动创建 |
| 12 | GitHub push 事件被 control-api 接收 |
| 13 | 创建 Project 自动在 GitHub 建 repo + webhook |
| 14 | git push 端到端 webhook 接收验证 |
| 15 | Harness Delegate 在线 |
| 16 | Harness API 调用成功 |
| 17 | 创建 Project 自动在 Harness 建 project + pipeline |
| 18 | git push → CI 自动触发 → Artifact 创建 |
| 19 | 手动触发 CD → K8s 部署成功 |
| 20 | 回滚可用；Secret 加密存储 |
| 21 | Promotion 审批流阻断 protected env 的直接部署 |
| 22 | env-vars + config-files 完整 CRUD |
| 23 | 关键事件自动发送 Webhook 通知 |
| 24 | 写操作自动记录审计日志 |
| 25 | Deployment API 返回 K8s 实时 replica 状态 |
| 26 | Cloudflare Tunnel 替代 ngrok（地址固定） |
| 27 | 全链路端到端测试通过 |
| 28 | 单元测试全绿，文档与实现一致 |
| 29 | 前端项目启动，API client 基础结构完成 |
| 30 | 登录页可用，侧边栏路由正常 |
| 31 | Dashboard 数据展示，Organization 增删查完整 |
| 32 | Project 详情：Overview / Pipelines / Artifacts 可用 |
| 33 | UI 可触发部署、回滚，Logs Tab 实时刷新 |
| 34 | Env Vars + Config Files 完整管理 UI 可用 |
| 35 | Promotions 审批 + AuditLogs 查询可用 |
| 36 | Admin 页完整，前端内嵌 Go 二进制，K8s 访问正常 |
| 37 | Terraform 可用，第一个 AWS S3 bucket 用 Terraform 创建 |
| 38 | 创建 Project 自动 Terraform provision ECR + S3 + IAM |
| 39 | Terraform state 存储到 S3，多环境隔离 |
| 40 | ECS Fargate 基础设施（VPC + ALB + Cluster）用 Terraform 创建 |
| 41 | Cluster 支持 ecs-fargate 类型，创建时触发 Terraform |
| 42 | 部署到 Fargate：ECS Service 自动创建/更新 |
| 43 | Fargate 服务状态 + CloudWatch 日志查询可用 |
| 44 | 全链路 Fargate 部署：git push → ECR → ECS → ALB 访问 |
