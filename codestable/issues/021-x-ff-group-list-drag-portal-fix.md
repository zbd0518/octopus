---
kind: issue
title: "[ff] 修复分组列表视图成员拖动飞出与原位空白（transform 祖先劫持 fixed 定位）"
type: ff
status: closed
created: 2026-09-19
---

# [ff] 修复分组列表视图成员拖动飞出与原位空白

## 做了什么

分组页「列表视图」中，拖动分组内模型（成员）时被拖元素视觉上飞出到卡片外、原位置空白；松手后回到落点。修复后拖动元素贴随鼠标、成员列表内正常让位。

## 根因

列表视图的分组卡片由 `VirtualizedGrid` 渲染，每个虚拟行带 inline `transform: translateY(...)`（`web/src/components/common/VirtualizedGrid.tsx`）。`@hello-pangea/dnd` 拖动时给被拖元素设置 inline `position: fixed`（期望以视口为包含块）；CSS 规定祖先带 `transform` 时 `fixed` 的包含块变为该祖先 → 拖动坐标以虚拟行为基准，完全错乱（视觉"飞出"），库的让位/占位计算随之错乱（原位空白）。松手后 fixed 移除、回 normal flow（回到落点）。编辑器弹窗内拖动正常（不在虚拟行内），与症状分布一致。

## 改了哪些

仅 `web/src/components/modules/group/ItemList.tsx`（`MemberList`）：

1. 新增 `dragCloneContainer` state，挂载后指向 `document.body`（SSR 安全）。
2. `Draggable` render prop 中，`snapshot.isDragging` 时用 `createPortal` 把 `MemberItem` 挂到 body（库官方 portal 模式），脱离 transform 祖先；非拖动时原位渲染，行为不变。

注：最初尝试给 `DragDropContext` 传 `getContainerForClone`，读库源码（`dnd.esm.js` 的 `getClone()`）确认该 prop 属于 `Droppable` 且仅 `renderClone` 模式生效，已回退，改用 render prop portal。

## 怎样验证

- `npx tsc --noEmit`：`ItemList.tsx` 无报错（仓库存量测试文件报错与此无关）。
- `npx eslint src/components/modules/group/ItemList.tsx`：exit 0，无告警。
- `npm run test:i18n`：通过。
- 手测待用户确认：列表视图展开分组 → 拖动成员上下移动，拖动元素贴随鼠标、其余成员正常让位，松手落位正确；卡片视图与编辑器内拖动行为不变（它们不在 transform 祖先内，portal 分支只在 `isDragging` 时生效，不影响常态渲染）。

## 对 codestable 的影响

无制度记忆变化。候选沉淀：VirtualizedGrid（transform 虚拟行）内嵌 dnd 拖拽必须用 portal 模式——若 log/model/notification/site-channel 等其它虚拟化列表日后引入拖拽会复现同一坑，需要时再升格为 spec 条目。

---

## 附：隐私保护 entropy 误报收紧（2026-09-19，同日追加快改）

用户实测发现 entropy（高熵随机串）检测大量误报：文件路径 `/e/workspace/idea/kotlin_demo`、包名 `/client/exchange/MultiPlatformExchangeApi`、分支名 `-o-feature-lx-sdk-sync-doc-baseline-20260917`、词拼接 `multiplatformexchangeapi` 均被命中——它们字符多样性够但**不是随机串**。

**修复**（A+B 同时做）：

1. 检测器收紧（`internal/relay/privacy/validators.go`）：熵阈值 3.8→4.2、最短长度 20→24；新增 `looksLikeHumanIdentifier` 负向过滤——分隔符（`/ - _ .`）切出 >3 段判定为路径/分支名/包名，唯一字符占比 <0.6 判定为重复率高的词拼接，两者直接跳过。
2. 默认关闭（`internal/model/privacy.go` `CategoryEnabled`）：entropy 成为唯一默认禁用的类别，显式开启才参与检测；三语 UI 描述加「实验性：误报较多，默认关闭」。
3. 回归测试（`entropy_regression_test.go`）：用户日志中的全部 17 个误报样例逐一断言不命中；真随机密钥仍命中。

验证：`go build ./...`、relay/privacy/model 三包测试、`go vet`、前端 `test:i18n` 全绿。

---

## 附二：隐私保护命中日志开关（2026-09-19，同日追加快改）

前述命中明细日志（block/filter/还原三处）是无条件打印的，高频请求会刷屏。加开关：

1. 新增 setting `privacy_protection_log_enabled`（`internal/model/setting.go`，默认 `false`，并入布尔校验 case）。
2. `internal/relay/privacy_filter.go`：`privacyFilterConfig` 增加 `LogHits`（`loadPrivacyFilterConfig` 中读取，早于总开关判断返回）；`privacyPlaceholderMap` 增加 `logHits`（响应还原发生在请求侧之后，需要自带该标志），由 `relay.go` 装配时传入；block 命中、filter 脱敏、响应还原三处日志分别用这两个标志门控。
3. 前端 `PrivacyProtection.tsx`：总开关旁（左侧）加「命中日志」开关 + Hint（label「命中日志」，hint「在服务端日志打印每次拦截/脱敏/还原的命中内容，仅用于调试。默认关闭：高频请求会产生大量日志」）；`saveBooleanSetting` 从硬编码总开关泛化为 `(key, checked)`；`endpoints/setting.ts` 加 `PrivacyProtectionLogEnabled`。
4. 三语 locale 加 `setting.privacyProtection.logEnabled.{label,hint}`。

验证：`go build ./...`、`go vet`、relay/privacy/model 三包测试、tsc（无本改动相关错误，存量 .test.ts 报错与此无关）、eslint、`test:i18n` 全绿。
