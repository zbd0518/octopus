package stats

import (
	"context"
	"errors"
	"fmt"

	"github.com/lingyuins/octopus/internal/db"
	"github.com/lingyuins/octopus/internal/model"
	"github.com/lingyuins/octopus/internal/store"
	"github.com/lingyuins/octopus/internal/utils/log"
	"gorm.io/gorm"
)

// RefreshCache loads all statistics from the database into the in-memory caches.
func RefreshCache(ctx context.Context) error {
	dbConn := db.GetDB().WithContext(ctx)
	todayDate := today()

	var loadedDaily model.StatsDaily
	result := dbConn.Last(&loadedDaily)
	if result.Error != nil && !errors.Is(result.Error, gorm.ErrRecordNotFound) {
		return fmt.Errorf("failed to get daily stats: %v", result.Error)
	}
	if result.RowsAffected == 0 || loadedDaily.Date != todayDate {
		loadedDaily = model.StatsDaily{Date: todayDate}
	}

	var loadedTotal model.StatsTotal
	result = dbConn.First(&loadedTotal)
	if result.Error != nil && !errors.Is(result.Error, gorm.ErrRecordNotFound) {
		return fmt.Errorf("failed to get total stats: %v", result.Error)
	}
	if result.RowsAffected == 0 {
		loadedTotal = model.StatsTotal{ID: 1}
	} else if loadedTotal.ID == 0 {
		loadedTotal.ID = 1
	}

	var loadedChannels []model.StatsChannel
	result = dbConn.Find(&loadedChannels)
	if result.Error != nil {
		return fmt.Errorf("failed to get channels: %v", result.Error)
	}

	var loadedModels []model.StatsModel
	result = dbConn.Find(&loadedModels)
	if result.Error != nil {
		return fmt.Errorf("failed to get model stats: %v", result.Error)
	}

	var loadedHourly []model.StatsHourly
	result = dbConn.Where("date = ?", todayDate).Find(&loadedHourly)
	if result.Error != nil {
		return fmt.Errorf("failed to get hourly stats: %v", result.Error)
	}

	dailyCacheLock.Lock()
	dailyCache = loadedDaily
	dailyCacheLock.Unlock()

	totalCacheLock.Lock()
	totalCache = loadedTotal
	totalCacheLock.Unlock()

	channelCache.Clear()
	channelCacheNeedUpdateLock.Lock()
	channelCacheNeedUpdate = make(map[int]struct{})
	channelCacheNeedUpdateLock.Unlock()
	for _, v := range loadedChannels {
		channelCache.Set(v.ChannelID, v)
	}

	modelCache.Clear()
	modelCacheNeedUpdateLock.Lock()
	modelCacheNeedUpdate = make(map[int64]struct{})
	modelCacheNeedUpdateLock.Unlock()
	// 同步重置活跃时间追踪，并将从 DB 恢复的条目标记为"刚活跃"，避免启动后立刻被
	// PurgeIdleModelStats 回收（见 issue #124）。
	modelLastActivity.Range(func(k, _ any) bool { modelLastActivity.Delete(k); return true })
	for _, v := range loadedModels {
		modelCache.Set(v.ID, v)
		touchModelActivity(v.ID)
	}

	var loadedAPIKeys []model.StatsAPIKey
	result = dbConn.Find(&loadedAPIKeys)
	if result.Error != nil {
		return fmt.Errorf("failed to get api key stats: %v", result.Error)
	}

	apiKeyCache.Clear()
	apiKeyCacheNeedUpdateLock.Lock()
	apiKeyCacheNeedUpdate = make(map[int]struct{})
	apiKeyCacheNeedUpdateLock.Unlock()
	for _, v := range loadedAPIKeys {
		apiKeyCache.Set(v.APIKeyID, v)
	}

	hourlyCacheLock.Lock()
	hourlyCache = [24]model.StatsHourly{}
	for _, v := range loadedHourly {
		if v.Hour >= 0 && v.Hour < 24 {
			hourlyCache[v.Hour] = v
		}
	}
	hourlyCacheLock.Unlock()

	// Redis 后端：叠加未落盘的增量（崩溃恢复，issue #123）。
	// DB 存上次 SaveDB 的快照，Redis 存自上次 SaveDB 以来的增量，二者相加得当前值。
	applyRedisDeltas(ctx)

	return nil
}

// applyRedisDeltas 从 Redis 读取各 scope 的未落盘增量，叠加到内存镜像。
// 仅在 Redis 启用时执行；失败时记录日志但不阻塞启动（DB 数据仍可用）。
func applyRedisDeltas(ctx context.Context) {
	if !store.Enabled() {
		return
	}
	ss := store.GetStats()

	// total
	if m, err := ss.GetMetrics(ctx, statsScopeTotal, statsIDTotal); err == nil {
		totalCacheLock.Lock()
		totalCache.StatsMetrics.Add(m)
		totalCacheLock.Unlock()
	} else {
		log.Warnf("stats redis restore total: %v", err)
	}

	// daily（今日）
	if m, err := ss.GetMetrics(ctx, statsScopeDaily, today()); err == nil {
		dailyCacheLock.Lock()
		if dailyCache.Date == today() {
			dailyCache.StatsMetrics.Add(m)
		}
		dailyCacheLock.Unlock()
	} else {
		log.Warnf("stats redis restore daily: %v", err)
	}

	// hourly（今日各小时）
	nowTime := now()
	todayDate := nowTime.Format("20060102")
	hourlyCacheLock.Lock()
	for h := 0; h < 24; h++ {
		if m, err := ss.GetMetrics(ctx, statsScopeHourly, fmt.Sprintf("%s:%d", todayDate, h)); err == nil {
			if hourlyCache[h].Date == todayDate {
				hourlyCache[h].StatsMetrics.Add(m)
			}
		}
	}
	hourlyCacheLock.Unlock()
	_ = nowTime

	// channel（全量快照）
	if snaps, err := ss.SnapshotAll(ctx, statsScopeChannel); err == nil {
		// 先收集候选 ID，批量校验 channels 表，避免把已删除渠道重新 dirty。
		candidateIDs := make([]int, 0, len(snaps))
		parsed := make(map[int]model.StatsMetrics, len(snaps))
		for idStr, m := range snaps {
			var id int
			if _, err := fmt.Sscanf(idStr, "%d", &id); err != nil || id == 0 {
				continue
			}
			if isChannelDeleted(id) {
				_ = ss.Delete(ctx, statsScopeChannel, idStr)
				continue
			}
			candidateIDs = append(candidateIDs, id)
			parsed[id] = m
		}
		existing, err := loadExistingChannelIDSet(ctx, candidateIDs)
		if err != nil {
			log.Warnf("stats redis restore channels existence check: %v", err)
			// 查询失败时仍恢复，后续 SaveDB 的 filterLiveChannelIDs 会再兜底。
			existing = make(map[int]struct{}, len(candidateIDs))
			for _, id := range candidateIDs {
				existing[id] = struct{}{}
			}
		}
		// 孤儿先在锁外 drop，避免 dropDeletedChannelStats 与 channelMutationLock 死锁。
		for id := range parsed {
			if _, ok := existing[id]; !ok {
				dropDeletedChannelStats(id)
				delete(parsed, id)
			}
		}
		channelMutationLock.Lock()
		for id, m := range parsed {
			if isChannelDeleted(id) {
				continue
			}
			entry, ok := channelCache.Get(id)
			if !ok {
				entry = model.StatsChannel{ChannelID: id}
			}
			entry.StatsMetrics.Add(m)
			channelCache.Set(id, entry)
			channelCacheNeedUpdateLock.Lock()
			channelCacheNeedUpdate[id] = struct{}{}
			channelCacheNeedUpdateLock.Unlock()
		}
		channelMutationLock.Unlock()
	} else {
		log.Warnf("stats redis restore channels: %v", err)
	}

	// model（全量快照）
	if snaps, err := ss.SnapshotAll(ctx, statsScopeModel); err == nil {
		modelMutationLock.Lock()
		for idStr, m := range snaps {
			var id int64
			if _, err := fmt.Sscanf(idStr, "%d", &id); err != nil || id == 0 {
				continue
			}
			entry, ok := modelCache.Get(id)
			if !ok {
				entry = model.StatsModel{ID: id}
			}
			entry.StatsMetrics.Add(m)
			modelCache.Set(id, entry)
			modelCacheNeedUpdateLock.Lock()
			modelCacheNeedUpdate[id] = struct{}{}
			modelCacheNeedUpdateLock.Unlock()
		}
		modelMutationLock.Unlock()
	} else {
		log.Warnf("stats redis restore models: %v", err)
	}

	// apikey（全量快照）
	if snaps, err := ss.SnapshotAll(ctx, statsScopeAPIKey); err == nil {
		apiKeyMutationLock.Lock()
		for idStr, m := range snaps {
			var id int
			if _, err := fmt.Sscanf(idStr, "%d", &id); err != nil || id == 0 {
				continue
			}
			entry, ok := apiKeyCache.Get(id)
			if !ok {
				entry = model.StatsAPIKey{APIKeyID: id}
			}
			entry.StatsMetrics.Add(m)
			apiKeyCache.Set(id, entry)
			apiKeyCacheNeedUpdateLock.Lock()
			apiKeyCacheNeedUpdate[id] = struct{}{}
			apiKeyCacheNeedUpdateLock.Unlock()
		}
		apiKeyMutationLock.Unlock()
	} else {
		log.Warnf("stats redis restore apikeys: %v", err)
	}
}
