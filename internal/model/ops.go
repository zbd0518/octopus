package model

type OpsCacheStatus struct {
	Enabled             bool                          `json:"enabled"`
	RuntimeEnabled      bool                          `json:"runtime_enabled"`
	TTLSeconds          int                           `json:"ttl_seconds"`
	Threshold           int                           `json:"threshold"`
	MaxEntries          int                           `json:"max_entries"`
	CurrentEntries      int                           `json:"current_entries"`
	Hits                int64                         `json:"hits"`
	Misses              int64                         `json:"misses"`
	HitRate             float64                       `json:"hit_rate"`
	UsageRate           float64                       `json:"usage_rate"`
	ProviderPromptCache OpsProviderPromptCacheSummary `json:"provider_prompt_cache"`
}

type OpsProviderPromptCacheSummary struct {
	RequestCount         int64                                `json:"request_count"`
	CachedRequestCount   int64                                `json:"cached_request_count"`
	CacheRate            float64                              `json:"cache_rate"`
	CacheReuseRatio      float64                              `json:"cache_reuse_ratio"`
	CacheReadTokens      int64                                `json:"cache_read_tokens"`
	CacheWriteTokens     int64                                `json:"cache_write_tokens"`
	EstimatedCostSaved   float64                              `json:"estimated_cost_saved"`
	UsageSignalAvailable bool                                 `json:"usage_signal_available"`
	SampledLogCount      int64                                `json:"sampled_log_count"`
	ParsedLogCount       int64                                `json:"parsed_log_count"`
	Providers            []OpsProviderPromptCacheProviderItem `json:"providers"`
	Trend                []OpsProviderPromptCacheTrendPoint   `json:"trend"`
}

type OpsProviderPromptCacheProviderItem struct {
	ChannelID          int     `json:"channel_id"`
	ChannelName        string  `json:"channel_name"`
	RequestCount       int64   `json:"request_count"`
	CachedRequestCount int64   `json:"cached_request_count"`
	CacheRate          float64 `json:"cache_rate"`
	CacheReuseRatio    float64 `json:"cache_reuse_ratio"`
	CacheReadTokens    int64   `json:"cache_read_tokens"`
	CacheWriteTokens   int64   `json:"cache_write_tokens"`
	EstimatedCostSaved float64 `json:"estimated_cost_saved"`
}

type OpsProviderPromptCacheTrendPoint struct {
	Timestamp          int64   `json:"timestamp"`
	RequestCount       int64   `json:"request_count"`
	CachedRequestCount int64   `json:"cached_request_count"`
	CacheRate          float64 `json:"cache_rate"`
	CacheReadTokens    int64   `json:"cache_read_tokens"`
	CacheWriteTokens   int64   `json:"cache_write_tokens"`
	EstimatedCostSaved float64 `json:"estimated_cost_saved"`
}

type OpsQuotaKeyItem struct {
	APIKeyID            int     `json:"api_key_id"`
	Name                string  `json:"name"`
	Enabled             bool    `json:"enabled"`
	Expired             bool    `json:"expired"`
	Status              string  `json:"status"`
	SupportedModelCount int     `json:"supported_model_count"`
	HasPerModelQuota    bool    `json:"has_per_model_quota"`
	RateLimitRPM        int     `json:"rate_limit_rpm"`
	RateLimitTPM        int     `json:"rate_limit_tpm"`
	MaxCost             float64 `json:"max_cost"`
	MaxTokens           int64   `json:"max_tokens"` // Token 用量上限，0 表示不限制（issue #108）
	RequestCount        int64   `json:"request_count"`
	TotalCost           float64 `json:"total_cost"`
	// TotalTokens 与 SuccessRate 来自 stats 聚合，使配额页与 API 密钥详情
	// 共享同一字段集，消除两处数据分散（issue #87 P1-#3）。
	TotalTokens int64   `json:"total_tokens"`
	SuccessRate float64 `json:"success_rate"`
}

type OpsQuotaSummary struct {
	TotalKeyCount         int               `json:"total_key_count"`
	EnabledKeyCount       int               `json:"enabled_key_count"`
	AvailableKeyCount     int               `json:"available_key_count"`
	ExpiredKeyCount       int               `json:"expired_key_count"`
	LimitedKeyCount       int               `json:"limited_key_count"`
	UnlimitedKeyCount     int               `json:"unlimited_key_count"`
	ExhaustedKeyCount     int               `json:"exhausted_key_count"`
	PerModelQuotaKeyCount int               `json:"per_model_quota_key_count"`
	ActiveUsageKeyCount   int               `json:"active_usage_key_count"`
	TotalRPM              int               `json:"total_rpm"`
	TotalTPM              int               `json:"total_tpm"`
	TotalMaxCost          float64           `json:"total_max_cost"`
	TotalMaxTokens        int64             `json:"total_max_tokens"` // 所有 Key 的 Token 上限合计（issue #108）
	Keys                  []OpsQuotaKeyItem `json:"keys"`
}

type OpsHealthGroupItem struct {
	GroupID      int    `json:"group_id"`
	GroupName    string `json:"group_name"`
	EndpointType string `json:"endpoint_type"`
	Status       string `json:"status"`
	FailureCount int64  `json:"failure_count"`
	HealthScore  int    `json:"health_score"`
}

type OpsHealthStatus struct {
	DatabaseOK         bool                 `json:"database_ok"`
	CacheOK            bool                 `json:"cache_ok"`
	TaskRuntimeOK      bool                 `json:"task_runtime_ok"`
	RecentErrorCount   int64                `json:"recent_error_count"`
	HealthyGroupCount  int                  `json:"healthy_group_count"`
	WarningGroupCount  int                  `json:"warning_group_count"`
	DegradedGroupCount int                  `json:"degraded_group_count"`
	DownGroupCount     int                  `json:"down_group_count"`
	EmptyGroupCount    int                  `json:"empty_group_count"`
	FailingGroups      []OpsHealthGroupItem `json:"failing_groups"`
	CheckedAt          int64                `json:"checked_at"`
}

type OpsAIRouteServiceSummary struct {
	Name    string `json:"name"`
	BaseURL string `json:"base_url"`
	Model   string `json:"model"`
	Enabled bool   `json:"enabled"`
}

type OpsSystemSummary struct {
	Version                      string                     `json:"version"`
	Commit                       string                     `json:"commit"`
	BuildTime                    string                     `json:"build_time"`
	Repo                         string                     `json:"repo"`
	DatabaseType                 string                     `json:"database_type"`
	PublicAPIBaseURL             string                     `json:"public_api_base_url"`
	ProxyURL                     string                     `json:"proxy_url"`
	RelayLogKeepEnabled          bool                       `json:"relay_log_keep_enabled"`
	RelayLogKeepDays             int                        `json:"relay_log_keep_days"`
	RelayLogKeepCount            int                        `json:"relay_log_keep_count"`
	StatsSaveIntervalMinutes     int                        `json:"stats_save_interval_minutes"`
	SyncLLMIntervalHours         int                        `json:"sync_llm_interval_hours"`
	ModelInfoUpdateIntervalHours int                        `json:"model_info_update_interval_hours"`
	ImportEnabled                bool                       `json:"import_enabled"`
	ExportEnabled                bool                       `json:"export_enabled"`
	AIRouteTimeoutSeconds        int                        `json:"ai_route_timeout_seconds"`
	AIRouteParallelism           int                        `json:"ai_route_parallelism"`
	AIRouteLegacyMode            bool                       `json:"ai_route_legacy_mode"`
	AIRouteServiceCount          int                        `json:"ai_route_service_count"`
	AIRouteEnabledServiceCount   int                        `json:"ai_route_enabled_service_count"`
	AIRouteServices              []OpsAIRouteServiceSummary `json:"ai_route_services"`
	ChannelCount                 int                        `json:"channel_count"`
	GroupCount                   int                        `json:"group_count"`
	APIKeyCount                  int                        `json:"api_key_count"`

	// 维护策略概览（熔断器 / 重试 / 输出拦截），issue #87 P2-#6：
	// 将这些可操作维护项的状态聚合到运维中心，便于一眼掌握当前生效配置，
	// 配合「前往设置」入口完成调整，减少在设置页中翻找的成本。
	RelayRetryCount            int    `json:"relay_retry_count"`
	RelayRouteRetries          int    `json:"relay_route_retries"`
	RelayMaxTotalAttempts      int    `json:"relay_max_total_attempts"`
	RatelimitCooldownSec       int    `json:"ratelimit_cooldown_sec"`
	CircuitBreakerEnabled      bool   `json:"circuit_breaker_enabled"`
	CircuitBreakerThreshold    int    `json:"circuit_breaker_threshold"`
	CircuitBreakerCooldown     int    `json:"circuit_breaker_cooldown"`
	CircuitBreakerMaxCooldown  int    `json:"circuit_breaker_max_cooldown"`
	ResponseFilterEnabled      bool   `json:"response_filter_enabled"`
	ResponseFilterAction       string `json:"response_filter_action"`
	ResponseFilterKeywordCount int    `json:"response_filter_keyword_count"`
}

// --- Telemetry ---

type OpsTelemetrySummary struct {
	Hero                 OpsTelemetryHeroMetrics          `json:"hero"`
	RuntimeSignals       OpsTelemetryRuntimeSignals       `json:"runtime_signals"`
	DatabaseHealth       OpsTelemetryDatabaseHealth       `json:"database_health"`
	SessionQuotaActivity OpsTelemetrySessionQuotaActivity `json:"session_quota_activity"`
	PromptCache          OpsTelemetryPromptCache          `json:"prompt_cache"`
	ProviderHealth       OpsTelemetryProviderHealth       `json:"provider_health"`
	DrilldownShortcuts   []OpsTelemetryDrilldownShortcut  `json:"drilldown_shortcuts"`
}

type OpsTelemetryHeroMetrics struct {
	UptimeSeconds     int64   `json:"uptime_seconds"`
	TotalRequests     int64   `json:"total_requests"`
	AvgLatencyMs      float64 `json:"avg_latency_ms"`
	ErrorRate         float64 `json:"error_rate"`
	ActiveConnections int64   `json:"active_connections"`
	MemoryUsageMB     int64   `json:"memory_usage_mb"`
}

type OpsTelemetryRuntimeSignals struct {
	P95LatencyMs   float64                  `json:"p95_latency_ms"`
	ThroughputRPS  float64                  `json:"throughput_rps"`
	MemoryMB       int64                    `json:"memory_mb"`
	TrendSnapshots []OpsTelemetryTrendPoint `json:"trend_snapshots"`
}

type OpsTelemetryTrendPoint struct {
	Timestamp    int64   `json:"timestamp"`
	RequestDelta int64   `json:"request_delta"`
	FailedDelta  int64   `json:"failed_delta"`
	AvgLatencyMs float64 `json:"avg_latency_ms"`
	MemoryMB     int64   `json:"memory_mb"`
}

type OpsTelemetryDatabaseHealth struct {
	Status  string   `json:"status"`
	Issues  []string `json:"issues"`
	Repairs int      `json:"repairs"`
}

type OpsTelemetrySessionQuotaActivity struct {
	ActiveSessions      int `json:"active_sessions"`
	StickyBoundSessions int `json:"sticky_bound_sessions"`
	QuotaAlerts         int `json:"quota_alerts"`
	SessionsByAPIKey    int `json:"sessions_by_api_key"`
	QuotaMonitors       int `json:"quota_monitors"`
}

type OpsTelemetryPromptCache struct {
	Entries    int     `json:"entries"`
	HitRate    float64 `json:"hit_rate"`
	Hits       int64   `json:"hits"`
	Misses     int64   `json:"misses"`
	MaxEntries int     `json:"max_entries"`
	UsageRate  float64 `json:"usage_rate"`
}

type OpsTelemetryProviderHealth struct {
	Providers []OpsTelemetryProviderItem `json:"providers"`
	Active    int                        `json:"active"`
	Monitored int                        `json:"monitored"`
}

type OpsTelemetryProviderItem struct {
	ChannelID        int     `json:"channel_id"`
	ChannelName      string  `json:"channel_name"`
	Enabled          bool    `json:"enabled"`
	BaseURL          string  `json:"base_url"`
	RequestCount     int64   `json:"request_count"`
	SuccessRate      float64 `json:"success_rate"`
	AverageLatencyMs float64 `json:"average_latency_ms"`
	HealthStatus     string  `json:"health_status"`
	HealthHint       string  `json:"health_hint"`
}

type OpsTelemetryDrilldownShortcut struct {
	Key   string `json:"key"`
	Label string `json:"label"`
}
