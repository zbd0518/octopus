---
doc_type: issue-fix-note
issue: 2026-07-24-relay-log-bad-connection
status: completed
chosen_plan: A
related:
  - relay-log-bad-connection-report.md
  - relay-log-bad-connection-analysis.md
tags: [database, connection-pool, mariadb]
---

# relay log 刷盘 "driver: bad connection" 修复记录

## 1. 根因（摘要）

非 SQLite 连接池 `ConnMaxIdleTime=10min` / `ConnMaxLifetime=1h` 过长，在 TCP keepalive 关闭的 MariaDB 环境下，OS 回收闲置 TCP 连接后 Go `database/sql` 池无从感知。定时任务 `TaskRelayLogSave` 取出 stale 连接执行 `Create(&batch)` 时返回 `driver: bad connection`。

## 2. 改动

| 文件 | 改动 |
|---|---|
| `internal/db/db.go:409-416` | `configureConnectionPool` 非 SQLite 分支：`ConnMaxLifetime` 1h → **30min**；`ConnMaxIdleTime` 10min → **3min**；补充注释说明动机 |
| `internal/db/db.go` | `30m/3m` 提为包内常量 `nonSQLiteConnMaxLifetime` / `nonSQLiteConnMaxIdleTime`（REV-001 处理） |
| `internal/db/db_test.go` | 新增 `TestConfigureConnectionPoolNonSQLite`：`sql.Open("mysql", ...)` 走非 SQLite 分支，断言 `MaxOpenConnections=100` 与常量值（REV-001） |

未改业务逻辑、未引入重试/新抽象；仅调整连接池生命周期参数并补回归断言。

## 3. 验证

- [x] `go test ./internal/db/ -count=1` 通过（含新增 `TestConfigureConnectionPoolNonSQLite`）
- [x] 改动范围与 analysis 方案 A 一致（`db/db.go` + 回归测试 `db_test.go`）
- [x] REV-001 已处理：包内常量锁定 30m/3m，CI 防回归改回 10m/1h
- [ ] 运行时验证：需重启 octopus 服务后观察 2 分钟周期日志，确认不再出现 `relay log save db task failed: driver: bad connection`

运行时验证依赖服务重启与持续观察，代码层修复与回归测试已完成。

## 4. 遗留风险

1. **远程数据库**：若未来部署到跨网络 DB，3 分钟 idle 轮换可能略增连接建立开销；本地 MariaDB 可忽略。
2. **已在池中的 stale 连接**：重启前已建立的连接仍可能触发一次 `bad connection`；重启后新参数生效。
3. **共享连接池**：本修复对所有使用 `db.GetDB()` 的模块（alert / airoute / relaylog 等）统一生效，这是预期收益而非副作用。

## 5. 顺手发现

无。
