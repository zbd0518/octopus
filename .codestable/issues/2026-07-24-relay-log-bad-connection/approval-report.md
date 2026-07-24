---
doc_type: approval-report
issue: 2026-07-24-relay-log-bad-connection
---

# Approvals

## issue-report-confirm

- **status**: approved
- **ref**: relay-log-bad-connection-report.md
- **summary**: relay log 刷盘任务 "driver: bad connection" 问题报告。走 standard 路径（根因涉及连接池配置调整，有跨路径影响风险，不符合快通条件）。

请检查报告内容是否准确，确认后进入阶段 2 根因分析。

## issue-fix-plan

- **status**: approved
- **ref**: relay-log-bad-connection-analysis.md
- **chosen_plan**: A
- **summary**: 根因——连接池 `ConnMaxIdleTime=10min` 与 OS TCP keepalive 关闭不匹配，空闲连接被 OS 回收后 Go pool 无从感知。确认方案 A：将 `ConnMaxIdleTime` 降至 3 分钟、`ConnMaxLifetime` 降至 30 分钟，仅改 `db/db.go:409-412` 两行数值。