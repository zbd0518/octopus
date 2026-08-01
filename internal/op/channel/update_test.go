package channel

import (
	"context"
	"testing"
	"time"

	"github.com/lingyuins/octopus/internal/db"
	"github.com/lingyuins/octopus/internal/model"
)

// advancedFields 保存 Update() 高级设置字段的期望值，用于回归断言。
type advancedFields struct {
	autoSync             bool
	skipModelTest        bool
	disposable           bool
	expireAt             time.Time
	notifChannelID       int
	keySelectionStrategy string
	autoGroup            model.AutoGroupType
	customHeader         []model.CustomHeader
	poolID               int
}

// advancedUpdateRequest 构造携带全部 9 个高级设置字段的更新请求（回归 #182）。
func advancedUpdateRequest(id int) (*model.ChannelUpdateRequest, advancedFields) {
	f := advancedFields{
		autoSync:             true,
		skipModelTest:        true,
		disposable:           true,
		expireAt:             time.Now().Add(24 * time.Hour).Truncate(time.Second),
		notifChannelID:       7,
		keySelectionStrategy: "random",
		autoGroup:            model.AutoGroupTypeExact,
		customHeader: []model.CustomHeader{
			{HeaderKey: "X-Example-Header", HeaderValue: "example-value"},
			{HeaderKey: "X-Second", HeaderValue: "second"},
		},
		poolID: 42,
	}
	req := &model.ChannelUpdateRequest{
		ID:                   id,
		AutoSync:             &f.autoSync,
		SkipModelTest:        &f.skipModelTest,
		Disposable:           &f.disposable,
		ExpireAt:             &f.expireAt,
		NotifChannelID:       &f.notifChannelID,
		KeySelectionStrategy: &f.keySelectionStrategy,
		AutoGroup:            &f.autoGroup,
		CustomHeader:         &f.customHeader,
		PoolID:               &f.poolID,
	}
	return req, f
}

// assertAdvancedFields 断言渠道上的 9 个高级设置字段与期望值一致。
func assertAdvancedFields(t *testing.T, ch *model.Channel, want advancedFields) {
	t.Helper()
	if !ch.AutoSync || !ch.SkipModelTest || !ch.Disposable {
		t.Fatalf("bool fields not persisted: auto_sync=%v skip_model_test=%v disposable=%v",
			ch.AutoSync, ch.SkipModelTest, ch.Disposable)
	}
	if ch.ExpireAt == nil || ch.ExpireAt.Unix() != want.expireAt.Unix() {
		t.Fatalf("ExpireAt = %v, want %v", ch.ExpireAt, want.expireAt)
	}
	if ch.NotifChannelID == nil || *ch.NotifChannelID != want.notifChannelID {
		t.Fatalf("NotifChannelID = %v, want %d", ch.NotifChannelID, want.notifChannelID)
	}
	if ch.KeySelectionStrategy != want.keySelectionStrategy {
		t.Fatalf("KeySelectionStrategy = %q, want %q", ch.KeySelectionStrategy, want.keySelectionStrategy)
	}
	if ch.AutoGroup != want.autoGroup {
		t.Fatalf("AutoGroup = %d, want %d", ch.AutoGroup, want.autoGroup)
	}
	if len(ch.CustomHeader) != len(want.customHeader) {
		t.Fatalf("CustomHeader = %+v, want %+v", ch.CustomHeader, want.customHeader)
	}
	for i := range want.customHeader {
		if ch.CustomHeader[i] != want.customHeader[i] {
			t.Fatalf("CustomHeader[%d] = %+v, want %+v", i, ch.CustomHeader[i], want.customHeader[i])
		}
	}
	if ch.PoolID != want.poolID {
		t.Fatalf("PoolID = %d, want %d", ch.PoolID, want.poolID)
	}
}

// TestUpdatePersistsAllAdvancedFields 回归测试 #182：
// POST /api/v1/channel/update 提交 9 个高级设置字段必须真正落库，
// 且刷新后的缓存（Update 返回值）与 DB 读回值一致。
func TestUpdatePersistsAllAdvancedFields(t *testing.T) {
	setupBatchGroupTest(t)
	seedChannel(t, 1, 1)

	req, want := advancedUpdateRequest(1)
	updated, err := Update(req, context.Background())
	if err != nil {
		t.Fatalf("Update returned error: %v", err)
	}
	assertAdvancedFields(t, updated, want)

	var got model.Channel
	if err := db.GetDB().First(&got, 1).Error; err != nil {
		t.Fatalf("reload from DB failed: %v", err)
	}
	assertAdvancedFields(t, &got, want)
}

// TestUpdatePatchSemanticsLeavesOtherFieldsUntouched 验证白名单补丁语义：
// 仅更新单个字段时，其余 9 个高级字段保持不变。
func TestUpdatePatchSemanticsLeavesOtherFieldsUntouched(t *testing.T) {
	setupBatchGroupTest(t)
	seedChannel(t, 1, 1)

	req, want := advancedUpdateRequest(1)
	if _, err := Update(req, context.Background()); err != nil {
		t.Fatalf("initial Update returned error: %v", err)
	}

	name := "renamed"
	if _, err := Update(&model.ChannelUpdateRequest{ID: 1, Name: &name}, context.Background()); err != nil {
		t.Fatalf("name-only Update returned error: %v", err)
	}

	var got model.Channel
	if err := db.GetDB().First(&got, 1).Error; err != nil {
		t.Fatalf("reload from DB failed: %v", err)
	}
	if got.Name != name {
		t.Fatalf("Name = %q, want %q", got.Name, name)
	}
	assertAdvancedFields(t, &got, want)
}
