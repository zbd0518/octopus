package poolscheduler

import (
	"context"
	"errors"
	"fmt"
	"math"
	"math/rand/v2"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/lingyuins/octopus/internal/model"
	"github.com/lingyuins/octopus/internal/op/pool"
	"github.com/lingyuins/octopus/internal/op/setting"
	"gorm.io/gorm"
)

const ewmaAlpha = 0.3

var (
	ErrNoAvailableAccount = errors.New("no available account in pool")

	// globalPoolStats key: "poolID:accountID" -> *accountStats
	globalPoolStats sync.Map
	// globalPoolSlots key: "poolID:accountID" -> *int64 (atomic current concurrency)
	globalPoolSlots sync.Map
	// globalPoolSticky key: "poolID:sessionHash" -> *stickyEntry
	// sessionHash 含客户端请求携带的 model 名（基数不受控），缺少周期回收会导致
	// map 无界增长（见 issue #46 同类遗漏）。stickyEntry.LastActivity 用于 PurgeStaleSticky。
	globalPoolSticky sync.Map
	// globalRoundRobin key: poolID -> *uint64 (atomic counter)
	globalRoundRobin sync.Map

	// TriggerRefreshAsync 由 pooltokenrefresh 包在 init 时注入。
	// 选号遇到 token 过期的 OAuth 账号时异步触发刷新，不阻塞本次选号。
	// nil 表示刷新服务未启用（跳过触发）。
	TriggerRefreshAsync func(poolID, accountID int)

	// NotifyPoolStateChange is injected by the task layer. Implementations must
	// enqueue without blocking; nil leaves event delivery disabled.
	NotifyPoolStateChange func(poolID, accountID int, kind, detail string)

	// poolReportCh 是 ReportResult DB 写入的有界 worker pool。
	// 之前每个号池请求完成都 go func() 同步执行两次 pool.UpdateAccount（DB 写），
	// 无信号量/超时，高 QPS + 慢 DB 下 goroutine 会无限堆积（风暴）。改为固定 worker
	// + 带缓冲队列：入队非阻塞，队列满则丢弃当前 job（best-effort 计数，下次请求会
	// 再累积，且内存 EWMA 已在 ReportResult 同步更新，丢弃不影响正确性）。
	poolReportCh = make(chan poolReportJob, 1024)
	// poolReportWorkers 固定 worker 数，控制并发 DB 写 goroutine 上限。
	poolReportWorkers = 4
	// poolReportStartOnce 保证 worker pool 幂等启动（task.Init 可能被多次调用）。
	poolReportStartOnce sync.Once
	// poolReportDroppedCount 计数因队列满而丢弃的 ReportResult 上报次数（可观测）。
	poolReportDroppedCount atomic.Int64
)

// poolReportJob 是 ReportResult 异步 DB 写任务。
type poolReportJob struct {
	poolID       int
	accountID    int
	success      bool
	outputTokens int64
	// authErrorCount / authErrorWindowStart snapshot the in-memory auth-error
	// counter at the moment of a success report (B1-#7): before clearing the DB
	// mirror the worker consults this snapshot, so a delayed success job cannot
	// erase 401/403 evidence produced after the snapshot.
	authErrorCount       int
	authErrorWindowStart int64
}

type accountStats struct {
	mu           sync.Mutex
	errorRate    float64
	ttftMs       float64
	lastActivity time.Time
}

// stickyEntry 粘性会话条目。LastActivity 用于 PurgeStaleSticky 按空闲时长回收，
// 与 balancer.SessionEntry 同模式（见 issue #46 内存暴涨防护）。
type stickyEntry struct {
	AccountID    int
	LastActivity time.Time
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
	escapedStickyID := 0
	if sessionHash != "" {
		acct, ok, escapedID := trySticky(poolID, sessionHash, excludeIDs, poolDefaultConcurrency, modelName)
		if ok {
			return acct, nil
		}
		if escapedID > 0 {
			escapedStickyID = escapedID
		}
	}

	// L2: 获取可调度候选
	candidates, err := pool.ListSchedulableAccounts(poolID)
	if err != nil {
		return nil, err
	}
	candidates = filterExcluded(candidates, excludeIDs)
	candidates = filterByModel(candidates, modelName)
	// L2.5: 可选分层过滤（priority 阈值），管理员显式配置即遵守（不 fallback）。
	candidates = filterLayeredByPriority(candidates)
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

	// Escape only when another candidate can serve this request; caller-provided
	// exclusions, model constraints, priority filters and slot limits still apply.
	if escapedStickyID > 0 {
		alternatives := filterExcluded(candidates, []int{escapedStickyID})
		if len(alternatives) > 0 {
			candidates = alternatives
		}
	}

	// L4: 评分排序 + 选择
	selected := selectByStrategy(candidates, poolID)

	// L5: acquire 槽位 + 绑定粘性
	acquireSlot(poolID, selected.ID)
	// B1-#9: an escaped session does not rebind in this pass (the original
	// sticky entry is kept; the session returns to it once stats recover).
	if sessionHash != "" && escapedStickyID == 0 {
		globalPoolSticky.Store(stickyKey(poolID, sessionHash), &stickyEntry{
			AccountID:    selected.ID,
			LastActivity: time.Now(),
		})
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

	// 成功请求后清零鉴权错误计数（等价 sub2api clear-error 于测试成功）。
	job := poolReportJob{poolID: poolID, accountID: accountID, success: success, outputTokens: outputTokens}
	if success {
		// B1-#7: capture the mirror snapshot and reset the in-memory counter as
		// one critical section under the per-account evidence lock. At
		// Unix-second resolution a reset and a same-second 401/403 share the
		// same window_start, so only the lock-serialized ordering guarantees the
		// delayed success worker can tell post-snapshot evidence apart (see
		// clearAuthErrorMirror).
		job.authErrorCount, job.authErrorWindowStart = snapshotAndResetAuthError(poolID, accountID)
	}

	// 异步更新 DB 累计（best-effort，不阻塞请求路径）。经有界 worker pool 执行，
	// 入队非阻塞，队列满则丢弃当前 job（计数降级，下次请求会再累积）。
	select {
	case poolReportCh <- job:
	default:
		poolReportDroppedCount.Add(1)
	}
}

// applyReportToDB 执行 DB 累计写入（由 worker pool 调用）。
func applyReportToDB(job poolReportJob) {
	updates := map[string]interface{}{
		"total_requests": gormExpr("total_requests + 1"),
	}
	if !job.success {
		updates["total_errors"] = gormExpr("total_errors + 1")
	}
	if job.outputTokens > 0 {
		updates["total_tokens"] = gormExpr("total_tokens + ?", job.outputTokens)
	}
	_ = pool.UpdateAccount(job.poolID, job.accountID, updates)
	if job.success {
		clearAuthErrorMirror(job.poolID, job.accountID, job.authErrorCount, job.authErrorWindowStart)
	}
}

// clearAuthErrorMirror clears the DB mirror columns for a delayed success
// report (B1-#7). The decision runs under the account's evidence lock so it is
// serialized against mirror writes (ReportAuthErrorCount) and increments:
//
//   - If the in-memory counter holds post-reset evidence (count > 0), a new
//     401/403 arrived after the success snapshot — skip the clear and keep it.
//     The evidence is reconciled later by the next success (whose snapshot
//     includes it), the next mirror write, or the window purge.
//   - Otherwise the SQL "not newer" guard in ClearAuthErrorMirrorIfNotNewer
//     stays as a second line of defense against evidence written outside this
//     process (admin recover, purge zeroing).
func clearAuthErrorMirror(poolID, accountID int, snapshotCount int, snapshotWindowStart int64) {
	mu := authErrorLock(poolID, accountID)
	mu.Lock()
	defer mu.Unlock()
	if count, _ := authErrorSnapshot(poolID, accountID); count > 0 {
		return
	}
	_ = pool.ClearAuthErrorMirrorIfNotNewer(poolID, accountID, snapshotCount, snapshotWindowStart)
}

// StartReportWorkerPool 启动固定数量的 worker 消费 ReportResult 的 DB 写任务。
// 幂等：多次调用只启动一次 worker。ctx 取消后停止派发并丢弃残留 job。
// 应在 task.Init 时调用。
func StartReportWorkerPool(ctx context.Context) {
	poolReportStartOnce.Do(func() {
		for i := 0; i < poolReportWorkers; i++ {
			go func() {
				for {
					select {
					case job := <-poolReportCh:
						applyReportToDB(job)
					case <-ctx.Done():
						return
					}
				}
			}()
		}
	})
}

// DroppedReportCount 返回因队列满而被丢弃的 ReportResult 上报次数（可观测性）。
func DroppedReportCount() int64 {
	return poolReportDroppedCount.Load()
}

// SetRateLimitCooldown 设置 429 冷却。
func SetRateLimitCooldown(poolID, accountID int, until time.Time) {
	if err := pool.UpdateAccount(poolID, accountID, map[string]interface{}{
		"rate_limit_reset_at": until.Unix(),
	}); err == nil {
		notifyPoolStateChange(poolID, accountID, "rate_limit", until.Format(time.RFC3339))
	}
}

// SetOverload 设置过载冷却。
func SetOverload(poolID, accountID int, until time.Time) {
	if err := pool.UpdateAccount(poolID, accountID, map[string]interface{}{
		"overload_until": until.Unix(),
	}); err == nil {
		notifyPoolStateChange(poolID, accountID, "overload", until.Format(time.RFC3339))
	}
}

// SetError 将账号标记为 error 状态。
func SetError(poolID, accountID int) {
	if err := pool.UpdateAccount(poolID, accountID, map[string]interface{}{
		"status": "error",
	}); err == nil {
		notifyPoolStateChange(poolID, accountID, "error", "pool account entered error state")
	}
}

func notifyPoolStateChange(poolID, accountID int, kind, detail string) {
	if NotifyPoolStateChange != nil {
		NotifyPoolStateChange(poolID, accountID, kind, detail)
	}
}

// SetTempUnsched 设置临时不可调度（直到 until；reason 为 TempUnschedState JSON 或空字符串）。
// until 为零值表示清除。
func SetTempUnsched(poolID, accountID int, until time.Time, reason string) {
	updates := map[string]interface{}{
		"temp_unsched_until":  until.Unix(),
		"temp_unsched_reason": reason,
	}
	if until.IsZero() {
		updates["temp_unsched_until"] = int64(0)
		updates["temp_unsched_reason"] = ""
	}
	if err := pool.UpdateAccount(poolID, accountID, updates); err == nil && !until.IsZero() {
		notifyPoolStateChange(poolID, accountID, "temp_unsched", reason)
	}
}

// ClearTempUnsched 手动清除临时不可调度（测试成功/管理员恢复时）。
func ClearTempUnsched(poolID, accountID int) {
	SetTempUnsched(poolID, accountID, time.Time{}, "")
}

// ClearTempUnschedIfTrigger atomically clears the temporary unschedulable flag
// only when the DB's temp_unsched_reason still carries the given trigger tag
// (B1-#4). Used to "conditionally clear a block this flow wrote": blocks held
// by concurrent sources (401 window / 403 cooldown / manual admin block) are
// never erased. cleared=false means the current block does not belong to that
// trigger (or is already empty).
func ClearTempUnschedIfTrigger(poolID, accountID int, trigger string) (cleared bool, err error) {
	return pool.ClearTempUnschedIfTrigger(poolID, accountID, trigger)
}

// AcquireTempUnschedIfFree conditionally sets a temporary unschedulable block
// (B1-#4): the DB-conditional update only matches accounts that are not
// currently blocked (temp_unsched_until in the past), so a block created after
// the caller's account snapshot — 401 window / 403 cooldown / manual flag — is
// never overwritten. Returns false when an active block already holds the
// account; the caller then owns no cleanup.
func AcquireTempUnschedIfFree(poolID, accountID int, until time.Time, reason string) (bool, error) {
	return pool.AcquireTempUnschedIfFree(poolID, accountID, until, reason)
}

// ReportAuthErrorCount reports the current auth-error count to the DB mirror
// (for the admin recovery panel, and for lazy seeding by IncrementAuthError
// after a process restart, B1-#7). Also writes the window start: the
// in-memory counter entry's windowStart (now when the entry is absent); that
// value doubles as the evidence-freshness criterion for the delayed success
// clear.
// The DB write runs under the account's evidence lock so mirror writes and
// delayed-success clear decisions are serialized (see clearAuthErrorMirror).
func ReportAuthErrorCount(poolID, accountID int, count int) error {
	mu := authErrorLock(poolID, accountID)
	mu.Lock()
	defer mu.Unlock()
	windowStart := authErrorNow().Unix()
	if val, ok := globalAuthErrors.Load(authErrorKey(poolID, accountID)); ok {
		entry := val.(*authErrorEntry)
		windowStart = atomic.LoadInt64(&entry.windowStart)
	}
	return pool.UpdateAccount(poolID, accountID, map[string]interface{}{
		"auth_error_count":        count,
		"auth_error_window_start": windowStart,
	})
}

// RecoverAccount 管理员手动恢复账号：清除错误状态与所有冷却/禁用标记。
func RecoverAccount(poolID, accountID int) error {
	return pool.UpdateAccount(poolID, accountID, map[string]interface{}{
		"status":                  "active",
		"error_message":           "",
		"temp_unsched_until":      int64(0),
		"temp_unsched_reason":     "",
		"rate_limit_reset_at":     int64(0),
		"overload_until":          int64(0),
		"auth_error_count":        0,
		"auth_error_window_start": int64(0),
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
	RemoveAuthError(poolID, accountID)
	// 清理指向该账号的粘性条目。
	globalPoolSticky.Range(func(k, v interface{}) bool {
		if s := k.(string); len(s) > 0 && parsePoolID(s) == poolID {
			if entry, ok := v.(*stickyEntry); ok && entry.AccountID == accountID {
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

// PurgeStaleSticky 清理长时间无活动的粘性会话条目（后台任务调用）。globalPoolSticky
// 的 key 含客户端请求携带的 model 名（基数不受控），仅靠 RemovePool/RemoveAccount
// 和 trySticky 惰性删除无法回收一次性/随机 model 名的条目，会无界增长（见 issue #46
// 同类遗漏，balancer.PurgeIdleSessions 已修复，此处补齐）。返回删除的条目数。
func PurgeStaleSticky(idleThreshold time.Duration) int {
	if idleThreshold <= 0 {
		return 0
	}
	now := time.Now()
	removed := 0
	globalPoolSticky.Range(func(key, value any) bool {
		entry, ok := value.(*stickyEntry)
		if !ok {
			globalPoolSticky.Delete(key)
			removed++
			return true
		}
		if now.Sub(entry.LastActivity) >= idleThreshold {
			globalPoolSticky.Delete(key)
			removed++
		}
		return true
	})
	return removed
}

// trySticky returns (account, true, 0) on a sticky hit and (nil, false, 0) on
// a miss. When the sticky account escapes due to degraded EWMA stats it
// returns (nil, false, escapedID>0) — the sticky entry is kept (like the
// excludeIDs-hit / ModelMatches-mismatch branches, no Delete) and the session
// returns to the original binding once the account's stats recover.
func trySticky(poolID int, sessionHash string, excludeIDs []int, poolDefaultConcurrency int, modelName string) (*model.PoolAccount, bool, int) {
	key := stickyKey(poolID, sessionHash)
	val, ok := globalPoolSticky.Load(key)
	if !ok {
		return nil, false, 0
	}
	entry, ok := val.(*stickyEntry)
	if !ok {
		globalPoolSticky.Delete(key)
		return nil, false, 0
	}
	accountID := entry.AccountID
	for _, id := range excludeIDs {
		if id == accountID {
			return nil, false, 0
		}
	}
	acct, err := pool.GetAccount(poolID, accountID)
	if err != nil || !acct.IsSchedulable() {
		globalPoolSticky.Delete(key)
		return nil, false, 0
	}
	if !model.ModelMatches(acct.Models, modelName) {
		return nil, false, 0
	}
	// B1-#9 sticky escape: temporarily bypass the sticky binding when the
	// account's EWMA stats degrade (error rate / TTFT above thresholds). The
	// entry is kept, not rebound; the escape switch defaults to off, and when
	// disabled the behavior is byte-identical to before.
	if shouldEscapeStickyAccount(poolID, accountID) {
		return nil, false, accountID
	}
	limit := acct.EffectiveLoadFactor()
	if limit <= 0 {
		limit = acct.EffectiveConcurrency(poolDefaultConcurrency)
	}
	if !tryAcquireSlot(poolID, accountID, limit) {
		return nil, false, 0
	}
	// 粘性命中，刷新 LastActivity（活跃会话续期，与 balancer.SetSticky 一致）。
	entry.LastActivity = time.Now()
	return acct, true, 0
}

// shouldEscapeStickyAccount reports whether an account's EWMA stats have
// degraded enough to temporarily escape the sticky binding (B1-#9, aligned
// with sub2api openai_account_scheduler.go shouldEscapeStickyAccount: the TTFT
// dimension first, then the error rate; both thresholds use strict greater-than).
// Missing stats (account never had a ReportResult) or a disabled switch means
// no escape — identical to the disabled behavior.
func shouldEscapeStickyAccount(poolID, accountID int) bool {
	enabled, err := setting.GetBool(model.SettingKeyPoolStickyEscapeEnabled)
	if err != nil || !enabled {
		return false
	}
	errorRateThreshold, ttftThresholdMs, err := stickyEscapeThresholds()
	if err != nil {
		return false
	}
	val, ok := globalPoolStats.Load(statsKey(poolID, accountID))
	if !ok {
		return false
	}
	stats := val.(*accountStats)
	stats.mu.Lock()
	errorRate, ttftMs := stats.errorRate, stats.ttftMs
	stats.mu.Unlock()
	// ttftMs==0 means no TTFT sample yet; a threshold <=0 means the admin
	// disabled the TTFT dimension.
	if ttftMs > 0 && ttftThresholdMs > 0 && ttftMs > ttftThresholdMs {
		return true
	}
	if errorRate > errorRateThreshold {
		return true
	}
	return false
}

// stickyEscapeThresholds reads the escape thresholds (settings go through the
// in-process cache, same pattern as filterLayeredByPriority's
// SettingKeyPoolLayeredFilterEnabled read).
func stickyEscapeThresholds() (errorRate float64, ttftThresholdMs float64, err error) {
	rawRate, err := setting.GetString(model.SettingKeyPoolStickyEscapeErrorRate)
	if err != nil {
		return 0, 0, err
	}
	errorRate, err = strconv.ParseFloat(strings.TrimSpace(rawRate), 64)
	if err != nil || errorRate <= 0 || errorRate > 1 {
		return 0, 0, fmt.Errorf("invalid pool_sticky_escape_error_rate: %q", rawRate)
	}
	rawTTFT, err := setting.GetString(model.SettingKeyPoolStickyEscapeTTFTMs)
	if err != nil {
		return 0, 0, err
	}
	ttftThresholdMs, err = strconv.ParseFloat(strings.TrimSpace(rawTTFT), 64)
	if err != nil || ttftThresholdMs < 0 {
		return 0, 0, fmt.Errorf("invalid pool_sticky_escape_ttft_ms: %q", rawTTFT)
	}
	return errorRate, ttftThresholdMs, nil
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
	now := time.Now()
	for i := range accounts {
		acct := &accounts[i]
		if acct.Type != model.PoolTypeOAuth {
			continue
		}
		if !acct.IsTokenExpired() {
			continue
		}
		// 仍在退避窗口内的账号跳过（避兔选号路径绕过剈新退避机制）。
		if !acct.IsRefreshAllowed(now) {
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

// filterLayeredByPriority 可选分层过滤（设置启用时按 min_priority 过滤低优先级候选）。
// 结果为空不 fallback（管理员显式配置）。
func filterLayeredByPriority(candidates []model.PoolAccount) []model.PoolAccount {
	enabled, err := setting.GetBool(model.SettingKeyPoolLayeredFilterEnabled)
	if err != nil || !enabled {
		return candidates
	}
	minPriority, err := setting.GetInt(model.SettingKeyPoolMinPriority)
	if err != nil {
		return candidates
	}
	result := make([]model.PoolAccount, 0, len(candidates))
	for i := range candidates {
		if candidates[i].Priority >= minPriority {
			result = append(result, candidates[i])
		}
	}
	return result
}

func filterBySlot(candidates []model.PoolAccount, poolID, poolDefaultConcurrency int) []model.PoolAccount {
	result := make([]model.PoolAccount, 0, len(candidates))
	for i := range candidates {
		limit := candidates[i].EffectiveLoadFactor()
		if limit <= 0 {
			limit = candidates[i].EffectiveConcurrency(poolDefaultConcurrency)
		}
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
	case "least_loaded":
		return selectByLeastLoaded(candidates, poolID)
	default: // "ewma"
		return selectByEWMA(candidates, poolID)
	}
}

// selectByLeastLoaded 按当前占用槽位 / EffectiveLoadFactor 最小者选择。
// 无槽位记录视为 0，并列取首个。
func selectByLeastLoaded(candidates []model.PoolAccount, poolID int) model.PoolAccount {
	bestIdx := 0
	bestLoad := math.MaxFloat64
	for i := range candidates {
		key := statsKey(poolID, candidates[i].ID)
		load := 0.0
		if val, ok := globalPoolSlots.Load(key); ok {
			load = float64(atomic.LoadInt64(val.(*int64)))
		}
		factor := candidates[i].EffectiveLoadFactor()
		if factor <= 0 {
			factor = 1
		}
		ratio := load / float64(factor)
		if ratio < bestLoad {
			bestLoad = ratio
			bestIdx = i
		}
	}
	return candidates[bestIdx]
}

// B1-#8 scheduler factors (mirroring the "default 0 keeps existing behavior"
// policy of sub2api's GatewayOpenAIWSSchedulerScoreWeights, config.go:1362-1382).
const (
	// resetFactorHorizon is the normalization horizon: remaining time to reset
	// is folded into an inverse-readiness factor over a 7-day span.
	resetFactorHorizon = 7 * 24 * time.Hour
)

// quotaSnapshotParser allows tests to inject an observation point (golden
// tests assert no decryption happens when the weight is 0); production points
// at pool.ParseQuotaSnapshot.
var quotaSnapshotParser = pool.ParseQuotaSnapshot

// loadSchedulerFactorWeights reads the two factor weights (once per
// selectByEWMA call; settings go through the in-process cache; a failed read
// is treated as 0 = off).
func loadSchedulerFactorWeights() (wReset, wQuota float64) {
	return schedulerFactorWeight(model.SettingKeyPoolSchedulerWeightReset),
		schedulerFactorWeight(model.SettingKeyPoolSchedulerWeightQuota)
}

func schedulerFactorWeight(key model.SettingKey) float64 {
	raw, err := setting.GetString(key)
	if err != nil {
		return 0
	}
	w, err := strconv.ParseFloat(strings.TrimSpace(raw), 64)
	if err != nil || w < 0 {
		return 0
	}
	return w
}

func selectByEWMA(candidates []model.PoolAccount, poolID int) model.PoolAccount {
	// B1-#8: when both weights are 0 (the default) short-circuit at entry —
	// no factor computation, no quota snapshot decryption, no extra DB
	// access; byte-identical to the old implementation (locked by golden test).
	wReset, wQuota := loadSchedulerFactorWeights()
	factorsEnabled := wReset > 0 || wQuota > 0

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
		if factorsEnabled {
			// Factors are score deductions (lower score wins): candidates whose
			// reset is nearer (higher inverse-readiness factor) or whose quota
			// headroom is larger (higher headroom) score lower and win.
			// Each factor is guarded by its own weight: wQuota=0 never decrypts
			// the quota snapshot; wReset=0 skips the reset computation.
			if wReset > 0 {
				score -= wReset * resetReadinessFactor(&candidates[i])
			}
			if wQuota > 0 {
				score -= wQuota * quotaHeadroomFactor(&candidates[i])
			}
		}
		// weight 先于 priority 作为 tiebreaker：权重越高得分越低（越容易选中）。
		score -= float64(candidates[i].Weight) * 0.001
		// priority 作为第二 tiebreaker：高优先级减分。
		score -= float64(candidates[i].Priority) * 0.001
		if score < bestScore {
			bestScore = score
			bestIdx = i
		}
	}
	return candidates[bestIdx]
}

// resetReadinessFactor is the inverse-readiness factor (B1-#8): it takes the
// candidate's nearest future reset instant (the smallest positive difference
// of ExpiresAt / RateLimitResetAt; TokenExpiresAt is deliberately excluded —
// token expiry already has its own scheduling exclusion and refresh path),
// and the shorter the remaining time the higher the factor (larger deduction,
// higher priority), normalized over resetFactorHorizon. No future reset
// (unset 0 or already elapsed) → neutral 0, not participating in factor
// comparison.
func resetReadinessFactor(a *model.PoolAccount) float64 {
	now := time.Now().Unix()
	remaining := int64(0)
	for _, t := range []int64{a.ExpiresAt, a.RateLimitResetAt} {
		if d := t - now; d > 0 && (remaining == 0 || d < remaining) {
			remaining = d
		}
	}
	if remaining == 0 {
		return 0
	}
	factor := 1 - float64(remaining)/float64(int64(resetFactorHorizon/time.Second))
	if factor < 0 {
		return 0
	}
	return factor
}

// quotaHeadroomFactor is the quota-headroom factor (B1-#8): decrypts and
// parses the account's cached quota snapshot (QuotaResult used/total/reset_at
// shape) and returns 1-used/total truncated to [0,1]. Missing snapshot / parse
// failure / total<=0 → neutral 0. Only reachable when the quota weight is
// non-zero (selectByEWMA entry short-circuit); the default path pays no
// decryption cost.
func quotaHeadroomFactor(a *model.PoolAccount) float64 {
	used, total, ok := quotaSnapshotParser(a)
	if !ok || total <= 0 {
		return 0
	}
	headroom := 1 - used/total
	if headroom < 0 {
		return 0
	}
	if headroom > 1 {
		return 1
	}
	return headroom
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
