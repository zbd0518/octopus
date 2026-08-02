---
doc_type: issue-report
issue: 2026-07-24-relay-log-bad-connection
status: confirmed
issue_path: standard
severity: P2
summary: relay log 定时刷盘任务频繁报 "driver: bad connection"，MariaDB 连接池闲置连接失效导致写入失败
tags: [database, relay-log, connection-pool, mariadb]
---

# relay log 刷盘 "driver: bad connection" Issue Report

## 1. 问题现象

日志中频繁出现（每 2 分钟一次或多次）：

```
2026-07-24T14:05:17+08:00  WARN  task/init.go:130  relay log save db task failed: driver: bad connection; driver: bad connection
```

错误为 `database/sql` 标准错误，含义：连接池中取出的闲置连接已被 MariaDB 服务端回收或网络层断开，驱动尝试重用该连接执行 `Create(&batch)` 失败。字符串重复两次（`; driver: bad connection`）说明 `database/sql` 内置的自动重试（最多 1 次）也失败了。

MariaDB 侧 `Aborted_clients = 6` 确认有异常断开事件。

## 2. 复现步骤

**代码路径**（100% 可追踪）：

```
TaskRelayLogSave 定时器 (每 2 分钟触发)
  → task/init.go:129: relaylog.RelayLogSaveDBTask(context.Background())
    → relaylog/relaylog.go:366: relayLogFlushToDB(ctx)
      → relaylog/relaylog.go:205: db.GetLogDB() 返回主库连接
      → relaylog/relaylog.go:212: conn.WithContext(ctx).Create(&batch)
        ← "driver: bad connection" ← 连接已失效
```

**触发条件**：连接池中空闲连接超过 `ConnMaxIdleTime=10min` 后仍保留在池中，当下一次定时任务（2 分钟后）取出时，该连接可能已被操作系统 TCP 层回收或 MariaDB 端断开。

**复现频率**：频繁出现（用户反馈），取决于 TCP 栈回收频率和定时任务命中 stale 连接的随机性。

## 3. 期望 vs 实际

**期望行为**：TaskRelayLogSave 每 2 分钟定时将 relay_log 内存缓存批量刷入 DB，静默成功。

**实际行为**：连接失效时 `conn.Create(&batch)` 返回 `driver: bad connection`，向上传到 `RelayLogSaveDBTask` 返回 error，`init.go:129` 打印 WARN 日志。**缓存 batch 未落库**，保留在内存中等待下一次重试。若连接池问题持续存在，连续失败可能导致日志缓存溢出丢失（`relayLogMaxSize=200`）。

## 4. 环境信息

- 涉及模块 / 功能：`task/init.go`（定时任务注册）、`db/db.go`（连接池配置）、`relaylog/relaylog.go`（日志刷盘逻辑）
- 相关文件 / 函数：
  - `task/init.go:129-130` — 错误发生位置
  - `db/db.go:396-413` — `configureConnectionPool`，非 SQLite 连接池参数
  - `relaylog/relaylog.go:188-250` — `relayLogFlushToDB`
  - `relaylog/relaylog.go:352-367` — `RelayLogSaveDBTask`
- 运行环境：开发/本地（`127.0.0.1:3306` MariaDB）
- 数据库：MariaDB 10.x，库 `octopus`
- 日志库模式：**共用主库**（`log_type`/`log_path` 为空，`GetLogDB()` 回落到主库 `db`）
- 连接池配置：
  - `MaxOpenConns = 100`
  - `MaxIdleConns = 10`
  - `ConnMaxLifetime = 1 hour`
  - `ConnMaxIdleTime = 10 minutes`
- MariaDB 超时配置：
  - `wait_timeout = 28800s`（8 小时）
  - `connect_timeout = 10s`
  - `net_read_timeout = 30s`
  - `net_write_timeout = 60s`
- TCP keepalive：**未开启**（`tcp_keepalive_time=0, interval=0, probes=0`）
- 当前连接数：峰值 8，活跃 6
- Aborted_clients：6 次

## 5. 严重程度

**P2 中等** — 理由：

1. 定时任务每 2 分钟自动重试，单次失败不丢数据（缓存保留到下次刷盘成功）
2. 当前仅影响 relay_log 写入，不影响 API 请求处理
3. **风险升级点**：日志库共用主库，连接池配置对所有业务代码一致。若 API 请求路径中空闲连接也 hit 同类 `driver: bad connection`，影响面将扩展到业务功能。当前虽未观察到，但共享同一连接池使此风险不可忽视

## 备注

- 日志片段：`2026-07-24T14:05:17+08:00  WARN  task/init.go:130  relay log save db task failed: driver: bad connection; driver: bad connection`
- DBX 查询截图：`Aborted_clients = 6`、`Threads_connected = 6`、TCP keepalive 全部为 0
- 当前 `ConnMaxLifetime=1h` 远小于 MariaDB `wait_timeout=8h`，但问题不在超时值差异——`ConnMaxIdleTime=10min` 空闲回收后连接归还池时 `database/sql` 不验证存活，加上 TCP keepalive 未开启，OS 回收闲置 TCP 连接后连接池无从感知，下次取出即报 `driver: bad connection`