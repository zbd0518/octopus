---
doc_type: approval-report
unit: issues/002-x-2026-07-24-group-editor-no-key-blocks-site-channel
status: approved
---

# Approval Report

## Decision: issue-fast-path

**Status**: Approved  
**Ref**: `approval-report.md#issue-fast-path`  
**Owner answer**: 批准方案 A（2026-07-24）

### 问题

Access Token 投影渠道 keys 为空时，分组编辑器显示「无可用 Key」，用户感觉无法选用。  
已确认：仍可点选进右侧，属本次静态风险展示的 UX 回归（叠加真实空 Key 数据态）。

### 根因（读代码）

| 点 | 证据 |
|----|------|
| 本次新增 no-key 判定 | `editor-member-status.ts`：`enabled && channel_key.trim()` |
| 硬风险弱化样式 | `Editor.tsx`：`hardRisk = channelDisabled \|\| noEnabledKey` → opacity |
| 投影 Key 来源 | `sitesync/project.go` `buildChannelKeys`：跳过 masked / 非 ready token；Access Token 同步若未得到明文 token 且无 `APIKey` fallback → 渠道 `keys` 为空 |
| 列表 mask | 非空 Key 会 mask 成仍非空字符串；**空串 mask 仍为空** → 判定为空 Key 时库内也基本为空 |

### 修复方案（小范围）

**推荐（A）— 仅修 UX 回归，1–2 处前端：**

1. **「无可用 Key」降为软风险**：只保留徽章/汇总，**不**再与「渠道已禁用」共用 `hardRisk` 灰化/降透明度（避免「点不了」的感知）。  
2. 文案/汇总保持提示，**不禁止**添加（现状本就不禁，保持）。  
3. 不改后端投影、不改 relay。

**备选（B）— 同时查投影空 Key 数据根因：**  
超出 fast-track；若用户确认「以前这些渠道能真实转发成功」，再开 standard analyze 查 Access Token → SiteToken → ChannelKey 链路。

### 风险与 tradeoff

| 方案 | 收益 | 代价 |
|------|------|------|
| A | 立刻恢复「可正常选」的体感；范围极小 | 空 Key 渠道仍可选进组，运行时可能仍失败（与改前一致） |
| B | 可能真正补上 Key | 跨 sitesync / 账号凭证，范围大 |

### 推荐

**A（fast-track）**。与用户确认的现象（能点、有徽章）完全吻合；空 Key 数据态可记 residual risk。

### 后果

- 批准 A → 写 `issue_path: fast-track` + confirmed report → 立即 fix  
- 拒绝 → `issue_path: standard` → analyze 深挖投影 Key  
- 修订 → 调整方案后重提

### Next action

请选择：

1. **批准 fast-track（方案 A）**  
2. **拒绝，走 standard 分析投影空 Key**  
3. **修订方案**（说明要 A+B 或其它）

---

## Decision: issue-fix-completion

**Status**: Approved  
**Ref**: `approval-report.md#issue-fix-completion`  
**Owner answer**: 批准修复完成（2026-07-24）

### 修复结果摘要

- 方案 A 已实现：`hardRisk` / `hasHardRisk` 仅渠道禁用；「无可用 Key」只徽章 + 汇总
- 独立 code review：**passed**（`reviewer: subagent`）
- 验证：unit 12 pass + i18n pass
- 延后：hard/soft 分离自动化测试（REV-001）；投影空 Key 数据根因（方案 B）

### 请确认

1. **批准修复完成**（可 commit / 再编译）  
2. **拒绝**  
3. **修订**（说明还要改什么）
