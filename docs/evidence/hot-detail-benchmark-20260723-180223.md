# 热点详情接口 wrk 压测报告

## 压测目标

- 接口：`GET /api/articles/1`
- 说明：通过 `GET /api/articles/hot?limit=1` 获取当前热榜资源 ID 后，对该资源详情接口进行压测。
- 工具：`wrk`
- 环境：本地 Docker Compose

## 压测命令

```bash
wrk -t4 -c100 -d60s --latency http://localhost:8080/api/articles/1
```

## wrk 原始输出

```text
Running 1m test @ http://localhost:8080/api/articles/1
  4 threads and 100 connections
  Thread Stats   Avg      Stdev     Max   +/- Stdev
    Latency    89.27ms  230.39ms   1.62s    90.59%
    Req/Sec     7.22k     2.99k   12.79k    69.09%
  Latency Distribution
     50%    3.14ms
     75%   19.52ms
     90%  295.03ms
     99%    1.16s
  1404205 requests in 1.03m, 1.07GB read
Requests/sec:  22823.14
Transfer/sec:     17.83MB
```

## 指标采集口径

- 应用层：Prometheus 指标 `resource_community_http_requests_total` 和 `resource_community_http_request_duration_seconds_bucket`，使用压测前后差值计算。
- Redis：容器内执行 `redis-cli INFO stats` 和 `redis-cli INFO commandstats`，使用压测前后差值计算。
- MySQL：执行 `SHOW GLOBAL STATUS`，统计 `Questions`、`Slow_queries`、`Threads_connected` 和 `Connections`。

## 应用层指标

| 指标 | 结果 |
|---|---:|
| QPS | 22721.76 |
| P50 | 2.64ms |
| P95 | 5.29ms |
| P99 | 9.81ms |
| 5xx 数量 | 0 |
| 5xx 比例 | 0.00% |

## Redis 指标

| 指标 | 结果 |
|---|---:|
| keyspace hits | 1399388 |
| keyspace misses | 5052 |
| 命中率 | 99.64% |
| OPS | 28573.12 |
| 平均命令耗时 | 0.0021ms |

Redis 主要命令分布：

| 命令 | 调用次数 | 平均耗时 |
|---|---:|---:|
| GET | 1404440 | 0.0012ms |
| SCAN | 361320 | 0.0054ms |
| SET | 300 | 0.0076ms |
| DEL | 216 | 0.0045ms |
| ZINCRBY | 109 | 0.0201ms |

## MySQL 指标

| 指标 | 结果 |
|---|---:|
| QPS | 9.94 |
| Questions 增量 | 614 |
| 慢查询数 | 0 |
| 当前连接数 | 3 |
| 新建连接增量 | 7 |

## 结论

在缓存预热后的热点详情访问场景下，接口主要命中 Redis 详情缓存，Redis 命中率达到 99.64%，MySQL QPS 保持在 10 左右且无慢查询。应用层 Prometheus 口径显示 P95 为 5.29ms、P99 为 9.81ms，5xx 为 0。

注意：`wrk` 客户端侧 P99 为 1.16s，高于应用层 Prometheus 口径。该差异通常来自客户端连接排队、网络栈或压测端调度开销；面试展示时建议同时标注“服务端 Prometheus 口径”和“wrk 客户端口径”。
