---
doc_type: issue-fix-note
issue: 2026-07-26-codex-site-channel-key-and-client-disconnect
status: completed
related:
  - codex-site-channel-key-and-client-disconnect-report.md
  - codex-site-channel-key-and-client-disconnect-analysis.md
---

# Codex no available key + client disconnected 修复记录

## 根因（已确认）

1. **现象 A**：渠道/分组「测试模型」用 `GetChannelKey()`（空 model，绕过模型级冷却）；中继 `GetChannelKeyWithCooldown(resolvedModel)` 受 `(channel,key,model)` 冷却。空 key 时统一文案 `cooldown or disabled` 无法区分状态。
2. **现象 B**：客户端断开虽不记熔断，但仍 `AttemptFailed` + `metrics.Save(false)` 写 `RelayLog.Error`，streamSession 路径 `Warnf` 刷屏。

## 方案

按 analysis **方案 A** 落地。

## 改动

| 文件 | 改动 |
|---|---|
| `internal/model/channel.go` | 新增 `DescribeNoAvailableKey(model)`：无 key / 全禁用空 / 全冷却 / 兜底 |
| `internal/model/log.go` | 新增 `AttemptClientClosed = "client_closed"` |
| `internal/helper/group_probe.go` | 测试取 key 改为 `GetChannelKeyWithCooldown(item.ModelName, 300)` + 诊断 message |
| `internal/relay/relay.go` | skip 诊断文案；disconnect → `AttemptClientClosed`；**先于** 熔断/号池失败记账；continuing generation → `Debugf` |
| `internal/relay/media_relay.go` | skip 诊断文案对齐 |
| `internal/relay/retry_helper.go` | `PrepareCandidate` SkipReason 用诊断 |
| `internal/relay/metrics.go` | disconnect：不写 Error；有进度记 success，早断中性（不加 RequestFailed） |
| `web/src/api/endpoints/log.ts` | `AttemptStatus` 含 `client_closed` |
| `web/src/components/modules/log/Item.tsx` | 徽章/卡片 muted（同 skipped） |
| `web/public/locale/{zh_hans,zh_hant,en}.json` | `clientClosed` 文案 |

## 验证

```text
go test ./internal/model/ ./internal/helper/ ./internal/relay/ ./internal/op/relaylog/ -count=1
```

全部通过。新增：

- `TestDescribeNoAvailableKeyDistinguishesStates`
- `TestTestGroupModelItem_RespectsModelKeyCooldown`
- `TestClientDisconnectHadProgress`
- 既有 keyless probe 期望文案更新为 `no available key (channel has no keys)`
- 独立 code review（subagent）changes-requested → review-fix 后 **passed**

## 遗留风险

- 历史日志仍可能是 `failed` + Error 字符串；仅新请求受益。
- 上游持续 ≥400 仍会进入冷却；本 fix 修路径一致与可观测性，不消灭公益站 4xx。
- media 路径 client cancel 未全量对齐 `client_closed`（plan 默认 LLM 优先）。
- 探测对齐冷却后 UI 测试在冷却窗口内会失败——**预期一致性**，非假绿回归。
- 冷却诊断文案暂无 `retry_after` 秒数。

## 顺手发现（未改）

- media_relay 在 `c.Request.Context().Done()` 时仍可能以 `context.Canceled` 记失败；可另开 issue 对齐。
