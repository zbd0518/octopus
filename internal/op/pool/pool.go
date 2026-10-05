package pool

import (
	"errors"
	"time"

	"github.com/lingyuins/octopus/internal/db"
	"github.com/lingyuins/octopus/internal/model"
	"gorm.io/gorm"
)

// OnPoolDeletedHooks 池删除时的清理钩子（由 relay 层注入）。
var OnPoolDeletedHooks []func(poolID int)

// OnPoolAccountDeletedHooks 账号删除时的清理钩子。
var OnPoolAccountDeletedHooks []func(poolID, accountID int)

// --- Pool CRUD ---

func ListPools() ([]model.AccountPool, error) {
	var pools []model.AccountPool
	err := db.GetDB().Order("id").Find(&pools).Error
	return pools, err
}

func GetPool(id int) (*model.AccountPool, error) {
	var pool model.AccountPool
	if err := db.GetDB().First(&pool, id).Error; err != nil {
		return nil, err
	}
	return &pool, nil
}

func CreatePool(pool *model.AccountPool) error {
	if pool.Name == "" {
		return errors.New("pool name is required")
	}
	if pool.Strategy == "" {
		pool.Strategy = "ewma"
	}
	switch pool.Strategy {
	case "ewma", "round_robin", "random", "least_loaded":
	default:
		return errors.New("unsupported pool strategy")
	}
	if pool.DefaultConcurrency <= 0 {
		pool.DefaultConcurrency = 1
	}
	if pool.CooldownBaseSec <= 0 {
		pool.CooldownBaseSec = 300
	}
	return db.GetDB().Create(pool).Error
}

func UpdatePool(id int, updates map[string]interface{}) error {
	result := db.GetDB().Model(&model.AccountPool{}).Where("id = ?", id).Updates(updates)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

func DeletePool(id int) error {
	tx := db.GetDB().Begin()
	// 删除池内所有账号。
	if err := tx.Where("pool_id = ?", id).Delete(&model.PoolAccount{}).Error; err != nil {
		tx.Rollback()
		return err
	}
	// Delete the pool's scheduled test plans and their results (B4-#11) so
	// orphaned plans cannot keep firing.
	if err := tx.Where("pool_id = ?", id).Delete(&model.PoolScheduledTest{}).Error; err != nil {
		tx.Rollback()
		return err
	}
	if err := tx.Where("pool_id = ?", id).Delete(&model.PoolScheduledTestResult{}).Error; err != nil {
		tx.Rollback()
		return err
	}
	// 解除渠道关联。
	if err := tx.Model(&model.Channel{}).Where("pool_id = ?", id).Update("pool_id", 0).Error; err != nil {
		tx.Rollback()
		return err
	}
	// 删除池。
	if err := tx.Delete(&model.AccountPool{}, id).Error; err != nil {
		tx.Rollback()
		return err
	}
	if err := tx.Commit().Error; err != nil {
		return err
	}
	for _, hook := range OnPoolDeletedHooks {
		hook(id)
	}
	return nil
}

// --- Account CRUD ---

func ListAccounts(poolID int) ([]model.PoolAccount, error) {
	var accounts []model.PoolAccount
	err := db.GetDB().Where("pool_id = ?", poolID).Order("priority DESC, id").Find(&accounts).Error
	return accounts, err
}

func GetAccount(poolID, accountID int) (*model.PoolAccount, error) {
	var account model.PoolAccount
	if err := db.GetDB().Where("pool_id = ? AND id = ?", poolID, accountID).First(&account).Error; err != nil {
		return nil, err
	}
	return &account, nil
}

func CreateAccount(account *model.PoolAccount) error {
	if account.PoolID <= 0 {
		return errors.New("pool_id is required")
	}
	if account.Status == "" {
		account.Status = "active"
	}
	return db.GetDB().Create(account).Error
}

func UpdateAccount(poolID, accountID int, updates map[string]interface{}) error {
	result := db.GetDB().Model(&model.PoolAccount{}).
		Where("pool_id = ? AND id = ?", poolID, accountID).
		Updates(updates)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

func DeleteAccounts(poolID int, accountIDs []int) (int, error) {
	if len(accountIDs) == 0 {
		return 0, nil
	}
	result := db.GetDB().Where("pool_id = ? AND id IN ?", poolID, accountIDs).Delete(&model.PoolAccount{})
	if result.Error != nil {
		return 0, result.Error
	}
	for _, accountID := range accountIDs {
		for _, hook := range OnPoolAccountDeletedHooks {
			hook(poolID, accountID)
		}
	}
	return int(result.RowsAffected), nil
}

func ClearAccounts(poolID int) (int, error) {
	accounts, err := ListAccounts(poolID)
	if err != nil {
		return 0, err
	}
	ids := make([]int, 0, len(accounts))
	for _, account := range accounts {
		ids = append(ids, account.ID)
	}
	return DeleteAccounts(poolID, ids)
}

// UpdateAccountCredentialsIfUnchanged performs a compare-and-set update of a
// pool account: the update is applied only when the stored credentials blob
// still equals expectedOld. The comparison happens on the ciphertext (the DB
// storage form), never on the decrypted struct — serializing a decrypted
// credential back to JSON is not byte-stable, so a plaintext comparison would
// produce false CAS negatives.
//
// Returns false (with a nil error) when the condition did not match, which
// covers both "a concurrent writer already rotated the credentials" and "the
// account row no longer exists". Callers that lose the race must discard their
// write and re-read — the newer credential wins. Intended for the token
// refresh write-back path (B2-#2) so a stale refresh_token can never overwrite
// a freshly rotated one.
func UpdateAccountCredentialsIfUnchanged(poolID, accountID int, expectedOld string, updates map[string]interface{}) (bool, error) {
	result := db.GetDB().Model(&model.PoolAccount{}).
		Where("pool_id = ? AND id = ? AND credentials = ?", poolID, accountID, expectedOld).
		Updates(updates)
	if result.Error != nil {
		return false, result.Error
	}
	return result.RowsAffected > 0, nil
}

// ClearTempUnschedIfTrigger atomically clears the temporary unschedulable flag
// only when temp_unsched_reason still carries the given trigger tag (exact
// substring match on the JSON "trigger" field, implemented as a single UPDATE
// with no read-check-clear race). RowsAffected==0 means the block is currently
// held by a concurrent source (401 refresh window / 403 cooldown / manual admin
// block) and is left untouched. Returns whether a clear happened.
func ClearTempUnschedIfTrigger(poolID, accountID int, trigger string) (bool, error) {
	if trigger == "" {
		return false, nil
	}
	pattern := `%"trigger":"` + trigger + `"%`
	result := db.GetDB().Model(&model.PoolAccount{}).
		Where("pool_id = ? AND id = ? AND temp_unsched_reason LIKE ?", poolID, accountID, pattern).
		Updates(map[string]interface{}{
			"temp_unsched_until":  int64(0),
			"temp_unsched_reason": "",
		})
	if result.Error != nil {
		return false, result.Error
	}
	return result.RowsAffected > 0, nil
}

// ClearAuthErrorMirrorIfNotNewer conditionally zeroes the account's auth-error
// DB mirror columns (auth_error_count / auth_error_window_start): only when
// the stored evidence is not newer than the snapshot (count <= snapshotCount
// AND window_start <= snapshotWindowStart), as a single atomic UPDATE.
// RowsAffected==0 means the DB holds evidence newer than the snapshot and it
// is left untouched. Prevents an asynchronously delayed success report from
// erasing 401/403 evidence produced after its snapshot (B1-#7).
func ClearAuthErrorMirrorIfNotNewer(poolID, accountID int, snapshotCount int, snapshotWindowStart int64) error {
	result := db.GetDB().Model(&model.PoolAccount{}).
		Where("pool_id = ? AND id = ? AND auth_error_count <= ? AND auth_error_window_start <= ?",
			poolID, accountID, snapshotCount, snapshotWindowStart).
		Updates(map[string]interface{}{
			"auth_error_count":        0,
			"auth_error_window_start": int64(0),
		})
	return result.Error
}

// AcquireTempUnschedIfFree atomically sets a temporary scheduling block only
// when the account is not currently blocked: the single UPDATE matches rows
// whose temp_unsched_until is in the past (expired or never set), so a block
// created concurrently by another source — 401 window / 403 cooldown / manual
// flag — between the caller's account snapshot and this call is never
// overwritten. Returns true when this call acquired the block (the caller owns
// the cleanup); false means an active block already holds the account.
// Time comparison happens in the Go-passed parameter (unix seconds), keeping
// the predicate portable across SQLite / MySQL / PostgreSQL.
func AcquireTempUnschedIfFree(poolID, accountID int, until time.Time, reason string) (bool, error) {
	result := db.GetDB().Model(&model.PoolAccount{}).
		Where("pool_id = ? AND id = ? AND (temp_unsched_until IS NULL OR temp_unsched_until <= ?)",
			poolID, accountID, time.Now().Unix()).
		Updates(map[string]interface{}{
			"temp_unsched_until":  until.Unix(),
			"temp_unsched_reason": reason,
		})
	if result.Error != nil {
		return false, result.Error
	}
	return result.RowsAffected > 0, nil
}

func DeleteAccount(poolID, accountID int) error {
	result := db.GetDB().Where("pool_id = ? AND id = ?", poolID, accountID).Delete(&model.PoolAccount{})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	// Delete scheduled test plans scoped to this account plus their results (B4-#11).
	var planIDs []int
	if err := db.GetDB().Model(&model.PoolScheduledTest{}).
		Where("pool_id = ? AND account_id = ?", poolID, accountID).
		Pluck("id", &planIDs).Error; err != nil {
		return err
	}
	if len(planIDs) > 0 {
		if err := db.GetDB().Where("id IN ?", planIDs).Delete(&model.PoolScheduledTest{}).Error; err != nil {
			return err
		}
		if err := db.GetDB().Where("test_id IN ?", planIDs).Delete(&model.PoolScheduledTestResult{}).Error; err != nil {
			return err
		}
	}
	for _, hook := range OnPoolAccountDeletedHooks {
		hook(poolID, accountID)
	}
	return nil
}

// ListSchedulableAccounts 返回指定池中当前可调度的账号列表。
func ListSchedulableAccounts(poolID int) ([]model.PoolAccount, error) {
	var accounts []model.PoolAccount
	err := db.GetDB().
		Where("pool_id = ? AND status = 'active' AND schedulable = ?", poolID, true).
		Order("priority DESC, id").
		Find(&accounts).Error
	if err != nil {
		return nil, err
	}
	// 在 Go 层过滤冷却（时间戳比较跨方言更可靠）。
	result := make([]model.PoolAccount, 0, len(accounts))
	for i := range accounts {
		if accounts[i].IsSchedulable() {
			result = append(result, accounts[i])
		}
	}
	return result, nil
}

// --- Account Test / Import ---

// AccountTestResult 账号连通性测试结果。
type AccountTestResult struct {
	Success bool   `json:"success"`
	Status  int    `json:"status"`
	Latency int64  `json:"latency_ms"`
	Error   string `json:"error,omitempty"`
}

// ImportAccounts 批量导入账号。已在调用方完成解析与凭据加密。
func ImportAccounts(accounts []model.PoolAccount) error {
	if len(accounts) == 0 {
		return nil
	}
	for i := range accounts {
		if accounts[i].PoolID <= 0 {
			return errors.New("pool_id is required")
		}
		if accounts[i].Status == "" {
			accounts[i].Status = "active"
		}
	}
	return db.GetDB().Create(&accounts).Error
}

// ListAllAccounts 返回所有池的所有账号（供后台刷新/额度同步任务遍历）。
func ListAllAccounts() ([]model.PoolAccount, error) {
	var accounts []model.PoolAccount
	err := db.GetDB().Order("id").Find(&accounts).Error
	return accounts, err
}
