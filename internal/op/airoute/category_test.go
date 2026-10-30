package airoute

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/lingyuins/octopus/internal/db"
	"github.com/lingyuins/octopus/internal/model"
	"github.com/lingyuins/octopus/internal/op/channel"
	"github.com/lingyuins/octopus/internal/op/group"
	"github.com/lingyuins/octopus/internal/op/setting"
	"github.com/lingyuins/octopus/internal/utils/xurl"
)

func TestClassifyAIRouteChannelGroup(t *testing.T) {
	tests := map[string]string{
		"付费": "付费", "Premium": "付费", "Paid / OpenAI": "付费",
		"免费": "免费", "free-tier": "免费",
		"公益站": "免费", "Community": "免费", "公益": "免费", "Charity": "免费",
		"官方": "付费", "Official": "付费",
		"酒馆": "付费", "酒館": "付费", "野key": "免费", "野Key": "免费", "野KEY": "免费",
		"Plan": "付费", "plan": "付费", "PLAN": "付费", "Token/Coding plan": "付费",
		"群友Key（Free）": "免费", "LDC Key": "", "个人后端": "", "planet": "", "explanation": "",
		"Default": "", "普通中转": "", "official-paid": "付费",
	}
	for input, want := range tests {
		if got := classifyAIRouteChannelGroup(input); got != want {
			t.Errorf("classifyAIRouteChannelGroup(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestNormalizeAIRouteVendorAliases(t *testing.T) {
	tests := map[string]string{"GPT": "OpenAI", "moonshot-ai": "Kimi", "智谱AI": "GLM", "深度求索": "DeepSeek", "": "", "New Vendor": "New Vendor"}
	for input, want := range tests {
		if got := canonicalAIRouteVendor(input); got != want {
			t.Errorf("canonicalAIRouteVendor(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestNormalizeAIRouteCategoriesSplitsChannelTiers(t *testing.T) {
	routes := []model.AIRouteEntry{{
		RequestedModel: "gpt-4o",
		EndpointType:   model.EndpointTypeChat,
		Items: []model.AIRouteItemSpec{
			{ChannelID: 1, UpstreamModel: "gpt-4o", Priority: 1, Weight: 100},
			{ChannelID: 2, UpstreamModel: "gpt-4o", Priority: 2, Weight: 100},
			{ChannelID: 3, UpstreamModel: "gpt-4o", Priority: 3, Weight: 100},
			{ChannelID: 4, UpstreamModel: "gpt-4o", Priority: 4, Weight: 100},
		},
	}}
	resolver := aiRouteChannelTierResolver{tiers: map[int]string{1: "付费", 2: "免费", 4: "付费"}}
	got, err := normalizeAIRouteCategories(routes, model.AIRouteCategoryChannelTier, resolver)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("routes = %+v, want paid, free, and uncategorized routes", got)
	}
	if got[0].RequestedModel != "gpt-4o" || got[0].Category != "付费" || len(got[0].Items) != 2 {
		t.Fatalf("paid route = %+v, want two paid channels", got[0])
	}
	if got[1].RequestedModel != "gpt-4o-free" || got[1].Category != "免费" || got[1].Items[0].ChannelID != 2 {
		t.Fatalf("free route = %+v, want only the free channel", got[1])
	}
	if got[2].RequestedModel != "gpt-4o-unsorted" || got[2].Category != "" {
		t.Fatalf("uncategorized route = %+v, want an isolated unsorted route", got[2])
	}
	if want := []model.AIRouteItemSpec{routes[0].Items[0], routes[0].Items[3]}; !reflect.DeepEqual(got[0].Items, want) {
		t.Fatalf("paid items = %+v, want %+v", got[0].Items, want)
	}
	if want := routes[0].Items[2:3]; !reflect.DeepEqual(got[2].Items, want) {
		t.Fatalf("uncategorized items = %+v, want %+v", got[2].Items, want)
	}
}

func TestNormalizeAIRouteCategoriesUsesChannelTierInsteadOfModelName(t *testing.T) {
	routes := []model.AIRouteEntry{{
		RequestedModel: "gpt-4o",
		Items: []model.AIRouteItemSpec{
			{ChannelID: 1, UpstreamModel: "gpt-4o-free"},
			{ChannelID: 2, UpstreamModel: "gpt-4o"},
			{ChannelID: 3, UpstreamModel: "gpt-4o"},
			{ChannelID: 4, UpstreamModel: "gpt-4o"},
		},
	}}
	previousGroupGet := channel.GroupGet
	channel.GroupGet = func(id int, _ context.Context) (*model.ChannelGroup, error) {
		return &model.ChannelGroup{Name: map[int]string{1: "Free", 2: "Paid", 3: "公益站", 4: "官方"}[id]}, nil
	}
	t.Cleanup(func() { channel.GroupGet = previousGroupGet })
	channels := []model.Channel{{ID: 1, GroupID: 1}, {ID: 2, GroupID: 2}, {ID: 3, GroupID: 3}, {ID: 4, GroupID: 4}}
	got, _, err := applyAIRouteCategoryStrategy(context.Background(), model.AIRouteCategoryChannelTier, routes, channels)
	if err != nil {
		t.Fatal(err)
	}
	byCategory := make(map[string]model.AIRouteEntry, len(got))
	for _, route := range got {
		byCategory[route.Category] = route
	}
	if len(got) != 2 {
		t.Fatalf("routes = %+v, want only free and paid tiers", got)
	}
	paid := byCategory["付费"]
	if paid.RequestedModel != "gpt-4o" || paid.MatchRegex != "" {
		t.Errorf("paid route = %+v, want the original name", paid)
	}
	if want := []model.AIRouteItemSpec{routes[0].Items[1], routes[0].Items[3]}; !reflect.DeepEqual(paid.Items, want) {
		t.Errorf("paid items = %+v, want paid and official channels %+v", paid.Items, want)
	}
	free := byCategory["免费"]
	if free.RequestedModel != "gpt-4o-free" {
		t.Errorf("free route = %+v, want the free alias", free)
	}
	if want := []model.AIRouteItemSpec{routes[0].Items[0], routes[0].Items[2]}; !reflect.DeepEqual(free.Items, want) {
		t.Errorf("free items = %+v, want free and community channels %+v", free.Items, want)
	}
}

func TestNormalizeAIRouteCategoriesMergesSameTierRoutes(t *testing.T) {
	first := model.AIRouteItemSpec{ChannelID: 1, UpstreamModel: "gpt-4o", Priority: 1, Weight: 70}
	second := model.AIRouteItemSpec{ChannelID: 2, UpstreamModel: "gpt-4o", Priority: 2, Weight: 30}
	routes := []model.AIRouteEntry{
		{RequestedModel: "gpt-4o", Items: []model.AIRouteItemSpec{first}},
		{RequestedModel: " GPT-4o-PAID ", Items: []model.AIRouteItemSpec{first, second}},
		{RequestedModel: "gpt-4o 2", Items: []model.AIRouteItemSpec{{ChannelID: 3, UpstreamModel: "gpt-4o"}}},
	}
	resolver := aiRouteChannelTierResolver{tiers: map[int]string{1: "付费", 2: "付费", 3: "付费"}}
	got, err := normalizeAIRouteCategories(routes, model.AIRouteCategoryChannelTier, resolver)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("routes = %+v, want merged paid route and unrelated numbered route", got)
	}
	if got[0].RequestedModel != "gpt-4o" || got[0].Category != aiRouteCategoryPaid || !reflect.DeepEqual(got[0].Items, []model.AIRouteItemSpec{first, second}) {
		t.Errorf("merged route = %+v, want deduplicated channels preserving priority and weight", got[0])
	}
	if got[1].RequestedModel != "gpt-4o 2" || !reflect.DeepEqual(got[1].Items, routes[2].Items) {
		t.Errorf("unrelated route changed: %+v", got[1])
	}
}

func TestNormalizeAIRouteCategoriesKeepsDifferentEndpointsSeparate(t *testing.T) {
	routes := []model.AIRouteEntry{
		{RequestedModel: "shared", EndpointType: model.EndpointTypeChat, Items: []model.AIRouteItemSpec{{ChannelID: 1, UpstreamModel: "gpt-4o"}}},
		{RequestedModel: "shared-paid", EndpointType: model.EndpointTypeEmbeddings, Items: []model.AIRouteItemSpec{{ChannelID: 2, UpstreamModel: "text-embedding-3-small"}}},
	}
	resolver := aiRouteChannelTierResolver{tiers: map[int]string{1: "付费", 2: "付费"}}
	got, err := normalizeAIRouteCategories(routes, model.AIRouteCategoryChannelTier, resolver)
	if err != nil {
		t.Fatal(err)
	}
	got, _ = autoCorrectAIRouteTableRoutes(got, nil)
	if len(got) != 2 || got[0].RequestedModel != "shared" || got[1].RequestedModel != "shared (embeddings)" {
		t.Fatalf("routes = %+v, want separate endpoint-specific names", got)
	}
	for i := range routes {
		if got[i].EndpointType != routes[i].EndpointType || !reflect.DeepEqual(got[i].Items, routes[i].Items) {
			t.Errorf("endpoint route %d changed: %+v", i, got[i])
		}
	}
}

func TestResolveAIRouteTierRouteNameCollisionsReservesOriginalNames(t *testing.T) {
	got := resolveAIRouteTierRouteNameCollisions([]model.AIRouteEntry{
		{RequestedModel: "gpt-4o"}, {RequestedModel: "gpt-4o"}, {RequestedModel: "gpt-4o 2"},
	})
	if err := validateAIRouteTableRoutes(got); err != nil {
		t.Fatal(err)
	}
	for i, want := range []string{"gpt-4o", "gpt-4o 3", "gpt-4o 2"} {
		if got[i].RequestedModel != want {
			t.Errorf("name %d = %q, want %q", i, got[i].RequestedModel, want)
		}
	}
}

func TestNormalizeAIRouteGeneratedEntriesRespectsCategoryStrategy(t *testing.T) {
	routes := []model.AIRouteEntry{
		{RequestedModel: "gpt-4o", Category: "GPT", Items: []model.AIRouteItemSpec{{ChannelID: 1, UpstreamModel: "gpt-4o-free"}}},
		{RequestedModel: "gpt-4o", Category: "OpenAI", Items: []model.AIRouteItemSpec{{ChannelID: 2, UpstreamModel: "gpt-4o"}}},
		{RequestedModel: "gpt-4o", Category: "Anthropic", Items: []model.AIRouteItemSpec{{ChannelID: 3, UpstreamModel: "claude-3-5"}}},
	}
	vendor := normalizeAIRouteGeneratedEntries(routes, model.AIRouteCategoryVendor)
	if len(vendor) != 2 || vendor[0].Category != "OpenAI" || len(vendor[0].Items) != 2 || vendor[1].Category != "Anthropic" {
		t.Fatalf("vendor routes = %+v, want aliases merged without mixing vendors", vendor)
	}
	tiers := normalizeAIRouteGeneratedEntries(routes, model.AIRouteCategoryChannelTier)
	if len(tiers) != 1 || tiers[0].RequestedModel != "gpt-4o" || tiers[0].Category != "" || len(tiers[0].Items) != 3 {
		t.Fatalf("pre-split routes = %+v, want channel-based classification deferred", tiers)
	}
	for _, route := range normalizeAIRouteGeneratedEntries(routes, model.AIRouteCategoryNone) {
		if route.Category != "" {
			t.Fatalf("none strategy retained unsolicited AI category: %+v", route)
		}
	}
}

func TestAIRouteTierRouteNameHandlesSuffixCase(t *testing.T) {
	for _, tc := range []struct {
		base string
		tier string
		want string
	}{
		{base: "GPT-4o-FREE", tier: aiRouteCategoryPaid, want: "GPT-4o"},
		{base: "GPT-4o-PAID", tier: aiRouteCategoryFree, want: "GPT-4o-free"},
		{base: "GPT-4o-COMMUNITY", tier: aiRouteCategoryFree, want: "GPT-4o-free"},
		{base: "GPT-4o-公益", tier: aiRouteCategoryFree, want: "GPT-4o-free"},
		{base: "GPT-4o-OFFICIAL", tier: aiRouteCategoryPaid, want: "GPT-4o"},
		{base: "GPT-4o-OFFICIAL", tier: "", want: "GPT-4o-unsorted"},
	} {
		t.Run(tc.base, func(t *testing.T) {
			if got := aiRouteTierRouteName(tc.base, tc.tier); got != tc.want {
				t.Fatalf("aiRouteTierRouteName(%q, %q) = %q, want %q", tc.base, tc.tier, got, tc.want)
			}
		})
	}
}

func TestAIRouteCategoryPromptOnlyForVendor(t *testing.T) {
	if strings.Contains(buildAIRouteSystemPrompt("", model.AIRouteCategoryChannelTier), "分类要求") {
		t.Fatal("channel-tier strategy must not ask AI to classify channel tiers")
	}
	prompt := buildAIRouteSystemPrompt("", model.AIRouteCategoryVendor)
	if !strings.Contains(prompt, "不要根据渠道协议") || !strings.Contains(prompt, "category") {
		t.Fatalf("vendor prompt lacks model-vendor classification rules: %q", prompt)
	}
	if !strings.Contains(buildAIRouteFollowUpSystemPrompt(model.AIRouteCategoryVendor), "分类要求") {
		t.Fatal("vendor follow-up must retain category instructions")
	}
}

func TestAIRouteCategoryPersistencePreservesManualCategory(t *testing.T) {
	ctx := initAIRouteCategoryTestDB(t)
	created, _, err := createAIRouteGroup(ctx, "vendor-model", model.EndpointTypeChat, "", "moonshot", []model.GroupItem{
		{ChannelID: 1, ModelName: "kimi-k2", Priority: 1, Weight: 100},
	})
	if err != nil {
		t.Fatalf("createAIRouteGroup: %v", err)
	}
	if created.Category != "Kimi" {
		t.Fatalf("created category = %q, want Kimi", created.Category)
	}

	manual := "custom"
	if _, err := group.GroupUpdate(&model.GroupUpdateRequest{ID: created.ID, Category: &manual}, ctx); err != nil {
		t.Fatal(err)
	}
	if err := fillAIRouteGroupCategory(ctx, created.ID, "DeepSeek"); err != nil {
		t.Fatal(err)
	}
	got, err := group.GroupGet(created.ID, ctx)
	if err != nil || got.Category != "custom" {
		t.Fatalf("manual category overwritten: group=%+v err=%v", got, err)
	}

	empty, _, err := createAIRouteGroup(ctx, "empty-category", model.EndpointTypeChat, "", "", []model.GroupItem{
		{ChannelID: 1, ModelName: "gpt-4o", Priority: 1, Weight: 100},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := fillAIRouteGroupCategory(ctx, empty.ID, "OpenAI"); err != nil {
		t.Fatal(err)
	}
	got, err = group.GroupGet(empty.ID, ctx)
	if err != nil || got.Category != "OpenAI" {
		t.Fatalf("empty category not filled: group=%+v err=%v", got, err)
	}
}

func TestGenerateAIRouteForGroupPreservesGenerationErrorWithChannelTier(t *testing.T) {
	ctx := initAIRouteCategoryTestDB(t)
	created, _, err := createAIRouteGroup(ctx, "gpt-4o", model.EndpointTypeChat, "", "", []model.GroupItem{
		{ChannelID: 1, ModelName: "gpt-4o", Priority: 1, Weight: 100},
	})
	if err != nil {
		t.Fatal(err)
	}
	cache := setting.GetCache()
	previous := cache.GetAll()
	cache.Clear()
	t.Cleanup(func() {
		cache.Clear()
		for key, value := range previous {
			cache.Set(key, value)
		}
	})
	_, wantErr := generateAIRoutesFromModelList(ctx, nil, created.Name, model.EndpointTypeChat, model.AIRouteCategoryChannelTier, nil)
	if wantErr == nil {
		t.Fatal("generation without configured services should fail")
	}
	_, gotErr := generateAIRouteForGroup(ctx, created.ID, nil, nil, nil, model.AIRouteCategoryChannelTier, nil)
	if gotErr == nil || gotErr.Error() != wantErr.Error() {
		t.Fatalf("generation error = %v, want original error %v", gotErr, wantErr)
	}
}

func TestGenerateAIRouteTableMergesSplitRoutesAndPersistsCategories(t *testing.T) {
	ctx := initAIRouteCategoryTestDB(t)
	xurl.SetSSRFAllowPrivateForTest(true)
	t.Cleanup(func() { xurl.SetSSRFAllowPrivateForTest(false) })

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		content := `{"routes":[{"requested_model":"gpt-4o","items":[{"channel_id":1,"upstream_model":"gpt-4o"},{"channel_id":3,"upstream_model":"gpt-4o-free"}]},{"requested_model":"gpt-4o-official","items":[{"channel_id":2,"upstream_model":"gpt-4o"}]},{"requested_model":"gpt-4o-community","items":[{"channel_id":4,"upstream_model":"gpt-4o"}]}]}`
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]string{"content": content}}}})
	}))
	defer server.Close()
	cache := setting.GetCache()
	previous := cache.GetAll()
	cache.Clear()
	t.Cleanup(func() {
		cache.Clear()
		for key, value := range previous {
			cache.Set(key, value)
		}
	})
	cache.Set(model.SettingKeyAIRouteBaseURL, server.URL)
	cache.Set(model.SettingKeyAIRouteAPIKey, "test-key")
	cache.Set(model.SettingKeyAIRouteModel, "analyzer")

	previousGroupGet := channel.GroupGet
	channel.GroupGet = func(id int, _ context.Context) (*model.ChannelGroup, error) {
		return &model.ChannelGroup{Name: map[int]string{1: "付费", 2: "官方", 3: "免费", 4: "公益站"}[id]}, nil
	}
	t.Cleanup(func() { channel.GroupGet = previousGroupGet })
	channels := []model.Channel{{ID: 1, GroupID: 1}, {ID: 2, GroupID: 2}, {ID: 3, GroupID: 3}, {ID: 4, GroupID: 4}}
	inputs := []model.AIRouteModelInput{
		{ChannelID: 1, Model: "gpt-4o"}, {ChannelID: 2, Model: "gpt-4o"},
		{ChannelID: 3, Model: "gpt-4o-free"}, {ChannelID: 4, Model: "gpt-4o"},
	}
	inputSet := map[int]map[string]struct{}{
		1: {"gpt-4o": {}}, 2: {"gpt-4o": {}}, 3: {"gpt-4o-free": {}}, 4: {"gpt-4o": {}},
	}
	for run := 0; run < 2; run++ {
		result, err := generateAIRouteTable(ctx, inputs, channels, inputSet, model.AIRouteCategoryChannelTier, nil)
		if err != nil {
			t.Fatal(err)
		}
		if result.GroupCount != 2 || result.RouteCount != 2 {
			t.Fatalf("run %d result = %+v, want two merged routes", run, result)
		}
		groups, err := group.GroupList(ctx)
		if err != nil || len(groups) != 2 {
			t.Fatalf("run %d groups = %+v, err = %v, want no duplicate groups", run, groups, err)
		}
		for _, g := range groups {
			wantCategory := aiRouteCategoryPaid
			wantChannels := []int{1, 2}
			if g.Name == "gpt-4o-free" {
				wantCategory, wantChannels = aiRouteCategoryFree, []int{3, 4}
			} else if g.Name != "gpt-4o" {
				t.Fatalf("unexpected numbered or tier-specific group: %+v", g)
			}
			gotChannels := make([]int, 0, len(g.Items))
			for _, item := range g.Items {
				gotChannels = append(gotChannels, item.ChannelID)
			}
			if g.Category != wantCategory || !reflect.DeepEqual(gotChannels, wantChannels) {
				t.Errorf("run %d group = %+v, want category %q and channels %v", run, g, wantCategory, wantChannels)
			}
		}
	}
}

func TestGenerateAIRouteTableSkipsConflictingTierWithoutTouchingOriginal(t *testing.T) {
	for _, tc := range []struct {
		name            string
		originalName    string
		originalTier    string
		originalChannel int
	}{
		{name: "free original rejects paid channels", originalName: "gpt-4o", originalTier: aiRouteCategoryFree, originalChannel: 2},
		{name: "paid original rejects free channels", originalName: "gpt-4o-free", originalTier: aiRouteCategoryPaid, originalChannel: 1},
		{name: "uncategorized free original rejects paid channels", originalName: "gpt-4o", originalTier: "", originalChannel: 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, _ := setupAIRouteCategoryGenerationTest(t, `{"routes":[{"requested_model":"gpt-4o","items":[{"channel_id":1,"upstream_model":"gpt-4o"},{"channel_id":2,"upstream_model":"gpt-4o"}]},{"requested_model":"gpt-4o-mini","items":[{"channel_id":3,"upstream_model":"gpt-4o-mini"}]}]}`)
			original, _, err := createAIRouteGroup(ctx, tc.originalName, model.EndpointTypeChat, "", tc.originalTier, []model.GroupItem{
				{ChannelID: tc.originalChannel, ModelName: "gpt-4o", Priority: 1, Weight: 100},
			})
			if err != nil {
				t.Fatal(err)
			}
			originalItems := append([]model.GroupItem(nil), original.Items...)
			channels, inputs, inputSet := aiRouteCategoryGenerationInputs()

			for run := 0; run < 2; run++ {
				result, err := generateAIRouteTable(ctx, inputs, channels, inputSet, model.AIRouteCategoryChannelTier, nil)
				if result == nil || err == nil || !strings.Contains(err.Error(), "冲突") || !strings.Contains(err.Error(), "未修改原分组") {
					t.Fatalf("run %d result = %+v, err = %v, want a reported conflict with other routes preserved", run, result, err)
				}
				got, getErr := group.GroupGet(original.ID, ctx)
				if getErr != nil {
					t.Fatal(getErr)
				}
				if got.Name != original.Name || got.Category != original.Category || !reflect.DeepEqual(got.Items, originalItems) {
					t.Fatalf("run %d original group changed: got %+v, want items %+v", run, got, originalItems)
				}
				groups, listErr := group.GroupList(ctx)
				if listErr != nil || len(groups) != 3 {
					t.Fatalf("run %d groups = %+v, err = %v, want stable groups without numbered duplicates", run, groups, listErr)
				}
			}
		})
	}
}

func TestCheckAIRouteTableGroupCategoryRecognizesAliases(t *testing.T) {
	resolver := aiRouteChannelTierResolver{tiers: map[int]string{1: aiRouteCategoryPaid, 2: aiRouteCategoryFree}}
	for _, tc := range []struct {
		category string
		route    string
		channel  int
	}{
		{category: "公益", route: aiRouteCategoryPaid, channel: 2},
		{category: "公益站", route: aiRouteCategoryPaid, channel: 2},
		{category: "官方", route: aiRouteCategoryFree, channel: 1},
	} {
		g := model.Group{Name: "gpt-4o", Category: tc.category, Items: []model.GroupItem{{ChannelID: tc.channel}}}
		err := checkAIRouteTableGroupCategory(g, model.AIRouteEntry{Category: tc.route}, resolver)
		if err == nil || !strings.Contains(err.Error(), "未修改原分组") {
			t.Fatalf("category %q versus %q: err = %v, want explicit preservation", tc.category, tc.route, err)
		}
	}
}

func TestGenerateAIRouteForGroupRejectsAmbiguousTierWithoutSync(t *testing.T) {
	ctx, _ := setupAIRouteCategoryGenerationTest(t, `{"routes":[{"requested_model":"gpt-4o","items":[{"channel_id":1,"upstream_model":"gpt-4o"},{"channel_id":2,"upstream_model":"gpt-4o"}]}]}`)
	created, _, err := createAIRouteGroup(ctx, "gpt-4o", model.EndpointTypeChat, "", "", []model.GroupItem{
		{ChannelID: 2, ModelName: "gpt-4o", Priority: 1, Weight: 100},
		{ChannelID: 1, ModelName: "gpt-4o", Priority: 2, Weight: 100},
	})
	if err != nil {
		t.Fatal(err)
	}
	originalItems := append([]model.GroupItem(nil), created.Items...)
	channels, inputs, inputSet := aiRouteCategoryGenerationInputs()

	result, err := generateAIRouteForGroup(ctx, created.ID, inputs, channels, inputSet, model.AIRouteCategoryChannelTier, nil)
	if result != nil || err == nil || !strings.Contains(err.Error(), "无法可靠选择") {
		t.Fatalf("result = %+v, err = %v, want mixed-tier selection failure", result, err)
	}
	got, getErr := group.GroupGet(created.ID, ctx)
	if getErr != nil || !reflect.DeepEqual(got.Items, originalItems) {
		t.Fatalf("failed generation modified original items: got %+v, want %+v, err = %v", got.Items, originalItems, getErr)
	}
}

func TestSelectAIRouteForGroupAndCategoryInfersAndMatchesTierNames(t *testing.T) {
	routes := []model.AIRouteEntry{
		{RequestedModel: "gpt-4o", Category: aiRouteCategoryPaid},
		{RequestedModel: "gpt-4o-free", Category: aiRouteCategoryFree},
	}
	resolver := aiRouteChannelTierResolver{tiers: map[int]string{1: aiRouteCategoryPaid, 2: aiRouteCategoryFree, 3: ""}}
	for _, tc := range []struct {
		name  string
		group model.Group
		want  string
	}{
		{name: "empty category free original name", group: model.Group{Name: "gpt-4o", Items: []model.GroupItem{{ChannelID: 2}}}, want: aiRouteCategoryFree},
		{name: "empty category paid suffixed name", group: model.Group{Name: "gpt-4o-free", Items: []model.GroupItem{{ChannelID: 1}}}, want: aiRouteCategoryPaid},
		{name: "explicit free original name", group: model.Group{Name: "gpt-4o", Category: "免费"}, want: aiRouteCategoryFree},
		{name: "explicit paid suffixed name", group: model.Group{Name: "gpt-4o-free", Category: "官方"}, want: aiRouteCategoryPaid},
	} {
		t.Run(tc.name, func(t *testing.T) {
			selected, err := selectAIRouteForGroupAndCategory(tc.group, routes, model.AIRouteCategoryChannelTier, resolver)
			if err != nil || selected.Category != tc.want {
				t.Fatalf("selected = %+v, err = %v, want %q", selected, err, tc.want)
			}
		})
	}
	for _, tc := range []struct {
		name  string
		items []model.GroupItem
	}{
		{name: "mixed tiers", items: []model.GroupItem{{ChannelID: 1}, {ChannelID: 2}}},
		{name: "unknown tier", items: []model.GroupItem{{ChannelID: 3}}},
		{name: "no existing items", items: nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := selectAIRouteForGroupAndCategory(model.Group{Name: "gpt-4o", Items: tc.items}, routes, model.AIRouteCategoryChannelTier, resolver)
			if err == nil || strings.Contains(err.Error(), "未包含") {
				t.Fatalf("err = %v, want explicit unreliable-tier error instead of paid fallback", err)
			}
		})
	}
}

func setupAIRouteCategoryGenerationTest(t *testing.T, content string) (context.Context, *httptest.Server) {
	t.Helper()
	ctx := initAIRouteCategoryTestDB(t)
	xurl.SetSSRFAllowPrivateForTest(true)
	t.Cleanup(func() { xurl.SetSSRFAllowPrivateForTest(false) })
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]string{"content": content}}}})
	}))
	t.Cleanup(server.Close)
	cache := setting.GetCache()
	previous := cache.GetAll()
	cache.Clear()
	t.Cleanup(func() {
		cache.Clear()
		for key, value := range previous {
			cache.Set(key, value)
		}
	})
	cache.Set(model.SettingKeyAIRouteBaseURL, server.URL)
	cache.Set(model.SettingKeyAIRouteAPIKey, "test-key")
	cache.Set(model.SettingKeyAIRouteModel, "analyzer")
	previousGroupGet := channel.GroupGet
	channel.GroupGet = func(id int, _ context.Context) (*model.ChannelGroup, error) {
		return &model.ChannelGroup{Name: map[int]string{1: "官方", 2: "公益站", 3: "付费"}[id]}, nil
	}
	t.Cleanup(func() { channel.GroupGet = previousGroupGet })
	return ctx, server
}

func aiRouteCategoryGenerationInputs() ([]model.Channel, []model.AIRouteModelInput, map[int]map[string]struct{}) {
	channels := []model.Channel{{ID: 1, GroupID: 1}, {ID: 2, GroupID: 2}, {ID: 3, GroupID: 3}}
	inputs := []model.AIRouteModelInput{{ChannelID: 1, Model: "gpt-4o"}, {ChannelID: 2, Model: "gpt-4o"}, {ChannelID: 3, Model: "gpt-4o-mini"}}
	inputSet := map[int]map[string]struct{}{1: {"gpt-4o": {}}, 2: {"gpt-4o": {}}, 3: {"gpt-4o-mini": {}}}
	return channels, inputs, inputSet
}

func TestSelectAIRouteForGroupAndCategory(t *testing.T) {
	routes := []model.AIRouteEntry{
		{RequestedModel: "gpt-4o", Category: "付费"},
		{RequestedModel: "gpt-4o-free", Category: "免费"},
	}
	g := model.Group{Name: "gpt-4o-free", Category: "custom", Items: []model.GroupItem{{ChannelID: 2}}}
	resolver := aiRouteChannelTierResolver{tiers: map[int]string{2: "免费"}}
	selected, err := selectAIRouteForGroupAndCategory(g, routes, model.AIRouteCategoryChannelTier, resolver)
	if err != nil || selected.Category != "免费" {
		t.Fatalf("existing tier should select the matching split route: %+v, %v", selected, err)
	}
	g.Name = "gpt-4o"
	selected, err = selectAIRouteForGroupAndCategory(g, routes, model.AIRouteCategoryChannelTier, resolver)
	if err != nil || selected.Category != "付费" {
		t.Fatalf("manual non-tier category should retain original route selection: %+v, %v", selected, err)
	}
}

func TestSelectAIRouteForGroupAndCategoryMapsCommunityAndOfficial(t *testing.T) {
	routes := []model.AIRouteEntry{
		{RequestedModel: "gpt-4o", Category: aiRouteCategoryPaid},
		{RequestedModel: "gpt-4o-free", Category: aiRouteCategoryFree},
	}
	for _, tc := range []struct {
		name     string
		category string
		want     string
	}{
		{name: "gpt-4o-free", category: "公益站", want: aiRouteCategoryFree},
		{name: "gpt-4o", category: "官方", want: aiRouteCategoryPaid},
	} {
		t.Run(tc.category, func(t *testing.T) {
			selected, err := selectAIRouteForGroupAndCategory(model.Group{Name: tc.name, Category: tc.category}, routes, model.AIRouteCategoryChannelTier, nil)
			if err != nil || selected.Category != tc.want {
				t.Fatalf("selected = %+v, err = %v, want category %q", selected, err, tc.want)
			}
		})
	}
}

func initAIRouteCategoryTestDB(t *testing.T) context.Context {
	t.Helper()
	ctx := context.Background()
	dsn := "file:" + strings.NewReplacer("/", "-", "\\", "-", " ", "-").Replace(t.Name()) + "?mode=memory&cache=shared"
	if err := db.InitDB("sqlite", dsn, false); err != nil {
		t.Fatalf("init db: %v", err)
	}
	if err := group.RefreshAllCache(ctx); err != nil {
		t.Fatalf("refresh group cache: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return ctx
}
