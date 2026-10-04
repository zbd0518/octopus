package poolscheduler

import (
	"sync/atomic"
	"testing"
	"time"

	"github.com/lingyuins/octopus/internal/model"
	"github.com/lingyuins/octopus/internal/op/pool"
	"github.com/lingyuins/octopus/internal/op/setting"
	"github.com/lingyuins/octopus/internal/utils/crypto"
)

// setSchedulerWeightsForTest 设置两个因子权重并在测试后还原默认值 0（默认关闭）。
func setSchedulerWeightsForTest(t *testing.T, wReset, wQuota string) {
	t.Helper()
	set := func(key model.SettingKey, value string) {
		if err := setting.SetString(key, value); err != nil {
			t.Fatalf("set %s=%s: %v", key, value, err)
		}
	}
	set(model.SettingKeyPoolSchedulerWeightReset, wReset)
	set(model.SettingKeyPoolSchedulerWeightQuota, wQuota)
	t.Cleanup(func() {
		_ = setting.SetString(model.SettingKeyPoolSchedulerWeightReset, "0")
		_ = setting.SetString(model.SettingKeyPoolSchedulerWeightQuota, "0")
	})
}

// countingQuotaParser 在真实解析器外套一层计数观测点（golden 测试用）。
func countingQuotaParser(t *testing.T) *int64 {
	t.Helper()
	var calls int64
	prev := quotaSnapshotParser
	quotaSnapshotParser = func(a *model.PoolAccount) (float64, float64, bool) {
		atomic.AddInt64(&calls, 1)
		return prev(a)
	}
	t.Cleanup(func() { quotaSnapshotParser = prev })
	return &calls
}

func initCryptoForQuotaTest(t *testing.T) {
	t.Helper()
	// 与全仓测试约定一致的密钥（勿改字符串）；sync.Once 保证幂等。
	crypto.Init("octopus-test-encryption-key")
}

func TestResetReadinessFactor_NeutralAndMinPositive(t *testing.T) {
	now := time.Now().Unix()

	// 未设置（0）→ 中性。
	if got := resetReadinessFactor(&model.PoolAccount{}); got != 0 {
		t.Fatalf("unset reset should be neutral, got %v", got)
	}
	// 已过期（负差）→ 中性。
	if got := resetReadinessFactor(&model.PoolAccount{ExpiresAt: now - 100}); got != 0 {
		t.Fatalf("expired reset should be neutral, got %v", got)
	}

	// 最小正差：RateLimitResetAt(+30min) 早于 ExpiresAt(+1h) → 按 30min 折算。
	a := &model.PoolAccount{ExpiresAt: now + 3600, RateLimitResetAt: now + 1800}
	want := 1 - 1800.0/float64(int64(resetFactorHorizon/time.Second))
	if got := resetReadinessFactor(a); diff(got, want) > 1e-9 {
		t.Fatalf("min-positive factor=%v want %v", got, want)
	}

	// 剩余越短因子越高（短 reset 更好）。
	long := &model.PoolAccount{ExpiresAt: now + 6*24*3600}
	if resetReadinessFactor(a) <= resetReadinessFactor(long) {
		t.Fatalf("shorter remaining reset must yield a higher factor")
	}
}

// TestSelectByEWMA_WeightZeroGolden（golden）：默认权重 0 时不做任何因子计算
//（零解密调用），选择结果与旧实现一致。
func TestSelectByEWMA_WeightZeroGolden(t *testing.T) {
	poolID, _ := setupSchedulerPoolDB(t)
	a1 := addAccount(t, poolID, &model.PoolAccount{Name: "factor-a1"})
	a2 := addAccount(t, poolID, &model.PoolAccount{Name: "factor-a2"})

	globalPoolStats.Store(statsKey(poolID, a1), &accountStats{errorRate: 0.8, ttftMs: 0, lastActivity: time.Now()})
	globalPoolStats.Store(statsKey(poolID, a2), &accountStats{errorRate: 0.2, ttftMs: 0, lastActivity: time.Now()})
	t.Cleanup(func() {
		globalPoolStats.Delete(statsKey(poolID, a1))
		globalPoolStats.Delete(statsKey(poolID, a2))
	})

	calls := countingQuotaParser(t)

	got := selectByEWMA([]model.PoolAccount{
		{ID: a1, Quota: `{"used":99,"total":100}`},
		{ID: a2},
	}, poolID)
	if got.ID != a2 {
		t.Fatalf("weight=0 must keep legacy selection (a2), got %d", got.ID)
	}
	if c := atomic.LoadInt64(calls); c != 0 {
		t.Fatalf("weight=0 must not decrypt quota snapshots, got %d calls", c)
	}
}

// TestSelectByEWMA_ShorterResetWins：wReset>0 时，reset 剩余更短的账号更优先。
func TestSelectByEWMA_ShorterResetWins(t *testing.T) {
	poolID, _ := setupSchedulerPoolDB(t)
	setSchedulerWeightsForTest(t, "1", "0")

	now := time.Now().Unix()
	a1 := addAccount(t, poolID, &model.PoolAccount{Name: "reset-soon", ExpiresAt: now + 3600})
	a2 := addAccount(t, poolID, &model.PoolAccount{Name: "reset-late", ExpiresAt: now + 6*24*3600})

	got := selectByEWMA([]model.PoolAccount{
		{ID: a1, ExpiresAt: now + 3600},
		{ID: a2, ExpiresAt: now + 6*24*3600},
	}, poolID)
	if got.ID != a1 {
		t.Fatalf("shorter reset should win, got %d want %d", got.ID, a1)
	}
}

// TestSelectByEWMA_UnsetResetNeutral：无未来 reset 的账号因子恒为 0（中性、
// 无加分），reset 明确且临近的账号获得优先。
func TestSelectByEWMA_UnsetResetNeutral(t *testing.T) {
	poolID, _ := setupSchedulerPoolDB(t)
	setSchedulerWeightsForTest(t, "1", "0")

	now := time.Now().Unix()
	a1 := addAccount(t, poolID, &model.PoolAccount{Name: "reset-unset"})
	a2 := addAccount(t, poolID, &model.PoolAccount{Name: "reset-1h", ExpiresAt: now + 3600})

	got := selectByEWMA([]model.PoolAccount{{ID: a1}, {ID: a2, ExpiresAt: now + 3600}}, poolID)
	if got.ID != a2 {
		t.Fatalf("reset-aware account should outrank neutral unset account, got %d want %d", got.ID, a2)
	}
}

// TestSelectByEWMA_QuotaHeadroomWins：wQuota>0 时，额度余量更大的账号更优先；
// 快照缺失的账号因子中性（0）。
func TestSelectByEWMA_QuotaHeadroomWins(t *testing.T) {
	initCryptoForQuotaTest(t)
	poolID, _ := setupSchedulerPoolDB(t)
	setSchedulerWeightsForTest(t, "0", "1")

	a1 := addAccount(t, poolID, &model.PoolAccount{Name: "quota-low"})
	a2 := addAccount(t, poolID, &model.PoolAccount{Name: "quota-high"})
	a3 := addAccount(t, poolID, &model.PoolAccount{Name: "quota-missing"})

	// 真实加密链路：加密快照落库形状与生产一致。
	a1Quota := pool.EncryptCredentials(`{"used":90,"total":100,"reset_at":0}`)
	a2Quota := pool.EncryptCredentials(`{"used":10,"total":100,"reset_at":0}`)

	got := selectByEWMA([]model.PoolAccount{
		{ID: a1, Quota: a1Quota},
		{ID: a2, Quota: a2Quota},
		{ID: a3},
	}, poolID)
	if got.ID != a2 {
		t.Fatalf("higher quota headroom should win, got %d want %d", got.ID, a2)
	}
}

// TestSelectByEWMA_BothWeightsZeroNoDecryption：reset 与 quota 权重同时非 0 时
// 才会触碰解析器；仅 reset 权重非 0 时不做任何解密（成本守卫）。
func TestSelectByEWMA_BothWeightsZeroNoDecryption(t *testing.T) {
	poolID, _ := setupSchedulerPoolDB(t)
	setSchedulerWeightsForTest(t, "1", "0")

	a1 := addAccount(t, poolID, &model.PoolAccount{Name: "no-decrypt-a"})
	a2 := addAccount(t, poolID, &model.PoolAccount{Name: "no-decrypt-b"})
	calls := countingQuotaParser(t)

	_ = selectByEWMA([]model.PoolAccount{{ID: a1, Quota: `{"used":1,"total":100}`}, {ID: a2}}, poolID)
	if c := atomic.LoadInt64(calls); c != 0 {
		t.Fatalf("reset-only weight must not decrypt quota snapshots, got %d calls", c)
	}
}

// TestParseQuotaSnapshot_Shapes：加密 / 明文 / 非法 / total<=0 的解析矩阵。
func TestParseQuotaSnapshot_Shapes(t *testing.T) {
	initCryptoForQuotaTest(t)

	enc := pool.EncryptCredentials(`{"used":30,"total":100,"reset_at":1759500000}`)
	used, total, ok := pool.ParseQuotaSnapshot(&model.PoolAccount{Quota: enc})
	if !ok || used != 30 || total != 100 {
		t.Fatalf("encrypted snapshot: used=%v total=%v ok=%v", used, total, ok)
	}

	used, total, ok = pool.ParseQuotaSnapshot(&model.PoolAccount{Quota: `{"used":5,"total":200}`})
	if !ok || used != 5 || total != 200 {
		t.Fatalf("plaintext snapshot: used=%v total=%v ok=%v", used, total, ok)
	}

	if _, _, ok := pool.ParseQuotaSnapshot(&model.PoolAccount{Quota: "not-json"}); ok {
		t.Fatalf("invalid JSON must not parse")
	}
	if _, _, ok := pool.ParseQuotaSnapshot(&model.PoolAccount{Quota: `{"used":1,"total":0}`}); ok {
		t.Fatalf("total<=0 must not parse")
	}
	if _, _, ok := pool.ParseQuotaSnapshot(&model.PoolAccount{}); ok {
		t.Fatalf("empty quota must not parse")
	}
	if _, _, ok := pool.ParseQuotaSnapshot(nil); ok {
		t.Fatalf("nil account must not parse")
	}
}

func diff(a, b float64) float64 {
	if a > b {
		return a - b
	}
	return b - a
}
