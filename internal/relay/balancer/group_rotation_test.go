package balancer

import (
	"testing"

	"github.com/lingyuins/octopus/internal/model"
)

func TestGroupRotationIndependentAndPrecheckDoesNotAdvance(t *testing.T) {
	for _, mode := range []model.GroupMode{model.GroupModeRoundRobin, model.GroupModeWeighted} {
		first := model.Group{ID: 919801 + int(mode), Mode: mode, Items: []model.GroupItem{{ChannelID: 1, Weight: 1}, {ChannelID: 2, Weight: 1}}}
		second := first
		second.ID += 100
		counts := map[int]int{}
		for range 10 {
			if !HasCandidates(first, nil) {
				t.Fatal("missing candidates")
			}
			iter := NewIterator(first, 0, "m", nil)
			iter.Next()
			counts[iter.Item().ChannelID]++
			NewIterator(second, 0, "m", nil)
		}
		if counts[1] != 5 || counts[2] != 5 {
			t.Fatalf("mode %v distribution %v", mode, counts)
		}
	}
}

func TestGroupRotationWeightedRatio(t *testing.T) {
	group := model.Group{ID: 919900, Mode: model.GroupModeWeighted, Items: []model.GroupItem{{ChannelID: 1, Weight: 3}, {ChannelID: 2, Weight: 1}}}
	counts := map[int]int{}
	for range 40 {
		iter := NewIterator(group, 0, "m", nil)
		iter.Next()
		counts[iter.Item().ChannelID]++
	}
	if counts[1] != 30 || counts[2] != 10 {
		t.Fatal(counts)
	}
}
