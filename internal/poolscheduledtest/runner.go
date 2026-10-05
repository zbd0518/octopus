package poolscheduledtest

import (
	"strings"
	"sync"
	"time"

	"github.com/lingyuins/octopus/internal/db"
	"github.com/lingyuins/octopus/internal/model"
	"github.com/lingyuins/octopus/internal/op/pool"
	"github.com/lingyuins/octopus/internal/relay/poolscheduler"
	"github.com/lingyuins/octopus/internal/utils/log"
)

// testConcurrency caps concurrent account probes per Run pass (same shape as
// poolhealthcheck's probeSem and pooltokenrefresh's refreshSem).
var testSem = make(chan struct{}, 4)

// timeNow is a test seam for the runner clock.
var timeNow = time.Now

// Run executes one scan pass (called every minute by the task scheduler):
// claims every enabled plan whose next_run_at has arrived, tests the target
// accounts via pool.TestAccount, optionally recovers accounts that pass, and
// records a result row per executed test. Failure of a probe never marks the
// account in error — recovery-only semantics are deliberate (unlike
// poolhealthcheck), so a stale plan cannot take accounts offline.
func Run() {
	now := timeNow()
	plans, err := duePlans(now)
	if err != nil {
		log.Warnf("poolscheduledtest: list due plans failed: %v", err)
		return
	}
	for i := range plans {
		runPlan(&plans[i], now)
	}
}

// runPlan claims one plan (advance last/next run before probing so a long
// pass cannot re-enqueue the same plan) and tests its target accounts.
func runPlan(plan *model.PoolScheduledTest, now time.Time) {
	schedule, err := ParseCronExpr(plan.CronExpr)
	if err != nil {
		// Create/update validation makes this unreachable; defensive no-op
		// with a loud log instead of silently disabling the plan.
		log.Warnf("poolscheduledtest: plan %d has invalid cron %q: %v", plan.ID, plan.CronExpr, err)
		return
	}
	next := schedule.Next(now)
	if next.IsZero() {
		next = now.Add(time.Hour)
	}
	// Claim: advance the schedule first, then probe.
	if err := claimPlan(plan.ID, now.Unix(), next.Unix()); err != nil {
		log.Warnf("poolscheduledtest: claim plan %d failed: %v", plan.ID, err)
		return
	}

	targets, err := resolveTargets(plan)
	if err != nil {
		log.Warnf("poolscheduledtest: resolve targets for plan %d failed: %v", plan.ID, err)
		return
	}

	var wg sync.WaitGroup
	for _, acct := range targets {
		wg.Add(1)
		testSem <- struct{}{}
		go func(acct model.PoolAccount) {
			defer wg.Done()
			defer func() { <-testSem }()
			testAccountForPlan(plan, &acct)
		}(acct)
	}
	wg.Wait()
}

// claimPlan advances last_run_at / next_run_at so the next scan skips this
// plan until the following scheduled slot.
func claimPlan(planID int, lastRun, nextRun int64) error {
	return db.GetDB().Model(&model.PoolScheduledTest{}).
		Where("id = ?", planID).
		Updates(map[string]interface{}{
			"last_run_at": lastRun,
			"next_run_at": nextRun,
		}).Error
}

// resolveTargets returns the accounts a plan covers. Whole-pool plans cover
// every account in the pool; account-scoped plans resolve the single account.
func resolveTargets(plan *model.PoolScheduledTest) ([]model.PoolAccount, error) {
	if plan.AccountID == nil {
		return pool.ListAccounts(plan.PoolID)
	}
	acct, err := pool.GetAccount(plan.PoolID, *plan.AccountID)
	if err != nil {
		return nil, err
	}
	return []model.PoolAccount{*acct}, nil
}

// testAccountForPlan runs one connectivity test and records its outcome.
// Accounts without a bound model cannot be probed (TestAccount needs a model
// name): whole-pool plans skip them silently (same policy as
// poolhealthcheck), while an explicitly targeted account gets a failure row
// so the plan never looks silently dead.
func testAccountForPlan(plan *model.PoolScheduledTest, acct *model.PoolAccount) {
	modelName := firstBoundModel(acct.Models)
	if modelName == "" {
		if plan.AccountID != nil {
			_ = appendResult(&model.PoolScheduledTestResult{
				TestID:    plan.ID,
				AccountID: acct.ID,
				Success:   false,
				Detail:    "account has no bound model to test with",
			})
		}
		return
	}

	res, err := pool.TestAccount(plan.PoolID, acct.ID, modelName)
	result := model.PoolScheduledTestResult{
		TestID:    plan.ID,
		AccountID: acct.ID,
	}
	switch {
	case err != nil:
		result.Success = false
		result.Detail = err.Error()
	case res == nil:
		result.Success = false
		result.Detail = "empty test result"
	default:
		result.Success = res.Success
		result.DurationMS = res.Latency
		if res.Error != "" {
			result.Detail = res.Error
		}
	}
	if err := appendResult(&result); err != nil {
		log.Warnf("poolscheduledtest: append result for plan %d failed: %v", plan.ID, err)
	}

	// Auto-recover only on a passing probe. Unsupported platforms (e.g.
	// volcengine cookie accounts, which cannot run a standard chat probe)
	// surface as success=false rows and are never "recovered".
	if result.Success && plan.AutoRecover {
		if err := poolscheduler.RecoverAccount(plan.PoolID, acct.ID); err != nil {
			log.Warnf("poolscheduledtest: recover account %d/%d failed: %v", plan.PoolID, acct.ID, err)
		}
	}
}

// firstBoundModel returns the first non-empty entry of a Models CSV.
func firstBoundModel(csv string) string {
	for _, m := range strings.Split(csv, ",") {
		if trimmed := strings.TrimSpace(m); trimmed != "" {
			return trimmed
		}
	}
	return ""
}
