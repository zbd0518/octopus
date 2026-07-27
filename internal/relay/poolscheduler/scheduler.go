package poolscheduler

import (
	"errors"
	"fmt"
	"math"
	"math/rand/v2"
	"sync"
	"sync/atomic"
	"time"

	"github.com/lingyuins/octopus/internal/model"
	"github.com/lingyuins/octopus/internal/op/pool"
	"gorm.io/gorm"
)

const ewmaAlpha = 0.3

var (
	ErrNoAvailableAccount = errors.New("no available account in pool")

	// globalPoolStats key: "poolID:accountID" -> *accountStats
	globalPoolStats sync.Map
	// globalPoolSlots key: "poolID:accountID" -> *int64 (atomic current concurrency)
	globalPoolSlots sync.Map
	// globalPoolSticky key: "poolID:sessionHash" -> accountID
	globalPoolSticky sync.Map
	// globalRoundRobin key: poolID -> *uint64 (atomic counter)
	globalRoundRobin sync.Map

	// TriggerRefreshAsync 由 pooltokenrefresh 包在 init 时注入。
	// 选号遇到 token 过期的 OAuth 账号时异步触发刷新，不阻塞本次选号。
	// nil 表示刷新服务未启用（跳过触发）。
	TriggerRefreshAsync func(poolID, accountID int)
)

type accountStats struct {
	mu           sync.Mutex
	errorRate    float64
	ttftMs       float64
	lastActivity time.Time
}

func statsKey(poolID, accountID int) string {
	return fmt.Sprintf("%d:%d", poolID, accountID)
}

func stickyKey(poolID int, sessionHash string) string {
	return fmt.Sprintf("%d:%s", poolID, sessionHash)
}

// SelectAccount 从指定池选择一个可用账号。
// sessionHash 非空时启用粘性；excludeIDs 排除已尝试过的账号。
// modelName 非空时按账号绑定的模型列表过滤（空 Models 表示不限）。
// 返回选中的账号（已 acquire 并发槽位），调用方完成后必须调用 ReleaseSlot。
func SelectAccount(poolID int, sessionHash string, excludeIDs []int, poolDefaultConcurrency int, modelName string) (*model.PoolAccount, error) {
	// L1: 粘性会话
	if sessionHash != "" {
		if acct, ok := trySticky(poolID, sessionHash, excludeIDs, poolDefaultConcurrency, modelName); ok {
			return acct, nil
		}
	}

	// L2: 获取可调度候选
	candidates, err := pool.ListSchedulableAccounts(poolID)
	if err != nil {
		return nil, err
	}
	candidates = filterExcluded(candidates, excludeIDs)
	candidates = filterByModel(candidates, modelName)
	if len(candidates) == 0 {
		// 候选为空时，尝试触发池内 token 过期的 OAuth 账号刷新（异步，不阻塞）。
		triggerRefreshForExpired(poolID, modelName)
		return nil, ErrNoAvailableAccount
	}

	// L3: 并发槽位过滤
	candidates = filterBySlot(candidates, poolID, poolDefaultConcurrency)
	if len(candidates) == 0 {
		return nil, ErrNoAvailableAccount
	}

	// L4: 评分排序 + 选择
	selected := selectByStrategy(candidates, poolID)

	// L5: acquire 槽位 + 绑定粘性
	acquireSlot(poolID, selected.ID)
	if sessionHash != "" {
		globalPoolSticky.Store(stickyKey(poolID, sessionHash), selected.ID)
	}
	return &selected, nil
}

// ReportResult 上报请求结果，更新 EWMA 统计和 DB 累计计数。
func ReportResult(poolID, accountID int, success bool, ttftMs float64, outputTokens int64) {
	key := statsKey(poolID, accountID)
	val, _ := globalPoolStats.LoadOrStore(key, &accountStats{lastActivity: time.Now()})
	stats := val.(*accountStats)
	stats.mu.Lock()
	if success {
		stats.errorRate = (1-ewmaAlpha)*stats.errorRate + ewmaAlpha*0
		if ttftMs > 0 {
			if stats.ttftMs == 0 {
				stats.ttftMs = ttftMs
			} else {
				stats.ttftMs = (1-ewmaAlpha)*stats.ttftMs + ewmaAlpha*ttftMs
			}
		}
	} else {
		stats.errorRate = (1-ewmaAlpha)*stats.errorRate + ewmaAlpha*1
	}
	stats.lastActivity = time.Now()
	stats.mu.Unlock()

	// 异步更新 DB 累计（best-effort，不阻塞请求路径）。
	go func() {
		updates := map[string]interface{}{
			"total_requests": gormExpr("total_requests + 1"),
		}
		if !success {
			updates["total_errors"] = gormExpr("total_errors + 1")
		}
		if outputTokens > 0 {
			updates["total_tokens"] = gormExpr("total_tokens + ?", outputTokens)
		}
		_ = pool.UpdateAccount(poolID, accountID, updates)
	}()
}

// SetRateLimitCooldown 设置 429 冷却。
func SetRateLimitCooldown(poolID, accountID int, until time.Time) {
	_ = pool.UpdateAccount(poolID, accountID, map[string]interface{}{
		"rate_limit_reset_at": until.Unix(),
	})
}

// SetOverload 设置过载冷却。
func SetOverload(poolID, accountID int, until time.Time) {
	_ = pool.UpdateAccount(poolID, accountID, map[string]interface{}{
		"overload_until": until.Unix(),
	})
}

// SetError 将账号标记为 error 状态。
func SetError(poolID, accountID int) {
	_ = pool.UpdateAccount(poolID, accountID, map[string]interface{}{
		"status": "error",
	})
}

// ReleaseSlot 释放并发槽位。
func ReleaseSlot(poolID, accountID int) {
	key := statsKey(poolID, accountID)
	if val, ok := globalPoolSlots.Load(key); ok {
		atomic.AddInt64(val.(*int64), -1)
	}
}

// RemovePool 清理池相关的所有内存状态。
func RemovePool(poolID int) {
	globalPoolStats.Range(func(k, _ interface{}) bool {
		if s := k.(string); len(s) > 0 && parsePoolID(s) == poolID {
			globalPoolStats.Delete(k)
			globalPoolSlots.Delete(k)
		}
		return true
	})
	globalPoolSticky.Range(func(k, _ interface{}) bool {
		if s := k.(string); len(s) > 0 && parsePoolID(s) == poolID {
			globalPoolSticky.Delete(k)
		}
		return true
	})
	globalRoundRobin.Delete(poolID)
}

// RemoveAccount 清理单个账号的内存状态。
func RemoveAccount(poolID, accountID int) {
	key := statsKey(poolID, accountID)
	globalPoolStats.Delete(key)
	globalPoolSlots.Delete(key)
	// 清理指向该账号的粘性条目。
	globalPoolSticky.Range(func(k, v interface{}) bool {
		if s := k.(string); len(s) > 0 && parsePoolID(s) == poolID {
			if v.(int) == accountID {
				globalPoolSticky.Delete(k)
			}
		}
		return true
	})
}

// PurgeStale 清理长时间无活动的内存统计（后台任务调用）。
func PurgeStale(idleThreshold time.Duration) {
	cutoff := time.Now().Add(-idleThreshold)
	globalPoolStats.Range(func(k, v interface{}) bool {
		stats := v.(*accountStats)
		stats.mu.Lock()
		idle := stats.lastActivity.Before(cutoff)
		stats.mu.Unlock()
		if idle {
			globalPoolStats.Delete(k)
			globalPoolSlots.Delete(k)
		}
		return true
	})
}

func trySticky(poolID int, sessionHash string, excludeIDs []int, poolDefaultConcurrency int, modelName string) (*model.PoolAccount, bool) {
	val, ok := globalPoolSticky.Load(stickyKey(poolID, sessionHash))
	if !ok {
		return nil, false
	}
	accountID := val.(int)
	for _, id := range excludeIDs {
		if id == accountID {
			return nil, false
		}
	}
	acct, err := pool.GetAccount(poolID, accountID)
	if err != nil || !acct.IsSchedulable() {
		globalPoolSticky.Delete(stickyKey(poolID, sessionHash))
		return nil, false
	}
	if !model.ModelMatches(acct.Models, modelName) {
		return nil, false
	}
	limit := acct.EffectiveConcurrency(poolDefaultConcurrency)
	if !tryAcquireSlot(poolID, accountID, limit) {
		return nil, false
	}
	return acct, true
}

// filterByModel 按账号绑定的模型列表过滤候选。models 为空表示不限。
func filterByModel(candidates []model.PoolAccount, modelName string) []model.PoolAccount {
	if modelName == "" {
		return candidates
	}
	result := make([]model.PoolAccount, 0, len(candidates))
	for i := range candidates {
		if model.ModelMatches(candidates[i].Models, modelName) {
			result = append(result, candidates[i])
		}
	}
	return result
}

// triggerRefreshForExpired 扫描池内 token 过期的 OAuth 账号，异步触发刷新。
// 仅在候选为空时调用，避免每次请求都扫描。失败不影响调用方。
func triggerRefreshForExpired(poolID int, modelName string) {
	if TriggerRefreshAsync == nil {
		return
	}
	// ListAccounts 返回池内全部账号（不过滤可调度性），用于发现过期 OAuth 账号。
	accounts, err := pool.ListAccounts(poolID)
	if err != nil {
		return
	}
	for i := range accounts {
		acct := &accounts[i]
		if acct.Type != model.PoolTypeOAuth {
			continue
		}
		if !acct.IsTokenExpired() {
			continue
		}
		// 仅刷新与请求模型匹配的账号（避免刷新无关账号）。
		if !model.ModelMatches(acct.Models, modelName) {
			continue
		}
		go TriggerRefreshAsync(poolID, acct.ID)
	}
}

func filterExcluded(candidates []model.PoolAccount, excludeIDs []int) []model.PoolAccount {
	if len(excludeIDs) == 0 {
		return candidates
	}
	excludeSet := make(map[int]struct{}, len(excludeIDs))
	for _, id := range excludeIDs {
		excludeSet[id] = struct{}{}
	}
	result := make([]model.PoolAccount, 0, len(candidates))
	for i := range candidates {
		if _, excluded := excludeSet[candidates[i].ID]; !excluded {
			result = append(result, candidates[i])
		}
	}
	return result
}

func filterBySlot(candidates []model.PoolAccount, poolID, poolDefaultConcurrency int) []model.PoolAccount {
	result := make([]model.PoolAccount, 0, len(candidates))
	for i := range candidates {
		limit := candidates[i].EffectiveConcurrency(poolDefaultConcurrency)
		key := statsKey(poolID, candidates[i].ID)
		val, _ := globalPoolSlots.LoadOrStore(key, new(int64))
		current := atomic.LoadInt64(val.(*int64))
		if current < int64(limit) {
			result = append(result, candidates[i])
		}
	}
	return result
}

func selectByStrategy(candidates []model.PoolAccount, poolID int) model.PoolAccount {
	// 获取池策略（best-effort，失败默认 ewma）。
	strategy := "ewma"
	if p, err := pool.GetPool(poolID); err == nil {
		strategy = p.Strategy
	}

	switch strategy {
	case "round_robin":
		val, _ := globalRoundRobin.LoadOrStore(poolID, new(uint64))
		idx := atomic.AddUint64(val.(*uint64), 1) - 1
		return candidates[idx%uint64(len(candidates))]
	case "random":
		return candidates[rand.IntN(len(candidates))]
	default: // "ewma"
		return selectByEWMA(candidates, poolID)
	}
}

func selectByEWMA(candidates []model.PoolAccount, poolID int) model.PoolAccount {
	bestIdx := 0
	bestScore := math.MaxFloat64
	for i := range candidates {
		key := statsKey(poolID, candidates[i].ID)
		score := 0.0
		if val, ok := globalPoolStats.Load(key); ok {
			stats := val.(*accountStats)
			stats.mu.Lock()
			// 综合得分：错误率权重 0.7 + 归一化 TTFT 权重 0.3。
			// 分数越低越优。
			score = stats.errorRate*0.7 + (stats.ttftMs/10000.0)*0.3
			stats.mu.Unlock()
		}
		// priority 作为 tiebreaker：高优先级减分。
		score -= float64(candidates[i].Priority) * 0.001
		if score < bestScore {
			bestScore = score
			bestIdx = i
		}
	}
	return candidates[bestIdx]
}

func acquireSlot(poolID, accountID int) {
	key := statsKey(poolID, accountID)
	val, _ := globalPoolSlots.LoadOrStore(key, new(int64))
	atomic.AddInt64(val.(*int64), 1)
}

func tryAcquireSlot(poolID, accountID int, limit int) bool {
	key := statsKey(poolID, accountID)
	val, _ := globalPoolSlots.LoadOrStore(key, new(int64))
	ptr := val.(*int64)
	for {
		current := atomic.LoadInt64(ptr)
		if current >= int64(limit) {
			return false
		}
		if atomic.CompareAndSwapInt64(ptr, current, current+1) {
			return true
		}
	}
}

func parsePoolID(key string) int {
	for i := 0; i < len(key); i++ {
		if key[i] == ':' {
			id := 0
			for j := 0; j < i; j++ {
				id = id*10 + int(key[j]-'0')
			}
			return id
		}
	}
	return -1
}

func gormExpr(expr string, args ...interface{}) interface{} {
	return gorm.Expr(expr, args...)
}
