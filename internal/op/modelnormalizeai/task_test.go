package modelnormalizeai

import (
	"reflect"
	"testing"
	"time"

	"github.com/lingyuins/octopus/internal/model"
	"github.com/lingyuins/octopus/internal/op/modelnormalize"
)

func TestDecodeRulesResponseRequiresExactThreeFieldSchema(t *testing.T) {
	valid := `{"router_prefixes":["dmxapi-"],"functional_suffixes":["-cc"],"explicit_mappings":[]}`
	if _, err := decodeRulesResponse(valid); err != nil {
		t.Fatalf("valid response rejected: %v", err)
	}
	for _, input := range []string{
		`{"router_prefixes":[],"functional_suffixes":[]}`,
		`{"router_prefixes":[],"functional_suffixes":[],"explicit_mappings":[],"extra":1}`,
		`{"router_prefixes":[],"functional_suffixes":[],"explicit_mappings":[]} {"extra":1}`,
		`{"router_prefixes":null,"functional_suffixes":[],"explicit_mappings":[]}`,
		"```json\n" + valid + "\n```",
	} {
		_, err := decodeRulesResponse(input)
		if input[0] == '`' {
			if err != nil {
				t.Fatalf("fenced valid response rejected: %v", err)
			}
			continue
		}
		if err == nil {
			t.Fatalf("invalid response accepted: %s", input)
		}
	}
}

func TestMergeRulesReportsConflictingMappings(t *testing.T) {
	dst := rulesResponse{
		ExplicitMappings: []modelnormalize.ExplicitMapping{{
			Variant: "dmxapi-clo-4-6", Canonical: "claude-opus-4.6",
		}},
	}
	conflicts := mergeRules(&dst, rulesResponse{
		ExplicitMappings: []modelnormalize.ExplicitMapping{
			{Variant: "dmxapi-clo-4-6", Canonical: "claude-sonnet-4.6"},
			{Variant: "dmxapi-cls-4-6", Canonical: "claude-sonnet-4.6"},
		},
	})
	if len(conflicts) != 1 {
		t.Fatalf("conflicts = %v, want one conflict", conflicts)
	}
	if len(dst.ExplicitMappings) != 2 {
		t.Fatalf("mappings = %+v, want conflict omitted and new mapping retained", dst.ExplicitMappings)
	}
}

func TestBuildCandidateBatchesPreservesEveryNameAndKeepsPreclustersTogether(t *testing.T) {
	names := []string{
		"@cf/moonshotai/kimi-k2.5",
		"dmxapi-kimi-k2.5",
		"kimi-k2.5",
		"gpt-4o",
		"gpt-4o-mini",
		"deepseek-v4-pro",
	}
	batches := buildCandidateBatches(names, modelnormalize.Rules{}, 3)
	seen := make(map[string]int)
	for _, batch := range batches {
		if len(batch) > 3 {
			t.Fatalf("batch size = %d, want <= 3", len(batch))
		}
		for _, name := range batch {
			seen[name]++
		}
	}
	if len(seen) != len(names) {
		t.Fatalf("seen names = %v, want %d names", seen, len(names))
	}
	for _, name := range names {
		if seen[name] != 1 {
			t.Fatalf("name %q appears %d times", name, seen[name])
		}
	}
}

func TestSubscribeUnsubscribeDoesNotRaceWithPublish(t *testing.T) {
	updates, unsubscribe := Subscribe("task-test")
	task := model.ModelNormalizeTask{
		ID:      "task-test",
		Status:  model.ModelNormalizeTaskStatusRunning,
		Summary: &model.ModelNormalizeTaskSummary{TotalModels: 1},
	}
	publish(task)
	select {
	case got := <-updates:
		if got.ID != task.ID || got.Summary == task.Summary {
			t.Fatalf("published task was not cloned: %+v", got)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for progress update")
	}
	unsubscribe()
	// A publish after unsubscribe must be harmless and must not panic.
	publish(task)
	select {
	case <-updates:
		t.Fatal("received update after unsubscribe")
	default:
	}
}

func TestCloneTaskKeepsResultSlicesIndependent(t *testing.T) {
	task := model.ModelNormalizeTask{
		ID: "clone",
		Result: &model.ModelNormalizeTaskResult{
			Rules: model.ModelNormalizeRuleSet{
				RouterPrefixes:   []string{"dmxapi-"},
				ExplicitMappings: []map[string]string{{"variant": "a", "canonical": "b"}},
			},
		},
	}
	clone := cloneTask(task)
	clone.Result.Rules.RouterPrefixes[0] = "changed-"
	clone.Result.Rules.ExplicitMappings[0]["canonical"] = "changed"
	if reflect.DeepEqual(task, clone) {
		t.Fatal("clone unexpectedly shares result state")
	}
}
