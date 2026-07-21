package ops

import (
	"context"
	"fmt"
	"github.com/lingyuins/octopus/internal/utils/json"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/lingyuins/octopus/internal/conf"
	"github.com/lingyuins/octopus/internal/db"
	"github.com/lingyuins/octopus/internal/model"
	"github.com/lingyuins/octopus/internal/op/airoute"
	"github.com/lingyuins/octopus/internal/op/analytics"
	"github.com/lingyuins/octopus/internal/op/apikey"
	"github.com/lingyuins/octopus/internal/op/cacheusage"
	"github.com/lingyuins/octopus/internal/op/channel"
	"github.com/lingyuins/octopus/internal/op/group"
	"github.com/lingyuins/octopus/internal/op/llm"
	"github.com/lingyuins/octopus/internal/op/relaylog"
	"github.com/lingyuins/octopus/internal/op/setting"
	"github.com/lingyuins/octopus/internal/op/stats"
	"github.com/lingyuins/octopus/internal/utils/semantic_cache"
	"github.com/lingyuins/octopus/internal/utils/telemetry"
)

const (
	opsHealthErrorWindow           = 24 * time.Hour
	opsFailingGroupLimit           = 6
	semanticCacheDefaultTTLSeconds = 3600
	semanticCacheDefaultThreshold  = 98
	semanticCacheDefaultMaxEntries = 1000
	semanticCacheDefaultTimeoutSec = 10
)

var processStartTime = time.Now()

type opsQuotaUsage struct {
	RequestCount int64
	TotalCost    float64
	TotalTokens  int64
	SuccessCount int64
}

type opsProviderPromptCacheUsage struct {
	PromptTokens             int64
	TotalInputTokens         int64
	CachedTokens             int64
	CacheCreationInputTokens int64
}

type opsProviderPromptCacheAggregate struct {
	ChannelName        string
	RequestCount       int64
	CachedRequestCount int64
	TotalInputTokens   int64
	CacheReadTokens    int64
	CacheWriteTokens   int64
	EstimatedCostSaved float64
}

func OpsCacheStatusGet(ctx context.Context) (*model.OpsCacheStatus, error) {
	enabled, err := setting.GetBool(model.SettingKeySemanticCacheEnabled)
	if err != nil {
		return nil, err
	}
	ttlSeconds, err := setting.GetInt(model.SettingKeySemanticCacheTTL)
	if err != nil {
		return nil, err
	}
	threshold, err := setting.GetInt(model.SettingKeySemanticCacheThreshold)
	if err != nil {
		return nil, err
	}
	maxEntries, err := setting.GetInt(model.SettingKeySemanticCacheMaxEntries)
	if err != nil {
		return nil, err
	}

	hits, misses, size := semantic_cache.Stats()
	status := buildOpsCacheStatus(enabled, semantic_cache.RuntimeEnabled(), ttlSeconds, threshold, maxEntries, hits, misses, size)
	status.ProviderPromptCache = buildOpsProviderPromptCacheSummary(ctx)
	return &status, nil
}

func RefreshSemanticCacheRuntime() error {
	cfg, ok, err := buildSemanticCacheRuntimeConfigFromSettings()
	if err != nil {
		return err
	}
	if !ok {
		semantic_cache.Reset()
		return nil
	}
	semantic_cache.ApplyRuntimeConfig(cfg)
	return nil
}

func OpsQuotaSummaryGet(ctx context.Context) (*model.OpsQuotaSummary, error) {
	apiKeys, err := apikey.List(ctx)
	if err != nil {
		return nil, err
	}

	summary := buildOpsQuotaSummary(apiKeys, stats.APIKeyList(), time.Now())
	return &summary, nil
}

func OpsHealthStatusGet(ctx context.Context) (*model.OpsHealthStatus, error) {
	cacheStatus, err := OpsCacheStatusGet(ctx)
	if err != nil {
		return nil, err
	}

	recentErrorCount, err := loadOpsRecentErrorCount(ctx, time.Now().Add(-opsHealthErrorWindow))
	if err != nil {
		return nil, err
	}

	groupHealth, err := analytics.AnalyticsGroupHealthGet(ctx)
	if err != nil {
		return nil, err
	}

	status := buildOpsHealthStatus(
		pingDatabase(ctx),
		!cacheStatus.Enabled || cacheStatus.RuntimeEnabled,
		opsTaskRuntimeOK(),
		recentErrorCount,
		groupHealth,
		time.Now(),
	)
	return &status, nil
}

func OpsSystemSummaryGet(ctx context.Context) (*model.OpsSystemSummary, error) {
	proxyURL, err := setting.GetString(model.SettingKeyProxyURL)
	if err != nil {
		return nil, err
	}
	publicAPIBaseURL, err := setting.GetString(model.SettingKeyPublicAPIBaseURL)
	if err != nil {
		return nil, err
	}
	relayLogKeepEnabled, err := setting.GetBool(model.SettingKeyRelayLogKeepEnabled)
	if err != nil {
		return nil, err
	}
	relayLogKeepDays, err := setting.GetInt(model.SettingKeyRelayLogKeepPeriod)
	if err != nil {
		return nil, err
	}
	relayLogKeepCount, err := setting.GetInt(model.SettingKeyRelayLogKeepCount)
	if err != nil {
		return nil, err
	}
	statsSaveIntervalMinutes, err := setting.GetInt(model.SettingKeyStatsSaveInterval)
	if err != nil {
		return nil, err
	}
	syncLLMIntervalHours, err := setting.GetInt(model.SettingKeySyncLLMInterval)
	if err != nil {
		return nil, err
	}
	modelInfoUpdateIntervalHours, err := setting.GetInt(model.SettingKeyModelInfoUpdateInterval)
	if err != nil {
		return nil, err
	}
	aiRouteGroupID, err := setting.GetInt(model.SettingKeyAIRouteGroupID)
	if err != nil {
		return nil, err
	}
	aiRouteTimeoutSeconds, err := setting.GetInt(model.SettingKeyAIRouteTimeoutSeconds)
	if err != nil {
		return nil, err
	}
	aiRouteParallelism, err := setting.GetInt(model.SettingKeyAIRouteParallelism)
	if err != nil {
		return nil, err
	}

	channels, err := channel.List(ctx)
	if err != nil {
		return nil, err
	}
	groups, err := group.GroupList(ctx)
	if err != nil {
		return nil, err
	}
	apiKeys, err := apikey.List(ctx)
	if err != nil {
		return nil, err
	}

	aiRouteServices, legacyMode := loadOpsAIRouteServicesSummary()
	enabledServiceCount := 0
	for _, service := range aiRouteServices {
		if service.Enabled {
			enabledServiceCount++
		}
	}

	summary := &model.OpsSystemSummary{
		Version:                      conf.Version,
		Commit:                       conf.Commit,
		BuildTime:                    conf.BuildTime,
		Repo:                         conf.Repo,
		DatabaseType:                 conf.AppConfig.Database.Type,
		PublicAPIBaseURL:             strings.TrimSpace(publicAPIBaseURL),
		ProxyURL:                     strings.TrimSpace(proxyURL),
		RelayLogKeepEnabled:          relayLogKeepEnabled,
		RelayLogKeepDays:             relayLogKeepDays,
		RelayLogKeepCount:            relayLogKeepCount,
		StatsSaveIntervalMinutes:     statsSaveIntervalMinutes,
		SyncLLMIntervalHours:         syncLLMIntervalHours,
		ModelInfoUpdateIntervalHours: modelInfoUpdateIntervalHours,
		ImportEnabled:                true,
		ExportEnabled:                true,
		AIRouteGroupID:               aiRouteGroupID,
		AIRouteTimeoutSeconds:        aiRouteTimeoutSeconds,
		AIRouteParallelism:           aiRouteParallelism,
		AIRouteLegacyMode:            legacyMode,
		AIRouteServiceCount:          len(aiRouteServices),
		AIRouteEnabledServiceCount:   enabledServiceCount,
		AIRouteServices:              aiRouteServices,
		ChannelCount:                 len(channels),
		GroupCount:                   len(groups),
		APIKeyCount:                  len(apiKeys),
	}
	return summary, nil
}

func buildOpsCacheStatus(
	enabled bool,
	runtimeEnabled bool,
	ttlSeconds int,
	threshold int,
	maxEntries int,
	hits int64,
	misses int64,
	size int,
) model.OpsCacheStatus {
	totalLookups := hits + misses
	hitRate := 0.0
	if totalLookups > 0 {
		hitRate = (float64(hits) / float64(totalLookups)) * 100
	}

	usageRate := 0.0
	if maxEntries > 0 {
		usageRate = (float64(size) / float64(maxEntries)) * 100
	}

	return model.OpsCacheStatus{
		Enabled:        enabled,
		RuntimeEnabled: runtimeEnabled,
		TTLSeconds:     ttlSeconds,
		Threshold:      threshold,
		MaxEntries:     maxEntries,
		CurrentEntries: size,
		Hits:           hits,
		Misses:         misses,
		HitRate:        hitRate,
		UsageRate:      usageRate,
	}
}

// providerPromptCacheResult caches the expensive provider prompt cache summary
// which loads relay logs including response_content. It is called from multiple
// ops endpoints (cache, health, telemetry), so a short TTL avoids redundant DB queries.
var (
	providerPromptCacheMu     sync.RWMutex
	providerPromptCacheResult model.OpsProviderPromptCacheSummary
	providerPromptCacheExp    time.Time
)

const providerPromptCacheTTL = 60 * time.Second

func buildOpsProviderPromptCacheSummary(ctx context.Context) model.OpsProviderPromptCacheSummary {
	providerPromptCacheMu.RLock()
	if time.Now().Before(providerPromptCacheExp) {
		cached := providerPromptCacheResult
		providerPromptCacheMu.RUnlock()
		return cached
	}
	providerPromptCacheMu.RUnlock()

	start := opsHourlyWindowStart(time.Now(), stats.StatsLocation())
	logs := loadOpsProviderPromptCacheLogs(ctx, start)
	result := buildOpsProviderPromptCacheSummaryFromLogs(logs, start)

	providerPromptCacheMu.Lock()
	providerPromptCacheResult = result
	providerPromptCacheExp = time.Now().Add(providerPromptCacheTTL)
	providerPromptCacheMu.Unlock()

	return result
}

// opsHourlyWindowStart 计算过去 24 小时（按整点对齐）的窗口起点，返回 UTC 时间。
// loc 为统计时区，窗口按 loc 的整点对齐后转回 UTC 用于 DB 时间过滤。
func opsHourlyWindowStart(now time.Time, loc *time.Location) time.Time {
	localNow := now.In(loc)
	localStart := localNow.Add(-23 * time.Hour).Truncate(time.Hour)
	return localStart.In(time.UTC)
}

func buildOpsProviderPromptCacheSummaryFromLogs(
	logs []model.RelayLog,
	start time.Time,
) model.OpsProviderPromptCacheSummary {
	const bucketCount = 24

	summary := model.OpsProviderPromptCacheSummary{
		Providers: []model.OpsProviderPromptCacheProviderItem{},
		Trend:     make([]model.OpsProviderPromptCacheTrendPoint, bucketCount),
	}

	for i := 0; i < bucketCount; i++ {
		summary.Trend[i] = model.OpsProviderPromptCacheTrendPoint{
			Timestamp: start.Add(time.Duration(i) * time.Hour).Unix(),
		}
	}

	providers := make(map[int]opsProviderPromptCacheAggregate)
	var totalInputTokens int64

	summary.SampledLogCount = int64(len(logs))

	for _, relayLog := range logs {
		usage, ok := parseOpsProviderPromptCacheUsageFromLog(relayLog)
		if !ok {
			continue
		}
		summary.ParsedLogCount++
		if relayLog.SemanticCacheHit {
			// 语义缓存命中不是上游 prompt cache，跳过以免污染指标。
			continue
		}
		// Count as cached if provider returned cached_tokens (>0) or has cache_write tokens (Anthropic prompt cache)
		isCached := usage.CachedTokens > 0 || usage.CacheCreationInputTokens > 0

		aggregate := providers[relayLog.ChannelId]
		if aggregate.ChannelName == "" {
			aggregate.ChannelName = strings.TrimSpace(relayLog.ChannelName)
		}
		aggregate.RequestCount++
		if isCached {
			aggregate.CachedRequestCount++
		}
		aggregate.TotalInputTokens += usage.TotalInputTokens
		totalInputTokens += usage.TotalInputTokens
		aggregate.CacheReadTokens += usage.CachedTokens
		aggregate.CacheWriteTokens += usage.CacheCreationInputTokens
		aggregate.EstimatedCostSaved += estimateOpsProviderPromptCacheSaved(relayLog.ActualModelName, usage)
		providers[relayLog.ChannelId] = aggregate

		bucketIndex := int((time.Unix(relayLog.Time, 0).Sub(start)) / time.Hour)
		if bucketIndex >= 0 && bucketIndex < bucketCount {
			summary.Trend[bucketIndex].RequestCount++
			if isCached {
				summary.Trend[bucketIndex].CachedRequestCount++
			}
			summary.Trend[bucketIndex].CacheReadTokens += usage.CachedTokens
			summary.Trend[bucketIndex].CacheWriteTokens += usage.CacheCreationInputTokens
			summary.Trend[bucketIndex].EstimatedCostSaved += estimateOpsProviderPromptCacheSaved(relayLog.ActualModelName, usage)
		}
	}

	for channelID, aggregate := range providers {
		summary.RequestCount += aggregate.RequestCount
		summary.CachedRequestCount += aggregate.CachedRequestCount
		summary.CacheReadTokens += aggregate.CacheReadTokens
		summary.CacheWriteTokens += aggregate.CacheWriteTokens
		summary.EstimatedCostSaved += aggregate.EstimatedCostSaved

		item := model.OpsProviderPromptCacheProviderItem{
			ChannelID:          channelID,
			ChannelName:        resolveOpsProviderPromptCacheChannelName(channelID, aggregate.ChannelName),
			RequestCount:       aggregate.RequestCount,
			CachedRequestCount: aggregate.CachedRequestCount,
			CacheRate:          percent(aggregate.CachedRequestCount, aggregate.RequestCount),
			CacheReuseRatio:    percent(aggregate.CacheReadTokens, aggregate.TotalInputTokens),
			CacheReadTokens:    aggregate.CacheReadTokens,
			CacheWriteTokens:   aggregate.CacheWriteTokens,
			EstimatedCostSaved: aggregate.EstimatedCostSaved,
		}
		summary.Providers = append(summary.Providers, item)
	}

	sort.SliceStable(summary.Providers, func(i, j int) bool {
		if summary.Providers[i].EstimatedCostSaved != summary.Providers[j].EstimatedCostSaved {
			return summary.Providers[i].EstimatedCostSaved > summary.Providers[j].EstimatedCostSaved
		}
		if summary.Providers[i].CacheReadTokens != summary.Providers[j].CacheReadTokens {
			return summary.Providers[i].CacheReadTokens > summary.Providers[j].CacheReadTokens
		}
		if summary.Providers[i].RequestCount != summary.Providers[j].RequestCount {
			return summary.Providers[i].RequestCount > summary.Providers[j].RequestCount
		}
		return summary.Providers[i].ChannelName < summary.Providers[j].ChannelName
	})

	summary.CacheRate = percent(summary.CachedRequestCount, summary.RequestCount)
	summary.CacheReuseRatio = percent(summary.CacheReadTokens, totalInputTokens)
	summary.UsageSignalAvailable = summary.ParsedLogCount > 0
	for i := range summary.Trend {
		summary.Trend[i].CacheRate = percent(summary.Trend[i].CachedRequestCount, summary.Trend[i].RequestCount)
	}

	return summary
}

func resolveOpsProviderPromptCacheChannelName(channelID int, channelName string) string {
	if name := strings.TrimSpace(channelName); name != "" {
		return name
	}
	if channelID == 0 {
		return "Unknown"
	}
	return fmt.Sprintf("Channel %d", channelID)
}

func loadOpsProviderPromptCacheLogs(ctx context.Context, since time.Time) []model.RelayLog {
	logs := make([]model.RelayLog, 0)
	seen := make(map[int64]struct{})
	relayLogCache, relayLogCacheLock := relaylog.GetCacheAndLock()
	relayLogCacheLock.Lock()
	for _, relayLog := range relayLogCache {
		if relayLog.Time < since.Unix() {
			continue
		}
		// 缓存条目可能仍带 ResponseContent；优先用落库列，避免后续路径依赖大字段。
		logs = append(logs, model.RelayLog{
			ID:               relayLog.ID,
			Time:             relayLog.Time,
			ChannelId:        relayLog.ChannelId,
			ChannelName:      relayLog.ChannelName,
			ActualModelName:  relayLog.ActualModelName,
			InputTokens:      relayLog.InputTokens,
			CacheReadTokens:  relayLog.CacheReadTokens,
			SemanticCacheHit: relayLog.SemanticCacheHit,
			// ResponseContent 仅作兼容回退（旧缓存条目未填 CacheReadTokens 时）。
			ResponseContent: relayLog.ResponseContent,
		})
		seen[relayLog.ID] = struct{}{}
	}
	relayLogCacheLock.Unlock()

	keepEnabled, err := setting.GetBool(model.SettingKeyRelayLogKeepEnabled)
	if err != nil || !keepEnabled || db.GetLogDB() == nil {
		return logs
	}

	// 只选轻量列 + 已落库的 cache_read_tokens / semantic_cache_hit，禁止 response_content。
	type promptCacheLogRow struct {
		ID               int64  `gorm:"column:id"`
		Time             int64  `gorm:"column:time"`
		ChannelID        int    `gorm:"column:channel_id"`
		ChannelName      string `gorm:"column:channel_name"`
		ActualModelName  string `gorm:"column:actual_model_name"`
		InputTokens      int    `gorm:"column:input_tokens"`
		CacheReadTokens  int    `gorm:"column:cache_read_tokens"`
		SemanticCacheHit bool   `gorm:"column:semantic_cache_hit"`
	}
	var dbRows []promptCacheLogRow
	if err := db.GetLogDB().WithContext(ctx).
		Table("relay_logs").
		Select("id", "time", "channel_id", "channel_name", "actual_model_name", "input_tokens", "cache_read_tokens", "semantic_cache_hit").
		Where("time >= ?", since.Unix()).
		Order("time ASC").
		Find(&dbRows).Error; err != nil {
		return logs
	}

	for _, row := range dbRows {
		if _, ok := seen[row.ID]; ok {
			continue
		}
		logs = append(logs, model.RelayLog{
			ID:               row.ID,
			Time:             row.Time,
			ChannelId:        row.ChannelID,
			ChannelName:      row.ChannelName,
			ActualModelName:  row.ActualModelName,
			InputTokens:      row.InputTokens,
			CacheReadTokens:  row.CacheReadTokens,
			SemanticCacheHit: row.SemanticCacheHit,
		})
	}
	return logs
}

// parseOpsProviderPromptCacheUsageFromLog 优先使用落库列 CacheReadTokens/InputTokens，
// 仅当落库列为空时才回退解析 ResponseContent（兼容内存缓存中的旧条目）。
func parseOpsProviderPromptCacheUsageFromLog(relayLog model.RelayLog) (opsProviderPromptCacheUsage, bool) {
	if relayLog.CacheReadTokens > 0 || relayLog.InputTokens > 0 {
		usage := opsProviderPromptCacheUsage{
			PromptTokens:     int64(relayLog.InputTokens),
			CachedTokens:     int64(relayLog.CacheReadTokens),
			TotalInputTokens: int64(relayLog.InputTokens),
		}
		if usage.TotalInputTokens <= 0 && usage.CachedTokens <= 0 {
			return opsProviderPromptCacheUsage{}, false
		}
		return usage, true
	}
	if strings.TrimSpace(relayLog.ResponseContent) == "" {
		return opsProviderPromptCacheUsage{}, false
	}
	return parseOpsProviderPromptCacheUsage(relayLog.ResponseContent)
}

func parseOpsProviderPromptCacheUsage(responseContent string) (opsProviderPromptCacheUsage, bool) {
	signals, ok := cacheusage.ParseProviderPromptCacheUsageSignals(responseContent)
	if !ok {
		return opsProviderPromptCacheUsage{}, false
	}

	usage := opsProviderPromptCacheUsage{
		PromptTokens:             signals.PromptTokens,
		CachedTokens:             signals.CachedTokens,
		CacheCreationInputTokens: signals.CacheCreationInputTokens,
	}
	usage.TotalInputTokens = usage.PromptTokens
	if usage.CacheCreationInputTokens > 0 {
		usage.TotalInputTokens += usage.CachedTokens + usage.CacheCreationInputTokens
	}

	if usage.TotalInputTokens <= 0 && usage.CachedTokens <= 0 && usage.CacheCreationInputTokens <= 0 {
		return opsProviderPromptCacheUsage{}, false
	}
	return usage, true
}

func estimateOpsProviderPromptCacheSaved(modelName string, usage opsProviderPromptCacheUsage) float64 {
	modelName = strings.TrimSpace(modelName)
	if modelName == "" {
		return 0
	}

	price, err := llm.Get(strings.ToLower(modelName))
	if err != nil || price.Input <= 0 {
		return 0
	}

	cacheReadSavings := float64(usage.CachedTokens) * (price.Input - price.CacheRead) * 1e-6
	cacheWriteCost := 0.0
	if price.CacheWrite > 0 {
		cacheWriteCost = float64(usage.CacheCreationInputTokens) * (price.CacheWrite - price.Input) * 1e-6
	}

	saved := cacheReadSavings - cacheWriteCost
	if saved < 0 {
		return 0
	}
	return saved
}

func percent(part int64, total int64) float64 {
	if total <= 0 || part <= 0 {
		return 0
	}
	return float64(part) / float64(total) * 100
}

func buildSemanticCacheRuntimeConfigFromSettings() (semantic_cache.RuntimeConfig, bool, error) {
	enabled, err := setting.GetBool(model.SettingKeySemanticCacheEnabled)
	if err != nil {
		return semantic_cache.RuntimeConfig{}, false, err
	}
	if !enabled {
		return semantic_cache.RuntimeConfig{}, false, nil
	}

	ttlSeconds, err := setting.GetInt(model.SettingKeySemanticCacheTTL)
	if err != nil || ttlSeconds <= 0 {
		ttlSeconds = semanticCacheDefaultTTLSeconds
	}

	thresholdRaw, err := setting.GetInt(model.SettingKeySemanticCacheThreshold)
	if err != nil || thresholdRaw < 0 || thresholdRaw > 100 {
		thresholdRaw = semanticCacheDefaultThreshold
	}

	maxEntries, err := setting.GetInt(model.SettingKeySemanticCacheMaxEntries)
	if err != nil || maxEntries <= 0 {
		maxEntries = semanticCacheDefaultMaxEntries
	}

	baseURL, err := setting.GetString(model.SettingKeySemanticCacheEmbeddingBaseURL)
	if err != nil {
		return semantic_cache.RuntimeConfig{}, false, err
	}
	modelName, err := setting.GetString(model.SettingKeySemanticCacheEmbeddingModel)
	if err != nil {
		return semantic_cache.RuntimeConfig{}, false, err
	}
	baseURL = strings.TrimSpace(baseURL)
	modelName = strings.TrimSpace(modelName)
	if baseURL == "" || modelName == "" {
		return semantic_cache.RuntimeConfig{}, false, nil
	}

	apiKey, err := setting.GetString(model.SettingKeySemanticCacheEmbeddingAPIKey)
	if err != nil {
		return semantic_cache.RuntimeConfig{}, false, err
	}

	timeoutSeconds, err := setting.GetInt(model.SettingKeySemanticCacheEmbeddingTimeoutSeconds)
	if err != nil || timeoutSeconds <= 0 {
		timeoutSeconds = semanticCacheDefaultTimeoutSec
	}

	return semantic_cache.RuntimeConfig{
		Enabled:          true,
		MaxEntries:       maxEntries,
		Threshold:        float64(thresholdRaw) / 100.0,
		TTL:              time.Duration(ttlSeconds) * time.Second,
		EmbeddingBaseURL: baseURL,
		EmbeddingAPIKey:  strings.TrimSpace(apiKey),
		EmbeddingModel:   modelName,
		EmbeddingTimeout: time.Duration(timeoutSeconds) * time.Second,
	}, true, nil
}

func buildOpsQuotaSummary(apiKeys []model.APIKey, stats []model.StatsAPIKey, now time.Time) model.OpsQuotaSummary {
	usageByKeyID := make(map[int]opsQuotaUsage, len(stats))
	for _, stat := range stats {
		usageByKeyID[stat.APIKeyID] = opsQuotaUsage{
			RequestCount: stat.RequestSuccess + stat.RequestFailed,
			TotalCost:    stat.InputCost + stat.OutputCost,
			TotalTokens:  stat.InputToken + stat.OutputToken,
			SuccessCount: stat.RequestSuccess,
		}
	}

	items := make([]model.OpsQuotaKeyItem, 0, len(apiKeys))
	summary := model.OpsQuotaSummary{
		TotalKeyCount: len(apiKeys),
	}

	nowUnix := now.Unix()
	for _, apiKey := range apiKeys {
		usage := usageByKeyID[apiKey.ID]
		expired := apiKey.ExpireAt > 0 && apiKey.ExpireAt <= nowUnix
		hasPerModelQuota := hasPerModelQuota(apiKey.PerModelQuotaJSON)
		supportedModelCount := countCommaSeparated(apiKey.SupportedModels)
		limited := apiKey.RateLimitRPM > 0 || apiKey.RateLimitTPM > 0 || apiKey.MaxCost > 0 || apiKey.MaxTokens > 0 || hasPerModelQuota
		exhausted := (apiKey.MaxCost > 0 && usage.TotalCost >= apiKey.MaxCost) || (apiKey.MaxTokens > 0 && usage.TotalTokens >= apiKey.MaxTokens)

		status := "open"
		switch {
		case !apiKey.Enabled:
			status = "disabled"
		case expired:
			status = "expired"
		case exhausted:
			status = "exhausted"
		case limited:
			status = "limited"
		}

		if apiKey.Enabled {
			summary.EnabledKeyCount++
		}
		if apiKey.Enabled && !expired {
			summary.AvailableKeyCount++
		}
		if expired {
			summary.ExpiredKeyCount++
		}
		if limited {
			summary.LimitedKeyCount++
		} else {
			summary.UnlimitedKeyCount++
		}
		if exhausted {
			summary.ExhaustedKeyCount++
		}
		if hasPerModelQuota {
			summary.PerModelQuotaKeyCount++
		}
		if usage.RequestCount > 0 {
			summary.ActiveUsageKeyCount++
		}
		if apiKey.RateLimitRPM > 0 {
			summary.TotalRPM += apiKey.RateLimitRPM
		}
		if apiKey.RateLimitTPM > 0 {
			summary.TotalTPM += apiKey.RateLimitTPM
		}
		if apiKey.MaxCost > 0 {
			summary.TotalMaxCost += apiKey.MaxCost
		}
		if apiKey.MaxTokens > 0 {
			summary.TotalMaxTokens += apiKey.MaxTokens
		}

		name := strings.TrimSpace(apiKey.Name)
		if name == "" {
			name = "Key"
		}
		successRate := 0.0
		if usage.RequestCount > 0 {
			successRate = float64(usage.SuccessCount) / float64(usage.RequestCount) * 100
		}
		items = append(items, model.OpsQuotaKeyItem{
			APIKeyID:            apiKey.ID,
			Name:                name,
			Enabled:             apiKey.Enabled,
			Expired:             expired,
			Status:              status,
			SupportedModelCount: supportedModelCount,
			HasPerModelQuota:    hasPerModelQuota,
			RateLimitRPM:        apiKey.RateLimitRPM,
			RateLimitTPM:        apiKey.RateLimitTPM,
			MaxCost:             apiKey.MaxCost,
			MaxTokens:           apiKey.MaxTokens,
			RequestCount:        usage.RequestCount,
			TotalCost:           usage.TotalCost,
			TotalTokens:         usage.TotalTokens,
			SuccessRate:         successRate,
		})
	}

	sort.SliceStable(items, func(i, j int) bool {
		leftRank := opsQuotaStatusRank(items[i].Status)
		rightRank := opsQuotaStatusRank(items[j].Status)
		if leftRank != rightRank {
			return leftRank < rightRank
		}
		if items[i].RequestCount != items[j].RequestCount {
			return items[i].RequestCount > items[j].RequestCount
		}
		if items[i].TotalCost != items[j].TotalCost {
			return items[i].TotalCost > items[j].TotalCost
		}
		return items[i].Name < items[j].Name
	})

	summary.Keys = items
	return summary
}

func buildOpsHealthStatus(
	databaseOK bool,
	cacheOK bool,
	taskRuntimeOK bool,
	recentErrorCount int64,
	groupHealth []model.AnalyticsGroupHealthItem,
	now time.Time,
) model.OpsHealthStatus {
	status := model.OpsHealthStatus{
		DatabaseOK:       databaseOK,
		CacheOK:          cacheOK,
		TaskRuntimeOK:    taskRuntimeOK,
		RecentErrorCount: recentErrorCount,
		CheckedAt:        now.Unix(),
	}

	failingGroups := make([]model.OpsHealthGroupItem, 0, opsFailingGroupLimit)
	for _, group := range groupHealth {
		switch group.Status {
		case "healthy":
			status.HealthyGroupCount++
		case "warning":
			status.WarningGroupCount++
		case "degraded":
			status.DegradedGroupCount++
		case "down":
			status.DownGroupCount++
		case "empty":
			status.EmptyGroupCount++
		}

		if group.Status == "healthy" {
			continue
		}
		if len(failingGroups) >= opsFailingGroupLimit {
			continue
		}
		failingGroups = append(failingGroups, model.OpsHealthGroupItem{
			GroupID:      group.GroupID,
			GroupName:    group.GroupName,
			EndpointType: group.EndpointType,
			Status:       group.Status,
			FailureCount: group.FailureCount,
			HealthScore:  group.HealthScore,
		})
	}

	status.FailingGroups = failingGroups
	return status
}

func pingDatabase(ctx context.Context) bool {
	gormDB := db.GetDB()
	if gormDB == nil {
		return false
	}

	sqlDB, err := gormDB.DB()
	if err != nil {
		return false
	}
	if err := sqlDB.PingContext(ctx); err != nil {
		return false
	}
	return true
}

func opsTaskRuntimeOK() bool {
	statsSaveIntervalMinutes, err := setting.GetInt(model.SettingKeyStatsSaveInterval)
	if err != nil || statsSaveIntervalMinutes < 1 {
		return false
	}
	syncLLMIntervalHours, err := setting.GetInt(model.SettingKeySyncLLMInterval)
	if err != nil || syncLLMIntervalHours < 1 {
		return false
	}
	modelInfoUpdateIntervalHours, err := setting.GetInt(model.SettingKeyModelInfoUpdateInterval)
	if err != nil || modelInfoUpdateIntervalHours < 1 {
		return false
	}
	if _, err := setting.GetBool(model.SettingKeyRelayLogKeepEnabled); err != nil {
		return false
	}
	return true
}

func loadOpsRecentErrorCount(ctx context.Context, since time.Time) (int64, error) {
	startUnix := since.Unix()
	var errorCount int64

	keepEnabled, err := setting.GetBool(model.SettingKeyRelayLogKeepEnabled)
	if err != nil {
		return 0, err
	}

	if keepEnabled {
		if logDB := db.GetLogDB(); logDB != nil {
			if err := logDB.WithContext(ctx).
				Model(&model.RelayLog{}).
				Where("error <> ''").
				Where("time >= ?", startUnix).
				Count(&errorCount).Error; err != nil {
				return 0, err
			}
		}
	}
	cache, lock := relaylog.GetCacheAndLock()
	lock.Lock()
	for _, logItem := range cache {
		if logItem.Error != "" && logItem.Time >= startUnix {
			errorCount++
		}
	}
	lock.Unlock()

	return errorCount, nil
}

func loadOpsAIRouteServicesSummary() ([]model.OpsAIRouteServiceSummary, bool) {
	rawServices, _ := setting.GetString(model.SettingKeyAIRouteServices)
	rawServices = strings.TrimSpace(rawServices)
	if rawServices != "" && rawServices != "[]" {
		var configs []model.AIRouteServiceConfig
		if err := json.Unmarshal([]byte(rawServices), &configs); err == nil {
			return buildOpsAIRouteServices(configs), false
		}
	}

	baseURL, _ := setting.GetString(model.SettingKeyAIRouteBaseURL)
	apiKey, _ := setting.GetString(model.SettingKeyAIRouteAPIKey)
	modelName, _ := setting.GetString(model.SettingKeyAIRouteModel)
	if strings.TrimSpace(baseURL) == "" && strings.TrimSpace(apiKey) == "" && strings.TrimSpace(modelName) == "" {
		return buildOpsAIRouteServices(nil), false
	}

	enabled := strings.TrimSpace(baseURL) != "" && strings.TrimSpace(apiKey) != "" && strings.TrimSpace(modelName) != ""
	configs := []model.AIRouteServiceConfig{{
		Name:    "legacy",
		BaseURL: baseURL,
		APIKey:  apiKey,
		Model:   modelName,
		Enabled: &enabled,
	}}
	return buildOpsAIRouteServices(configs), true
}

func buildOpsAIRouteServices(configs []model.AIRouteServiceConfig) []model.OpsAIRouteServiceSummary {
	services := make([]model.OpsAIRouteServiceSummary, 0, len(configs))
	for i, cfg := range configs {
		services = append(services, model.OpsAIRouteServiceSummary{
			Name:    airoute.NormalizeAIRouteServiceName(cfg, i+1),
			BaseURL: strings.TrimSpace(cfg.BaseURL),
			Model:   strings.TrimSpace(cfg.Model),
			Enabled: cfg.IsEnabled(),
		})
	}

	sort.SliceStable(services, func(i, j int) bool {
		if services[i].Enabled != services[j].Enabled {
			return services[i].Enabled
		}
		return services[i].Name < services[j].Name
	})
	return services
}

func hasPerModelQuota(raw string) bool {
	value := strings.TrimSpace(raw)
	return value != "" && value != "{}" && value != "null"
}

func countCommaSeparated(raw string) int {
	if strings.TrimSpace(raw) == "" {
		return 0
	}

	seen := make(map[string]struct{})
	count := 0
	for _, part := range strings.Split(raw, ",") {
		value := strings.TrimSpace(part)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		count++
	}
	return count
}

func opsQuotaStatusRank(status string) int {
	switch status {
	case "exhausted":
		return 0
	case "expired":
		return 1
	case "disabled":
		return 2
	case "limited":
		return 3
	default:
		return 4
	}
}
func countQuotaMonitors(apiKeys []model.APIKey) int {
	count := 0
	for _, k := range apiKeys {
		if k.MaxCost > 0 || k.MaxTokens > 0 || k.RateLimitRPM > 0 || k.RateLimitTPM > 0 || hasPerModelQuota(k.PerModelQuotaJSON) {
			count++
		}
	}
	return count
}

func buildOpsTelemetryHeroMetrics(snap telemetry.Snapshot, total model.StatsTotal, uptimeSeconds int64) model.OpsTelemetryHeroMetrics {
	requests := total.RequestSuccess + total.RequestFailed
	failures := total.RequestFailed
	waitTime := total.WaitTime
	if requests <= 0 {
		requests = snap.TotalRequests
		failures = snap.TotalFailures
	}

	avgLatency := snap.AvgLatencyMs
	if requests > 0 && waitTime > 0 {
		avgLatency = float64(waitTime) / float64(requests)
	}

	errorRate := snap.ErrorRate
	if requests > 0 {
		errorRate = float64(failures) / float64(requests) * 100
	}

	return model.OpsTelemetryHeroMetrics{
		UptimeSeconds:     uptimeSeconds,
		TotalRequests:     requests,
		AvgLatencyMs:      avgLatency,
		ErrorRate:         errorRate,
		ActiveConnections: snap.ActiveConnections,
		MemoryUsageMB:     snap.MemoryMB,
	}
}

func buildOpsTelemetryRuntimeSignals(snap telemetry.Snapshot, sample opsTelemetryLogSample, now time.Time) model.OpsTelemetryRuntimeSignals {
	trends := make([]model.OpsTelemetryTrendPoint, 0, len(snap.TrendSnapshots))
	for _, tp := range snap.TrendSnapshots {
		trends = append(trends, model.OpsTelemetryTrendPoint{
			Timestamp:    tp.Timestamp,
			RequestDelta: tp.RequestDelta,
			FailedDelta:  tp.FailedDelta,
			AvgLatencyMs: tp.AvgLatencyMs,
			MemoryMB:     tp.MemoryMB,
		})
	}

	p95 := snap.P95LatencyMs
	if p95 <= 0 {
		p95 = opsTelemetryP95FromSample(sample.Latencies)
	}

	throughput := sample.ThroughputRPS
	if throughput <= 0 {
		throughput = snap.ThroughputRPS
	}

	if len(sample.Trend) > 0 {
		trends = sample.Trend
		for i := range trends {
			if trends[i].MemoryMB == 0 {
				trends[i].MemoryMB = snap.MemoryMB
			}
		}
	}

	return model.OpsTelemetryRuntimeSignals{
		P95LatencyMs:   p95,
		ThroughputRPS:  throughput,
		MemoryMB:       snap.MemoryMB,
		TrendSnapshots: trends,
	}
}

func opsTelemetryP95FromSample(latencies []int) float64 {
	if len(latencies) == 0 {
		return 0
	}
	sorted := append([]int(nil), latencies...)
	sort.Ints(sorted)
	idx := (95*len(sorted) + 99) / 100
	if idx < 1 {
		idx = 1
	}
	if idx > len(sorted) {
		idx = len(sorted)
	}
	return float64(sorted[idx-1])
}

// opsTelemetryLogSample 是 telemetry 日志回退路径的有界采样结果。
// 禁止把 1h 全量 relay_logs 行装入内存。
type opsTelemetryLogSample struct {
	Latencies     []int
	ThroughputRPS float64
	Trend         []model.OpsTelemetryTrendPoint
}

// telemetryLogsCache 缓存有界采样结果，避免 60s 轮询反复扫库。
var (
	telemetryLogsCacheMu  sync.RWMutex
	telemetryLogsCache    opsTelemetryLogSample
	telemetryLogsCacheKey int64
	telemetryLogsCacheExp time.Time
)

const telemetryLogsCacheTTL = 60 * time.Second

// opsTelemetryLatencySampleLimit 限制 p95 回退采样条数，避免高 QPS 下 O(n) 内存。
const opsTelemetryLatencySampleLimit = 2000

func loadOpsTelemetryLogs(ctx context.Context, since time.Time) opsTelemetryLogSample {
	sinceUnix := since.Unix()
	now := time.Now()

	telemetryLogsCacheMu.RLock()
	if now.Before(telemetryLogsCacheExp) && telemetryLogsCacheKey == sinceUnix {
		cached := telemetryLogsCache
		telemetryLogsCacheMu.RUnlock()
		return cached
	}
	telemetryLogsCacheMu.RUnlock()

	sample := opsTelemetryLogSample{}

	// 内存缓存体量有界，直接采样。
	cache, lock := relaylog.GetCacheAndLock()
	lock.Lock()
	cacheLogs := make([]model.RelayLog, 0, 64)
	for _, logItem := range cache {
		if logItem.Time < sinceUnix {
			continue
		}
		cacheLogs = append(cacheLogs, logItem)
	}
	lock.Unlock()

	sample.Latencies = opsTelemetryCollectLatencies(nil, cacheLogs, opsTelemetryLatencySampleLimit)
	sample.ThroughputRPS = opsTelemetryThroughputFromCache(cacheLogs, now, time.Minute)
	sample.Trend = buildOpsTelemetryTrendFromLogs(cacheLogs, now, 0)

	keepEnabled, err := setting.GetBool(model.SettingKeyRelayLogKeepEnabled)
	if err != nil || !keepEnabled || db.GetLogDB() == nil {
		telemetryLogsCacheMu.Lock()
		telemetryLogsCache = sample
		telemetryLogsCacheKey = sinceUnix
		telemetryLogsCacheExp = time.Now().Add(telemetryLogsCacheTTL)
		telemetryLogsCacheMu.Unlock()
		return sample
	}

	// DB：仅拉最近 N 条 use_time 做 p95 回退；吞吐与趋势用 SQL 聚合，不装全量行。
	type latencyRow struct {
		UseTime int `gorm:"column:use_time"`
	}
	var latencyRows []latencyRow
	if err := db.GetLogDB().WithContext(ctx).
		Table("relay_logs").
		Select("use_time").
		Where("time >= ? AND use_time > 0", sinceUnix).
		Order("time DESC").
		Limit(opsTelemetryLatencySampleLimit).
		Scan(&latencyRows).Error; err == nil {
		for _, row := range latencyRows {
			if row.UseTime > 0 {
				sample.Latencies = append(sample.Latencies, row.UseTime)
			}
		}
		if len(sample.Latencies) > opsTelemetryLatencySampleLimit {
			sample.Latencies = sample.Latencies[:opsTelemetryLatencySampleLimit]
		}
	}

	// 最近 1 分钟吞吐：COUNT 聚合。
	minuteSince := now.Add(-time.Minute).Unix()
	var minuteCount int64
	if err := db.GetLogDB().WithContext(ctx).
		Table("relay_logs").
		Where("time >= ? AND time <= ?", minuteSince, now.Unix()).
		Count(&minuteCount).Error; err == nil && minuteCount > 0 {
		sample.ThroughputRPS = float64(minuteCount) / 60.0
	}

	// 12×5min 趋势桶：DB 端按时间桶聚合，只返回 12 行。
	const bucketCount = 12
	const bucketDuration = 5 * time.Minute
	start := now.Add(-time.Duration(bucketCount-1) * bucketDuration).Truncate(bucketDuration)
	startUnix := start.Unix()
	bucketSec := int64(bucketDuration / time.Second)
	type trendAggRow struct {
		Bucket       int64   `gorm:"column:bucket"`
		RequestDelta int64   `gorm:"column:request_delta"`
		FailedDelta  int64   `gorm:"column:failed_delta"`
		AvgLatencyMs float64 `gorm:"column:avg_latency_ms"`
	}
	// SQLite/MySQL/Postgres 通用：用整数除法做桶。
	var trendRows []trendAggRow
	bucketExpr := fmt.Sprintf("((time - %d) / %d)", startUnix, bucketSec)
	if err := db.GetLogDB().WithContext(ctx).
		Table("relay_logs").
		Select(bucketExpr+` AS bucket,
			COUNT(*) AS request_delta,
			COALESCE(SUM(CASE WHEN error <> '' THEN 1 ELSE 0 END), 0) AS failed_delta,
			COALESCE(AVG(CASE WHEN use_time > 0 THEN use_time END), 0) AS avg_latency_ms`).
		Where("time >= ?", startUnix).
		Group(bucketExpr).
		Scan(&trendRows).Error; err == nil && len(trendRows) > 0 {
		points := make([]model.OpsTelemetryTrendPoint, bucketCount)
		for i := 0; i < bucketCount; i++ {
			points[i] = model.OpsTelemetryTrendPoint{
				Timestamp: start.Add(time.Duration(i) * bucketDuration).Unix(),
			}
		}
		for _, row := range trendRows {
			if row.Bucket < 0 || int(row.Bucket) >= bucketCount {
				continue
			}
			points[row.Bucket].RequestDelta = row.RequestDelta
			points[row.Bucket].FailedDelta = row.FailedDelta
			points[row.Bucket].AvgLatencyMs = row.AvgLatencyMs
		}
		// 合并内存缓存趋势（尚未落库的请求）。
		if len(cacheLogs) > 0 {
			cacheTrend := buildOpsTelemetryTrendFromLogs(cacheLogs, now, 0)
			for i := range points {
				if i < len(cacheTrend) {
					points[i].RequestDelta += cacheTrend[i].RequestDelta
					points[i].FailedDelta += cacheTrend[i].FailedDelta
					// 缓存侧延迟样本少，仅在 DB 桶为空时回填 avg。
					if points[i].AvgLatencyMs == 0 {
						points[i].AvgLatencyMs = cacheTrend[i].AvgLatencyMs
					}
				}
			}
		}
		sample.Trend = points
	}

	telemetryLogsCacheMu.Lock()
	telemetryLogsCache = sample
	telemetryLogsCacheKey = sinceUnix
	telemetryLogsCacheExp = time.Now().Add(telemetryLogsCacheTTL)
	telemetryLogsCacheMu.Unlock()
	return sample
}

func opsTelemetryCollectLatencies(dst []int, logs []model.RelayLog, limit int) []int {
	if limit <= 0 {
		return dst
	}
	for _, logItem := range logs {
		if logItem.UseTime <= 0 {
			continue
		}
		dst = append(dst, logItem.UseTime)
		if len(dst) >= limit {
			break
		}
	}
	return dst
}

func opsTelemetryThroughputFromCache(logs []model.RelayLog, now time.Time, window time.Duration) float64 {
	if window <= 0 {
		return 0
	}
	since := now.Add(-window).Unix()
	var count int64
	for _, logItem := range logs {
		if logItem.Time >= since && logItem.Time <= now.Unix() {
			count++
		}
	}
	if count == 0 {
		return 0
	}
	return float64(count) / window.Seconds()
}

func buildOpsTelemetryTrendFromLogs(logs []model.RelayLog, now time.Time, memoryMB int64) []model.OpsTelemetryTrendPoint {
	const bucketCount = 12
	const bucketDuration = 5 * time.Minute

	start := now.Add(-time.Duration(bucketCount-1) * bucketDuration).Truncate(bucketDuration)
	points := make([]model.OpsTelemetryTrendPoint, bucketCount)
	for i := 0; i < bucketCount; i++ {
		points[i] = model.OpsTelemetryTrendPoint{
			Timestamp: start.Add(time.Duration(i) * bucketDuration).Unix(),
			MemoryMB:  memoryMB,
		}
	}

	waitTimes := make([]int64, bucketCount)
	for _, logItem := range logs {
		bucketIndex := int((time.Unix(logItem.Time, 0).Sub(start)) / bucketDuration)
		if bucketIndex < 0 || bucketIndex >= bucketCount {
			continue
		}
		points[bucketIndex].RequestDelta++
		if logItem.Error != "" {
			points[bucketIndex].FailedDelta++
		}
		if logItem.UseTime > 0 {
			waitTimes[bucketIndex] += int64(logItem.UseTime)
		}
	}
	for i := range points {
		if points[i].RequestDelta > 0 && waitTimes[i] > 0 {
			points[i].AvgLatencyMs = float64(waitTimes[i]) / float64(points[i].RequestDelta)
		}
	}
	return points
}

func TelemetrySummaryGet(ctx context.Context) (*model.OpsTelemetrySummary, error) {
	summary := &model.OpsTelemetrySummary{}
	snap := telemetry.Global().Snapshot()
	now := time.Now()
	totalStats := stats.TotalGet()
	telemetryLogs := loadOpsTelemetryLogs(ctx, now.Add(-time.Hour))

	// ── Hero ──
	summary.Hero = buildOpsTelemetryHeroMetrics(snap, totalStats, int64(time.Since(processStartTime).Seconds()))

	// ── RuntimeSignals ──
	summary.RuntimeSignals = buildOpsTelemetryRuntimeSignals(snap, telemetryLogs, now)

	// ── DatabaseHealth ──
	dbOK := pingDatabase(ctx)
	taskOK := opsTaskRuntimeOK()
	dbStatus := "healthy"
	var dbIssues []string
	if !dbOK {
		dbStatus = "degraded"
		dbIssues = append(dbIssues, "Database connection failed")
	}
	if !taskOK {
		if dbStatus == "healthy" {
			dbStatus = "degraded"
		}
		dbIssues = append(dbIssues, "Background task runtime degraded")
	}

	summary.DatabaseHealth = model.OpsTelemetryDatabaseHealth{
		Status:  dbStatus,
		Issues:  dbIssues,
		Repairs: 0,
	}

	// ── SessionQuotaActivity ──
	apiKeys, err := apikey.List(ctx)
	sessionsByAPIKey := 0
	if err == nil {
		sessionsByAPIKey = len(apiKeys)
	}

	summary.SessionQuotaActivity = model.OpsTelemetrySessionQuotaActivity{
		ActiveSessions:      int(snap.ActiveSessions),
		StickyBoundSessions: int(snap.StickyBoundSessions),
		QuotaAlerts:         int(snap.QuotaAlerts),
		SessionsByAPIKey:    sessionsByAPIKey,
		QuotaMonitors:       countQuotaMonitors(apiKeys),
	}

	// ── PromptCache ──
	cacheStatus, err := OpsCacheStatusGet(ctx)
	if err == nil {
		summary.PromptCache = model.OpsTelemetryPromptCache{
			Entries:    cacheStatus.CurrentEntries,
			HitRate:    cacheStatus.HitRate,
			Hits:       cacheStatus.Hits,
			Misses:     cacheStatus.Misses,
			MaxEntries: cacheStatus.MaxEntries,
			UsageRate:  cacheStatus.UsageRate,
		}
	}

	// ── ProviderHealth ──
	channels, err := channel.List(ctx)
	if err == nil {
		statsChannels := stats.ChannelList()
		statsMap := make(map[int]model.StatsChannel, len(statsChannels))
		for _, sc := range statsChannels {
			statsMap[sc.ChannelID] = sc
		}

		providers := make([]model.OpsTelemetryProviderItem, 0, len(channels))
		active := 0
		for _, ch := range channels {
			stats := statsMap[ch.ID]
			requests := stats.RequestSuccess + stats.RequestFailed
			var successRate float64
			if requests > 0 {
				successRate = float64(stats.RequestSuccess) / float64(requests) * 100
			}
			var avgLat float64
			if requests > 0 {
				avgLat = float64(stats.WaitTime) / float64(requests)
			}

			healthStatus := "healthy"
			healthHint := "Normal"
			if !ch.Enabled {
				healthStatus = "disabled"
				healthHint = "Channel disabled"
			} else if requests > 0 && float64(stats.RequestFailed)/float64(requests) > 0.5 {
				healthStatus = "degraded"
				healthHint = "High failure rate"
			} else if requests > 0 && stats.RequestFailed > 0 {
				healthStatus = "warning"
				healthHint = "Some failures"
			}

			baseURL := ""
			if len(ch.BaseUrls) > 0 {
				baseURL = ch.BaseUrls[0].URL
			}

			providers = append(providers, model.OpsTelemetryProviderItem{
				ChannelID:        ch.ID,
				ChannelName:      ch.Name,
				Enabled:          ch.Enabled,
				BaseURL:          baseURL,
				RequestCount:     requests,
				SuccessRate:      successRate,
				AverageLatencyMs: avgLat,
				HealthStatus:     healthStatus,
				HealthHint:       healthHint,
			})

			if ch.Enabled {
				active++
			}
		}

		sort.SliceStable(providers, func(i, j int) bool {
			aStatus := providers[i].HealthStatus
			bStatus := providers[j].HealthStatus
			aBad := aStatus == "degraded" || aStatus == "disabled"
			bBad := bStatus == "degraded" || bStatus == "disabled"
			if aBad != bBad {
				return aBad
			}
			if providers[i].RequestCount != providers[j].RequestCount {
				return providers[i].RequestCount > providers[j].RequestCount
			}
			return providers[i].ChannelName < providers[j].ChannelName
		})

		summary.ProviderHealth = model.OpsTelemetryProviderHealth{
			Providers: providers,
			Active:    active,
			Monitored: len(channels),
		}
	}

	// ── DrilldownShortcuts ──
	summary.DrilldownShortcuts = []model.OpsTelemetryDrilldownShortcut{
		{Key: "cache", Label: "Prompt Cache"},
		{Key: "quota", Label: "Quota Summary"},
		{Key: "health", Label: "Health Status"},
		{Key: "system", Label: "System Config"},
		{Key: "audit", Label: "Audit Log"},
	}

	return summary, nil
}

func processMemoryMB() int64 {
	var mem runtime.MemStats
	runtime.ReadMemStats(&mem)
	return int64(mem.Alloc / (1024 * 1024))
}
