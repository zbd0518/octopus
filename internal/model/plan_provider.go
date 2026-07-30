package model

import "time"

// PlanProviderType 区分额度查询和套餐查询两类厂商
type PlanProviderType string

const (
	PlanProviderTypeBalance   PlanProviderType = "balance"
	PlanProviderTypeTokenPlan PlanProviderType = "tokenplan"
)

// PlanProviderCategory 厂商具体分类
type PlanProviderCategory string

const (
	// 额度类厂商
	PlanProviderDeepSeek    PlanProviderCategory = "deepseek"
	PlanProviderKimi        PlanProviderCategory = "kimi"
	PlanProviderSiliconFlow PlanProviderCategory = "siliconflow"
	PlanProviderOpenRouter  PlanProviderCategory = "openrouter"
	PlanProviderStepFun     PlanProviderCategory = "stepfun"
	PlanProvider302AI       PlanProviderCategory = "302ai"
	PlanProviderNovita      PlanProviderCategory = "novita"
	PlanProviderOpenAI      PlanProviderCategory = "openai"

	// TokenPlan 类厂商
	PlanProviderMiniMax        PlanProviderCategory = "minimax"
	PlanProviderZhipu          PlanProviderCategory = "zhipu"
	PlanProviderStepFunPlan    PlanProviderCategory = "stepfun_plan"
	PlanProviderSenseNovaPlan  PlanProviderCategory = "sensenova_plan"
	PlanProviderMiMoPlan       PlanProviderCategory = "mimo_plan"
	PlanProviderCodex          PlanProviderCategory = "codex"
	PlanProviderBailianPlan    PlanProviderCategory = "bailian_plan"
	PlanProviderVolcenginePlan PlanProviderCategory = "volcengine_plan"
	// Kimi For Coding 套餐（区别于 Kimi 余额查询）
	PlanProviderKimiPlan PlanProviderCategory = "kimi_plan"
	// 智谱 GLM 团队套餐（Team Plan，需组织/项目 ID）
	PlanProviderZhipuTeam PlanProviderCategory = "zhipu_team"
	// 火山方舟 Agent Plan（AK/SK 签名方式，区别于 Cookie+CSRF 的 volcengine_plan）
	PlanProviderVolcenginePlanAK PlanProviderCategory = "volcengine_plan_ak"
)

// PlanProviderCategoryInfo 厂商元信息（非 DB 字段）
type PlanProviderCategoryInfo struct {
	Category    PlanProviderCategory `json:"category"`
	Name        string               `json:"name"`
	Type        PlanProviderType     `json:"type"`
	BaseURL     string               `json:"base_url"`
	Models      string               `json:"models"` // 默认模型列表（逗号分隔）
	Description string               `json:"description"`
	HelpURL     string               `json:"help_url"`
}

// PlanProviderCategories 所有支持的厂商
var PlanProviderCategories = []PlanProviderCategoryInfo{
	{
		Category:    PlanProviderDeepSeek,
		Name:        "DeepSeek",
		Type:        PlanProviderTypeBalance,
		BaseURL:     "https://api.deepseek.com/v1",
		Models:      "deepseek-chat,deepseek-reasoner",
		Description: "DeepSeek 官方 API 余额查询",
		HelpURL:     "https://platform.deepseek.com/api_keys",
	},
	{
		Category:    PlanProviderKimi,
		Name:        "Kimi (月之暗面)",
		Type:        PlanProviderTypeBalance,
		BaseURL:     "https://api.moonshot.cn/v1",
		Models:      "moonshot-v1-8k,moonshot-v1-32k,moonshot-v1-128k,kimi-latest",
		Description: "Kimi API 余额查询，支持 Coding Plan",
		HelpURL:     "https://platform.moonshot.cn/console/api-keys",
	},
	{
		Category:    PlanProviderSiliconFlow,
		Name:        "SiliconFlow (硅基流动)",
		Type:        PlanProviderTypeBalance,
		BaseURL:     "https://api.siliconflow.com/v1",
		Models:      "*",
		Description: "硅基流动 API 余额查询",
		HelpURL:     "https://cloud.siliconflow.cn/account/ak",
	},
	{
		Category:    PlanProviderOpenRouter,
		Name:        "OpenRouter",
		Type:        PlanProviderTypeBalance,
		BaseURL:     "https://openrouter.ai/api/v1",
		Models:      "*",
		Description: "OpenRouter Credits 余额查询",
		HelpURL:     "https://openrouter.ai/keys",
	},
	{
		Category:    PlanProviderStepFun,
		Name:        "StepFun (阶跃星辰)",
		Type:        PlanProviderTypeBalance,
		BaseURL:     "https://api.stepfun.ai/v1",
		Models:      "step-3.7-flash,step-3.7,step-3.7-pro",
		Description: "阶跃星辰 API 余额查询",
		HelpURL:     "https://platform.stepfun.com/console/apikey",
	},
	{
		Category:    PlanProvider302AI,
		Name:        "302.ai",
		Type:        PlanProviderTypeBalance,
		BaseURL:     "https://api.302.ai/v1",
		Models:      "*",
		Description: "302.ai 中转站余额查询",
		HelpURL:     "https://dashboard.302.ai",
	},
	{
		Category:    PlanProviderNovita,
		Name:        "Novita AI",
		Type:        PlanProviderTypeBalance,
		BaseURL:     "https://api.novita.ai/v1",
		Models:      "*",
		Description: "Novita AI 余额查询（1 unit = 0.0001 USD）",
		HelpURL:     "https://novita.ai/dashboard/keys",
	},
	{
		Category:    PlanProviderOpenAI,
		Name:        "OpenAI",
		Type:        PlanProviderTypeBalance,
		BaseURL:     "https://api.openai.com/v1",
		Models:      "gpt-4.1,gpt-4.1-mini,gpt-4o,gpt-4o-mini,o3,o4-mini",
		Description: "OpenAI 余额查询（部分账户可用 /v1/balances 接口）",
		HelpURL:     "https://platform.openai.com/api-keys",
	},
	{
		Category:    PlanProviderMiniMax,
		Name:        "MiniMax Token Plan",
		Type:        PlanProviderTypeTokenPlan,
		BaseURL:     "https://www.minimaxi.com/v1",
		Models:      "MiniMax-M3,MiniMax-M2.7,MiniMax-M2.7-highspeed",
		Description: "MiniMax Token Plan 套餐用量查询（含日/周额度）",
		HelpURL:     "https://platform.minimaxi.com/user-center/basic-information/interface-key",
	},
	{
		Category:    PlanProviderZhipu,
		Name:        "智谱 GLM Coding Plan",
		Type:        PlanProviderTypeTokenPlan,
		BaseURL:     "https://open.bigmodel.cn/api/paas/v4",
		Models:      "GLM-5.2,GLM-5.1,GLM-5,GLM-5-Turbo",
		Description: "智谱 GLM Coding Plan 套餐用量查询（含日/月/季/年额度）",
		HelpURL:     "https://open.bigmodel.cn/usercenter/apikeys",
	},
	{
		Category:    PlanProviderStepFunPlan,
		Name:        "StepFun 套餐 (阶跃星辰)",
		Type:        PlanProviderTypeTokenPlan,
		BaseURL:     "https://platform.stepfun.com",
		Models:      "step-3.7-flash,step-3.7,step-3.7-pro",
		Description: "StepFun 套餐额度查询（Oasis-Token 必填，约 30 分钟有效期）。可选填 API Key 自动创建/复用转发渠道（接入点 api.stepfun.com/step_plan/v1）",
		HelpURL:     "https://platform.stepfun.com/plan-subscribe",
	},
	{
		Category:    PlanProviderSenseNovaPlan,
		Name:        "SenseNova 套餐 (商汤日日新)",
		Type:        PlanProviderTypeTokenPlan,
		BaseURL:     "https://platform.sensenova.cn",
		Models:      "sensenova-6.7-flash-lite,sensenova-u1-fast,deepseek-v4-flash",
		Description: "SenseNova Coding Plan 套餐用量查询（控制台 Bearer Token 必填，约 3 小时有效期）。可选填 API Key 自动创建/复用转发渠道（接入点 token.sensenova.cn/v1）",
		HelpURL:     "https://platform.sensenova.cn/console",
	},
	{
		Category:    PlanProviderMiMoPlan,
		Name:        "MiMo 套餐 (小米)",
		Type:        PlanProviderTypeTokenPlan,
		BaseURL:     "https://platform.xiaomimimo.com",
		Models:      "MiMo-V2-Pro,MiMo-V2-Flash,MiMo-V2-Edge",
		Description: "小米 MiMo Token Plan 套餐用量查询。支持两种鉴权方式：① passToken（小米账号 SSO Token，有效期 30 天滚动刷新，可自动刷新 serviceToken，但安全风险——passToken 可能存在横向移动风险，未进行全方面测试，请自行判断是否需要使用）；② serviceToken（仅 MiMo 平台 Token，有效期约 1 天，过期需手动更新）。在浏览器登录 platform.xiaomimimo.com 后，按 F12 → Application → Cookies 复制。",
		HelpURL:     "https://platform.xiaomimimo.com/console/plan-manage",
	},
	{
		Category:    PlanProviderCodex,
		Name:        "ChatGPT Codex 套餐",
		Type:        PlanProviderTypeTokenPlan,
		BaseURL:     "https://chatgpt.com",
		Models:      "gpt-5,gpt-5-codex,gpt-5.1,gpt-5.1-codex,gpt-5.2,gpt-5.2-codex",
		Description: "ChatGPT Codex 套餐用量查询（WHAM API）+ 自动创建 Codex 转发渠道。需填入 OAuth JSON 凭据（含 access_token 和 account_id），从 ChatGPT 订阅账号获取。系统将自动创建 Codex 类型渠道（接入点 chatgpt.com/backend-api/codex/responses）。access_token 有效期较短，过期后需重新获取。",
		HelpURL:     "https://chatgpt.com",
	},
	{
		Category:    PlanProviderBailianPlan,
		Name:        "百炼 Token Plan (阿里云)",
		Type:        PlanProviderTypeTokenPlan,
		BaseURL:     "https://bailian.console.aliyun.com",
		Models:      "qwen3.8-max-preview,qwen3.7-max,qwen3.7-plus,qwen3.6-flash,glm-5.2,deepseek-v4-pro",
		Description: "阿里云百炼 Token Plan 套餐用量查询（控制台 Cookie 鉴权）。在浏览器登录 bailian.console.aliyun.com 后，按 F12 → Application → Cookies 复制完整 Cookie。可选填 API Key（sk-sp-...）自动创建转发渠道（接入点 token-plan.cn-beijing.maas.aliyuncs.com/compatible-mode/v1）",
		HelpURL:     "https://bailian.console.aliyun.com/cn-beijing?tab=plan#/efm/subscription/token-plan/personal",
	},
	{
		Category:    PlanProviderVolcenginePlan,
		Name:        "火山方舟 Agent Plan",
		Type:        PlanProviderTypeTokenPlan,
		BaseURL:     "https://console.volcengine.com",
		Models:      "auto,doubao-seed-evolving,doubao-seed-2-1-turbo,kimi-k3",
		Description: "火山方舟 Agent Plan 套餐用量查询（控制台 Cookie + CSRF Token 鉴权）。在浏览器登录 console.volcengine.com/ark 后，按 F12 → Network 找任意 plan 接口，复制完整 Cookie 和 x-csrf-token 请求头，用 `Cookie值|||x-csrf-token值` 格式填入。可选填 API Key（ark-...）自动创建转发渠道（接入点 ark.cn-beijing.volces.com/api/plan/v3）",
		HelpURL:     "https://console.volcengine.com/ark/region:cn-beijing/subscription/agent-plan",
	},
	{
		Category:    PlanProviderKimiPlan,
		Name:        "Kimi For Coding 套餐",
		Type:        PlanProviderTypeTokenPlan,
		BaseURL:     "https://api.kimi.com/coding/v1",
		Models:      "kimi-latest",
		Description: "Kimi For Coding 套餐用量查询（5 小时/周窗口）。使用 Kimi API Key（sk- 开头），与余额查询的 Moonshot API 共用同一把 Key。",
		HelpURL:     "https://platform.moonshot.cn/console/api-keys",
	},
	{
		Category:    PlanProviderZhipuTeam,
		Name:        "智谱 GLM 团队套餐 (Team Plan)",
		Type:        PlanProviderTypeTokenPlan,
		BaseURL:     "https://open.bigmodel.cn/api/paas/v4",
		Models:      "GLM-5.2,GLM-5.1,GLM-5,GLM-5-Turbo",
		Description: "智谱 GLM Team Plan 套餐用量查询（5 小时/周窗口）。需填入团队 API Key、组织 ID、项目 ID（三者缺一不可）。响应格式与个人版一致。",
		HelpURL:     "https://open.bigmodel.cn/console/organization",
	},
	{
		Category:    PlanProviderVolcenginePlanAK,
		Name:        "火山方舟 Agent Plan (AK/SK)",
		Type:        PlanProviderTypeTokenPlan,
		BaseURL:     "https://ark.cn-beijing.volces.com/api/coding/v3",
		Models:      "auto,doubao-seed-evolving,doubao-seed-2-1-turbo,kimi-k3",
		Description: "火山方舟 Agent Plan 用量查询（AK/SK 签名方式）。需填入火山引擎账号的 AccessKey ID 和 Secret（与推理 API Key 是两套凭据），格式 `AK值|||SK值`。签名通过后先查 Agent Plan（GetAFPUsage），无订阅再查 Coding Plan（GetCodingPlanUsage）。可选填 API Key（ark-...）自动创建转发渠道（接入点 ark.cn-beijing.volces.com/api/plan/v3）。",
		HelpURL:     "https://console.volcengine.com/iam/identitymanage/key",
	},
}

// PlanProvider 持久化的 Plan Provider 记录
type PlanProvider struct {
	ID            int                  `json:"id" gorm:"primaryKey"`
	Name          string               `json:"name" gorm:"not null"`
	Category      PlanProviderCategory `json:"category" gorm:"type:varchar(32);not null;index"`
	ProviderType  PlanProviderType     `json:"provider_type" gorm:"type:varchar(16);not null;default:'balance'"`
	APIKey        string               `json:"api_key" gorm:"not null"`
	ForwardAPIKey string               `json:"forward_api_key" gorm:"default:''"`
	// TeamOrganizationID / TeamProjectID 仅智谱团队版（zhipu_team）使用：
	// 请求头 bigmodel-organization / bigmodel-project，与 API Key 三者配对。
	TeamOrganizationID string `json:"team_organization_id" gorm:"default:''"`
	TeamProjectID      string `json:"team_project_id" gorm:"default:''"`
	BaseURL            string `json:"base_url" gorm:"not null"`
	ChannelID          int    `json:"channel_id" gorm:"not null;default:0;index"`
	// 代理配置：目前仅 Codex 类厂商（chatgpt.com 国内不可直连）使用，
	// 与 Channel/Site 的代理模型一致；其他厂商默认 direct。
	ProxyMode     ProxyUsageMode `json:"proxy_mode" gorm:"type:varchar(16);not null;default:'direct'"`
	ProxyConfigID *int           `json:"proxy_config_id"`
	Balance       float64        `json:"balance" gorm:"default:0"`
	BalanceUsed   float64        `json:"balance_used" gorm:"default:0"`
	// TokenPlan 专用
	QuotaTotal    float64    `json:"quota_total" gorm:"default:0"`
	QuotaUsed     float64    `json:"quota_used" gorm:"default:0"`
	QuotaResetAt  *time.Time `json:"quota_reset_at"`
	WeeklyTotal   float64    `json:"weekly_total" gorm:"default:0"`
	WeeklyUsed    float64    `json:"weekly_used" gorm:"default:0"`
	WeeklyResetAt *time.Time `json:"weekly_reset_at"`
	// FiveHour 档：仅部分厂商（如火山方舟 Agent Plan）提供 5 小时窗口配额
	FiveHourTotal   float64    `json:"five_hour_total" gorm:"default:0"`
	FiveHourUsed    float64    `json:"five_hour_used" gorm:"default:0"`
	FiveHourResetAt *time.Time `json:"five_hour_reset_at"`
	Status          string     `json:"status" gorm:"type:varchar(32);not null;default:'active'"`
	LastRefresh     *time.Time `json:"last_refresh"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
}

// PlanProviderListItem 列表响应
type PlanProviderListItem struct {
	PlanProvider
	Models         string `json:"models"`       // 从 Channel 继承的模型
	ChannelName    string `json:"channel_name"` // 关联渠道名称
	ChannelEnabled bool   `json:"channel_enabled"`
}
