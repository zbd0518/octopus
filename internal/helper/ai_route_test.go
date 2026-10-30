package helper

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/lingyuins/octopus/internal/apperror"
	"github.com/lingyuins/octopus/internal/model"
	"github.com/lingyuins/octopus/internal/op/airoute"
)

func snapshotAIRouteProgressEntries() map[string]aiRouteProgressEntry {
	snapshot := make(map[string]aiRouteProgressEntry)
	aiRouteProgress.Range(func(key, value any) bool {
		id, ok := key.(string)
		if !ok {
			return true
		}
		entry, ok := value.(aiRouteProgressEntry)
		if !ok {
			return true
		}
		snapshot[id] = entry
		return true
	})
	return snapshot
}

func restoreAIRouteProgressEntries(snapshot map[string]aiRouteProgressEntry) {
	aiRouteProgress = sync.Map{}
	for id, entry := range snapshot {
		aiRouteProgress.Store(id, entry)
	}
}

func TestStartGenerateAIRoutePersistsAndReusesCategoryStrategy(t *testing.T) {
	t.Setenv("OCTOPUS_DEV_MOCK_SUCCESS", "false")
	setupHelperDB(t)
	originalProgress := snapshotAIRouteProgressEntries()
	aiRouteProgress = sync.Map{}
	t.Cleanup(func() { restoreAIRouteProgressEntries(originalProgress) })

	req := model.GenerateAIRouteRequest{Scope: model.AIRouteScopeTable, CategoryStrategy: model.AIRouteCategoryChannelTier}
	created, err := StartGenerateAIRoute(req)
	if err != nil {
		t.Fatal(err)
	}
	if created.CategoryStrategy != req.CategoryStrategy {
		t.Fatalf("created strategy = %q, want %q", created.CategoryStrategy, req.CategoryStrategy)
	}
	assertPersistedAIRouteStrategy(t, created.ID, req.CategoryStrategy)
	deadline := time.Now().Add(5 * time.Second)
	for {
		persisted, getErr := airoute.AIRouteTaskGet(context.Background(), created.ID)
		if getErr != nil {
			t.Fatal(getErr)
		}
		if persisted != nil && persisted.Done {
			if persisted.CategoryStrategy != req.CategoryStrategy {
				t.Fatalf("completed real task strategy = %q, want %q", persisted.CategoryStrategy, req.CategoryStrategy)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("real non-mock task did not reach a persisted terminal state")
		}
		time.Sleep(10 * time.Millisecond)
	}

	active := model.GenerateAIRouteProgress{
		ID: "active-category-task", Scope: model.AIRouteScopeTable,
		CategoryStrategy: model.AIRouteCategoryVendor, Status: model.AIRouteTaskStatusRunning,
	}
	if err := airoute.AIRouteTaskCreate(context.Background(), active); err != nil {
		t.Fatal(err)
	}
	activeRequest := model.GenerateAIRouteRequest{Scope: model.AIRouteScopeTable, CategoryStrategy: model.AIRouteCategoryVendor}
	reused, err := StartGenerateAIRoute(activeRequest)
	if err != nil || reused.ID != active.ID {
		t.Fatalf("reused = %+v, err = %v, want task %q", reused, err, active.ID)
	}
	conflict := activeRequest
	conflict.CategoryStrategy = model.AIRouteCategoryChannelTier
	if _, err = StartGenerateAIRoute(conflict); apperror.Status(err) != 409 || apperror.Code(err) != "group.aiRoute.categoryStrategy.conflict" {
		t.Fatalf("conflict error = %v, want translatable HTTP 409", err)
	}

	progress := cloneAIRouteProgress(created)
	mergeAIRouteProgressSnapshot(&progress, model.GenerateAIRouteProgress{
		Scope: req.Scope, Status: model.AIRouteTaskStatusRunning, CurrentStep: model.AIRouteTaskStepAnalyzingBatches,
	})
	if progress.CategoryStrategy != req.CategoryStrategy {
		t.Fatalf("snapshot merge dropped strategy: %q", progress.CategoryStrategy)
	}
	markAIRouteProgressHeartbeat(&progress)
	finalizeAIRouteProgress(&progress, nil, errors.New("controlled test completion"), nil)
	if err := persistAIRouteProgress(&progress, false); err != nil {
		t.Fatal(err)
	}
	assertPersistedAIRouteStrategy(t, created.ID, req.CategoryStrategy)
}

func assertPersistedAIRouteStrategy(t *testing.T, id string, want model.AIRouteCategoryStrategy) {
	t.Helper()
	progress, err := airoute.AIRouteTaskGet(context.Background(), id)
	if err != nil || progress == nil || progress.CategoryStrategy != want {
		t.Fatalf("persisted progress = %+v, err = %v, want strategy %q", progress, err, want)
	}
}

func TestGetGenerateAIRouteProgressExpiresDoneEntries(t *testing.T) {
	originalNow := aiRouteProgressNow
	originalProgress := snapshotAIRouteProgressEntries()
	defer func() {
		aiRouteProgressNow = originalNow
		restoreAIRouteProgressEntries(originalProgress)
	}()

	aiRouteProgress = sync.Map{}
	now := time.Date(2026, 4, 18, 12, 0, 0, 0, time.UTC)
	aiRouteProgressNow = func() time.Time { return now }

	storeAIRouteProgress(&model.GenerateAIRouteProgress{
		ID:   "done-progress",
		Done: true,
	})

	if _, ok := GetGenerateAIRouteProgress("done-progress"); !ok {
		t.Fatal("GetGenerateAIRouteProgress() ok = false, want true before expiry")
	}

	now = now.Add(aiRouteProgressDoneTTL + time.Second)
	if _, ok := GetGenerateAIRouteProgress("done-progress"); ok {
		t.Fatal("GetGenerateAIRouteProgress() ok = true, want false after expiry")
	}
}

func TestFinalizeAIRouteProgressPreservesResultOnPartialFailure(t *testing.T) {
	now := time.Date(2026, 4, 18, 12, 0, 0, 0, time.UTC)
	originalNow := aiRouteProgressNow
	defer func() {
		aiRouteProgressNow = originalNow
	}()
	aiRouteProgressNow = func() time.Time { return now }

	progress := &model.GenerateAIRouteProgress{
		Status:          model.AIRouteTaskStatusRunning,
		CurrentStep:     model.AIRouteTaskStepWritingGroups,
		ProgressPercent: 96,
	}
	result := &model.GenerateAIRouteResult{
		Scope:      model.AIRouteScopeTable,
		GroupCount: 2,
		RouteCount: 4,
		ItemCount:  9,
	}

	finalizeAIRouteProgress(progress, result, &airoute.AIRoutePartialFailureError{
		Message: "AI 路由部分失败，但已保留成功写入的 2 个分组",
		Cause:   errors.New("第 2/3 批 AI 分析失败"),
	}, nil)

	if progress.Status != model.AIRouteTaskStatusFailed {
		t.Fatalf("finalizeAIRouteProgress() status = %q, want failed", progress.Status)
	}
	if !progress.ResultReady {
		t.Fatal("finalizeAIRouteProgress() result_ready = false, want true")
	}
	if progress.Result == nil || progress.Result.GroupCount != 2 {
		t.Fatalf("finalizeAIRouteProgress() result = %+v, want preserved result", progress.Result)
	}
	if progress.Message != "AI 路由部分失败，但已保留成功写入的 2 个分组" {
		t.Fatalf("finalizeAIRouteProgress() message = %q, want preserved partial failure message", progress.Message)
	}
}
