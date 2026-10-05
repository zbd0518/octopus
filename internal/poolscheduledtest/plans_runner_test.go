package poolscheduledtest

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/lingyuins/octopus/internal/db"
	"github.com/lingyuins/octopus/internal/model"
	"github.com/lingyuins/octopus/internal/op/pool"
	"github.com/lingyuins/octopus/internal/relay/poolscheduler"
	"github.com/lingyuins/octopus/internal/utils/crypto"
)

// Shared test DB (on Windows TempDir handles stay open across tests, so the
// DB cannot be rebuilt per test). db.InitDB runs the full migration chain
// once; the migration registry is consumed afterwards.
var (
	testDBOnce sync.Once
	testDBErr  error
)

func ensureTestDB(t *testing.T) {
	t.Helper()
	testDBOnce.Do(func() {
		crypto.Init("octopus-test-encryption-key")
		dir, err := os.MkdirTemp("", "poolscheduledtest-test-*")
		if err != nil {
			testDBErr = err
			return
		}
		testDBErr = db.InitDB("sqlite", filepath.Join(dir, "sched-test.db"), false)
	})
	if testDBErr != nil {
		t.Fatalf("init shared test db: %v", testDBErr)
	}
}

func createTestPoolAndAccount(t *testing.T, baseURL string) (poolID, accountID int) {
	t.Helper()
	ensureTestDB(t)
	p := &model.AccountPool{Name: fmt.Sprintf("sched-test-pool-%d", time.Now().UnixNano()), Enabled: true}
	if err := pool.CreatePool(p); err != nil {
		t.Fatalf("create pool: %v", err)
	}
	a := &model.PoolAccount{
		PoolID:      p.ID,
		Name:        "sched-test-account",
		Platform:    model.PoolPlatformOpenAI,
		Type:        model.PoolTypeAPIKey,
		Models:      "gpt-test",
		Credentials: `{"type":"apikey","api_key":"sk-test"}`,
		BaseURL:     baseURL,
	}
	if err := pool.CreateAccount(a); err != nil {
		t.Fatalf("create account: %v", err)
	}
	t.Cleanup(func() {
		poolscheduler.RemovePool(p.ID)
		_ = pool.DeletePool(p.ID)
	})
	return p.ID, a.ID
}

func makePlanDue(t *testing.T, planID int) {
	t.Helper()
	if err := db.GetDB().Model(&model.PoolScheduledTest{}).Where("id = ?", planID).
		Update("next_run_at", time.Now().Add(-time.Minute).Unix()).Error; err != nil {
		t.Fatalf("make plan due: %v", err)
	}
}

// TestPlanCRUDAndValidation covers create/update/list/delete plus the
// route-enforced cron validation and result-history trimming.
func TestPlanCRUDAndValidation(t *testing.T) {
	poolID, _ := createTestPoolAndAccount(t, "http://127.0.0.1:9")

	if _, err := CreatePlan(poolID, nil, "0 9 * * 1", true, false); err == nil {
		t.Fatalf("day-of-week field must be rejected")
	}

	plan, err := CreatePlan(poolID, nil, "*/30 * * * *", true, true)
	if err != nil {
		t.Fatalf("create plan: %v", err)
	}
	if plan.NextRunAt == 0 {
		t.Fatalf("create must seed next_run_at")
	}
	if plan.AccountID != nil {
		t.Fatalf("nil account_id must stay nil (whole-pool scope)")
	}

	wholePoolID := 0
	updated, err := UpdatePlan(poolID, plan.ID, "0 9,21 * * *", &wholePoolID, false, false)
	if err != nil {
		t.Fatalf("update plan: %v", err)
	}
	if updated.AccountID == nil || *updated.AccountID != 0 {
		t.Fatalf("update must persist the account scope")
	}
	if updated.Enabled {
		t.Fatalf("update must persist enabled=false")
	}

	if _, err := UpdatePlan(poolID, plan.ID, "0 9 * * 1", nil, true, false); err == nil {
		t.Fatalf("update must reject unsupported cron grammar")
	}

	plans, err := ListPlans(poolID)
	if err != nil || len(plans) != 1 {
		t.Fatalf("list plans: %v (%d)", err, len(plans))
	}

	// Regression: a plan created disabled must stay disabled (GORM replaces
	// zero-valued fields carrying a default tag with the DDL default on
	// INSERT; Enabled therefore carries no default tag).
	disabled, err := CreatePlan(poolID, nil, "* * * * *", false, false)
	if err != nil {
		t.Fatalf("create disabled plan: %v", err)
	}
	reloaded, err := GetPlan(poolID, disabled.ID)
	if err != nil {
		t.Fatalf("get disabled plan: %v", err)
	}
	if reloaded.Enabled {
		t.Fatalf("create with enabled=false must persist disabled")
	}
	t.Cleanup(func() { _ = DeletePlan(poolID, disabled.ID) })

	if err := DeletePlan(poolID, plan.ID); err != nil {
		t.Fatalf("delete plan: %v", err)
	}
	if _, err := GetPlan(poolID, plan.ID); err == nil {
		t.Fatalf("plan must be gone after delete")
	}
}

// TestResultHistoryTrimmedToNewest100 locks in the delete-old-on-insert cap.
func TestResultHistoryTrimmedToNewest100(t *testing.T) {
	ensureTestDB(t)
	// Standalone result rows need no live plan for this storage-level check.
	for i := 0; i < maxResultsPerPlan+10; i++ {
		if err := appendResult(&model.PoolScheduledTestResult{TestID: 990001, AccountID: 1, Success: true}); err != nil {
			t.Fatalf("append result %d: %v", i, err)
		}
	}
	results, err := ListResults(0, 990001, 0)
	if err != nil {
		// ListResults validates pool ownership; query directly instead for
		// this storage-level assertion.
		results = nil
		if err := db.GetDB().Where("test_id = ?", 990001).Order("id DESC").Find(&results).Error; err != nil {
			t.Fatalf("list results: %v", err)
		}
	}
	if len(results) != maxResultsPerPlan {
		t.Fatalf("result history = %d rows, want %d", len(results), maxResultsPerPlan)
	}
	t.Cleanup(func() {
		db.GetDB().Where("test_id = ?", 990001).Delete(&model.PoolScheduledTestResult{})
	})
}

// TestRunRecoversErrorAccountWhenAutoRecoverEnabled covers the full pass:
// due plan → probe a mock-200 upstream → result row → errored account revived
// only when the plan has auto_recover enabled.
func TestRunRecoversErrorAccountWhenAutoRecoverEnabled(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"id":"1","choices":[]}`))
	}))
	t.Cleanup(upstream.Close)

	poolID, accountID := createTestPoolAndAccount(t, upstream.URL)

	poolscheduler.SetError(poolID, accountID)
	acct, err := pool.GetAccount(poolID, accountID)
	if err != nil || acct.Status != "error" {
		t.Fatalf("precondition: account must be in error state (%v, %v)", acct, err)
	}

	plan, err := CreatePlan(poolID, &accountID, "* * * * *", true, true)
	if err != nil {
		t.Fatalf("create plan: %v", err)
	}
	makePlanDue(t, plan.ID)
	Run()

	results, err := ListResults(poolID, plan.ID, 0)
	if err != nil || len(results) != 1 {
		t.Fatalf("expected exactly one result row, got %d (%v)", len(results), err)
	}
	if !results[0].Success {
		t.Fatalf("result must record success, detail=%q", results[0].Detail)
	}
	acct, err = pool.GetAccount(poolID, accountID)
	if err != nil {
		t.Fatalf("get account: %v", err)
	}
	if acct.Status != "active" {
		t.Fatalf("auto_recover must revive the errored account, status=%q", acct.Status)
	}
	fresh, err := GetPlan(poolID, plan.ID)
	if err != nil {
		t.Fatalf("get plan: %v", err)
	}
	if fresh.NextRunAt <= timeNow().Unix() {
		t.Fatalf("run must advance next_run_at (%d)", fresh.NextRunAt)
	}
}

// TestRunWithoutAutoRecoverOnlyRecordsResults: auto_recover=false is
// test-only — a passing probe must NOT revive the account.
func TestRunWithoutAutoRecoverOnlyRecordsResults(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"id":"1","choices":[]}`))
	}))
	t.Cleanup(upstream.Close)

	poolID, accountID := createTestPoolAndAccount(t, upstream.URL)
	poolscheduler.SetError(poolID, accountID)

	plan, err := CreatePlan(poolID, &accountID, "* * * * *", true, false)
	if err != nil {
		t.Fatalf("create plan: %v", err)
	}
	makePlanDue(t, plan.ID)
	Run()

	results, err := ListResults(poolID, plan.ID, 0)
	if err != nil || len(results) != 1 || !results[0].Success {
		t.Fatalf("expected one successful result row, got %v (%v)", results, err)
	}
	acct, err := pool.GetAccount(poolID, accountID)
	if err != nil {
		t.Fatalf("get account: %v", err)
	}
	if acct.Status != "error" {
		t.Fatalf("auto_recover=false must not revive the account, status=%q", acct.Status)
	}
}

// TestRunFailureRecordsResultWithoutRecovery: a failing probe writes a
// success=false row and never marks the account in error (recovery-only
// semantics) and never revives it either.
func TestRunFailureRecordsResultWithoutRecovery(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":"boom"}`))
	}))
	t.Cleanup(upstream.Close)

	poolID, accountID := createTestPoolAndAccount(t, upstream.URL)

	plan, err := CreatePlan(poolID, &accountID, "* * * * *", true, true)
	if err != nil {
		t.Fatalf("create plan: %v", err)
	}
	makePlanDue(t, plan.ID)
	Run()

	results, err := ListResults(poolID, plan.ID, 0)
	if err != nil || len(results) != 1 {
		t.Fatalf("expected one result row, got %d (%v)", len(results), err)
	}
	if results[0].Success {
		t.Fatalf("500 upstream must record success=false")
	}
	acct, err := pool.GetAccount(poolID, accountID)
	if err != nil {
		t.Fatalf("get account: %v", err)
	}
	if acct.Status != "active" {
		t.Fatalf("a failing scheduled test must not mark the account in error, status=%q", acct.Status)
	}
}

// TestRunSkipsNonDuePlans: an enabled plan whose next_run_at is in the future
// must not execute on this pass.
func TestRunSkipsNonDuePlans(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{}`))
	}))
	t.Cleanup(upstream.Close)

	poolID, accountID := createTestPoolAndAccount(t, upstream.URL)
	plan, err := CreatePlan(poolID, &accountID, "* * * * *", true, true)
	if err != nil {
		t.Fatalf("create plan: %v", err)
	}
	// Freshly created: next_run_at is strictly in the future.
	Run()
	results, _ := ListResults(poolID, plan.ID, 0)
	if len(results) != 0 {
		t.Fatalf("non-due plan must not run, got %d result rows", len(results))
	}
}
