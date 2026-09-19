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
