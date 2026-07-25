---
doc_type: issue-report
issue: 2026-07-24-group-editor-no-key-blocks-site-channel
status: confirmed
issue_path: fast-track
severity: P1
summary: 站点 Access Token 投影渠道 keys 为空时，分组编辑器标「无可用 Key」，用户感觉无法选用（本次静态风险改动回归）
tags:
  - group
  - editor
  - site
  - channel-key
---

# 分组编辑器对空 Key 投影渠道标「无可用 Key」 Issue Report

## 1. 问题现象

在**站点管理**通过 **Access Token** 添加的账号，映射/投影后的渠道里 **API Key 为空**。  
进入**路由分组**新增/编辑时，该渠道模型会显示 **「无可用 Key」**，用户感觉「无法选用」。  
用户确认：实际 **仍可点进右侧已选列表**，但带徽章，和改前「可以直接选、无提示」体验不一致。  
用户怀疑与本次「分组编辑器成员静态风险」改动相关。

## 2. 复现步骤

1. 站点管理用 Access Token 添加账号并完成映射/同步，得到投影渠道  
2. 打开渠道详情：观察到 API Key 为空（或无可启用 Key）  
3. 打开路由分组 → 编辑/新增 → 左侧候选或右侧已选中看到该渠道模型  
4. 观察到：出现「无可用 Key」徽章（及硬风险弱化样式）  

复现频率：稳定（在 keys 为空的投影渠道上）

## 3. 期望 vs 实际

**期望行为**：与改前一致，Access Token 映射渠道在分组编辑器中可正常选用；若仅展示风险，不应表现为「选不了」。

**实际行为**：显示「无可用 Key」；可选中但仍被硬风险样式弱化，用户感知为无法选用。

## 4. 环境信息

- 涉及模块 / 功能：分组编辑器、站点投影渠道、渠道 Key 列表  
- 相关文件 / 函数（线索，非已确认根因）：  
  - `web/src/components/modules/group/editor-member-status.ts`（`hasEnabledChannelKey`）  
  - `web/src/components/modules/group/Editor.tsx`（`hardRisk` / 徽章）  
  - `internal/sitesync/project.go`（`buildChannelKeys`）  
  - `internal/sitesync/sync.go`（Access Token 同步与 token 回落）  
- 运行环境：本地新编译版本（含 group-editor-member-status）  
- 其他上下文：紧随 `feat(group): 分组编辑器展示成员静态风险状态`（`ac728b4`）

## 5. 严重程度

**P1 严重**：影响站点 Access Token 投影渠道的组包体验；有绕过（仍可点选），但易误判为不可用。

## 6. 补充线索

- 用户选择复现形态：**2** — 能点进右侧，但有「无可用 Key」徽章  
- 添加按钮代码路径：`disabled={isSelected}` 仅，无 Key 不禁用点击  
- 真正转发路径仍要求 `ChannelKey` 非空（空 Key 进组后运行时也可能失败）— 是否「以前能跑」待 fix 阶段再验数据  

## 7. 路径判定

- 根因 file:line 已能指出（本次编辑器静态风险 + 空 keys 数据态）  
- 修复点预计 1–2 处（前端风险判定/样式，或窄化 no-key 对投影渠道的语义）  
- 无跨模块协议变更  

→ 建议 **fast-track**（见 approval-report）
