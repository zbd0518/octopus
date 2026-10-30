package airoute

import (
	"context"
	"fmt"
	"strings"
	"unicode"

	"github.com/lingyuins/octopus/internal/model"
	"github.com/lingyuins/octopus/internal/op/channel"
	"github.com/lingyuins/octopus/internal/op/group"
)

const (
	aiRouteCategoryPaid      = "付费"
	aiRouteCategoryFree      = "免费"
	maxAIRouteCategoryLength = 191
)

type aiRouteCategoryResolver interface {
	ChannelTier(channelID int) (string, error)
}

type aiRouteChannelTierResolver struct {
	tiers map[int]string
}

func (r aiRouteChannelTierResolver) ChannelTier(channelID int) (string, error) {
	return r.tiers[channelID], nil
}

func newAIRouteChannelTierResolver(ctx context.Context, channels []model.Channel) (aiRouteChannelTierResolver, error) {
	resolver := aiRouteChannelTierResolver{tiers: make(map[int]string, len(channels))}
	if channel.GroupGet == nil {
		return resolver, fmt.Errorf("渠道分组查询未初始化")
	}
	for _, ch := range channels {
		if ch.GroupID <= 0 {
			continue
		}
		channelGroup, err := channel.GroupGet(ch.GroupID, ctx)
		if err != nil {
			return resolver, fmt.Errorf("读取渠道 %d 的渠道分组失败: %w", ch.ID, err)
		}
		if channelGroup != nil {
			resolver.tiers[ch.ID] = classifyAIRouteChannelGroup(channelGroup.Name)
		}
	}
	return resolver, nil
}

func classifyAIRouteChannelGroup(name string) string {
	tokens := aiRouteNameTokens(name)
	for _, token := range tokens {
		switch token {
		case "free", "gratis", "免费", "无偿", "公益", "公益站", "community", "charity", "野key":
			return aiRouteCategoryFree
		case "paid", "pay", "premium", "计费", "付费", "收费", "官方", "official", "酒馆", "酒館", "plan":
			return aiRouteCategoryPaid
		}
	}
	return ""
}

func aiRouteNameTokens(value string) []string {
	return strings.FieldsFunc(strings.ToLower(strings.TrimSpace(value)), func(r rune) bool {
		return !(unicode.IsLetter(r) || unicode.IsDigit(r))
	})
}

func applyAIRouteCategoryStrategy(
	ctx context.Context,
	strategy model.AIRouteCategoryStrategy,
	routes []model.AIRouteEntry,
	channels []model.Channel,
) ([]model.AIRouteEntry, aiRouteCategoryResolver, error) {
	if strategy == model.AIRouteCategoryNone {
		return routes, nil, nil
	}

	var resolver aiRouteCategoryResolver
	if strategy == model.AIRouteCategoryChannelTier {
		tiers, err := newAIRouteChannelTierResolver(ctx, channels)
		if err != nil {
			return nil, nil, err
		}
		resolver = tiers
	}
	result, err := normalizeAIRouteCategories(routes, strategy, resolver)
	if err != nil {
		return nil, nil, err
	}
	return result, resolver, nil
}

func normalizeAIRouteCategories(routes []model.AIRouteEntry, strategy model.AIRouteCategoryStrategy, resolver aiRouteCategoryResolver) ([]model.AIRouteEntry, error) {
	result := make([]model.AIRouteEntry, 0, len(routes))
	for _, route := range routes {
		switch strategy {
		case model.AIRouteCategoryVendor:
			route.Category = canonicalAIRouteVendor(route.Category)
			result = append(result, route)
		case model.AIRouteCategoryChannelTier:
			tiers := make(map[string][]model.AIRouteItemSpec)
			order := make([]string, 0, 4)
			for _, item := range route.Items {
				tier, err := resolver.ChannelTier(item.ChannelID)
				if err != nil {
					return nil, err
				}
				if _, ok := tiers[tier]; !ok {
					order = append(order, tier)
				}
				tiers[tier] = append(tiers[tier], item)
			}
			for _, tier := range order {
				items := tiers[tier]
				if len(items) == 0 {
					continue
				}
				next := route
				next.Items = items
				next.Category = tier
				next.MatchRegex = ""
				next.RequestedModel = aiRouteTierRouteName(route.RequestedModel, tier)
				result = append(result, next)
			}
		default:
			result = append(result, route)
		}
	}
	return normalizeAIRouteCategorizedEntries(result), nil
}

func resolveAIRouteTierRouteNameCollisions(routes []model.AIRouteEntry) []model.AIRouteEntry {
	nameCount := make(map[string]int, len(routes))
	for _, route := range routes {
		nameCount[strings.ToLower(strings.TrimSpace(route.RequestedModel))]++
	}
	if len(nameCount) == len(routes) {
		return routes
	}

	usedNames := make(map[string]struct{}, len(routes))
	for name := range nameCount {
		usedNames[name] = struct{}{}
	}
	seenNames := make(map[string]struct{}, len(routes))
	for i := range routes {
		name := strings.TrimSpace(routes[i].RequestedModel)
		nameKey := strings.ToLower(name)
		if _, seen := seenNames[nameKey]; !seen {
			seenNames[nameKey] = struct{}{}
			continue
		}
		candidate := ensureUniqueAIRouteRouteName(name, usedNames)
		if candidate != name && strings.TrimSpace(routes[i].MatchRegex) == "" {
			routes[i].MatchRegex = buildAIRouteExactMatchRegex(name)
		}
		routes[i].RequestedModel = candidate
		usedNames[strings.ToLower(candidate)] = struct{}{}
	}
	return routes
}

func aiRouteTierRouteName(base, tier string) string {
	base = strings.TrimSpace(base)
	suffix := aiRouteTierSuffix(base)
	switch tier {
	case aiRouteCategoryPaid:
		if suffix != "" {
			return strings.TrimSuffix(base, suffix)
		}
		return base
	case aiRouteCategoryFree:
		if suffix != "" {
			return strings.TrimSuffix(base, suffix) + "-free"
		}
		return base + "-free"
	default:
		if suffix != "" {
			return strings.TrimSuffix(base, suffix) + "-unsorted"
		}
		return base + "-unsorted"
	}
}

func aiRouteTierSuffix(name string) string {
	name = strings.TrimSpace(name)
	for _, suffix := range []string{"-free", "-公益", "-community", "-official", "-paid"} {
		if strings.HasSuffix(strings.ToLower(name), suffix) {
			return name[len(name)-len(suffix):]
		}
	}
	return ""
}

func canonicalAIRouteVendor(value string) string {
	normalized := strings.ToLower(strings.Join(strings.Fields(strings.TrimSpace(value)), " "))
	normalized = strings.ReplaceAll(normalized, "_", "-")
	aliases := map[string]string{
		"gpt": "OpenAI", "openai": "OpenAI", "chatgpt": "OpenAI",
		"deepseek": "DeepSeek", "深度求索": "DeepSeek",
		"glm": "GLM", "zhipu": "GLM", "zhipuai": "GLM", "智谱": "GLM", "智谱ai": "GLM",
		"kimi": "Kimi", "moonshot": "Kimi", "moonshotai": "Kimi", "moonshot-ai": "Kimi", "月之暗面": "Kimi",
		"anthropic": "Anthropic", "claude": "Anthropic",
		"google": "Google", "gemini": "Google",
		"alibaba": "Qwen", "aliyun": "Qwen", "qwen": "Qwen", "通义千问": "Qwen",
		"bytedance": "Doubao", "doubao": "Doubao", "豆包": "Doubao",
		"minimax": "MiniMax", "mimo": "MiMo", "xiaomi": "MiMo",
		"baidu": "ERNIE", "ernie": "ERNIE", "文心一言": "ERNIE",
		"meta": "Meta", "llama": "Meta", "mistral": "Mistral AI", "mistralai": "Mistral AI", "mistral-ai": "Mistral AI",
		"xai": "xAI", "grok": "xAI",
	}
	if canonical, ok := aliases[normalized]; ok {
		return canonical
	}
	if normalized == "" {
		return ""
	}
	return strings.TrimSpace(value)
}

func fillAIRouteGroupCategory(ctx context.Context, groupID int, category string) error {
	category = strings.TrimSpace(category)
	if category == "" || len([]rune(category)) > maxAIRouteCategoryLength {
		return nil
	}
	current, err := group.GroupGet(groupID, ctx)
	if err != nil {
		return err
	}
	if strings.TrimSpace(current.Category) != "" {
		return nil
	}
	_, err = group.GroupUpdate(&model.GroupUpdateRequest{ID: groupID, Category: &category}, ctx)
	return err
}

func normalizeAIRouteTierCategory(category string) string {
	switch strings.ToLower(strings.TrimSpace(category)) {
	case "免费", "公益", "公益站", "free", "community":
		return aiRouteCategoryFree
	case "付费", "官方", "paid", "official":
		return aiRouteCategoryPaid
	default:
		return ""
	}
}

func inferAIRouteGroupTier(g model.Group, resolver aiRouteCategoryResolver) (string, error) {
	if category := normalizeAIRouteTierCategory(g.Category); category != "" {
		return category, nil
	}
	if strings.TrimSpace(g.Category) != "" {
		return "", nil
	}
	if resolver == nil {
		return "", fmt.Errorf("分组 %q 没有分类，且无法推断已有渠道档位", g.Name)
	}

	category := ""
	for _, item := range g.Items {
		tier, err := resolver.ChannelTier(item.ChannelID)
		if err != nil {
			return "", err
		}
		if tier != aiRouteCategoryFree && tier != aiRouteCategoryPaid {
			return "", fmt.Errorf("分组 %q 包含无法判定档位的渠道，未修改原路由项", g.Name)
		}
		if category != "" && category != tier {
			return "", fmt.Errorf("分组 %q 同时包含免费和付费渠道，无法可靠选择分类路由，未修改原路由项", g.Name)
		}
		category = tier
	}
	if category == "" {
		return "", fmt.Errorf("分组 %q 没有可用于推断档位的已有渠道，未修改原路由项", g.Name)
	}
	return category, nil
}

func aiRouteTierNameMatches(groupName, routeName, category string) bool {
	groupName = strings.TrimSpace(groupName)
	routeName = strings.TrimSpace(routeName)
	if strings.EqualFold(groupName, routeName) {
		return true
	}
	base := strings.TrimSuffix(groupName, aiRouteTierSuffix(groupName))
	return strings.EqualFold(aiRouteTierRouteName(base, category), routeName)
}

func selectAIRouteForGroupAndCategory(g model.Group, routes []model.AIRouteEntry, strategy model.AIRouteCategoryStrategy, resolver aiRouteCategoryResolver) (model.AIRouteEntry, error) {
	if strategy != model.AIRouteCategoryChannelTier {
		return selectAIRouteForGroup(g, routes)
	}
	category := normalizeAIRouteTierCategory(g.Category)
	if category == "" && strings.TrimSpace(g.Category) != "" {
		return selectAIRouteForGroup(g, routes)
	}
	if category == "" {
		var err error
		category, err = inferAIRouteGroupTier(g, resolver)
		if err != nil {
			return model.AIRouteEntry{}, err
		}
	}
	for _, route := range routes {
		if aiRouteTierNameMatches(g.Name, route.RequestedModel, category) && strings.EqualFold(route.Category, category) {
			return route, nil
		}
	}
	return model.AIRouteEntry{}, fmt.Errorf("AI 返回结果未包含目标分组 %q 的%s分类路由，未修改原路由项", g.Name, category)
}

func checkAIRouteTableGroupCategory(g model.Group, route model.AIRouteEntry, resolver aiRouteCategoryResolver) error {
	if route.Category != aiRouteCategoryFree && route.Category != aiRouteCategoryPaid {
		return nil
	}
	current := strings.TrimSpace(g.Category)
	if current == "" {
		inferred, err := inferAIRouteGroupTier(g, resolver)
		if err != nil {
			return err
		}
		if inferred != route.Category {
			return fmt.Errorf("分组 %q 的已有渠道档位为%s，与目标%s路由冲突，已跳过且未修改原分组", g.Name, inferred, route.Category)
		}
		return nil
	}
	category := normalizeAIRouteTierCategory(current)
	if category == "" || category == route.Category {
		return nil
	}
	return fmt.Errorf("分组 %q 的分类 %q 与目标%s路由冲突，已跳过且未修改原分组", g.Name, current, route.Category)
}
