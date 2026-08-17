# GoCommunity: 高并发资源社区平台

GoCommunity 是一个用 Go + Vue 实现的资源分享社区系统，覆盖用户认证、资源发布、Feed 分发、点赞评论收藏、关注关系、积分激励、异步任务和可观测性。项目同时提供 Prometheus / Grafana 配置，可作为 [OnCallAgent](https://github.com/changhen2004/OncallAgent) 的真实业务告警与排障数据源。

## 功能特性

- 用户认证：注册、登录、Access / Refresh 双 Token、刷新续期、登出后通过 Token Version 令旧 Token 失效。
- 资源内容：资源发布、草稿 / 发布 / 归档状态、封面上传、内容图片上传、分类标签、关键词搜索、详情访问控制。
- Feed 分发：最新流、热门流、关注流；关注流使用 `(created_at, id)` 游标分页。
- 互动体系：点赞、评论、收藏、作者关注关系。
- 积分体系：每日签到、发布奖励、优质互动奖励、积分解锁付费资源、特权兑换。
- 异步任务：RabbitMQ 发布浏览、点赞、评论、收藏等事件，独立 Worker 消费并更新热度、积分和统计。
- 缓存治理：Redis 详情缓存、列表缓存、热榜 ZSet、点赞数缓存、关注流缓存、积分摘要缓存、限流计数和消费幂等。
- 可观测性：`/metrics` Prometheus 指标、Grafana 预置仪表盘、告警规则、`/healthz` 健康检查、慢请求日志、可选 pprof。
- 前端体验：Vue 3 + Vite + TypeScript + Pinia + Element Plus，覆盖登录注册、资源列表、详情、发布、个人中心等页面。

## 架构

```text
Vue3 前端
   |
   | REST API + JWT
   v
Gin HTTP API
   |-- MySQL 8.4: 用户、资源、评论、收藏、积分、解锁记录
   |-- Redis 7: 缓存、热榜、限流、消费幂等
   |-- RabbitMQ: 异步任务交换机和队列
             |
             v
          Worker: 热度、积分、统计回写

Gin /metrics --> Prometheus --> Grafana / 告警规则 --> OnCallAgent 可读取告警
```

## 技术栈

| 分层 | 技术 |
|---|---|
| 后端 | Go 1.25、Gin、GORM、Viper |
| 存储 | MySQL 8.4、Redis 7 |
| 消息 | RabbitMQ direct exchange |
| 认证 | JWT、bcrypt、Token Version |
| 前端 | Vue 3、Vite、TypeScript、Pinia、Element Plus、Axios |
| 可观测 | Prometheus、Grafana、pprof |
| 测试 | Go test、miniredis、SQLite、Vitest |
| 部署 | Docker Compose |

## 快速开始

```bash
cp .env.example .env
docker compose up --build
```

Docker Compose 会启动 MySQL、Redis、RabbitMQ、后端 API、Worker、前端、Prometheus 和 Grafana。

| 服务 | 地址 |
|---|---|
| 前端 | http://localhost:5173 |
| 后端 API | http://localhost:8080 |
| 后端健康检查 | http://localhost:8080/healthz |
| 后端指标 | http://localhost:8080/metrics |
| Prometheus | http://localhost:9091 |
| Grafana | http://localhost:3001，默认 `admin/admin` |
| RabbitMQ 管理台 | http://localhost:15674，默认 `guest/guest` |

后端容器内部监听 `3000`，Compose 映射到宿主机 `8080`。前端开发服务通过 Vite 代理 `/api` 和 `/uploads` 到后端。

## 本地开发

后端需要在 `backend/` 目录运行，并读取 `backend/config/config.yaml`。如果使用本机依赖服务，请按需调整 DSN、Redis 和 RabbitMQ 地址，或通过 `RESOURCE_COMMUNITY_GO_*` 环境变量覆盖。

```bash
cd backend
go mod download
go run .
go run ./cmd/worker
```

前端：

```bash
cd frontend
npm ci
npm run dev
```

常用环境变量见 `.env.example`，包括 `RESOURCE_COMMUNITY_GO_DATABASE_DSN`、`RESOURCE_COMMUNITY_GO_REDIS_ADDR`、`RESOURCE_COMMUNITY_GO_RABBITMQ_URL`、`RESOURCE_COMMUNITY_GO_JWT_SECRET` 和 `RESOURCE_COMMUNITY_GO_UPLOAD_DIR`。

## API 概览

| 方法 | 路径 | 说明 |
|---|---|---|
| `GET` | `/healthz` | 健康检查 |
| `GET` | `/metrics` | Prometheus 指标 |
| `POST` | `/api/auth/register` | 注册 |
| `POST` | `/api/auth/login` | 登录 |
| `POST` | `/api/auth/refresh` | Refresh Token 续期 |
| `POST` | `/api/auth/logout` | 登出，需要认证 |
| `GET` | `/api/articles` | 资源列表，支持 `page`、`pageSize`、`sort`、`keyword`、`tag` |
| `GET` | `/api/articles/hot` | 热门资源 |
| `GET` | `/api/articles/:id` | 资源详情，可选认证以判断付费内容是否已解锁 |
| `POST` | `/api/articles` | 发布资源，需要认证 |
| `POST` / `DELETE` | `/api/articles/:id/like` | 点赞 / 取消点赞 |
| `GET` | `/api/articles/:id/like` | 点赞数 |
| `GET` / `POST` | `/api/articles/:id/comments` | 评论列表 / 创建评论 |
| `DELETE` | `/api/comments/:id` | 删除评论 |
| `POST` / `DELETE` | `/api/articles/:id/favorite` | 收藏 / 取消收藏 |
| `GET` | `/api/me/favorites` | 我的收藏 |
| `POST` / `DELETE` | `/api/authors/:id/follow` | 关注 / 取消关注作者 |
| `GET` | `/api/authors/:id/social-status` | 作者关注状态 |
| `GET` | `/api/me/following/articles` | 关注流 |
| `GET` | `/api/me/points` | 积分摘要 |
| `GET` | `/api/me/points/records` | 积分流水 |
| `POST` | `/api/me/check-in` | 每日签到 |
| `POST` | `/api/articles/:id/unlock` | 积分解锁资源 |
| `POST` | `/api/me/points/redeem` | 兑换特权 |
| `POST` | `/api/uploads/cover` | 上传封面 |
| `POST` | `/api/uploads/content-images` | 上传内容图片 |

## 核心实现

### 缓存与热点治理

资源详情采用 `Redis -> MySQL -> 回填 Redis` 的读路径。不存在的资源会写入短 TTL 空值，避免缓存穿透；`JitterTTL` 在基础 TTL 上增加最多 20% 抖动，降低集中失效风险；详情缓存回源用进程内请求合并，热点 Key 失效时同一资源只放行一次数据库查询。

列表、热门、关注流和积分摘要均有独立缓存 Key。发布、点赞、评论、收藏、解锁等写入路径会删除相关缓存，保证读侧最终一致。

热门资源基于 Redis ZSet，初始分数为 `50 + created_at/86400`，互动权重为浏览 `+1`、点赞 `+8`、评论 `+12`、收藏 `+10`。Worker 在消费事件后更新热榜和统计。

### 异步任务

API 通过 RabbitMQ 发布以下任务：`article.published`、`article.viewed`、`article.liked`、`comment.created`、`comment.deleted`、`favorite.created`、`favorite.deleted`。

Worker 使用 Redis 幂等存储标记任务处理状态。消费失败时根据 `x-retry-count` 最多重试 3 次，之后投递到 `<queue>.dlq`，并写入 `x-failure-reason`。当前主进程和 Worker 启动都依赖 RabbitMQ 可连接，RabbitMQ 初始化失败会退出。

### 安全与限流

密码使用 bcrypt 哈希；JWT 中携带 Token Version，登出时递增用户版本号，使旧 Access / Refresh Token 失效。Redis 固定窗口限流覆盖注册、登录、发布、评论和签到，超限返回 `429` 并尽量写入 `Retry-After`。

### 可观测性

后端暴露 `resource_community_http_requests_total` 和 `resource_community_http_request_duration_seconds`。指标路径标签使用 Gin 路由模板，例如 `/api/articles/:id`，避免动态 ID 造成高基数。Prometheus 告警覆盖后端不可抓取、1 分钟 5xx 错误率高于 5%、整体 P95 延迟高于 500ms。

## 与 OnCallAgent 联动

GoCommunity 的 Prometheus 告警和运维场景可直接被 OnCallAgent 用于演示：

```text
GoCommunity 指标和告警 -> Prometheus -> OnCallAgent /plan -> RAG runbook 命中和排障建议
```

OnCallAgent 仓库内置了以下 runbook：

| 故障场景 | runbook |
|---|---|
| 接口 P95 升高 | `resource-community-p95-latency.md` |
| 错误率升高 | `resource-community-error-rate.md` |
| 热榜异常 | `resource-community-hot-ranking.md` |
| RabbitMQ 积压 | `resource-community-rabbitmq-backlog.md` |

## 压测记录

已有压测证据保存在 `docs/benchmark.md` 和 `docs/evidence/hot-detail-benchmark-20260723-180223.md`。文档中分别记录了列表接口 wrk 客户端口径，以及热点详情接口服务端 Prometheus 口径。引用性能数据时请标明口径，避免混用客户端排队耗时和服务端处理耗时。

## 测试与 CI

```bash
cd backend
go test ./...

cd ../frontend
npm ci
npm run test
npm run build
```

GitHub Actions 会执行后端测试、前端构建、`docker compose config` 校验，以及前后端镜像构建检查。

可观测性演练脚本位于 `scripts/observability_drill.sh`，配套测试为 `scripts/test_observability_drill.sh`。

## 项目结构

```text
GoCommunity/
├── backend/
│   ├── cmd/worker/          # Worker 进程入口
│   ├── config/              # 配置加载、MySQL、Redis、RabbitMQ、迁移
│   ├── internal/
│   │   ├── app/             # 路由、鉴权、限流、指标、观测中间件
│   │   ├── auth/            # 用户认证
│   │   ├── article/         # 资源、Feed、热榜、详情缓存
│   │   ├── asyncjob/        # 异步任务类型和发布器
│   │   ├── cachekey/        # Redis Key 和 TTL 策略
│   │   ├── comment/         # 评论
│   │   ├── favorite/        # 收藏
│   │   ├── media/           # 上传
│   │   ├── points/          # 积分、解锁、特权
│   │   ├── social/          # 关注关系
│   │   └── worker/          # 消费、重试、死信、幂等
│   └── utils/               # JWT 和密码工具
├── frontend/                # Vue 3 前端
├── observability/           # Prometheus / Grafana 配置
├── docs/                    # 压测和证据文档
├── scripts/                 # 演练脚本
├── .github/workflows/       # CI
└── docker-compose.yml
```

## 后续方向

- Redis Cluster 或本地多级缓存。
- MySQL 读写分离和冷热数据归档。
- OpenTelemetry 链路追踪。
- Kubernetes 部署和弹性伸缩。
- 与 OnCallAgent 打通告警自动分析、处置和复盘闭环。
