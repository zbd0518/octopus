---
doc_type: audit-finding
audit: 2026-07-23-request-cli-channel-spoof
finding_id: "maintainability-01"
nature: maintainability
severity: P2
confidence: high
suggested_action: cs-refactor
status: open
---

# Finding 03：Codex Desktop User-Agent 硬编码版本，上游升级后可能失效

## 速答

`header_profile=codex` 使用固定字符串 `Codex Desktop/0.131.0 ... 26.519.21041`，无配置项、无跟随官方版本的机制；中转若校验最低版本或完整 UA 白名单，可能突然全部失败。

## 关键证据

- `internal/transformer/rewrite/config.go:40` / `51`：

```go
"User-Agent": "Codex Desktop/0.131.0 (Windows 10.0.19045; x86_64) unknown (Codex Desktop; 26.519.21041)",
```

- 前后端均无「自定义 Codex UA」字段；只能改代码或用 `custom_header` 再盖一层

## 影响

- 维护成本：官方 Codex Desktop 版本 bump 后需人工跟进
- 故障模式：整批依赖该 profile 的渠道同时 403/400，排障困难

## 建议

- `cs-refactor`：UA 提到配置 / 常量集中处，或允许 `header_profile` 扩展字段覆盖 UA
- 短期：文档写明当前指纹版本，并说明可用 `custom_header` 覆盖 `User-Agent`
