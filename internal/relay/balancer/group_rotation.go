package balancer

import (
	"sync"
	"time"

	"github.com/lingyuins/octopus/internal/model"
)

type groupRotation struct {
	items []model.GroupItem
	next  uint64
	seen  time.Time
}

var groupRotations = struct {
	sync.Mutex
	entries map[int]*groupRotation
}{entries: make(map[int]*groupRotation)}

func HasCandidates(group model.Group, excluded map[int]struct{}) bool {
	for _, item := range group.Items {
		if _, blocked := excluded[item.ChannelID]; !blocked {
			return true
		}
	}
	return false
}

func groupRotationCandidates(group model.Group) []model.GroupItem {
	sequence := group.Items
	if group.Mode == model.GroupModeWeighted {
		sequence = smoothWRRSequence(sequence)
	}
	if len(sequence) == 0 {
		return nil
	}
	groupRotations.Lock()
	defer groupRotations.Unlock()
	now := time.Now()
	state := groupRotations.entries[group.ID]
	if state == nil || !sameRotationItems(state.items, sequence) {
		state = &groupRotation{items: append([]model.GroupItem(nil), sequence...)}
		groupRotations.entries[group.ID] = state
	}
	offset := state.next % uint64(len(sequence))
	state.next++
	state.seen = now
	if len(groupRotations.entries) > 1024 {
		for id, candidate := range groupRotations.entries {
			if now.Sub(candidate.seen) > time.Hour {
				delete(groupRotations.entries, id)
			}
		}
	}
	result := make([]model.GroupItem, len(sequence))
	for i := range result {
		result[i] = sequence[(int(offset)+i)%len(sequence)]
	}
	return result
}

func sameRotationItems(left, right []model.GroupItem) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}
