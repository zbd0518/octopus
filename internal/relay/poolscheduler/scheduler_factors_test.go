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

// setSchedulerWeightsForTest sets the two factor weights and restores the
// defaults (0 = off) after the test.
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

// countingQuotaParser wraps the real parser with a counting observation point
// (used by the golden tests).
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
	// The key shared by the whole-repo test convention (never change the string);
	// sync.Once makes it idempotent.
	crypto.Init("octopus-test-encryption-key")
}

func TestResetReadinessFactor_NeutralAndMinPositive(t *testing.T) {
	now := time.Now().Unix()

	// Unset (0) -> neutral.
	if got := resetReadinessFactor(&model.PoolAccount{}); got != 0 {
		t.Fatalf("unset reset should be neutral, got %v", got)
	}
	// Already elapsed (negative difference) -> neutral.
	if got := resetReadinessFactor(&model.PoolAccount{ExpiresAt: now - 100}); got != 0 {
		t.Fatalf("expired reset should be neutral, got %v", got)
	}

	// Smallest positive difference: RateLimitResetAt(+30min) precedes
	// ExpiresAt(+1h) -> folded over 30min.
	a := &model.PoolAccount{ExpiresAt: now + 3600, RateLimitResetAt: now + 1800}
	want := 1 - 1800.0/float64(int64(resetFactorHorizon/time.Second))
	if got := resetReadinessFactor(a); diff(got, want) > 1e-9 {
		t.Fatalf("min-positive factor=%v want %v", got, want)
	}

	// The shorter the remainder the higher the factor (a near reset is better).
	long := &model.PoolAccount{ExpiresAt: now + 6*24*3600}
	if resetReadinessFactor(a) <= resetReadinessFactor(long) {
		t.Fatalf("shorter remaining reset must yield a higher factor")
	}
}

// TestSelectByEWMA_WeightZeroGolden (golden): with the default weight 0 no
// factor computation happens (zero decrypt calls) and the selection matches the
// old implementation.
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

// TestSelectByEWMA_ShorterResetWins: with wReset>0 the account whose reset is
// nearer wins.
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

// TestSelectByEWMA_UnsetResetNeutral: accounts without a future reset always
// get factor 0 (neutral, no bonus); an account with a concrete near reset is
// preferred.
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

// TestSelectByEWMA_QuotaHeadroomWins: with wQuota>0 the account with more quota
// headroom wins; a missing snapshot yields the neutral factor (0).
func TestSelectByEWMA_QuotaHeadroomWins(t *testing.T) {
	initCryptoForQuotaTest(t)
	poolID, _ := setupSchedulerPoolDB(t)
	setSchedulerWeightsForTest(t, "0", "1")

	a1 := addAccount(t, poolID, &model.PoolAccount{Name: "quota-low"})
	a2 := addAccount(t, poolID, &model.PoolAccount{Name: "quota-high"})
	a3 := addAccount(t, poolID, &model.PoolAccount{Name: "quota-missing"})

	// Real crypto path: the encrypted snapshot is stored in the production shape.
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

// TestSelectByEWMA_BothWeightsZeroNoDecryption: the parser is only touched when
// the quota weight is non-zero; with only the reset weight non-zero no
// decryption happens at all (cost guard).
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

// TestParseQuotaSnapshot_Shapes: parse matrix over encrypted / plaintext /
// invalid / total<=0 inputs.
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
