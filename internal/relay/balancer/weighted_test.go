package balancer

import (
	"testing"

	"github.com/lingyuins/octopus/internal/model"
)

// TestWeightedCandidatesSmoothWRRFrequency 统计 Smooth WRR 多轮调用后每个渠道的
// 出现频率，权重 5/1/1 应近似 5/7、1/7、1/7（±2 百分点容差）。
// 验证 B1：高权重渠道不再吃满前 N 个位置垄断流量，低权重渠道按权重比例分到流量。
func TestWeightedCandidatesSmoothWRRFrequency(t *testing.T) {
	items := []model.GroupItem{
		{ChannelID: 1, ModelName: "gpt-4", Weight: 5, Priority: 1},
		{ChannelID: 2, ModelName: "gpt-4", Weight: 1, Priority: 2},
		{ChannelID: 3, ModelName: "gpt-4", Weight: 1, Priority: 3},
	}

	// 每轮 Candidates 返回完整展开序列（长度 = 权重和 7），
	// 统计首元素（relay 顺序消费时第一个被命中的渠道）即可。
	const rounds = 7000
	counts := map[int]int{}
	b := &Weighted{}
	for i := 0; i < rounds; i++ {
		got := b.Candidates(items)
		if len(got) == 0 {
			t.Fatalf("round %d: Candidates() returned empty", i)
		}
		counts[got[0].ChannelID]++
	}

	total := 5 + 1 + 1
	want := map[int]float64{1: 5.0 / float64(total), 2: 1.0 / 7.0, 3: 1.0 / 7.0}
	const tolerance = 0.02
	for chID, w := range want {
		gotFreq := float64(counts[chID]) / float64(rounds)
		if gotFreq < w-tolerance || gotFreq > w+tolerance {
			t.Fatalf("channel %d frequency = %.4f, want %.4f±%.2f (counts=%d)", chID, gotFreq, w, tolerance, counts[chID])
		}
	}
}

// TestWeightedCandidatesSequenceExpansion 验证展开序列的结构性质：
// 长度 = 权重和；每个渠道出现次数 = 自身权重；高权重渠道穿插分布（不连续霸屏）。
func TestWeightedCandidatesSequenceExpansion(t *testing.T) {
	items := []model.GroupItem{
		{ChannelID: 1, ModelName: "gpt-4", Weight: 5, Priority: 1},
		{ChannelID: 2, ModelName: "gpt-4", Weight: 1, Priority: 2},
		{ChannelID: 3, ModelName: "gpt-4", Weight: 1, Priority: 3},
	}

	got := (&Weighted{}).Candidates(items)
	if len(got) != 7 {
		t.Fatalf("Candidates() len = %d, want 7 (sum of weights)", len(got))
	}

	counts := map[int]int{}
	for _, item := range got {
		counts[item.ChannelID]++
	}
	if counts[1] != 5 || counts[2] != 1 || counts[3] != 1 {
		t.Fatalf("counts = %v, want {1:5, 2:1, 3:1}", counts)
	}

	// 平滑性（环形语义）：Candidates 返回从轮转 offset 开始的完整周期窗口。
	// 权重 5/7 的渠道在 7 长度环形序列上，由鸽笼原理必然存在一处 3 连
	// （A A B A C A A 环形中 6,0,1 相邻）；线性视角最大连续 2。霸屏的判定
	// 语义是"长时间连续"，3 连在 5/7 占比下不可消除，故断言上限 3。
	maxRun := 0
	run := 0
	for _, item := range got {
		if item.ChannelID == 1 {
			run++
			if run > maxRun {
				maxRun = run
			}
		} else {
			run = 0
		}
	}
	if maxRun > 3 {
		t.Fatalf("channel 1 max consecutive run = %d, want <= 3 (pigeonhole bound for 5/7 on a ring)", maxRun)
	}
}

// TestWeightedCandidatesZeroWeightAsOne 权重 0/负数按 1 处理：
// 3 个渠道权重 0/0/0 → 展开序列长度 3，各出现 1 次，退化为轮询。
func TestWeightedCandidatesZeroWeightAsOne(t *testing.T) {
	items := []model.GroupItem{
		{ChannelID: 1, ModelName: "gpt-4", Weight: 0, Priority: 1},
		{ChannelID: 2, ModelName: "gpt-4", Weight: -3, Priority: 2},
		{ChannelID: 3, ModelName: "gpt-4", Weight: 0, Priority: 3},
	}

	got := (&Weighted{}).Candidates(items)
	if len(got) != 3 {
		t.Fatalf("Candidates() len = %d, want 3", len(got))
	}
	counts := map[int]int{}
	for _, item := range got {
		counts[item.ChannelID]++
	}
	for chID := 1; chID <= 3; chID++ {
		if counts[chID] != 1 {
			t.Fatalf("channel %d count = %d, want 1", chID, counts[chID])
		}
	}
}

// TestWeightedCandidatesSingleItem 单渠道：序列长度 = 权重，全部是该渠道。
func TestWeightedCandidatesSingleItem(t *testing.T) {
	items := []model.GroupItem{
		{ChannelID: 42, ModelName: "gpt-4", Weight: 5, Priority: 1},
	}

	got := (&Weighted{}).Candidates(items)
	if len(got) != 5 {
		t.Fatalf("Candidates() len = %d, want 5", len(got))
	}
	for i, item := range got {
		if item.ChannelID != 42 {
			t.Fatalf("result[%d].ChannelID = %d, want 42", i, item.ChannelID)
		}
	}
}

// TestWeightedCandidatesEmpty 空列表返回 nil。
func TestWeightedCandidatesEmpty(t *testing.T) {
	got := (&Weighted{}).Candidates(nil)
	if got != nil {
		t.Fatalf("Candidates(nil) = %v, want nil", got)
	}
}

// TestSmoothWRRSequenceHugeWeightsCompressed 权重和超过 maxWeightedSequenceLen
// 时等比例压缩：20 渠道 × weight 100（sum=2000）→ 压缩后 sum <= 1000，
// 每渠道出现次数仍按比例（全部相等 → 各 50 或压缩后近似）。
func TestSmoothWRRSequenceHugeWeightsCompressed(t *testing.T) {
	items := make([]model.GroupItem, 20)
	for i := range items {
		items[i] = model.GroupItem{ChannelID: i + 1, ModelName: "gpt-4", Weight: 100, Priority: 1}
	}

	got := smoothWRRSequence(items)
	if len(got) > maxWeightedSequenceLen {
		t.Fatalf("sequence len = %d, want <= %d", len(got), maxWeightedSequenceLen)
	}

	counts := map[int]int{}
	for _, item := range got {
		counts[item.ChannelID]++
	}
	// 压缩后仍应等权：每渠道出现次数相差 <= 1。
	minC, maxC := -1, -1
	for _, c := range counts {
		if minC < 0 || c < minC {
			minC = c
		}
		if maxC < 0 || c > maxC {
			maxC = c
		}
	}
	if minC > 0 && maxC-minC > 1 {
		t.Fatalf("compressed counts spread = [%d, %d], want <= 1", minC, maxC)
	}
}
