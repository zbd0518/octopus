---
kind: issue
title: "渠道 API Key 配置新增「批量导入 Base64」"
type: ff
status: closed
created: 2026-10-16
---

# 渠道 API Key 配置新增「批量导入 Base64」

> **读者：** 以后搜到这条时——「改了啥、怎么信、动没动制度记忆」。

渠道创建 / 编辑表单的 API Key 配置区，在原有「批量导入」按钮旁新增「批量导入 Base64」按钮，弹出独立弹窗：粘贴被 Base64 编码的密钥，弹窗内实时解码预览，确认后与普通批量导入走同一套去重合并逻辑写入渠道 Key 列表。解码后的明文自动剔除中文与中文标点，只留 Key。核心诉求（很多密钥是 base64 编码的，需要先 decode 再导入；decode 后移除中文）已满足。

- 改动：
  - `web/src/components/modules/channel/bulk-keys.ts` — 新增 `looksLikeBase64Payload` / `splitBase64Payloads` / `decodeBulkBase64Keys`：拆分粘贴文本为 base64 候选段、解码（兼容 base64url、缺省 padding、data URL 前缀与引号包装）、再复用 `parseBulkKeys` 解析明文；**解码后按 `CHINESE_PATTERN`（`\p{Script=Han}` + CJK 标点 + 全角标点 + 中文引号）剔除中文**，纯中文说明行变空行被丢弃、`key|中文备注` 只剩 Key、全角括号包裹的备注一并消失；无法解码或解出但不像 Key 的段落归入 `failed`（`invalid-base64` / `invalid-utf8` / `not-a-key`），不阻断其余导入。防误造脏 Key：碎片解出的短串不算 Key；逐段全失败且段数 > 1 时回退为「拼接整体解码」，覆盖单个 base64 被换行折断粘贴的场景。
  - `web/src/components/modules/channel/Form.tsx` — API Key 配置区新增按钮与 `bulkBase64Open` / `bulkBase64Text` 状态、`handleOpenBulkBase64` / `handleApplyBulkBase64`；弹窗含解码预览（成功项绿色 ✓、失败项红色 ✗ 并列出来源）、统计行；导入复用 `mergeBulkKeys`，部分失败时用 warning toast 提示失败段数。
  - i18n `web/public/locale/{zh_hans,zh_hant,en}.json` — 新增 `channel.form.bulkBase64.*`（button/title/description/placeholder/hint/cancel/preview/previewEmpty/invalid/total/empty/noNew/success/successPartial），hint 注明「解码后的中文与中文标点会被自动剔除」。
  - `web/src/components/modules/channel/bulk-keys.test.ts` — 新增 9 条 `bulk-keys` 解码用例（含中文剔除、纯中文判 not-a-key、多行明文、失败段保留、折断回退）。
  - `web/package.json` — `test:unit` 注册 `bulk-keys.test.ts`（此前该文件未注册）。
- 验证：`node tests/i18n.test.mjs` 通过（三语言 key 对齐）；`node --test` 跑全部 unit（358 pass / 0 fail，含新增 16 条 bulk-keys 用例）；`npx eslint src/components/modules/channel` 仅剩 `index.tsx` 既有的 `FolderTree` 未使用警告（非本次）；`npx tsc --noEmit` 仅剩 5 个既有测试文件的 18 条报错（与本次无关，同 022 记录）。未跑浏览器手测（需启动前后端）。
- codestable：无稳定业务规则变化（渠道 Key 导入属既有能力的前端扩展，未触中继 / 渠道选择等 spec 承诺）；新增本快改记录。

顺手发现：`bulk-keys.test.ts` 早已存在却未注册进 `test:unit`，本次一并补上。
