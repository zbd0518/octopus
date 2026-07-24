---
doc_type: issue-report
issue: 2026-07-24-openai-chat-metadata-400
status: confirmed
issue_path: fast-track
severity: P1
created: 2026-07-24
---

# Claude Code 经 Chat 渠道转发时 metadata 被上游拒绝（400）

## 现象

使用 Claude Code 通过本项目（Octopus）转发请求时，渠道 `vibes-自持-签到/免费key/default-Chat (grok-4.5)` 失败，错误：

```text
attempt 1/4: none (bad request, client error): 400:
{"error":{"message":"Argument not supported: metadata","type":"bad_response_status_code","param":"","code":"bad_response_status_code"}}
```

同次请求里其它渠道另有超时 / 502（`subgrok`），与本 issue 无关；本 issue 仅针对 **400 Argument not supported: metadata**。

## 复现

1. 客户端：Claude Code，模型链为 `grok-4.5 → ChatCompletions → vibes-.../default-Chat`。
2. Claude Code 以 Anthropic Messages 协议发送请求，请求体含 `metadata.user_id`（设备/会话标识）。
3. Octopus 将请求转成 OpenAI Chat Completions 后发往兼容上游（免费中继 / Grok 代理等）。
4. 上游返回 400，指出不支持 `metadata` 参数。

## 期望 vs 实际

- **期望**：出站 Chat Completions 对不兼容上游时，不发送仅官方 OpenAI 支持、且对推理无必要的 `metadata` 字段；请求可正常完成。
- **实际**：Chat 出站仍把内部请求上的 `Metadata` 原样序列化进 `/v1/chat/completions` body，兼容上游拒绝并 400。

## 环境

- 模块：出站转换器 `internal/transformer/outbound/openai`
- 相关文件：
  - `internal/transformer/inbound/anthropic/messages.go`（入站把 Anthropic `metadata.user_id` 写入 `InternalLLMRequest.Metadata`）
  - `internal/transformer/outbound/openai/chat.go`（Chat Completions 出站，当前未剥离 `Metadata`）
  - `internal/transformer/outbound/openai/response.go`（Responses 出站，**已**剥离 `Metadata`，注释写明同类 400）
- 渠道类型：OpenAI Chat Completions（`default-Chat`）
- 上游：OpenAI 兼容中继（非官方 OpenAI 全量协议）

## 严重程度

**P1 严重**：Claude Code → 兼容 Chat 上游这条常见路径直接 400，无客户端侧可用绕过（客户端总会带 metadata）。

## 线索（非结论）

- Responses 路径已有修复与测试：`response.go` 中 `responsesReq.Metadata = nil`，`response_test.go` 中 `TestResponseOutbound_TransformRequest_StripsMetadata`。
- Chat 路径 `SanitizeRequestForOpenAICompat` 会清 `Include` / 部分 `ExtraBody`，但**不清** `Metadata`。
- 用户侧截图与日志中失败标签为 `chat`，不是 `responses`。

## 路径判定（代码已读后）

满足快速通道条件：

1. 根因可指向明确 file:line：Chat 出站未剥离 `Metadata`。
2. 修复约 1 处主逻辑 + 1 个测试，无跨模块 API 变更。
3. 与既有 Responses 行为对齐，官方 OpenAI 省略 `metadata` 安全。

拟走 **fast-track**，待 owner 确认修复方案后直接 fix。
