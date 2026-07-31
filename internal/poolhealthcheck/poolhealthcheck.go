// Package poolhealthcheck 号池账号健康巡检：
// 周期遍历所有 schedulable + active 账号，调用 op/pool.TestAccount 探测；
// 累计失败到阈值后 SetError，成功则 ClearTempUnsched 并清除错误状态。
package poolhealthcheck

import (
	"fmt"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/lingyuins/octopus/internal/model"
	"github.com/lingyuins/octopus/internal/op/pool"
	"github.com/lingyuins/octopus/internal/op/setting"
	"github.com/lingyuins/octopus/internal/relay/poolscheduler"
	"github.com/lingyuins/octopus/internal/utils/log"
)

// 巡检并发上限（与 pooltokenrefresh.refreshSem 同型）。
var probeSem = make(chan struct{}, 4)

// failCounter 累计账号连续失败次数：`poolID:accountID` -> int
var failCounter sync.Map

func failKey(poolID, accountID int) string {
	return fmt.Sprintf("%d:%d", poolID, accountID)
}

// Run 单次巡检（由 task 周期调用；幂等、并发安全）。
func Run() {
	enabled, err := setting.GetBool(model.SettingKeyPoolHealthCheckEnabled)
	if err != nil || !enabled {
		return
	}
	threshold, err := setting.GetInt(model.SettingKeyPoolHealthCheckFailThreshold)
	if err != nil || threshold < 1 {
		threshold = 3
	}

	accounts, err := pool.ListAllAccounts()
	if err != nil {
		log.Warnf("poolhealthcheck: list accounts failed: %v", err)
		return
	}
	var wg sync.WaitGroup
	for i := range accounts {
		a := &accounts[i]
		// 仅巡检可调度 + active；disabled/error 状态已由人工或调度器处理。
		if !a.Schedulable || a.Status != "active" {
			continue
		}
		modelName := firstBoundModel(a.Models)
		if modelName == "" {
			// 账号未绑定任何模型，跳过——没有具体模型无法发探测请求。
			continue
		}
		wg.Add(1)
		probeSem <- struct{}{}
		go func(poolID, accountID int, name string) {
			defer wg.Done()
			defer func() { <-probeSem }()
			probeOnce(poolID, accountID, name, threshold)
		}(a.PoolID, a.ID, modelName)
	}
	wg.Wait()
}

// probeOnce 对单个账号执行一次探测，按结果更新状态机。
func probeOnce(poolID, accountID int, modelName string, threshold int) {
	res, err := pool.TestAccount(poolID, accountID, modelName)
	if err != nil {
		markFailure(poolID, accountID, threshold, err.Error())
		return
	}
	if res == nil || !res.Success {
		msg := "unknown"
		if res != nil && res.Error != "" {
			msg = res.Error
		}
		markFailure(poolID, accountID, threshold, msg)
		return
	}
	// 成功：清空连续失败计数 + 清除错误状态 + 恢复可调度。
	failCounter.Delete(failKey(poolID, accountID))
	_ = poolscheduler.RecoverAccount(poolID, accountID)
}

// markFailure 累计连续失败次数；达阈值后 SetError。
func markFailure(poolID, accountID int, threshold int, msg string) {
	key := failKey(poolID, accountID)
	val, _ := failCounter.LoadOrStore(key, new(int64))
	cnt := val.(*int64)
	current := int(atomic.AddInt64(cnt, 1))
	if current >= threshold {
		failCounter.Delete(key)
		poolscheduler.SetError(poolID, accountID)
		_ = pool.UpdateAccount(poolID, accountID, map[string]interface{}{
			"error_message": fmt.Sprintf("health check failed %d times: %s", current, msg),
		})
		return
	}
	_ = pool.UpdateAccount(poolID, accountID, map[string]interface{}{
		"error_message": fmt.Sprintf("health check failed (%d/%d): %s", current, threshold, msg),
	})
}

// firstBoundModel 返回 Models CSV 中第一个非空项；空 CSV 返回空串。
func firstBoundModel(csv string) string {
	for _, m := range strings.Split(csv, ",") {
		if trimmed := strings.TrimSpace(m); trimmed != "" {
			return trimmed
		}
	}
	return ""
}
