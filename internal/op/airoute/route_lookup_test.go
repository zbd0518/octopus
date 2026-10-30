package airoute

import (
	"testing"

	"github.com/lingyuins/octopus/internal/model"
)

func TestAIRouteGroupLookupRejectsCaseAmbiguity(t *testing.T) {
	groups := map[string]model.Group{
		airouteGroupKey(model.EndpointTypeChat, "Model"): {
			Name:         "Model",
			EndpointType: model.EndpointTypeChat,
		},
		airouteGroupKey(model.EndpointTypeChat, "model"): {
			Name:         "model",
			EndpointType: model.EndpointTypeChat,
		},
	}
	key, ambiguous := airouteGroupLookupKey(groups, model.EndpointTypeChat, "MODEL")
	if !ambiguous {
		t.Fatal("case-insensitive duplicate groups were not reported as ambiguous")
	}
	if key != airouteGroupKey(model.EndpointTypeChat, "MODEL") {
		t.Fatalf("ambiguous lookup key = %q", key)
	}
}

func TestAIRouteGroupLookupUsesSingleCaseInsensitiveMatch(t *testing.T) {
	groups := map[string]model.Group{
		airouteGroupKey(model.EndpointTypeChat, "Model"): {
			Name:         "Model",
			EndpointType: model.EndpointTypeChat,
		},
	}
	key, ambiguous := airouteGroupLookupKey(groups, model.EndpointTypeChat, "MODEL")
	if ambiguous {
		t.Fatal("single case-insensitive match was reported as ambiguous")
	}
	if key != airouteGroupKey(model.EndpointTypeChat, "Model") {
		t.Fatalf("lookup key = %q", key)
	}
}
