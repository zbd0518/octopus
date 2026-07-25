---
doc_type: issue-fix-note
issue: 2026-07-24-group-editor-no-key-blocks-site-channel
status: implemented
date: 2026-07-24
---

# 分组编辑器无可用 Key 软提示 Fix Note

## 根因

`feat(group) 成员静态风险` 将「无可用 Key」与「渠道已禁用」一并作为 `hardRisk`，对左侧候选与右侧已选做 **opacity / grayscale** 弱化。  
Access Token 投影渠道常出现 `keys` 为空（数据态既有），于是被标成硬风险，**可选但仍显灰**，用户感知为「无法选中」。  
添加按钮本身从未因 no-key 而 `disabled`。

## 改动

| 文件 | 变更 |
|------|------|
| `web/src/components/modules/group/Editor.tsx` | `hardRisk = channelDisabled` only；`noEnabledKey` 仅徽章 |
| `web/src/components/modules/group/ItemList.tsx` | `hasHardRisk = isDisabled` only；无 Key 仍显示徽章 |

未改：判定函数、汇总条、后端投影、relay。

## 验证

```text
editor-member-status + editor-filters unit → 12 pass
pnpm --dir web test:i18n → passed
```

手工预期：空 Key 投影渠道可正常点选进右侧；仍见「无可用 Key」徽章，但无灰化。

## 遗留风险

- 投影渠道 keys 仍可能为空；运行时转发仍要求非空 Key（与改前一致）。
- 若需真正补齐 Access Token → ChannelKey 链路，另开 issue / standard 分析。
