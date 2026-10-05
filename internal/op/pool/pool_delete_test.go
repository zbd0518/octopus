package pool

import (
	"fmt"
	"testing"
	"time"

	"github.com/lingyuins/octopus/internal/db"
	"github.com/lingyuins/octopus/internal/model"
)

func TestDeletePoolRemovesOnlyItsScheduledResults(t *testing.T) {
	setupPoolTestDB(t)
	pools := make([]model.AccountPool, 2)
	plans := make([]model.PoolScheduledTest, 2)
	for i := range pools {
		pools[i].Name = fmt.Sprintf("delete-pool-%d-%d", time.Now().UnixNano(), i)
		if err := CreatePool(&pools[i]); err != nil {
			t.Fatal(err)
		}
		plans[i] = model.PoolScheduledTest{PoolID: pools[i].ID, CronExpr: "*/30 * * * *"}
		if err := db.GetDB().Create(&plans[i]).Error; err != nil {
			t.Fatal(err)
		}
		result := model.PoolScheduledTestResult{TestID: plans[i].ID, Success: true}
		if err := db.GetDB().Create(&result).Error; err != nil {
			t.Fatal(err)
		}
	}
	for i := range pools {
		i := i
		t.Cleanup(func() {
			if err := DeletePool(pools[i].ID); err != nil {
				t.Errorf("cleanup pool: %v", err)
			}
		})
	}
	if err := DeletePool(pools[0].ID); err != nil {
		t.Fatalf("delete pool: %v", err)
	}
	for i, want := range []int64{0, 1} {
		var count int64
		if err := db.GetDB().Model(&model.PoolScheduledTestResult{}).Where("test_id = ?", plans[i].ID).Count(&count).Error; err != nil {
			t.Fatal(err)
		}
		if count != want {
			t.Fatalf("plan %d result count = %d, want %d", i, count, want)
		}
	}
	if _, err := GetPool(pools[0].ID); err == nil {
		t.Fatal("deleted pool still exists")
	}
}

func TestDeleteEmptyPool(t *testing.T) {
	setupPoolTestDB(t)
	p := &model.AccountPool{Name: "delete-empty-pool"}
	if err := CreatePool(p); err != nil {
		t.Fatal(err)
	}
	if err := DeletePool(p.ID); err != nil {
		t.Fatalf("delete empty pool: %v", err)
	}
}
