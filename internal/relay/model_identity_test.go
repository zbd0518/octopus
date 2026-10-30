package relay

import (
	"context"
	"testing"

	dm "github.com/lingyuins/octopus/internal/model"
	"github.com/lingyuins/octopus/internal/op/channel"
	"github.com/lingyuins/octopus/internal/op/group"
	"github.com/lingyuins/octopus/internal/op/modelmapping"
)

func TestModelMappingGlobalScopedAndFallback(t *testing.T) {
	setupDiagnosticsRelay(t, false)
	ctx := context.Background()
	channel.GetCache().Set(929201, dm.Channel{ID: 929201, Enabled: true})
	defer channel.GetCache().Del(929201)
	original := dm.Group{Name: "source", EndpointType: "chat", Items: []dm.GroupItem{{ChannelID: 929201, ModelName: "old", Weight: 1}}}
	if err := group.GroupCreate(&original, ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := modelmapping.Create(ctx, &dm.ModelMappingCreateRequest{Name: "global", Pattern: "source", MatchType: dm.MatchExact, TargetModel: "target"}); err != nil {
		t.Fatal(err)
	}
	g, identity, err := resolveRelayModel(ctx, "chat", "source")
	if err != nil {
		t.Fatal(err)
	}
	if g.ID != original.ID || identity.candidate(g.Items[0]) != "target" {
		t.Fatal("missing-target fallback failed", identity)
	}
	target := dm.Group{Name: "target", EndpointType: "chat", Items: []dm.GroupItem{{ChannelID: 929201, ModelName: "actual", Weight: 1}}}
	if err := group.GroupCreate(&target, ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := modelmapping.Create(ctx, &dm.ModelMappingCreateRequest{Name: "chain", Pattern: "target", MatchType: dm.MatchExact, TargetModel: "unexpected"}); err != nil {
		t.Fatal(err)
	}
	g, identity, err = resolveRelayModel(ctx, "chat", "source")
	if err != nil || g.ID != target.ID || identity.candidate(g.Items[0]) != "actual" {
		t.Fatal("mapping chained or ignored target group", identity, err)
	}
	if _, err := modelmapping.Create(ctx, &dm.ModelMappingCreateRequest{Name: "scoped", Pattern: "source", MatchType: dm.MatchExact, TargetModel: "scoped-upstream", ScopeGroupID: &target.ID}); err != nil {
		t.Fatal(err)
	}
	g, identity, err = resolveRelayModel(ctx, "chat", "source")
	if err != nil || identity.candidate(g.Items[0]) != "scoped-upstream" {
		t.Fatal("scoped mapping did not apply", identity, err)
	}
}
