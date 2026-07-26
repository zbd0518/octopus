package planprovider

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/lingyuins/octopus/internal/db"
	"github.com/lingyuins/octopus/internal/model"
	"github.com/lingyuins/octopus/internal/op"
	"github.com/lingyuins/octopus/internal/transformer/outbound"
	"github.com/lingyuins/octopus/internal/utils/log"
)

const planChannelGroupName = "Plan"

// ListProviders 列出所有 Plan Provider
func ListProviders(ctx context.Context, providerType model.PlanProviderType) ([]model.PlanProviderListItem, error) {
	var providers []model.PlanProvider
	if err := db.GetDB().WithContext(ctx).
		Where("provider_type = ?", providerType).
		Order("id ASC").
		Find(&providers).Error; err != nil {
		return nil, fmt.Errorf("list plan providers: %w", err)
	}

	result := make([]model.PlanProviderListItem, 0, len(providers))
	for _, p := range providers {
		item := model.PlanProviderListItem{PlanProvider: p}
		item.APIKey = ""
		item.ForwardAPIKey = ""
		if p.ChannelID > 0 {
			channel, err := op.ChannelGet(p.ChannelID, ctx)
			if err == nil {
				item.Models = channel.Model
				item.ChannelName = channel.Name
				item.ChannelEnabled = channel.Enabled
			}
		}
		result = append(result, item)
	}
	return result, nil
}

// AddProvider 添加 Plan Provider：查询额度 → （可选）创建/复用 Channel 并归入渠道 Plan 分组
//
// apiKey 是主凭据（balance 类厂商的 sk- key，或 stepfun_plan 的 Oasis-Token）。
// forwardAPIKey 仅 stepfun_plan 使用：可选的 sk- API Key，用于转发。
//   - 填了：创建或复用接入点 api.stepfun.com/step_plan/v1 的渠道，key 追加到模型相同的已有渠道。
//   - 不填：仅监控套餐额度，不创建渠道。
func AddProvider(ctx context.Context, category model.PlanProviderCategory, apiKey, forwardAPIKey, customName string) (*model.PlanProvider, error) {
	info := getCategoryInfo(category)
	if info == nil {
		return nil, fmt.Errorf("unknown plan provider category: %s", category)
	}

	apiKey = strings.TrimSpace(apiKey)
	if apiKey == "" {
		return nil, fmt.Errorf("API key is required")
	}

	forwardAPIKey = normalizePlanForwardAPIKey(category, strings.TrimSpace(forwardAPIKey))

	name := customName
	if name == "" {
		name = info.Name
	}

	// 1. 查询余额 / TokenPlan
	var balance, balanceUsed float64
	var quotaTotal, quotaUsed, weeklyTotal, weeklyUsed float64
	var fiveHourTotal, fiveHourUsed float64
	var quotaResetAt, weeklyResetAt, fiveHourResetAt *string

	if info.Type == model.PlanProviderTypeBalance {
		result, err := QueryBalance(ctx, category, apiKey, info.BaseURL)
		if err != nil {
			return nil, fmt.Errorf("query balance: %w", err)
		}
		balance = result.Balance
		balanceUsed = result.BalanceUsed
	} else {
		result, err := QueryTokenPlan(ctx, category, apiKey, info.BaseURL)
		if err != nil {
			return nil, fmt.Errorf("query tokenplan: %w", err)
		}
		quotaTotal = result.QuotaTotal
		quotaUsed = result.QuotaUsed
		if result.QuotaResetAt != nil {
			s := result.QuotaResetAt.Format("2006-01-02 15:04:05")
			quotaResetAt = &s
		}
		weeklyTotal = result.WeeklyTotal
		weeklyUsed = result.WeeklyUsed
		if result.WeeklyResetAt != nil {
			s := result.WeeklyResetAt.Format("2006-01-02 15:04:05")
			weeklyResetAt = &s
		}
		fiveHourTotal = result.FiveHourTotal
		fiveHourUsed = result.FiveHourUsed
		if result.FiveHourResetAt != nil {
			s := result.FiveHourResetAt.Format("2006-01-02 15:04:05")
			fiveHourResetAt = &s
		}
	}

	// 2. 渠道创建/复用
	var channelID int
	needCreateChannel := info.Type == model.PlanProviderTypeBalance ||
		(isConsoleTokenPlanCategory(category) && forwardAPIKey != "") ||
		category == model.PlanProviderCodex
	if needCreateChannel {
		channelGroupID, err := ensurePlanChannelGroup(ctx)
		if err != nil {
			return nil, fmt.Errorf("ensure plan channel group: %w", err)
		}

		// 渠道接入点与凭据：balance 类用 info.BaseURL + apiKey；
		// 控制台 token plan 类用各自的转发 API 地址 + forwardAPIKey；
		// Codex 用 info.BaseURL + apiKey（OAuth JSON），channel type 为 Codex。
		channelBaseURL := info.BaseURL
		channelModel := info.Models
		channelKey := apiKey
		channelName := fmt.Sprintf("[%s] %s", info.Name, name)
		if customName != "" {
			channelName = fmt.Sprintf("[%s] %s", info.Name, customName)
		}
		channelType := outbound.OutboundTypeOpenAIChat
		if isConsoleTokenPlanCategory(category) {
			channelBaseURL = planForwardAPIBaseURL(category)
			channelKey = forwardAPIKey
			channelName = fmt.Sprintf("[%s] %s", planForwardLabel(category), name)
		}
		if category == model.PlanProviderCodex {
			channelType = outbound.OutboundTypeCodex
			channelKey = apiKey // OAuth JSON, same as monitoring credential
		}

		// 查找可复用的已有渠道（同 category + 接入点 + 模型相同的渠道）
		reuseChannelID := findReusablePlanChannel(ctx, category, channelBaseURL, channelModel)

		if reuseChannelID > 0 {
			// 复用：追加 key 到已有渠道
			addReq := &model.ChannelUpdateRequest{
				ID: reuseChannelID,
				KeysToAdd: []model.ChannelKeyAddRequest{
					{Enabled: true, ChannelKey: channelKey},
				},
			}
			if _, err := op.ChannelUpdate(addReq, ctx); err != nil {
				return nil, fmt.Errorf("reuse channel add key: %w", err)
			}
			channelID = reuseChannelID
		} else {
			// 新建渠道
			channel := &model.Channel{
				Name:    channelName,
				GroupID: channelGroupID,
				Type:    channelType,
				Enabled: true,
				BaseUrls: []model.BaseUrl{
					{URL: channelBaseURL, Delay: 0},
				},
				Keys: []model.ChannelKey{
					{Enabled: true, ChannelKey: channelKey},
				},
				Model:     channelModel,
				AutoSync:  false,
				AutoGroup: model.AutoGroupTypeNone,
			}
			if err := op.ChannelCreate(channel, ctx); err != nil {
				return nil, fmt.Errorf("create channel: %w", err)
			}
			channelID = channel.ID
		}
	}

	// 3. 持久化 PlanProvider
	now := time.Now()
	provider := &model.PlanProvider{
		Name:          name,
		Category:      category,
		ProviderType:  info.Type,
		APIKey:        apiKey,
		ForwardAPIKey: forwardAPIKey,
		BaseURL:       info.BaseURL,
		ChannelID:     channelID,
		Balance:       balance,
		BalanceUsed:   balanceUsed,
		QuotaTotal:    quotaTotal,
		QuotaUsed:     quotaUsed,
		WeeklyTotal:   weeklyTotal,
		WeeklyUsed:    weeklyUsed,
		FiveHourTotal: fiveHourTotal,
		FiveHourUsed:  fiveHourUsed,
	}

	if quotaResetAt != nil {
		if t, err := time.Parse("2006-01-02 15:04:05", *quotaResetAt); err == nil {
			provider.QuotaResetAt = &t
		}
	}
	if weeklyResetAt != nil {
		if t, err := time.Parse("2006-01-02 15:04:05", *weeklyResetAt); err == nil {
			provider.WeeklyResetAt = &t
		}
	}
	if fiveHourResetAt != nil {
		if t, err := time.Parse("2006-01-02 15:04:05", *fiveHourResetAt); err == nil {
			provider.FiveHourResetAt = &t
		}
	}

	provider.LastRefresh = &now
	provider.CreatedAt = now
	provider.UpdatedAt = now

	if err := db.GetDB().WithContext(ctx).Create(provider).Error; err != nil {
		// 补偿：provider 落盘失败，回滚已创建的 channel（仅新建的，复用的不删）
		if channelID > 0 && needCreateChannel {
			// 只在新建渠道时补偿；复用渠道追加的 key 留在渠道上（无害，用户可手动清理）
			if !isReusedChannel(ctx, channelID) {
				if delErr := op.ChannelDel(channelID, ctx); delErr != nil {
					log.Warnf("planprovider: compensate delete channel %d after provider create failed: %v", channelID, delErr)
				}
			}
		}
		return nil, fmt.Errorf("create plan provider record: %w", err)
	}

	return provider, nil
}

// RefreshProvider 刷新 Plan Provider 的余额 / TokenPlan
func RefreshProvider(ctx context.Context, id int) (*model.PlanProvider, error) {
	var provider model.PlanProvider
	if err := db.GetDB().WithContext(ctx).First(&provider, id).Error; err != nil {
		return nil, fmt.Errorf("find plan provider: %w", err)
	}

	if provider.ProviderType == model.PlanProviderTypeBalance {
		result, err := QueryBalance(ctx, provider.Category, provider.APIKey, provider.BaseURL)
		if err != nil {
			return nil, fmt.Errorf("refresh balance: %w", err)
		}
		provider.Balance = result.Balance
		provider.BalanceUsed = result.BalanceUsed
	} else {
		result, err := QueryTokenPlan(ctx, provider.Category, provider.APIKey, provider.BaseURL)
		if err != nil {
			return nil, fmt.Errorf("refresh tokenplan: %w", err)
		}
		provider.QuotaTotal = result.QuotaTotal
		provider.QuotaUsed = result.QuotaUsed
		if result.QuotaResetAt != nil {
			provider.QuotaResetAt = result.QuotaResetAt
		}
		provider.WeeklyTotal = result.WeeklyTotal
		provider.WeeklyUsed = result.WeeklyUsed
		if result.WeeklyResetAt != nil {
			provider.WeeklyResetAt = result.WeeklyResetAt
		}
		provider.FiveHourTotal = result.FiveHourTotal
		provider.FiveHourUsed = result.FiveHourUsed
		provider.FiveHourResetAt = result.FiveHourResetAt
	}

	now := time.Now()
	provider.LastRefresh = &now
	provider.UpdatedAt = now

	if err := db.GetDB().WithContext(ctx).Save(&provider).Error; err != nil {
		return nil, fmt.Errorf("save plan provider: %w", err)
	}

	return &provider, nil
}

// DeleteProvider 删除 Plan Provider，同时删除关联的 Channel。
//
// 顺序：先删 provider 记录，再用 op.ChannelDel 清理 channel 及其依赖
// （GroupItems / ChannelKeys / StatsChannel + chCache / keyCache / stats cache + Redis scope）。
// op.ChannelDel 是 channel 删除的统一入口，内部事务性删除并触发 cache 失效，
// 不能用裸 DB 删除替代——否则会留下 chCache/keyCache/stats cache 残留，且非事务性
// （见 issue #126 修复项；对比 task/channel_expire.go 同样使用 ch.Delete + OnChannelDeleted）。
//
// 若 provider 删除成功而 channel 删除失败：provider 已不存在，留下一个 channel 孤儿，
// 用户可在渠道管理页手动删除；这比反过来（provider 指向不存在的 channel）更易恢复。
func DeleteProvider(ctx context.Context, id int) error {
	var provider model.PlanProvider
	if err := db.GetDB().WithContext(ctx).First(&provider, id).Error; err != nil {
		return fmt.Errorf("find plan provider: %w", err)
	}

	// 先删 provider 记录。
	if err := db.GetDB().WithContext(ctx).Delete(&provider).Error; err != nil {
		return fmt.Errorf("delete plan provider: %w", err)
	}

	// 再清理关联的 channel（含 group items / channel keys / stats channel / cache）。
	if provider.ChannelID > 0 {
		if err := op.ChannelDel(provider.ChannelID, ctx); err != nil {
			// channel 可能已被手动删除（channel.Get 失败），或删除过程出错。
			// provider 记录已删，记录日志但不阻塞返回——避免调用方卡在
			// "channel 不存在"的边缘 case 下无法重试（重试时 provider 已不存在）。
			log.Warnf("planprovider: delete channel %d for provider %d failed: %v (provider record already deleted)", provider.ChannelID, id, err)
		}
	}

	return nil
}

// GetCategories 获取所有支持的厂商分类
func GetCategories(providerType model.PlanProviderType) []model.PlanProviderCategoryInfo {
	result := make([]model.PlanProviderCategoryInfo, 0)
	for _, c := range model.PlanProviderCategories {
		if c.Type == providerType {
			result = append(result, c)
		}
	}
	return result
}

// --- internal helpers ---

func getCategoryInfo(category model.PlanProviderCategory) *model.PlanProviderCategoryInfo {
	for _, c := range model.PlanProviderCategories {
		if c.Category == category {
			return &c
		}
	}
	return nil
}

// ensurePlanChannelGroup 确保名为 Plan 的渠道分组（ChannelGroup）存在，返回其 ID。
// 与 ensurePlanGroup 不同：后者操作路由分组（Group 表），本函数操作渠道分组
// （ChannelGroup 表），用于 channel.GroupID 字段的归属。
func ensurePlanChannelGroup(ctx context.Context) (int, error) {
	groups, err := op.ChannelGroupList(ctx)
	if err != nil {
		return 0, fmt.Errorf("list channel groups: %w", err)
	}
	for _, g := range groups {
		if g.Name == planChannelGroupName {
			return g.ID, nil
		}
	}
	newGroup, err := op.ChannelGroupCreate(planChannelGroupName, ctx)
	if err != nil {
		return 0, fmt.Errorf("create plan channel group: %w", err)
	}
	return newGroup.ID, nil
}

// stepFunPlanAPIBaseURL 是 StepFun 套餐转发的 API 接入点。
const stepFunPlanAPIBaseURL = "https://api.stepfun.com/step_plan/v1"

// senseNovaPlanAPIBaseURL 是 SenseNova 套餐转发的 API 接入点。
const senseNovaPlanAPIBaseURL = "https://token.sensenova.cn/v1"

// bailianPlanAPIBaseURL 是百炼 Token Plan 转发的 API 接入点。
const bailianPlanAPIBaseURL = "https://token-plan.cn-beijing.maas.aliyuncs.com/compatible-mode/v1"

// volcenginePlanAPIBaseURL 是火山方舟 Agent Plan 转发的 API 接入点（OpenAI 兼容）。
const volcenginePlanAPIBaseURL = "https://ark.cn-beijing.volces.com/api/plan/v3"

func normalizePlanForwardAPIKey(category model.PlanProviderCategory, forwardAPIKey string) string {
	if !isConsoleTokenPlanCategory(category) {
		return ""
	}
	return forwardAPIKey
}

// isConsoleTokenPlanCategory 判断是否为"控制台 token plan"类厂商
// （使用控制台会话 token 查套餐、可选 sk- key 创建转发渠道的厂商）。
func isConsoleTokenPlanCategory(category model.PlanProviderCategory) bool {
	return category == model.PlanProviderStepFunPlan || category == model.PlanProviderSenseNovaPlan || category == model.PlanProviderBailianPlan || category == model.PlanProviderVolcenginePlan
}

// planForwardAPIBaseURL 返回控制台 token plan 类厂商的转发 API 接入点。
func planForwardAPIBaseURL(category model.PlanProviderCategory) string {
	switch category {
	case model.PlanProviderStepFunPlan:
		return stepFunPlanAPIBaseURL
	case model.PlanProviderSenseNovaPlan:
		return senseNovaPlanAPIBaseURL
	case model.PlanProviderBailianPlan:
		return bailianPlanAPIBaseURL
	case model.PlanProviderVolcenginePlan:
		return volcenginePlanAPIBaseURL
	default:
		return ""
	}
}

// planForwardLabel 返回控制台 token plan 类厂商的渠道名前缀标签。
func planForwardLabel(category model.PlanProviderCategory) string {
	switch category {
	case model.PlanProviderStepFunPlan:
		return "StepFun Plan"
	case model.PlanProviderSenseNovaPlan:
		return "SenseNova Plan"
	case model.PlanProviderBailianPlan:
		return "Bailian Plan"
	case model.PlanProviderVolcenginePlan:
		return "Volcengine Plan"
	default:
		return "Plan"
	}
}

// findReusablePlanChannel 查找可复用的 Plan 渠道。
// 条件：已有同 category 的 PlanProvider 记录，且其关联渠道的接入点和模型列表均相同。
// 返回渠道 ID，未找到返回 0。
func findReusablePlanChannel(ctx context.Context, category model.PlanProviderCategory, baseURL, modelList string) int {
	var providers []model.PlanProvider
	if err := db.GetDB().WithContext(ctx).
		Where("category = ? AND channel_id > 0", category).
		Find(&providers).Error; err != nil {
		return 0
	}
	normalizedBaseURL := strings.TrimRight(strings.TrimSpace(baseURL), "/")
	for _, p := range providers {
		ch, err := op.ChannelGet(p.ChannelID, ctx)
		if err != nil || ch == nil {
			continue
		}
		if len(ch.BaseUrls) == 0 {
			continue
		}
		chBaseURL := strings.TrimRight(strings.TrimSpace(ch.BaseUrls[0].URL), "/")
		if chBaseURL != normalizedBaseURL {
			continue
		}
		// 模型列表相同则复用（模型相同的合并）
		if normalizeModelList(ch.Model) == normalizeModelList(modelList) {
			return ch.ID
		}
	}
	return 0
}

// isReusedChannel 判断渠道是否是已有 provider 关联的复用渠道（非本次新建）。
// 通过查 PlanProvider 表看是否有其他 provider 也指向该渠道。
func isReusedChannel(ctx context.Context, channelID int) bool {
	var count int64
	db.GetDB().WithContext(ctx).Model(&model.PlanProvider{}).
		Where("channel_id = ?", channelID).Count(&count)
	return count > 0
}

// normalizeModelList 规范化模型列表用于比较：去空格、去重、排序后拼接。
func normalizeModelList(modelList string) string {
	parts := strings.Split(modelList, ",")
	seen := make(map[string]struct{})
	var result []string
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		if _, ok := seen[p]; ok {
			continue
		}
		seen[p] = struct{}{}
		result = append(result, p)
	}
	// 排序保证顺序无关
	for i := 0; i < len(result); i++ {
		for j := i + 1; j < len(result); j++ {
			if result[i] > result[j] {
				result[i], result[j] = result[j], result[i]
			}
		}
	}
	return strings.Join(result, ",")
}
