# GoCommunity — 高并发资源社区平台

基于 Go 实现的资源分享社区系统，覆盖“内容生产 → Feed 分发 → 互动反馈 → 积分激励”的完整业务闭环，针对热点缓存、消息可靠性与服务可观测性做了工程化设计，前端使用 Vue3 + TypeScript 提供完整可交互界面。

> **联动项目**：本项目的 Prometheus 告警与排障文档可作为 [OncallAgent](https://github.com/changhen2004/OncallAgent)（AIOps 排障 Agent）的真实业务数据源，详见[与 OncallAgent 联动](#与-oncallagent-联动)。

## 功能特性

- **用户认证**：注册 / 登录 / JWT 双 Token（Access + Refresh）续期 / Token Version 登出即失效
- **内容服务**：资源发布、列表、详情、分类与标签、关键词搜索、积分解锁付费资源
- **Feed 流**：最新流、关注流、热门流三种内容分发
- **互动体系**：点赞、评论、收藏、作者关注关系
- **积分体系**：每日签到、发布奖励、优质互动奖励、积分解锁资源、特权兑换
- **异步任务**：浏览 / 点赞 / 评论 / 收藏等非核心链路通过 RabbitMQ 异步化，Worker 独立进程消费
- **可观测性**：Prometheus 指标、Grafana 预置仪表盘、告警规则、pprof、慢请求日志
- **工程化**：Docker Compose 一键启动、GitHub Actions CI、健康检查、前端 Vitest 测试

## 架构总览

```text
┌──────────────────────────┐
│  Vue3 + Vite + TS 前端     │
└────────────┬─────────────┘
             │ REST API + JWT
┌────────────▼───────────────────────────────┐
│            Gin HTTP API（backend）          │
│  鉴权 → 限流 → 业务模块 → 读缓存 → 异步发布   │
└──────┬──────────────────────┬──────────────┘
       │                      │
┌──────▼───────┐     ┌────────▼──────────────────┐
│  MySQL 8.4   │     │  Redis 7                   │
│ 业务数据主存储 │     │ 缓存 / 热榜 ZSet / 限流 / 幂等 │
└──────────────┘     └───────────────────────────┘
       │ 发布异步事件
┌──────▼───────────────────────┐
│  RabbitMQ（direct exchange）   │
└──────┬───────────────────────┘
       │ 消费
┌──────▼──────────────────┐
│  Worker（独立进程）        │
│ 热度 / 积分 / 统计 → 回写 DB │
└─────────────────────────┘

backend ──/metrics──▶ Prometheus ──▶ Grafana（告警规则 + 仪表盘）
```

## 技术栈

| 分层 | 技术 | 用途 |
|---|---|---|
| 后端 | Go 1.25 / Gin | 服务端与 HTTP API 框架 |
| 存储 | MySQL 8.4 + GORM | 业务数据持久化与 ORM |
| 缓存 | Redis 7 | 缓存、热榜、限流、消息幂等 |
| 消息 | RabbitMQ | 异步任务解耦 |
| 认证 | JWT + bcrypt | 双 Token 鉴权、密码哈希 |
| 可观测 | Prometheus + Grafana | 指标采集与可视化 |
| 前端 | Vue3 + Vite + TypeScript + Pinia + Element Plus | 管理端界面 |
| 部署 | Docker Compose | 一键编排全部服务 |

## 快速开始

```bash
cp .env.example .env        # 按需修改端口与密钥
docker compose up --build
```

| 服务 | 地址 |
|---|---|
| 前端 | http://localhost:5173 |
| 后端 API | http://localhost:8080 |
| Prometheus | http://localhost:9091 |
| Grafana（admin/admin） | http://localhost:3001 |
| RabbitMQ 管理台 | http://localhost:15674 |

## 核心设计

### 1. 缓存治理：热点详情的“三防”与失效策略

资源详情是典型读多写少场景，读取链路为 `Redis → Miss → 回源 MySQL → 回填`，并针对三种缓存异常分别治理：

- **防穿透**：不存在的资源 ID 写入空值缓存 `__NULL__`（TTL 30s + 抖动），避免恶意请求直接打到 MySQL
- **防雪崩**：`JitterTTL(base)` 在基础 TTL 上叠加 0~20% 随机偏移，避免大量 Key 同时过期（详情 10min、列表 5min、热榜 3min）
- **防击穿**：进程内 singleflight（`cacheFills map + Mutex`）合并同一 Key 的并发回源，热 Key 失效时只放行一次 DB 查询，其余请求等待并共享结果

写入侧采用**按前缀失效**：发布 / 互动后删除 `articles:list:*`、`articles:hot:*`、`articles:detail:*` 等缓存，保证最终一致，避免脏数据长期驻留。

### 2. 异步任务与消息可靠性

点赞、评论、浏览等行为不阻塞主链路：API 先更新状态并发布事件到 RabbitMQ，由独立 Worker 进程消费，负责热度累加、积分发放与统计更新。

- **有限重试**：消费失败按 `x-retry-count` 重投，超过 3 次进入死信队列
- **死信治理**：`<queue>.dlq` 队列保存原始消息、失败原因（`x-failure-reason`）与重试次数，供人工介入
- **幂等消费**：Redis `SETNX(jobID)` 标记 `processing`（10min）/ `done`（24h），消息重投不会重复加分、重复加热度
- **降级**：RabbitMQ 不可用时回退 `NoopPublisher` 同步执行，保证核心链路不中断

### 3. Feed 分发与游标分页

- **最新流**：按发布时间倒序；**热门流**：基于 Redis ZSet，score = 初始热度 + 浏览×1 + 点赞×8 + 评论×12 + 收藏×10，Worker 异步累加，读侧 `ZREVRANGE` 后批量回填
- **关注流**：采用 `(created_at, id)` 游标分页替代 offset 深分页，多取 1 条判断 `hasMore` 并返回 `nextCursor`；按用户维度缓存 45s，兼顾实时性与一致性

### 4. 安全与限流

- 密码 bcrypt 哈希；JWT 双 Token，Refresh 续期；引入 **Token Version**：登出时递增版本号，旧 Access/Refresh Token 立即失效，弥补 JWT 难以主动吊销的短板
- Redis 固定窗口限流（超限返回 429 + `Retry-After`）：注册 5 次/分/IP、登录 10 次/分/IP、发布 10 次/分/用户、评论 20 次/分/用户、签到 2 次/分/用户

### 5. 可观测性

- `/metrics` 暴露请求总数 Counter 与延迟直方图；`path` 标签使用 Gin 路由模板（如 `/api/articles/:id`），避免动态 ID 造成高基数
- 预置告警规则：后端宕机（critical）、1 分钟 5xx 错误率 > 5%、整体 P95 延迟 > 500ms
- 慢请求日志（默认 > 500ms 记 WARN）、可选 pprof、`/healthz` 健康检查

## 压测结果

环境：本地 Docker Compose 单机；工具：`wrk`。

```bash
wrk -t4 -c100 -d60s --latency "http://localhost:8080/api/articles?page=1&pageSize=10"
```

| 指标 | 结果 |
|---|---|
| 总请求数 | 72000 |
| QPS | 1200 |
| 平均延迟 | 8.31ms |
| P95 延迟 | 20ms |

> 详细记录与复现说明见 [docs/benchmark.md](docs/benchmark.md)。压测数据以该文档为准，勿在简历 / README 中夸大。

## 与 OncallAgent 联动

项目提供“真实业务监控 → 告警 → 排障”的闭环数据源：

```text
GoCommunity ──Prometheus 告警──▶ OncallAgent ──RAG 知识库 + Agent──▶ 排障建议
```

| 故障场景 | 对应 runbook |
|---|---|
| 接口 P95 升高 | `resource-community-p95-latency` |
| 错误率升高 | `resource-community-error-rate` |
| 热榜异常 | `resource-community-hot-ranking` |
| RabbitMQ 积压 | `resource-community-rabbitmq-backlog` |

## 项目结构

```text
GoCommunity/
├── backend/                 # Go 后端
│   ├── cmd/worker/          # Worker 独立进程入口
│   ├── config/              # 配置加载、DB/Redis/RabbitMQ 初始化、迁移
│   ├── internal/
│   │   ├── app/             # 路由、鉴权中间件、限流、指标、观测日志
│   │   ├── auth/            # 用户认证（双 Token + Token Version）
│   │   ├── article/         # 内容、Feed、热榜、缓存治理
│   │   ├── comment/         # 评论
│   │   ├── favorite/        # 收藏
│   │   ├── social/          # 关注关系
│   │   ├── points/          # 积分、解锁、特权兑换
│   │   ├── media/           # 封面 / 内容图片上传
│   │   ├── asyncjob/        # 异步任务定义与 Publisher
│   │   ├── worker/          # 消费、重试、死信、幂等
│   │   └── cachekey/        # 缓存 Key 规范与 TTL 策略
│   └── utils/               # JWT、密码哈希
├── frontend/                # Vue3 + Vite + TS
├── observability/           # Prometheus / Grafana 配置、告警规则、仪表盘
├── scripts/                 # 压测演练与报告生成脚本
├── docs/                    # benchmark 等文档
├── .github/workflows/       # CI
└── docker-compose.yml
```

## 测试与 CI

- 前端：Vitest 测试覆盖 App 渲染、路由、文章接口（`frontend/src/**/*.spec.ts`）
- CI（GitHub Actions）：`go test ./...`、`npm run build`、`docker compose config` 校验、前后端镜像构建检查
- 可观测性演练：`scripts/observability_drill.sh` 生成基础流量并从 Prometheus 拉取 QPS / P50 / P95 / 错误率报告

## 后续优化方向

- 缓存集群化：Redis Cluster、本地多级缓存（如 freecache）
- 存储扩展：MySQL 读写分离、冷热数据归档
- 链路追踪：接入 OpenTelemetry 实现跨服务 Trace
- 部署演进：Kubernetes + HPA 弹性伸缩
- AIOps：与 OncallAgent 打通告警自动恢复闭环