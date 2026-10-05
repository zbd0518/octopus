package poolscheduledtest

import (
	"time"

	"github.com/lingyuins/octopus/internal/db"
	"github.com/lingyuins/octopus/internal/model"
	"gorm.io/gorm"
)

// maxResultsPerPlan caps the result history kept per plan (delete-old-on-insert).
const maxResultsPerPlan = 100

// ListPlans returns the scheduled test plans of one pool.
func ListPlans(poolID int) ([]model.PoolScheduledTest, error) {
	var plans []model.PoolScheduledTest
	err := db.GetDB().Where("pool_id = ?", poolID).Order("id").Find(&plans).Error
	return plans, err
}

// GetPlan returns one plan scoped to its pool.
func GetPlan(poolID, planID int) (*model.PoolScheduledTest, error) {
	var plan model.PoolScheduledTest
	if err := db.GetDB().Where("pool_id = ? AND id = ?", poolID, planID).First(&plan).Error; err != nil {
		return nil, err
	}
	return &plan, nil
}

// CreatePlan validates the cron expression, seeds next_run_at and stores the
// plan. accountID nil means the whole pool.
func CreatePlan(poolID int, accountID *int, cronExpr string, enabled, autoRecover bool) (*model.PoolScheduledTest, error) {
	schedule, err := ParseCronExpr(cronExpr)
	if err != nil {
		return nil, err
	}
	plan := &model.PoolScheduledTest{
		PoolID:      poolID,
		AccountID:   accountID,
		CronExpr:    cronExpr,
		Enabled:     enabled,
		AutoRecover: autoRecover,
		NextRunAt:   schedule.Next(time.Now()).Unix(),
	}
	if err := db.GetDB().Create(plan).Error; err != nil {
		return nil, err
	}
	return plan, nil
}

// UpdatePlan applies mutable fields and recomputes next_run_at from the
// (possibly changed) cron expression. A zero-value field in updates is a
// legitimate new value for bool/int columns, so the caller passes the full
// desired shape rather than a sparse diff.
func UpdatePlan(poolID, planID int, cronExpr string, accountID *int, enabled, autoRecover bool) (*model.PoolScheduledTest, error) {
	plan, err := GetPlan(poolID, planID)
	if err != nil {
		return nil, err
	}
	schedule, err := ParseCronExpr(cronExpr)
	if err != nil {
		return nil, err
	}
	plan.AccountID = accountID
	plan.CronExpr = cronExpr
	plan.Enabled = enabled
	plan.AutoRecover = autoRecover
	plan.NextRunAt = schedule.Next(time.Now()).Unix()
	if err := db.GetDB().Save(plan).Error; err != nil {
		return nil, err
	}
	return plan, nil
}

// DeletePlan removes a plan and its result history.
func DeletePlan(poolID, planID int) error {
	result := db.GetDB().Where("pool_id = ? AND id = ?", poolID, planID).Delete(&model.PoolScheduledTest{})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return db.GetDB().Where("test_id = ?", planID).Delete(&model.PoolScheduledTestResult{}).Error
}

// ListResults returns the newest result rows of one plan (oldest last).
func ListResults(poolID, planID int, limit int) ([]model.PoolScheduledTestResult, error) {
	if limit <= 0 || limit > maxResultsPerPlan {
		limit = maxResultsPerPlan
	}
	// Ensure the plan belongs to the pool before exposing its history.
	if _, err := GetPlan(poolID, planID); err != nil {
		return nil, err
	}
	var results []model.PoolScheduledTestResult
	err := db.GetDB().Where("test_id = ?", planID).Order("id DESC").Limit(limit).Find(&results).Error
	return results, err
}

// duePlans returns enabled plans whose next_run_at has arrived.
func duePlans(now time.Time) ([]model.PoolScheduledTest, error) {
	var plans []model.PoolScheduledTest
	err := db.GetDB().
		Where("enabled = ? AND next_run_at > 0 AND next_run_at <= ?", true, now.Unix()).
		Order("id").
		Find(&plans).Error
	return plans, err
}

// appendResult stores one result row and trims the history to the newest
// maxResultsPerPlan rows for the plan.
func appendResult(result *model.PoolScheduledTestResult) error {
	if err := db.GetDB().Create(result).Error; err != nil {
		return err
	}
	var stale []int64
	if err := db.GetDB().Model(&model.PoolScheduledTestResult{}).
		Where("test_id = ?", result.TestID).
		Order("id DESC").
		Offset(maxResultsPerPlan).
		Pluck("id", &stale).Error; err != nil {
		return err
	}
	if len(stale) == 0 {
		return nil
	}
	return db.GetDB().Where("id IN ?", stale).Delete(&model.PoolScheduledTestResult{}).Error
}
