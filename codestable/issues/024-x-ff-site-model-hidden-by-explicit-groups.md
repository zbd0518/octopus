---
kind: issue
title: "站点分组里有模型计数但站点渠道模型列表为空"
type: ff
status: closed
created: 2026-10-27
---

# 站点分组里有模型计数但站点渠道模型列表为空

> **读者：** 以后搜到这条时——「改了啥、怎么信、动没动制度记忆」。

站点卡片上账号行显示「模型 4」，但打开站点渠道弹窗后该分组的模型列表为空；本地投射渠道的模型列表同样缺失。根因是读取侧与投射侧用上游 `enable_groups` 二次否决了模型行自身所属分组：模型行由「该分组 token 实际拉到的 `/v1/models`」或「按 `enable_groups` 扩展到无 key 分组」写入，`group_key` 已经是归属结论；再用同一份上游元数据去否决它，只要上游分组命名（大小写、别名、名称 vs key）与本地 `group_key` 不一致，整组模型就会被静默隐藏——而站点卡片的计数直接取 `site_models` 行数，于是出现「计数 4、列表 0」。

- 改动：
  - `internal/op/site_channel.go` — `buildSiteChannelGroups` 不再调用 `siteModelBelongsToGroup` 过滤模型行，删除该函数；补注释说明「模型行 `group_key` 是归属权威，`enable_groups` 只用于向无 key 分组扩展」。
  - `internal/sitesync/project.go` — 同步移除 `siteModelBelongsToProjectedGroup` 过滤及其函数，投射渠道的 `Channel.Model` 按行归属生成，与站点渠道视图保持一致。
  - `internal/op/site_channel_test.go` — 新增 `TestSiteChannelAccountGetKeepsModelInItsOwnGroupWhenExplicitGroupsDiffer`：行落在 `default`、元数据 `enable_groups=["vip"]` 时 `ModelCount` 必须为 1。
  - `internal/sitesync/project_test.go` — 新增 `TestProjectAccountKeepsModelInItsOwnGroupWhenExplicitGroupsDiffer`：同形态模型必须出现在 `default` 投射渠道的 `Model` 列表中。
- 验证：`go build ./...`、`go vet ./internal/op ./internal/sitesync` 通过；`go test ./internal/op ./internal/sitesync -count=1` 全绿。把两个源文件临时 stash 回旧实现后，两条新用例均失败（`expected model count 1, got 0` / 渠道模型缺失），确认用例确实覆盖原缺陷。未在真实实例上手工复验。
- codestable：无稳定业务规则变化，属读取/投射一致性修复；`enable_groups` 的既有用途（向无 key 分组扩展模型，见 `expandExplicitGroupModelsToGroups`）保持不变。

排查线索（给未来的自己）：线上排查该现象时，看 `site_models` 的 `group_key` 与 `route_raw_payload.enable_groups` 是否互相矛盾，即可确认是否同一根因——旧实现下矛盾必然表现为「计数非零、列表为空」。

顺带观察：`internal/sitesync/sync_fetch.go` 的 `buildGlobalSiteModels` 已无调用方（旧的全量铺开设计残留），本次未删。
