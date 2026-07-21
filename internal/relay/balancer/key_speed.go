package balancer

import (
	"strings"
	"sync"
	"time"
)

// 速度优先的 Key 选择策略（与 key_cooldown.go / key_availability.go 同维度）。
//
// 背景：issue #140 要求「判断 TPS 和总时间进行 key 对比速度优先判断」。
// speed 策略维护每个 (channelID, keyID, modelName) 的 EMA 平滑 TPS（tokens/sec），
// 选 TPS 最高的 key；无数据时回退 Keys 数组顺序（初始无数据 → 用第一个 key）。
//
// TPS 计算方式：output_tokens / attempt_duration_seconds。使用输出 token 数
// 衡量上游生成速度，而非总 token（输入 token 由请求方决定，不反映上游速度）。
// EMA alpha=0.3 与 Auto 策略的 latency 平滑一致，抑制短期抖动。
//
// 与冷却/熔断/失败提示/可用度的关系：速度是「软优先级」（决定从多个可用 key 中
// 选哪个），冷却/熔断/失败提示是「硬隔离」（选中后仍会跳过）。所有候选均无 TPS
// 数据时回退 cost 策略防卡死。

const (
	keySpeedEMAAlpha = 0.3 // EMA 平滑系数，与 Auto 策略 latency 一致
)

// keySpeedEntry 记录某个 (channelID, keyID, modelName) 的速度统计。
type keySpeedEntry struct {
	mu sync.Mutex

	// EMA 平滑后的 TPS（tokens/sec）
	avgTPS float64

	// lastActivity 仅在写路径刷新，用于 PurgeStale 回收判定。
	// 读路径不触碰，避免频繁查询的垃圾 key 永不满足空闲阈值（与 key_availability issue #124 一致）。
	lastActivity time.Time
}

// globalKeySpeed 全局速度统计存储，key: "channelID:keyID:modelName"。
// 与 globalKeyCooldown / globalKeyAvailability / globalBreaker 同维度，便于复用清理。
var globalKeySpeed sync.Map // key: string -> *keySpeedEntry

// speedKey 构造存储 key，与 cooldownKey / availabilityKey 格式一致。
func speedKey(channelID, keyID int, modelName string) string {
	return buildKey3(channelID, keyID, strings.TrimSpace(modelName))
}

// getOrCreateSpeedEntry 读取或创建条目（初始 avgTPS=0，表示无数据）。
func getOrCreateSpeedEntry(key string) *keySpeedEntry {
	now := time.Now()
	v, ok := globalKeySpeed.Load(key)
	if ok {
		if entry, ok := v.(*keySpeedEntry); ok {
			return entry
		}
	}
	entry := &keySpeedEntry{lastActivity: now}
	actual, _ := globalKeySpeed.LoadOrStore(key, entry)
	if e, ok := actual.(*keySpeedEntry); ok {
		return e
	}
	return entry
}

// GetKeyTPS 查询某个 (channelID, keyID, modelName) 的 EMA 平滑 TPS。
// 返回 0 表示无数据（冷启动）。modelName 为空或 keyID=0 时返回 0（后台任务不评分）。
func GetKeyTPS(channelID, keyID int, modelName string) float64 {
	if strings.TrimSpace(modelName) == "" || keyID == 0 {
		return 0
	}
	key := speedKey(channelID, keyID, modelName)
	v, ok := globalKeySpeed.Load(key)
	if !ok {
		return 0
	}
	entry, ok := v.(*keySpeedEntry)
	if !ok {
		globalKeySpeed.Delete(key)
		return 0
	}
	entry.mu.Lock()
	defer entry.mu.Unlock()
	return entry.avgTPS
}

// RecordKeySpeed 记录某次成功请求的 TPS，用 EMA 平滑。
// outputTokens 为该次请求的输出 token 数，durationMs 为本次尝试耗时（毫秒）。
// 仅记录成功请求（失败请求无法反映上游生成速度）。modelName 为空或 keyID=0 时跳过。
// durationMs ≤ 0 或 outputTokens ≤ 0 时跳过（无法计算有效 TPS）。
func RecordKeySpeed(channelID, keyID int, modelName string, outputTokens int64, durationMs int64) {
	if strings.TrimSpace(modelName) == "" || keyID == 0 || outputTokens <= 0 || durationMs <= 0 {
		return
	}
	tps := float64(outputTokens) / (float64(durationMs) / 1000.0)

	key := speedKey(channelID, keyID, modelName)
	entry := getOrCreateSpeedEntry(key)
	entry.mu.Lock()
	defer entry.mu.Unlock()

	if entry.avgTPS == 0 {
		entry.avgTPS = tps
	} else {
		entry.avgTPS = keySpeedEMAAlpha*tps + (1-keySpeedEMAAlpha)*entry.avgTPS
	}
	entry.lastActivity = time.Now()
}

// PurgeStaleKeySpeed 清理长时间未活动的速度条目，防止 map 无界增长。
// maxAge 为最大空闲时长，超过则删除。由 relay log flush 定时任务周期性调用。
// 清理后该 key 下次查询返回 0（冷启动语义），符合速度的短期软信号定位。
func PurgeStaleKeySpeed(maxAge time.Duration) int {
	if maxAge <= 0 {
		return 0
	}
	threshold := time.Now().Add(-maxAge)
	removed := 0
	globalKeySpeed.Range(func(k, v any) bool {
		entry, ok := v.(*keySpeedEntry)
		if !ok {
			globalKeySpeed.Delete(k)
			removed++
			return true
		}
		entry.mu.Lock()
		stale := entry.lastActivity.Before(threshold)
		entry.mu.Unlock()
		if stale {
			globalKeySpeed.Delete(k)
			removed++
		}
		return true
	})
	return removed
}

// RemoveChannelKeySpeed 删除指定渠道的所有速度条目。
// 在渠道被删除时调用，注册于 OnChannelDeletedHooks。
func RemoveChannelKeySpeed(channelID int) {
	prefix := buildKeyPrefix(channelID)
	globalKeySpeed.Range(func(key, _ any) bool {
		if k, ok := key.(string); ok && strings.HasPrefix(k, prefix) {
			globalKeySpeed.Delete(key)
		}
		return true
	})
}

// RemoveKeySpeed 删除指定 key 的所有速度条目（跨模型）。
// 供 key 禁用/删除时可选调用。按 ":keyID:" 子串定位。
func RemoveKeySpeed(keyID int) {
	if keyID == 0 {
		return
	}
	needle := buildKeyNeedle(keyID)
	globalKeySpeed.Range(func(key, _ any) bool {
		k, ok := key.(string)
		if !ok {
			return true
		}
		if strings.Contains(k, needle) {
			globalKeySpeed.Delete(key)
		}
		return true
	})
}
