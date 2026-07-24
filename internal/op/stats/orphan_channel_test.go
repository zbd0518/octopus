package stats

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/lingyuins/octopus/internal/db"
	"github.com/lingyuins/octopus/internal/model"
	"github.com/lingyuins/octopus/internal/transformer/outbound"
)

func setupOrphanChannelTestDB(t *testing.T) {
	t.Helper()
	testName := strings.NewReplacer("/", "-", "\\", "-", " ", "-").Replace(t.Name())
	// 开启 foreign_keys，复现 MySQL Error 1452 同类约束。
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared&_pragma=foreign_keys(ON)", testName)
	if err := db.InitDB("sqlite", dsn, true); err != nil {
		t.Fatalf("init db: %v", err)
	}
	t.Cleanup(func() {
		_ = db.Close()
		ClearAllCachesForTest()
	})
	ClearAllCachesForTest()
}

func seedChannel(t *testing.T, id int, name string) {
	t.Helper()
	ch := model.Channel{
		ID:       id,
		Name:     name,
		Type:     outbound.OutboundTypeOpenAIChat,
		Enabled:  true,
		BaseUrls: []model.BaseUrl{{URL: "https://example.com"}},
	}
	if err := db.GetDB().Create(&ch).Error; err != nil {
		t.Fatalf("create channel %d: %v", id, err)
	}
}

// TestSaveDBDropsOrphanChannelDirty 回归：已删除 channel 的 dirty stats 不得撞 FK，
// 且不得 requeue 毒死后续 SaveDB 周期。
func TestSaveDBDropsOrphanChannelDirty(t *testing.T) {
	setupOrphanChannelTestDB(t)

	const liveID = 11
	const orphanID = 121
	seedChannel(t, liveID, "live-channel")

	// 模拟：orphan 已从 channels 删除，但 stats 内存仍 dirty（竞态/在途请求）。
	// 不调用 OnChannelDeleted，专门覆盖"进程内尚未 tombstone"路径。
	ResetCachesForTest(
		model.StatsTotal{ID: 1},
		model.StatsDaily{Date: time.Now().Format("20060102")},
		0, 0, 0,
	)
	if err := ChannelUpdate(liveID, model.StatsMetrics{RequestSuccess: 3, InputToken: 30}); err != nil {
		t.Fatalf("ChannelUpdate live: %v", err)
	}
	// 直接写 cache/dirty，绕过 tombstone（模拟删除前 dirty、删除后 SaveDB 才跑）。
	channelCache.Set(orphanID, model.StatsChannel{
		ChannelID:    orphanID,
		StatsMetrics: model.StatsMetrics{RequestSuccess: 9, InputToken: 90},
	})
	channelCacheNeedUpdateLock.Lock()
	channelCacheNeedUpdate[orphanID] = struct{}{}
	channelCacheNeedUpdateLock.Unlock()

	if err := SaveDB(context.Background()); err != nil {
		t.Fatalf("SaveDB with orphan dirty: %v", err)
	}

	// live 渠道 stats 应落盘成功。
	var liveStats model.StatsChannel
	if err := db.GetDB().Where("channel_id = ?", liveID).First(&liveStats).Error; err != nil {
		t.Fatalf("load live stats: %v", err)
	}
	if liveStats.RequestSuccess != 3 {
		t.Fatalf("live RequestSuccess = %d, want 3", liveStats.RequestSuccess)
	}

	// orphan 不得写入 stats_channels。
	var orphanCount int64
	if err := db.GetDB().Model(&model.StatsChannel{}).Where("channel_id = ?", orphanID).Count(&orphanCount).Error; err != nil {
		t.Fatalf("count orphan stats: %v", err)
	}
	if orphanCount != 0 {
		t.Fatalf("orphan stats rows = %d, want 0", orphanCount)
	}

	// orphan 不得仍在 dirty 集合中。
	for _, id := range GetChannelDirtyIDs() {
		if id == orphanID {
			t.Fatalf("orphan channel %d still dirty after SaveDB", orphanID)
		}
	}

	// 再次 SaveDB 仍应成功（无 requeue 死循环）。
	if err := SaveDB(context.Background()); err != nil {
		t.Fatalf("second SaveDB: %v", err)
	}
}

// TestOnChannelDeletedBlocksLateChannelUpdate 删除后迟到的 ChannelUpdate 不得重新 dirty。
func TestOnChannelDeletedBlocksLateChannelUpdate(t *testing.T) {
	setupOrphanChannelTestDB(t)

	const channelID = 42
	seedChannel(t, channelID, "to-delete")

	if err := ChannelUpdate(channelID, model.StatsMetrics{RequestSuccess: 1}); err != nil {
		t.Fatalf("ChannelUpdate before delete: %v", err)
	}
	OnChannelDeleted(channelID)

	if err := ChannelUpdate(channelID, model.StatsMetrics{RequestSuccess: 5}); err != nil {
		t.Fatalf("late ChannelUpdate: %v", err)
	}
	// ChannelGet 也不得重新创建 dirty 空条目。
	got := ChannelGet(channelID)
	if got.RequestSuccess != 0 {
		t.Fatalf("ChannelGet after delete RequestSuccess = %d, want 0", got.RequestSuccess)
	}

	for _, id := range GetChannelDirtyIDs() {
		if id == channelID {
			t.Fatalf("deleted channel %d re-dirtied by late update/get", channelID)
		}
	}

	if err := SaveDB(context.Background()); err != nil {
		t.Fatalf("SaveDB after late update: %v", err)
	}
}

// TestFilterLiveChannelIDsTombstonesMissing 覆盖 filter 直接丢弃 DB 中不存在的 ID。
func TestFilterLiveChannelIDsTombstonesMissing(t *testing.T) {
	setupOrphanChannelTestDB(t)

	seedChannel(t, 1, "c1")
	// 2 不存在于 channels。

	live := filterLiveChannelIDs(context.Background(), []int{1, 2, 0})
	if len(live) != 1 || live[0] != 1 {
		t.Fatalf("filterLiveChannelIDs = %v, want [1]", live)
	}
	if !isChannelDeleted(2) {
		t.Fatal("missing channel 2 was not tombstoned")
	}
}
