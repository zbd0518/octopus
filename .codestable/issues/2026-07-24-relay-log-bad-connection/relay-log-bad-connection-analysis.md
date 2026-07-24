---
doc_type: issue-analysis
issue: 2026-07-24-relay-log-bad-connection
status: confirmed
root_cause_type: config
related: [relay-log-bad-connection-report.md]
tags: [database, connection-pool, mariadb, driver-bad-connection]
---

# relay log 刷盘 "driver: bad connection" 根因分析

## 1. 问题定位

| 关键位置 | 说明 |
|---|---|
| `db/db.go:409-412` | `configureConnectionPool` 对非 SQLite 库设置 `ConnMaxIdleTime=10min`、`ConnMaxLifetime=1h` |
| `task/init.go:129-130` | `TaskRelayLogSave` 非 SQLite 分支直接调用 `RelayLogSaveDBTask`，失败打印 WARN |
| `relaylog/relaylog.go:205` | `relayLogFlushToDB` 通过 `db.GetLogDB()` 获取连接（共用主库时返回 `db`） |
| `relaylog/relaylog.go:212` | `conn.WithContext(ctx).Create(&batch)` — 执行 SQL INSERT，命中 stale 连接 |
| `db/db.go:175-182` | `GetLogDB()` 在 `logDBType==""` 时回落到主库 `db`，与业务代码共享同一连接池 |

## 2. 失败路径还原

**正常路径**：

```
每 2 分钟 → TaskRelayLogSave 定时器触发
  → relaylog.RelayLogSaveDBTask(ctx)
    → relaylog.relayLogFlushToDB(ctx)
      → db.GetLogDB() 返回主库 *gorm.DB（共用连接池）
      → conn.WithContext(ctx).Create(&batch)
      → MySQL 驱动从 sql.DB 连接池取出空闲连接
      → 连接存活 → INSERT 成功 → 截断缓存 → 返回 nil
```

**失败路径**：

```
每 2 分钟 → TaskRelayLogSave 定时器触发
  → relaylog.RelayLogSaveDBTask(ctx)
    → relaylog.relayLogFlushToDB(ctx)
      → db.GetLogDB() 返回主库 *gorm.DB（共用连接池）
      → conn.WithContext(ctx).Create(&batch)
      → MySQL 驱动从 sql.DB 连接池取出空闲连接
      → ⚠️ 该连接已被 OS/MariaDB 端回收（TCP RST / timeout）
      → go-mysql-driver 尝试写入 → "driver: bad connection"
      → database/sql 自动重试（取新连接再试一次）
      → 新连接也可能 stale → 再次 "driver: bad connection"
      → relayLogFlushToDB 返回 error → RelayLogSaveDBTask 返回 error
      → init.go:129 log.Warnf("relay log save db task failed: %v", err)
      → ⚠️ 缓存 batch 未落库，保留在内存中等待下次重试
```

**分叉点**：`db/db.go:409-412` — `ConnMaxIdleTime=10min` 是触发条件：空闲超过 10 分钟的连接归还池后，不再被主动驱逐。当操作系统在 TCP keepalive 关闭的情况下回收闲置 TCP 连接时，Go `database/sql` 连接池无法感知，下次取出即报 `driver: bad connection`。

## 3. 根因

**根因类型**：config（连接池参数配置不当）

**根因描述**：

非 SQLite 数据库的连接池配置了 `ConnMaxLifetime=1h` 和 `ConnMaxIdleTime=10min`（`db/db.go:409-412`）。`ConnMaxIdleTime=10min` 意味着连接空闲超过 10 分钟后会在归还时被关闭，但 Go `database/sql` 的 lazy connection pruning 并非严格的实时行为——它只在连接**取出或归还**时检查。这就留下了一个窗口：

1. 连接从池中取出，使用完毕，归还到空闲池
2. 连接空闲约 10 分钟，理论上应被 `ConnMaxIdleTime` 关闭
3. 但在 `database/sql` 没有新请求触发 pool 扫描前，这连接仍在池中
4. 运行环境 TCP keepalive **未开启**（`tcp_keepalive_time=0`），OS 可能在 10 分钟之前就通过 TCP RST 回收了闲置连接
5. 下一轮定时任务（2 分钟后）取出该连接 → 驱动尝试写入 → `driver: bad connection`
6. `database/sql` 内置重试一次（再取一条连接），若第二条也是 stale，重试失败 → 错误返回，串联显示为 `driver: bad connection; driver: bad connection`

更根本的问题：Aborted_clients = 6 发生在本地环回（127.0.0.1）中，说明问题不是网络抖动，而是 Windows TCP 栈在没有 keepalive 的情况下对长时间空闲但仍有 keepalive 需求连接的清理策略。

**是否有多个根因**：否，单一根因——连接池配置与运行环境 TCP 特性不匹配。

## 4. 影响面

- **影响范围**：不仅限于 report 中观察到的 relay_log 定时任务。`db.GetDB()`（主库连接）被以下模块直接使用：
  - `op/alert` — 告警规则 CRUD、告警状态读写、通知渠道管理（`alert.go` 中 8+ 处调用）
  - `op/airoute` — AI 路由任务管理，含事务操作（`route.go:1596` `Begin()` + `task.go` 中 5+ 处调用）
  - `op/analytics` — 统计缓存刷新（`analytics_test.go` 以测试代码引用）
  - `op/relaylog` — 日志刷盘（本问题）+ 日志清理（`relaylog.go` 多路径）
  - 所有通过 `db.GetDB()` 读写主库的代码路径共享同一连接池

- **潜在受害模块**：`alert`、`airoute` 等高频 CRUD 模块在同一连接池中，同样暴露于 `driver: bad connection` 风险。虽然目前用户未报告这些模块有异常，但连接池共享意味着这只是概率事件——谁先踩到 stale 连接谁就报错。

- **数据完整性风险**：有。relay_log 缓存 batch 写入失败后，`relayLogFlushToDB` 返回前已经通过 `conn.Create(&batch)` 执行了 INSERT（部分写入可能已被 MySQL 接收但连接断开了）；如果 `database/sql` 的重试机制取了新连接并再次执行同一 batch（`Clauses(clause.OnConflict{DoNothing: true})` 做幂等保护），不会重复写入。但如果两次都失败，缓存 batch 留在内存中，而 `relayLogMaxSize=200` 的队列会继续积压——连续多次失败可能导致超过 200 条日志后触发丢弃。

- **严重程度复核**：**维持 P2**，但风险等级上调——共享连接池意味着影响面比 report 阶段看到的更大。如果观察到业务模块（alert/airoute）也开始报同类错误，应升级为 P1。

## 5. 修复方案

### 方案 A：降低 ConnMaxIdleTime + 缩短 ConnMaxLifetime（推荐）

- **做什么**：
  - `db/db.go:409-412`：将 `ConnMaxIdleTime` 从 10 分钟降低到 **3 分钟**（180s）
  - 将 `ConnMaxLifetime` 从 1 小时降低到 **30 分钟**
  - 仅改 `configureConnectionPool` 中非 SQLite 分支的两个数值
- **优点**：
  - 改动最小（2 个数字，`db/db.go` 1 个文件）
  - 连接在 OS TCP 回收前就被 Go pool 主动关闭，规避 stale 连接窗口
  - 对现有业务代码零影响——连接池行为与现有调用完全兼容
  - 本地 MariaDB（127.0.0.1）连接开销极低，3 分钟轮换不会增加可感知负载
- **缺点 / 风险**：
  - 稍微增加连接创建/销毁频率（可忽略，本地环回连接成本近乎零）
  - 如果未来部署到远程数据库（跨网络），3 分钟轮换可能稍显激进
- **影响面**：仅 `internal/db/db.go` 1 个文件，2 个数值

### 方案 B：方案 A + 增加连接池健康检查（重试策略加固）

- **做什么**：
  - 方案 A 的连接池参数调整
  - `relaylog/relaylog.go:relayLogFlushToDB` 中对 `driver: bad connection` 做显式重试（最多 2 次，间隔 1s）
  - 重试时通过 `conn.WithContext(ctx)` 重新获取连接执行 `Create`
- **优点**：
  - 双重防御：连接池减少 stale + 写入侧兜底重试
  - relay_log 写入是幂等的（`OnConflict{DoNothing: true}`），重试安全
- **缺点 / 风险**：
  - 改动范围更大（2 个文件）
  - 如果根本问题是连接池大面积污染，重试仍无效——治标不治本
  - 重试逻辑与 `database/sql` 内置重试重叠，可能冗余
- **影响面**：`db/db.go` + `relaylog/relaylog.go` 2 个文件

### 方案 C：方案 A + 添加 `database/sql` ConnCheck callback（Go 1.15+ driver-level 健康检查）

- **做什么**：
  - 方案 A 的连接池参数调整
  - 利用 `go-sql-driver/mysql` 的 `ConnectionValidator` 接口或通过 `SetConnMaxIdleTime` 策略更激进地驱逐
  - 或者在 `configureConnectionPool` 中设置 `ConnMaxIdleTime=1min`（极短空闲即关）
- **优点**：
  - 最彻底杜绝 stale 连接
- **缺点 / 风险**：
  - `ConnMaxIdleTime=1min` 太激进，本地虽无妨但远程部署场景下可能增加延迟
  - Go MySQL 驱动的 `ValidateConnection` 并非所有版本都支持，引入兼容性风险
  - 改动面和方案 A 相同，但参数更激进 → 性价比不高
- **影响面**：`db/db.go` 1 个文件

### 推荐方案

**推荐方案 A**，理由：

1. **改动范围最小** — 仅 1 个文件 2 个数值，零风险引入新逻辑
2. **根因最直接** — stale 连接的原因就是空闲时间过长，缩短 `ConnMaxIdleTime` 到 OS TCP 回收阈值以下即可消除窗口
3. **副作用最少** — 本地 MariaDB 连接开销可忽略；如果未来部署到远程库，可再按实际网络延迟调整
4. `database/sql` 已有自动重试 1 次的机制，方案 B 的额外重试是冗余；方案 C 的参数过于激进