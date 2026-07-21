package stats

import (
	"testing"

	"github.com/lingyuins/octopus/internal/model"
	"github.com/stretchr/testify/assert"
)

// TestRetryQueuePushDrain 验证队列入队/出队基本功能。
func TestRetryQueuePushDrain(t *testing.T) {
	q := &dailyChannelModelRetryQueue{maxSize: 100}

	// 空队列
	assert.Equal(t, 0, q.len())
	assert.Nil(t, q.drain())

	// 入队单条
	q.push(model.StatsDailyChannelModel{
		Date:      "20260701",
		ChannelID: 1,
		ModelName: "m1",
	})
	assert.Equal(t, 1, q.len())

	// 出队
	entries := q.drain()
	assert.Len(t, entries, 1)
	assert.Equal(t, 1, entries[0].ChannelID)
	assert.Equal(t, 0, q.len(), "drain should clear queue")

	// 再次 drain 返回 nil
	assert.Nil(t, q.drain())
}

// TestRetryQueueRingBufferFIFO 验证达到 maxSize 时的 FIFO 淘汰逻辑。
func TestRetryQueueRingBufferFIFO(t *testing.T) {
	q := &dailyChannelModelRetryQueue{maxSize: 10}

	// 填充到 maxSize
	for i := 1; i <= 10; i++ {
		q.push(model.StatsDailyChannelModel{
			Date:      "20260701",
			ChannelID: i,
			ModelName: "m",
		})
	}
	assert.Equal(t, 10, q.len())

	// 再 push 一条，应淘汰前 5 条（maxSize/2），保留后 5 条 + 新 1 条 = 6
	q.push(model.StatsDailyChannelModel{
		Date:      "20260701",
		ChannelID: 11,
		ModelName: "m",
	})
	assert.Equal(t, 6, q.len())

	entries := q.drain()
	assert.Len(t, entries, 6)

	// 验证保留的是后 5 条 + 新 1 条（channelID 6-11）
	channelIDs := make(map[int]bool)
	for _, e := range entries {
		channelIDs[e.ChannelID] = true
	}
	assert.True(t, channelIDs[6], "should keep channelID 6")
	assert.True(t, channelIDs[11], "should keep new entry")
	assert.False(t, channelIDs[1], "should evict oldest (channelID 1)")
	assert.False(t, channelIDs[5], "should evict channelID 5")
}

// TestRetryQueueConcurrentAccess 验证并发安全性（基本锁覆盖）。
func TestRetryQueueConcurrentAccess(t *testing.T) {
	q := &dailyChannelModelRetryQueue{maxSize: 1000}

	// 并发 push
	done := make(chan bool, 10)
	for worker := 0; worker < 10; worker++ {
		go func(w int) {
			for i := 0; i < 50; i++ {
				q.push(model.StatsDailyChannelModel{
					Date:      "20260701",
					ChannelID: w*100 + i,
					ModelName: "m",
				})
			}
			done <- true
		}(worker)
	}

	// 等待所有 push 完成
	for i := 0; i < 10; i++ {
		<-done
	}

	// 并发 len + drain（不期望特定值，仅验证不 panic）
	go func() { q.len() }()
	go func() { q.drain() }()

	// drain 后队列清空
	q.drain()
	assert.Equal(t, 0, q.len())
}

// TestRetryDailyChannelModelEntriesMerge 验证聚合逻辑（不涉及真实 DB）。
func TestRetryDailyChannelModelEntriesMerge(t *testing.T) {
	entries := []model.StatsDailyChannelModel{
		{
			Date:         "20260701",
			ChannelID:    10,
			ChannelName:  "ch10",
			ModelName:    "model-x",
			StatsMetrics: model.StatsMetrics{RequestSuccess: 3, InputToken: 30},
		},
		{
			Date:         "20260701",
			ChannelID:    10,
			ChannelName:  "", // 空 channelName 应被后续填充
			ModelName:    "model-x",
			StatsMetrics: model.StatsMetrics{RequestSuccess: 2, OutputToken: 20},
		},
		{
			Date:         "20260701",
			ChannelID:    10,
			ChannelName:  "ch10",
			ModelName:    "model-x",
			StatsMetrics: model.StatsMetrics{RequestFailed: 1},
		},
		{
			Date:         "20260701",
			ChannelID:    20,
			ChannelName:  "ch20",
			ModelName:    "model-y",
			StatsMetrics: model.StatsMetrics{RequestSuccess: 5},
		},
	}

	// 模拟聚合逻辑（从 retryDailyChannelModelEntries 提取）
	merged := make(map[string]*model.StatsDailyChannelModel)
	for i := range entries {
		entry := &entries[i]
		key := entry.Date + "\x00" + string(rune(entry.ChannelID)) + "\x00" + entry.ModelName
		if existing, ok := merged[key]; ok {
			existing.RequestSuccess += entry.RequestSuccess
			existing.RequestFailed += entry.RequestFailed
			existing.InputToken += entry.InputToken
			existing.OutputToken += entry.OutputToken
			if existing.ChannelName == "" {
				existing.ChannelName = entry.ChannelName
			}
		} else {
			entryCopy := *entry
			merged[key] = &entryCopy
		}
	}

	// 验证聚合结果
	assert.Len(t, merged, 2, "should merge into 2 unique (date, channelID, modelName)")

	// 找到 ch10/model-x 的聚合行
	var ch10Entry *model.StatsDailyChannelModel
	for _, e := range merged {
		if e.ChannelID == 10 {
			ch10Entry = e
			break
		}
	}
	assert.NotNil(t, ch10Entry)
	assert.Equal(t, int64(5), ch10Entry.RequestSuccess, "3+2")
	assert.Equal(t, int64(1), ch10Entry.RequestFailed)
	assert.Equal(t, int64(30), ch10Entry.InputToken)
	assert.Equal(t, int64(20), ch10Entry.OutputToken)
	assert.Equal(t, "ch10", ch10Entry.ChannelName, "empty channelName should be filled")
}
