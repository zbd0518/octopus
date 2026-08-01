package task

import (
	"context"
	"time"

	"github.com/lingyuins/octopus/internal/db"
	"github.com/lingyuins/octopus/internal/model"
	"github.com/lingyuins/octopus/internal/op/setting"
	"github.com/lingyuins/octopus/internal/planprovider"
	"github.com/lingyuins/octopus/internal/utils/log"
)

const TaskPlanProviderAutoRefresh = "plan_provider_auto_refresh"

// planProviderDue 判断监控是否到点刷新：单个覆盖间隔优先，否则用全局默认间隔。
func planProviderDue(p model.PlanProvider, globalIntervalMin int, now time.Time) bool {
	intervalMin := p.RefreshIntervalMin
	if intervalMin < 1 {
		intervalMin = globalIntervalMin
	}
	return p.LastRefresh == nil || now.Sub(*p.LastRefresh) >= time.Duration(intervalMin)*time.Minute
}

// PlanProviderAutoRefreshTask 额度监控自动刷新任务。
//
// tick 固定 1 分钟，任务内部按"单个覆盖间隔 → 全局默认间隔"逐监控判断是否到点刷新，
// 因此任意粒度（≥1 分钟）的间隔都能生效，全局默认间隔修改后下一轮自动生效，
// 无需 task.Update 联动。单个监控刷新失败只记日志，不影响其他监控。
func PlanProviderAutoRefreshTask() {
	ctx := context.Background()

	// 存量 DeepSeek 渠道模型迁移（幂等，迁移后不再触发）
	if _, err := planprovider.MigrateLegacyDeepSeekChannels(ctx); err != nil {
		log.Warnf("plan provider auto refresh: migrate legacy deepseek channels failed: %v", err)
	}

	globalMin, err := setting.GetInt(model.SettingKeyPlanProviderRefreshInterval)
	if err != nil || globalMin < 1 {
		globalMin = 30
	}

	var providers []model.PlanProvider
	if err := db.GetDB().WithContext(ctx).Where("status = ?", "active").Find(&providers).Error; err != nil {
		log.Warnf("plan provider auto refresh: list providers failed: %v", err)
		return
	}

	now := time.Now()
	refreshed := 0
	for _, p := range providers {
		if !planProviderDue(p, globalMin, now) {
			continue
		}
		if _, err := planprovider.RefreshProvider(ctx, p.ID); err != nil {
			log.Warnf("plan provider %d (%s) auto refresh failed: %v", p.ID, p.Name, err)
			continue
		}
		refreshed++
	}
	if refreshed > 0 {
		log.Debugf("plan provider auto refresh: refreshed %d provider(s)", refreshed)
	}
}
