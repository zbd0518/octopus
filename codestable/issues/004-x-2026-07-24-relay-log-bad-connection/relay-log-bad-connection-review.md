---
doc_type: issue-review
issue: 2026-07-24-relay-log-bad-connection
status: passed
reviewer: subagent
reviewed: 2026-07-24
round: 2
lane_a_state: completed
lane_a_ref: a5a99a68ae8fbcfe7
lane_a_reason: "independent Task agent review completed and locally verified"
lane_b_state: skipped
lane_b_ref: ""
lane_b_reason: "skipped-scope-ambiguous: workspace has unrelated dirty files; local line review of internal/db/db.go only"
---

# relay-log-bad-connection 代码审查报告

## 1. Scope And Inputs

- Design: `codestable/issues/004-x-2026-07-24-relay-log-bad-connection/relay-log-bad-connection-analysis.md`（方案 A 已批准）
- Checklist: none（issue 流程）
- Evidence pack: none
- Gate results: none
- DoD results: none
- Implementation evidence: fix-note + `internal/db/db.go` 连接池参数调整
- Diff basis: 本轮可归因改动仅为 `internal/db/db.go`（`ConnMaxLifetime`/`ConnMaxIdleTime`）
- Review mode: initial
- Baseline dirty files: channel / openai-chat 等无关 dirty（排除在审查范围外）

### Independent Review

- Detection: independent Task agent 可用；OCR CLI 可用但 workspace scope 不清晰故跳过
- 环节 A 独立隔离 Task agent: independent-agent + completed（`a5a99a68ae8fbcfe7`）
- 环节 B OCR CLI: skipped（scope-ambiguous → 本地行级审查 `internal/db/db.go`）
- OCR severity mapping: High→blocking/important, Medium→nit/suggestion, Low→discarded
- Merge policy: 环节 A findings 已对照源码与 go-sql-driver/mysql@v1.8.1 本地核验后合并
- Gate effect: none（环节 A 已完成；OCR 跳过不阻塞 subagent gate）

## 2. Diff Summary

- 新增：none
- 修改：`internal/db/db.go`（`configureConnectionPool` 非 SQLite 分支）
- 删除：none
- 未跟踪：`codestable/issues/004-x-2026-07-24-relay-log-bad-connection/`（issue 产物）
- 风险热点：连接池配置（跨所有 `GetDB()`/`GetLogDB()` 调用方）；无权限/数据迁移/API 契约变更

实际 diff：

```diff
- sqlDB.SetConnMaxLifetime(time.Hour)
- sqlDB.SetConnMaxIdleTime(10 * time.Minute)
+ sqlDB.SetConnMaxLifetime(30 * time.Minute)
+ sqlDB.SetConnMaxIdleTime(3 * time.Minute)
```

## 3. Adversarial Pass

- 假设的生产 bug：Windows 上 idle &lt; 3m 的半开 socket 仍触发 `driver: bad connection`，被误认为“已根治”
- 主动攻击过的反例：Windows `connCheck` 空操作、connectionCleaner 与 idle 窗口、flush 失败不截断 cache、测试假绿、热更新无效
- 结果：无 blocking 实现错误；1 条 important（缺回归测试）；Windows liveness 与 runtime 验收进 residual-risk / QA focus

## 4. Findings

### blocking

none

### important

- [x] REV-001 `internal/db/db_test.go` 缺少非 SQLite 连接池参数回归断言 → 已处理（round 2，2026-08-09）
  - Evidence: 现有 `TestConfigureConnectionPoolLimitsSQLiteConnections` 只断言 SQLite `MaxOpenConnections==1`；`database/sql.DBStats` 不直接暴露 maxIdleTime/maxLifetime 字段，但可用包内常量 + 调用 `configureConnectionPool` 的可观测意图，或把 30m/3m 提成常量再断言。当前 `go test ./internal/db` 无法防止改回 10m/1h。
  - Impact: 配置回归假绿；本 issue 验收仅靠人工 runtime 观察，CI 无锁。
  - Resolution: `30m/3m` 提为包内常量 `nonSQLiteConnMaxLifetime` / `nonSQLiteConnMaxIdleTime`，新增 `TestConfigureConnectionPoolNonSQLite`（`sql.Open("mysql", ...)` 走非 SQLite 分支，断言 `MaxOpenConnections=100` 与常量值）；`go test ./internal/db -count=1` 通过。

### nit

- [ ] REV-002 `internal/db/db.go:415-416` 魔法数可抽包内常量（可选，非必须）
- [ ] REV-003 注释写 “OS/server-side TCP cleanup” 略偏：report 中 MariaDB `wait_timeout=28800s`，更像 OS/半开 socket 而非 server wait_timeout

### suggestion

- [ ] REV-004 若 runtime 仍偶发，另开 issue 做可观测计数 / 有限 flush 重试（原方案 B，超出本批准范围）
- [ ] REV-005 远程 DB + TLS 部署时单独评估 3m idle 建连成本，勿在本 PR 加配置开关

### learning

- Windows + `go-sql-driver/mysql@v1.8.1`：`conncheck_dummy.go` 中 `connCheck` 恒返回 nil，`CheckConnLiveness` 默认 true 仍无探测能力
- Go `database/sql` 有 `connectionCleaner` 主动扫 idle，并非仅取出/归还时 prune；analysis 叙述可更精确
- 纯配置变更也应有断言测试，否则 `go test` 对回归无意义

### praise

- 严格按方案 A：单文件、两参数、无重试/新抽象
- SQLite 序列化写路径未误伤
- `OpenStandaloneWithOptions` → `configureConnectionPool` 使主库与独立 log DB 一并生效
- fix-note 诚实标明需重启与 runtime 观察

## 5. Test And QA Focus

- QA 必须重点复核：
  1. **重启** octopus 后加载新池参数（热更无效）
  2. 原环境（Windows + 本地 MariaDB 共用主库）观察 ≥30–60 分钟，`relay log save db task failed: driver: bad connection` 是否消失或显著下降
  3. 低流量（几乎无 API、仅 2 分钟定时任务）最易打中 idle 半开连接
  4. 有流量时共享 `GetDB()` 的 CRUD 无建连风暴
  5. SQLite 回归：后台写任务无 nested transaction
- 建议新增或加强的测试：`TestConfigureConnectionPoolNonSQLite`（或常量断言）
- 不能靠 review 完全确认的点：本机 OS 是否在 &lt;3m 内回收 loopback 空闲 TCP；3m/30m 是否足以消除用户环境 WARN

## 6. Residual Risk

- Windows 无有效 connCheck：3m 只缩小窗口，idle&lt;3m 的半开连接仍可能失败
- 热更新无效：必须重启
- 连续 flush 失败 + 高 QPS 仍可能触达 `relayLogMaxSize=200` 丢弃（预存行为）
- 远程环境建连成本略增
- **验收门禁**：合并 ≠ 问题已消失；以重启后 runtime 观察为准

## 7. Verdict

**status: passed**（round 2 复审，2026-08-09）

- 实现与批准方案 A **一致**，无 blocking 逻辑错误
- **REV-001 已按路径 1 处理**：包内常量 + `TestConfigureConnectionPoolNonSQLite` 回归断言，`go test ./internal/db -count=1` 通过；无 remaining important
- 验收门禁仍以重启后 runtime 观察为准（见 §5 QA Focus / §6 Residual Risk）

下一步：关闭 issue 并毕业回写 Project Spec（连接池生命周期已写入 `codestable/spec/index.md`「当前已稳定的能力与保证」）。
