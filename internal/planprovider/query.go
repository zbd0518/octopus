package planprovider

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/lingyuins/octopus/internal/model"
)

const requestTimeout = 15 * time.Second

// ProxyURLByConfigFunc 由 op 包在启动时注入（op/channel.go init），用于 pool 模式解析代理 URL。
// planprovider 不能直接 import op（op 反向依赖 helper，会形成循环），与 helper.ProxyURLByConfigFunc 同一模式。
// 未注入时 pool 模式查询返回错误。
var ProxyURLByConfigFunc func(id int, ctx context.Context) (string, error)

// planQueryHTTPClient 按代理模式构建套餐查询用 HTTP client。
//   - direct / 空：无代理（保持现有行为）；
//   - system：使用系统环境变量代理（http.ProxyFromEnvironment）；
//   - pool：通过 ProxyURLByConfigFunc 解析代理池配置 URL。
func planQueryHTTPClient(mode model.ProxyUsageMode, configID *int) (*http.Client, error) {
	switch mode {
	case model.ProxyUsageModeSystem:
		transport, ok := http.DefaultTransport.(*http.Transport)
		if !ok {
			return nil, fmt.Errorf("default transport is not *http.Transport")
		}
		cloned := transport.Clone()
		cloned.Proxy = http.ProxyFromEnvironment
		return &http.Client{Timeout: requestTimeout, Transport: cloned}, nil
	case model.ProxyUsageModePool:
		if configID == nil || *configID <= 0 {
			return nil, fmt.Errorf("proxy config id is required when proxy mode is pool")
		}
		if ProxyURLByConfigFunc == nil {
			return nil, fmt.Errorf("proxy configuration resolver is not initialized")
		}
		resolved, err := ProxyURLByConfigFunc(*configID, context.Background())
		if err != nil {
			return nil, err
		}
		proxyURL, err := url.Parse(resolved)
		if err != nil {
			return nil, fmt.Errorf("invalid proxy url: %w", err)
		}
		transport, ok := http.DefaultTransport.(*http.Transport)
		if !ok {
			return nil, fmt.Errorf("default transport is not *http.Transport")
		}
		cloned := transport.Clone()
		cloned.Proxy = http.ProxyURL(proxyURL)
		return &http.Client{Timeout: requestTimeout, Transport: cloned}, nil
	default:
		return &http.Client{Timeout: requestTimeout}, nil
	}
}

// BalanceResult 余额查询结果
type BalanceResult struct {
	Balance     float64 `json:"balance"`
	BalanceUsed float64 `json:"balance_used"`
	Currency    string  `json:"currency"`
}

// TokenPlanResult TokenPlan 查询结果
type TokenPlanResult struct {
	QuotaTotal    float64    `json:"quota_total"`
	QuotaUsed     float64    `json:"quota_used"`
	QuotaResetAt  *time.Time `json:"quota_reset_at"`
	WeeklyTotal   float64    `json:"weekly_total"`
	WeeklyUsed    float64    `json:"weekly_used"`
	WeeklyResetAt *time.Time `json:"weekly_reset_at"`
	// FiveHour 档：仅部分厂商（如火山方舟 Agent Plan）提供 5 小时窗口配额
	FiveHourTotal   float64    `json:"five_hour_total"`
	FiveHourUsed    float64    `json:"five_hour_used"`
	FiveHourResetAt *time.Time `json:"five_hour_reset_at"`
	// 各模型明细
	Models []TokenPlanModelUsage `json:"models,omitempty"`
}

// TokenPlanModelUsage 单个模型用量
type TokenPlanModelUsage struct {
	ModelName  string  `json:"model_name"`
	QuotaTotal float64 `json:"quota_total"`
	QuotaUsed  float64 `json:"quota_used"`
}

// QueryBalance 查询余额（额度类厂商）
func QueryBalance(ctx context.Context, category model.PlanProviderCategory, apiKey string, baseURL string) (*BalanceResult, error) {
	switch category {
	case model.PlanProviderDeepSeek:
		return queryDeepSeekBalance(ctx, apiKey)
	case model.PlanProviderKimi:
		return queryKimiBalance(ctx, apiKey)
	case model.PlanProviderSiliconFlow:
		return querySiliconFlowBalance(ctx, apiKey)
	case model.PlanProviderOpenRouter:
		return queryOpenRouterBalance(ctx, apiKey)
	case model.PlanProviderStepFun:
		return queryStepFunBalance(ctx, apiKey)
	case model.PlanProvider302AI:
		return query302AIBalance(ctx, apiKey)
	case model.PlanProviderNovita:
		return queryNovitaBalance(ctx, apiKey)
	case model.PlanProviderOpenAI:
		return queryOpenAIBalance(ctx, apiKey)
	default:
		return nil, fmt.Errorf("unsupported balance provider: %s", category)
	}
}

// QueryTokenPlan 查询套餐用量（TokenPlan 类厂商）
// proxyMode / proxyConfigID 目前仅 Codex 类使用（chatgpt.com 国内不可直连），其他厂商忽略。
func QueryTokenPlan(ctx context.Context, category model.PlanProviderCategory, apiKey string, baseURL string, proxyMode model.ProxyUsageMode, proxyConfigID *int) (*TokenPlanResult, error) {
	switch category {
	case model.PlanProviderMiniMax:
		return queryMiniMaxTokenPlan(ctx, apiKey)
	case model.PlanProviderSenseNovaPlan:
		return querySenseNovaPlanTokenPlan(ctx, apiKey)
	case model.PlanProviderStepFunPlan:
		return queryStepFunPlanTokenPlan(ctx, apiKey)
	case model.PlanProviderMiMoPlan:
		return queryMiMoPlanTokenPlan(ctx, apiKey)
	case model.PlanProviderZhipu:
		return queryZhipuTokenPlan(ctx, apiKey)
	case model.PlanProviderCodex:
		return queryCodexTokenPlan(ctx, apiKey, proxyMode, proxyConfigID)
	case model.PlanProviderBailianPlan:
		return queryBailianPlanTokenPlan(ctx, apiKey)
	case model.PlanProviderVolcenginePlan:
		return queryVolcenginePlanTokenPlan(ctx, apiKey)
	default:
		return nil, fmt.Errorf("unsupported tokenplan provider: %s", category)
	}
}

// --- DeepSeek ---

func queryDeepSeekBalance(ctx context.Context, apiKey string) (*BalanceResult, error) {
	body, err := doGet(ctx, "https://api.deepseek.com/user/balance", apiKey)
	if err != nil {
		return nil, err
	}

	var resp struct {
		IsAvailable  bool `json:"is_available"`
		BalanceInfos []struct {
			Currency        string `json:"currency"`
			TotalBalance    string `json:"total_balance"`
			GrantedBalance  string `json:"granted_balance"`
			ToppedUpBalance string `json:"topped_up_balance"`
		} `json:"balance_infos"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("deepseek: parse response: %w", err)
	}

	var total float64
	for _, info := range resp.BalanceInfos {
		if v := parseFloat(info.TotalBalance); v > 0 {
			total += v
		}
	}
	return &BalanceResult{Balance: total, BalanceUsed: 0, Currency: "CNY"}, nil
}

// --- Kimi ---

func queryKimiBalance(ctx context.Context, apiKey string) (*BalanceResult, error) {
	body, err := doGet(ctx, "https://api.moonshot.cn/v1/users/me/balance", apiKey)
	if err != nil {
		return nil, err
	}

	var resp struct {
		Code int `json:"code"`
		Data struct {
			AvailableBalance float64 `json:"available_balance"`
			VoucherBalance   float64 `json:"voucher_balance"`
			CashBalance      float64 `json:"cash_balance"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("kimi: parse response: %w", err)
	}
	if resp.Code != 0 {
		return nil, fmt.Errorf("kimi: API error code=%d", resp.Code)
	}

	return &BalanceResult{
		Balance:     resp.Data.AvailableBalance,
		BalanceUsed: 0,
		Currency:    "CNY",
	}, nil
}

// --- SiliconFlow ---

func querySiliconFlowBalance(ctx context.Context, apiKey string) (*BalanceResult, error) {
	body, err := doGet(ctx, "https://api.siliconflow.com/v1/user/info", apiKey)
	if err != nil {
		return nil, err
	}

	var resp struct {
		Code int  `json:"code"`
		Ok   bool `json:"ok"`
		Data struct {
			Balance       string `json:"balance"`
			ChargeBalance string `json:"chargeBalance"`
			TotalBalance  string `json:"totalBalance"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("siliconflow: parse response: %w", err)
	}
	if resp.Code != 20000 && !resp.Ok {
		return nil, fmt.Errorf("siliconflow: API error code=%d", resp.Code)
	}

	balance := parseFloat(resp.Data.TotalBalance)
	if balance == 0 {
		balance = parseFloat(resp.Data.Balance)
	}
	return &BalanceResult{Balance: balance, BalanceUsed: 0, Currency: "CNY"}, nil
}

// --- OpenRouter ---

func queryOpenRouterBalance(ctx context.Context, apiKey string) (*BalanceResult, error) {
	body, err := doGet(ctx, "https://openrouter.ai/api/v1/credits", apiKey)
	if err != nil {
		return nil, err
	}

	var resp struct {
		Data struct {
			TotalCredits float64 `json:"total_credits"`
			TotalUsage   float64 `json:"total_usage"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("openrouter: parse response: %w", err)
	}

	return &BalanceResult{
		Balance:     resp.Data.TotalCredits - resp.Data.TotalUsage,
		BalanceUsed: resp.Data.TotalUsage,
		Currency:    "USD",
	}, nil
}

// --- MiniMax Token Plan ---

func queryMiniMaxTokenPlan(ctx context.Context, apiKey string) (*TokenPlanResult, error) {
	body, err := doGet(ctx, "https://www.minimaxi.com/v1/api/openplatform/coding_plan/remains", apiKey)
	if err != nil {
		return nil, err
	}

	var resp struct {
		BaseResp struct {
			StatusCode int    `json:"status_code"`
			StatusMsg  string `json:"status_msg"`
		} `json:"base_resp"`
		StatusCode   int    `json:"status_code"`
		StatusMsg    string `json:"status_msg"`
		ModelRemains []struct {
			ModelName                 string  `json:"model_name"`
			StartTime                 int64   `json:"start_time"`
			EndTime                   int64   `json:"end_time"`
			RemainsTime               int64   `json:"remains_time"`
			CurrentIntervalTotalCount float64 `json:"current_interval_total_count"`
			CurrentIntervalUsageCount float64 `json:"current_interval_usage_count"`
			CurrentWeeklyTotalCount   float64 `json:"current_weekly_total_count"`
			CurrentWeeklyUsageCount   float64 `json:"current_weekly_usage_count"`
			WeeklyRemainsTime         int64   `json:"weekly_remains_time"`
		} `json:"model_remains"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("minimax: parse response: %w", err)
	}

	statusCode := resp.StatusCode
	if statusCode == 0 {
		statusCode = resp.BaseResp.StatusCode
	}
	if statusCode != 0 {
		statusMsg := resp.StatusMsg
		if statusMsg == "" {
			statusMsg = resp.BaseResp.StatusMsg
		}
		return nil, fmt.Errorf("minimax: API error code=%d msg=%s", statusCode, statusMsg)
	}

	result := &TokenPlanResult{}
	var models []TokenPlanModelUsage
	for _, m := range resp.ModelRemains {
		total := m.CurrentIntervalTotalCount
		usage := m.CurrentIntervalUsageCount
		// MiniMax 字段语义：total=该计费区间总额度，usage=已使用额度。
		// QuotaUsed 必须是"已使用"而非"剩余"（total-usage），否则前端用量百分比会被
		// 颠倒成剩余率（issue #126 修复项）。
		models = append(models, TokenPlanModelUsage{
			ModelName:  m.ModelName,
			QuotaTotal: total,
			QuotaUsed:  max(0, usage),
		})

		// 以第一个模型的数据作为汇总
		if result.QuotaTotal == 0 {
			result.QuotaTotal = total
			result.QuotaUsed = max(0, usage)
			if m.RemainsTime > 0 {
				t := time.Now().Add(time.Duration(m.RemainsTime) * time.Millisecond)
				result.QuotaResetAt = &t
			}
			if m.CurrentWeeklyTotalCount > 0 {
				result.WeeklyTotal = m.CurrentWeeklyTotalCount
				result.WeeklyUsed = max(0, m.CurrentWeeklyUsageCount)
				if m.WeeklyRemainsTime > 0 {
					t := time.Now().Add(time.Duration(m.WeeklyRemainsTime) * time.Millisecond)
					result.WeeklyResetAt = &t
				}
			}
		}
	}
	result.Models = models
	return result, nil
}

// --- 智谱 GLM Coding Plan ---

func queryZhipuTokenPlan(ctx context.Context, apiKey string) (*TokenPlanResult, error) {
	body, err := doGet(ctx, "https://open.bigmodel.cn/api/monitor/usage/quota/limit", apiKey)
	if err != nil {
		return nil, err
	}

	var resp struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
		Data struct {
			Limits []struct {
				ResourceType string  `json:"resource_type"`
				LimitPeriod  string  `json:"limit_period"`
				LimitValue   float64 `json:"limit_value"`
				UsedValue    float64 `json:"used_value"`
				RemainValue  float64 `json:"remain_value"`
				ResetTime    string  `json:"reset_time"`
			} `json:"limits"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("zhipu: parse response: %w", err)
	}
	if resp.Code != 200 {
		return nil, fmt.Errorf("zhipu: API error code=%d msg=%s", resp.Code, resp.Msg)
	}

	result := &TokenPlanResult{}
	for _, limit := range resp.Data.Limits {
		switch {
		case limit.ResourceType == "TOKENS_LIMIT" && (limit.LimitPeriod == "MONTH" || limit.LimitPeriod == "QUARTER" || limit.LimitPeriod == "YEAR"):
			result.QuotaTotal = limit.LimitValue
			result.QuotaUsed = limit.UsedValue
			if limit.ResetTime != "" {
				if t, err := time.Parse(time.RFC3339, limit.ResetTime); err == nil {
					result.QuotaResetAt = &t
				}
			}
		case limit.ResourceType == "REQUESTS_LIMIT" && limit.LimitPeriod == "MONTH":
			if result.QuotaTotal == 0 {
				result.QuotaTotal = limit.LimitValue
				result.QuotaUsed = limit.UsedValue
			}
		case limit.ResourceType == "TIME_LIMIT" && limit.LimitPeriod == "DAY":
			result.WeeklyTotal = limit.LimitValue
			result.WeeklyUsed = limit.UsedValue
			if limit.ResetTime != "" {
				if t, err := time.Parse(time.RFC3339, limit.ResetTime); err == nil {
					result.WeeklyResetAt = &t
				}
			}
		}
	}
	return result, nil
}

// --- StepFun 阶跃星辰 ---

func queryStepFunBalance(ctx context.Context, apiKey string) (*BalanceResult, error) {
	body, err := doGet(ctx, "https://api.stepfun.ai/v1/accounts", apiKey)
	if err != nil {
		return nil, err
	}

	var resp struct {
		Object              string  `json:"object"`
		Type                string  `json:"type"`
		Balance             float64 `json:"balance"`
		TotalCashBalance    float64 `json:"total_cash_balance"`
		TotalVoucherBalance float64 `json:"total_voucher_balance"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("stepfun: parse response: %w", err)
	}
	if resp.Object == "" {
		return nil, fmt.Errorf("stepfun: unexpected response")
	}

	return &BalanceResult{Balance: resp.Balance, BalanceUsed: 0, Currency: "CNY"}, nil
}

// stepFunPlanURL 是 StepFun 控制台套餐用量查询端点。
// 提取为包级变量以便测试覆盖（httptest mock server）。
var stepFunPlanURL = "https://platform.stepfun.com/api/step.openapi.devcenter.Dashboard/QueryStepPlanRateLimit"

// --- StepFun Plan 套餐用量（控制台 Oasis-Token 鉴权）---
//
// StepFun 控制台的套餐用量查询与 OpenAI 兼容 API 完全不同：
//   - 域名：platform.stepfun.com（控制台），非 api.stepfun.ai（API）
//   - 凭据：Oasis-Token（access_jwt...refresh_jwt 格式），非 sk- API key
//   - 鉴权：Cookie + oasis-appid header，非 Bearer auth
//   - access token 仅约 30 分钟有效，过期后查询失败需用户重新获取
//
// Oasis-Webid（device_id）从 refresh token payload 自动解码，用户只需填 Oasis-Token。
func queryStepFunPlanTokenPlan(ctx context.Context, oasisToken string) (*TokenPlanResult, error) {
	if oasisToken == "" {
		return nil, fmt.Errorf("stepfun_plan: oasis token is required")
	}

	// 从 refresh token（Oasis-Token 的 ... 后半段）解码 device_id 作为 Oasis-Webid
	webid := decodeStepFunWebID(oasisToken)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, stepFunPlanURL, strings.NewReader("{}"))
	if err != nil {
		return nil, fmt.Errorf("stepfun_plan: create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Connect-Protocol-Version", "1")
	req.Header.Set("Cookie", "Oasis-Token="+oasisToken+"; Oasis-Webid="+webid)
	req.Header.Set("oasis-appid", "10300")
	req.Header.Set("oasis-platform", "web")
	req.Header.Set("oasis-webid", webid)
	req.Header.Set("Origin", "https://platform.stepfun.com")
	req.Header.Set("Referer", "https://platform.stepfun.com/plan-subscribe")
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/149.0.0.0 Safari/537.36")

	client := &http.Client{Timeout: requestTimeout}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("stepfun_plan: http request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("stepfun_plan: read body: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		// 解析 Connect 错误信息，给出友好提示
		var errResp struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		}
		_ = json.Unmarshal(body, &errResp)
		msg := errResp.Message
		if msg == "" {
			msg = fmt.Sprintf("http status %d", resp.StatusCode)
		}
		// access token 过期或被判盗用时返回 401
		if resp.StatusCode == 401 {
			return nil, fmt.Errorf("stepfun_plan: 鉴权失败（%s），Oasis-Token 可能已过期，请重新获取", msg)
		}
		return nil, fmt.Errorf("stepfun_plan: %s", msg)
	}

	var data struct {
		Status              int    `json:"status"`
		Desc                string `json:"desc"`
		PlanFamily          int    `json:"plan_family"`
		PlanCreditRateLimit struct {
			SubscriptionCreditLeftRate  float64 `json:"subscription_credit_left_rate"`
			SubscriptionCreditResetTime string  `json:"subscription_credit_reset_time"`
			CreditBuckets               []struct {
				Type           int    `json:"type"`
				CreditTotal    string `json:"credit_total"`
				CreditResidual string `json:"credit_residual"`
				ExpireAt       string `json:"expire_at"`
				NextResetAt    string `json:"next_reset_at"`
			} `json:"credit_buckets"`
		} `json:"plan_credit_rate_limit"`
	}
	if err := json.Unmarshal(body, &data); err != nil {
		return nil, fmt.Errorf("stepfun_plan: parse response: %w", err)
	}

	result := &TokenPlanResult{}
	// 取第一个 credit bucket 作为主配额
	if len(data.PlanCreditRateLimit.CreditBuckets) > 0 {
		bucket := data.PlanCreditRateLimit.CreditBuckets[0]
		total := parseFloat(bucket.CreditTotal)
		residual := parseFloat(bucket.CreditResidual)
		result.QuotaTotal = total
		result.QuotaUsed = max(0, total-residual)

		// next_reset_at 是 Unix 时间戳字符串
		if bucket.NextResetAt != "" {
			if ts, err := strconv.ParseInt(bucket.NextResetAt, 10, 64); err == nil && ts > 0 {
				t := time.Unix(ts, 0)
				result.QuotaResetAt = &t
			}
		}
	}
	return result, nil
}

// decodeStepFunWebID 从 Oasis-Token 中解码 refresh token 的 device_id 字段作为 Oasis-Webid。
// Oasis-Token 格式为 "access_jwt...refresh_jwt"，取 ... 后半段解码 payload。
// 解码失败返回空字符串（服务端会接受空 webid 的请求，或返回明确鉴权错误）。
func decodeStepFunWebID(oasisToken string) string {
	// 分割 access...refresh
	idx := strings.Index(oasisToken, "...")
	if idx < 0 {
		return ""
	}
	refresh := oasisToken[idx+3:]
	// JWT 格式 header.payload.signature，取 payload
	parts := strings.Split(refresh, ".")
	if len(parts) < 2 {
		return ""
	}
	payload := parts[1]
	// base64url 解码
	decoded, err := base64.URLEncoding.DecodeString(payload)
	if err != nil {
		// 尝试不带 padding 的 base64url
		decoded, err = base64.RawURLEncoding.DecodeString(payload)
		if err != nil {
			return ""
		}
	}
	var claims struct {
		DeviceID string `json:"device_id"`
	}
	if err := json.Unmarshal(decoded, &claims); err != nil {
		return ""
	}
	return claims.DeviceID
}

// --- SenseNova 套餐用量（商汤日日新 Coding Plan）---
//
// 与 StepFun Plan 类似，使用控制台 token 鉴权查询套餐用量，但更简单：
//   - 标准 Bearer JWT 鉴权（非 Cookie + 自定义头）
//   - Token 有效期约 3 小时（比 StepFun 的 30 分钟长）
//   - GET 请求，URL 参数带 account_id 和 model_ids
//   - 响应是每模型剩余百分比（非绝对值）
//
// account_id 从 JWT payload 的 ext.tenant_id 字段自动解码。
var senseNovaPlanURL = "https://platform.sensenova.cn/lite/console/v1/user/coding-plan/usages"

func querySenseNovaPlanTokenPlan(ctx context.Context, token string) (*TokenPlanResult, error) {
	if token == "" {
		return nil, fmt.Errorf("sensenova_plan: token is required")
	}

	// 从 JWT 解码 tenant_id 作为 account_id
	accountID := decodeSenseNovaAccountID(token)
	if accountID == "" {
		return nil, fmt.Errorf("sensenova_plan: 无法从 Token 解码 account_id，请检查 Token 是否完整")
	}

	// 构造 URL：account_id + 固定模型列表
	u := senseNovaPlanURL + "?account_id=" + accountID
	for _, m := range senseNovaPlanModels {
		u += "&model_ids=" + m
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, fmt.Errorf("sensenova_plan: create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json, text/plain, */*")
	req.Header.Set("Referer", "https://platform.sensenova.cn/console")
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/149.0.0.0 Safari/537.36")

	client := &http.Client{Timeout: requestTimeout}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("sensenova_plan: http request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("sensenova_plan: read body: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("sensenova_plan: http status %d: %s", resp.StatusCode, string(body))
	}

	var data struct {
		ModelRemainingPercent map[string]float64 `json:"model_remaining_percent"`
	}
	if err := json.Unmarshal(body, &data); err != nil {
		return nil, fmt.Errorf("sensenova_plan: parse response: %w", err)
	}

	// 响应是每模型剩余百分比，汇总为总量/已用
	// 百分比无法直接换算成绝对额度，用百分比作为 total=100, used=100-pct
	result := &TokenPlanResult{
		QuotaTotal: 100,
	}
	var models []TokenPlanModelUsage
	for _, modelName := range senseNovaPlanModels {
		remaining := data.ModelRemainingPercent[modelName]
		models = append(models, TokenPlanModelUsage{
			ModelName:  modelName,
			QuotaTotal: 100,
			QuotaUsed:  max(0, 100-remaining),
		})
	}

	// 汇总：取所有模型中已用比例最高的作为总体已用
	maxUsed := 0.0
	for _, m := range models {
		if m.QuotaUsed > maxUsed {
			maxUsed = m.QuotaUsed
		}
	}
	result.QuotaUsed = maxUsed
	result.Models = models
	return result, nil
}

// senseNovaPlanModels 是 SenseNova Coding Plan 支持的模型列表。
// 查询时需要传入 model_ids 参数，服务端返回每模型的剩余百分比。
var senseNovaPlanModels = []string{
	"sensenova-6.7-flash-lite",
	"sensenova-u1-fast",
	"deepseek-v4-flash",
}

// decodeSenseNovaAccountID 从 SenseNova JWT 的 payload 中解码 ext.tenant_id。
func decodeSenseNovaAccountID(token string) string {
	parts := strings.Split(token, ".")
	if len(parts) < 2 {
		return ""
	}
	payload := parts[1]
	decoded, err := base64.URLEncoding.DecodeString(payload)
	if err != nil {
		decoded, err = base64.RawURLEncoding.DecodeString(payload)
		if err != nil {
			return ""
		}
	}
	var claims struct {
		Ext struct {
			TenantID string `json:"tenant_id"`
		} `json:"ext"`
	}
	if err := json.Unmarshal(decoded, &claims); err != nil {
		return ""
	}
	return claims.Ext.TenantID
}

// --- MiMo Token Plan (小米 MiMo Coding Plan) ---
//
// 支持两种鉴权模式：
//   - serviceToken 模式：用户提供完整浏览器 Cookie（含 api-platform_serviceToken），有效期约 1 天
//   - passToken 模式：用户提供小米账号 SSO Cookie（含 passToken=），系统通过 SSO 自动刷新 serviceToken，可长期有效
//
// API 端点：
//   - 用量：GET https://platform.xiaomimimo.com/api/v1/tokenPlan/usage
//   - 详情：GET https://platform.xiaomimimo.com/api/v1/tokenPlan/detail
var mimoPlanUsageURL = "https://platform.xiaomimimo.com/api/v1/tokenPlan/usage"
var mimoPlanDetailURL = "https://platform.xiaomimimo.com/api/v1/tokenPlan/detail"
var mimoGenLoginURL = "https://platform.xiaomimimo.com/api/v1/genLoginUrl?currentPath=%2Fconsole%2Fplan-manage"

func queryMiMoPlanTokenPlan(ctx context.Context, cookie string) (*TokenPlanResult, error) {
	if cookie == "" {
		return nil, fmt.Errorf("mimo_plan: cookie 不能为空")
	}

	isPassToken := strings.Contains(cookie, "passToken=")
	isServiceToken := strings.Contains(cookie, "api-platform_serviceToken")

	if !isPassToken && !isServiceToken {
		return nil, fmt.Errorf("mimo_plan: Cookie 缺少有效的鉴权字段，需包含 passToken= 或 api-platform_serviceToken")
	}

	// 清理 cookie 末尾多余分号/空格
	cookie = strings.TrimRight(strings.TrimSpace(cookie), "; ")

	// passToken 模式：先通过 SSO 刷新获取 serviceToken
	var serviceCookie string
	if isPassToken {
		var err error
		serviceCookie, err = refreshMiMoServiceToken(ctx, cookie)
		if err != nil {
			return nil, fmt.Errorf("mimo_plan: passToken 刷新 serviceToken 失败: %w", err)
		}
	} else {
		serviceCookie = cookie
	}

	// 查询用量
	usageBody, err := doMiMoGet(ctx, mimoPlanUsageURL, serviceCookie)
	if err != nil {
		return nil, fmt.Errorf("mimo_plan: query usage: %w", err)
	}

	var usageResp struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
		Data    struct {
			MonthUsage struct {
				Percent float64 `json:"percent"`
				Items   []struct {
					Name    string  `json:"name"`
					Used    float64 `json:"used"`
					Limit   float64 `json:"limit"`
					Percent float64 `json:"percent"`
				} `json:"items"`
			} `json:"monthUsage"`
			Usage struct {
				Percent float64 `json:"percent"`
				Items   []struct {
					Name    string  `json:"name"`
					Used    float64 `json:"used"`
					Limit   float64 `json:"limit"`
					Percent float64 `json:"percent"`
				} `json:"items"`
			} `json:"usage"`
		} `json:"data"`
	}
	if err := json.Unmarshal(usageBody, &usageResp); err != nil {
		return nil, fmt.Errorf("mimo_plan: parse usage response: %w", err)
	}
	if usageResp.Code != 0 {
		return nil, fmt.Errorf("mimo_plan: API error code=%d msg=%s", usageResp.Code, usageResp.Message)
	}

	// 查询套餐详情（获取到期时间）
	detailBody, err := doMiMoGet(ctx, mimoPlanDetailURL, serviceCookie)
	if err != nil {
		return nil, fmt.Errorf("mimo_plan: query detail: %w", err)
	}

	var detailResp struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
		Data    struct {
			PlanCode         string `json:"planCode"`
			PlanName         string `json:"planName"`
			CurrentPeriodEnd string `json:"currentPeriodEnd"`
			Expired          bool   `json:"expired"`
			EnableAutoRenew  bool   `json:"enableAutoRenew"`
		} `json:"data"`
	}
	if err := json.Unmarshal(detailBody, &detailResp); err != nil {
		return nil, fmt.Errorf("mimo_plan: parse detail response: %w", err)
	}
	if detailResp.Code != 0 {
		return nil, fmt.Errorf("mimo_plan: detail API error code=%d msg=%s", detailResp.Code, detailResp.Message)
	}

	// 组装结果。MiMo 会把订阅额度、补偿额度等拆成多个 item，展示层需要总量。
	result := &TokenPlanResult{}
	for _, item := range usageResp.Data.Usage.Items {
		result.QuotaTotal += item.Limit
		result.QuotaUsed += item.Used
	}
	for _, item := range usageResp.Data.MonthUsage.Items {
		result.WeeklyTotal += item.Limit
		result.WeeklyUsed += item.Used
	}

	// 到期时间
	if detailResp.Data.CurrentPeriodEnd != "" {
		if t, err := time.ParseInLocation("2006-01-02 15:04:05", detailResp.Data.CurrentPeriodEnd, time.Local); err == nil {
			result.QuotaResetAt = &t
		}
	}

	// 各模型明细（MiMo 不分模型，只有一个汇总）
	result.Models = []TokenPlanModelUsage{
		{
			ModelName:  "MiMo (Total)",
			QuotaTotal: result.QuotaTotal,
			QuotaUsed:  result.QuotaUsed,
		},
	}

	return result, nil
}

// refreshMiMoServiceToken 通过小米 SSO 流程用 passToken 获取新的 serviceToken Cookie。
//
// 流程：
//  1. GET genLoginUrl → 302 重定向到 account.xiaomi.com/pass/serviceLogin
//  2. 带 passToken Cookie 访问 SSO → 302 重定向到 /sts?auth=...
//  3. 访问 /sts → Set-Cookie 返回 api-platform_serviceToken 等
func refreshMiMoServiceToken(ctx context.Context, passTokenCookie string) (string, error) {
	// Step 1: 获取 SSO 登录 URL
	ssoURL, err := mimoFollowRedirect(ctx, mimoGenLoginURL, "")
	if err != nil {
		return "", fmt.Errorf("genLoginUrl: %w", err)
	}

	// Step 2: 带 passToken 访问 SSO，获取 /sts 回调 URL
	stsURL, err := mimoFollowRedirect(ctx, ssoURL, passTokenCookie)
	if err != nil {
		return "", fmt.Errorf("SSO authentication: %w", err)
	}

	// Step 3: 访问 /sts，从 Set-Cookie 提取 serviceToken
	serviceCookie, err := mimoGetServiceCookie(ctx, stsURL, passTokenCookie)
	if err != nil {
		return "", fmt.Errorf("/sts callback: %w", err)
	}

	return serviceCookie, nil
}

// mimoFollowRedirect 发送 GET 请求并返回 Location 头（不自动跟随重定向）。
func mimoFollowRedirect(ctx context.Context, reqURL, cookie string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return "", err
	}
	if cookie != "" {
		req.Header.Set("Cookie", cookie)
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/149.0.0.0 Safari/537.36")
	req.Header.Set("Referer", "https://platform.xiaomimimo.com/")

	client := &http.Client{
		Timeout: requestTimeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if loc := resp.Header.Get("Location"); loc != "" {
		return loc, nil
	}
	// 有些情况下 302 可能没有 Location 但有 JSON body
	if resp.StatusCode >= 300 && resp.StatusCode < 400 {
		return "", fmt.Errorf("redirect without Location header (status %d)", resp.StatusCode)
	}
	return "", fmt.Errorf("expected redirect but got status %d", resp.StatusCode)
}

// mimoGetServiceCookie 访问 /sts 回调并从 Set-Cookie 提取 serviceToken 构造完整 Cookie 字符串。
func mimoGetServiceCookie(ctx context.Context, stsURL, passTokenCookie string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, stsURL, nil)
	if err != nil {
		return "", err
	}
	if passTokenCookie != "" {
		req.Header.Set("Cookie", passTokenCookie)
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/149.0.0.0 Safari/537.36")

	client := &http.Client{
		Timeout: requestTimeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	var serviceToken, userId, slh, ph string
	for _, sc := range resp.Header.Values("Set-Cookie") {
		if v := extractMiMoCookieValue(sc, "api-platform_serviceToken"); v != "" {
			serviceToken = v
		}
		if v := extractMiMoCookieValue(sc, "userId"); v != "" {
			userId = v
		}
		if v := extractMiMoCookieValue(sc, "api-platform_slh"); v != "" {
			slh = v
		}
		if v := extractMiMoCookieValue(sc, "api-platform_ph"); v != "" {
			ph = v
		}
	}

	if serviceToken == "" {
		return "", fmt.Errorf("Set-Cookie 中未找到 api-platform_serviceToken (status %d)", resp.StatusCode)
	}

	var parts []string
	parts = append(parts, fmt.Sprintf(`api-platform_serviceToken="%s"`, serviceToken))
	if userId != "" {
		parts = append(parts, "userId="+userId)
	}
	if slh != "" {
		parts = append(parts, fmt.Sprintf(`api-platform_slh="%s"`, slh))
	}
	if ph != "" {
		parts = append(parts, fmt.Sprintf(`api-platform_ph="%s"`, ph))
	}
	return strings.Join(parts, "; "), nil
}

// extractMiMoCookieValue 从 Set-Cookie 头中提取指定 Cookie 的值。
func extractMiMoCookieValue(setCookie, name string) string {
	prefix := name + "="
	for _, part := range strings.Split(setCookie, ";") {
		part = strings.TrimSpace(part)
		if strings.HasPrefix(part, prefix) {
			val := strings.TrimPrefix(part, prefix)
			return strings.Trim(val, `"`)
		}
	}
	return ""
}

// doMiMoGet 执行 MiMo 平台的 GET 请求，使用 Cookie 鉴权。
func doMiMoGet(ctx context.Context, url, cookie string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Cookie", cookie)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/149.0.0.0 Safari/537.36")

	client := &http.Client{Timeout: requestTimeout}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("http request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read body: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("http status %d: %s", resp.StatusCode, string(body))
	}
	return body, nil
}

// --- 302.ai ---

func query302AIBalance(ctx context.Context, apiKey string) (*BalanceResult, error) {
	body, err := doGet(ctx, "https://api.302.ai/dashboard/balance", apiKey)
	if err != nil {
		return nil, err
	}

	// 302.ai returns a simple JSON: { balance: float64, total: float64, ... }
	var resp struct {
		Balance float64 `json:"balance"`
		Total   float64 `json:"total"`
		Used    float64 `json:"used"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("302ai: parse response: %w", err)
	}

	balance := resp.Balance
	if balance == 0 {
		balance = resp.Total
	}
	return &BalanceResult{Balance: balance, BalanceUsed: resp.Used, Currency: "CNY"}, nil
}

// --- Novita AI ---

func queryNovitaBalance(ctx context.Context, apiKey string) (*BalanceResult, error) {
	body, err := doGet(ctx, "https://api.novita.ai/openapi/v1/billing/balance/detail", apiKey)
	if err != nil {
		return nil, err
	}

	var resp struct {
		AvailableBalance    string `json:"availableBalance"`
		CashBalance         string `json:"cashBalance"`
		CreditLimit         string `json:"creditLimit"`
		PendingCharges      string `json:"pendingCharges"`
		OutstandingInvoices string `json:"outstandingInvoices"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("novita: parse response: %w", err)
	}

	// Novita balance unit: 1/10000 USD (10000 = $1.00)
	balance := parseFloat(resp.AvailableBalance) / 10000
	return &BalanceResult{Balance: balance, BalanceUsed: 0, Currency: "USD"}, nil
}

// --- OpenAI ---

func queryOpenAIBalance(ctx context.Context, apiKey string) (*BalanceResult, error) {
	// OpenAI 官方余额接口。/dashboard/billing/subscription 已被 OpenAI 废弃
	// （需 session token，API key 无法访问，2023 年底停用），故不再回退。
	body, err := doGet(ctx, "https://api.openai.com/v1/balances", apiKey)
	if err != nil {
		return nil, fmt.Errorf("openai: query /v1/balances failed (this endpoint requires an organization with grant credits; standard pay-as-you-go accounts may not be supported): %w", err)
	}

	var resp struct {
		TotalGrantedUSD   float64 `json:"total_granted_usd"`
		TotalUsedUSD      float64 `json:"total_used_usd"`
		TotalAvailableUSD float64 `json:"total_available_usd"`
		ExpiresAt         string  `json:"expires_at"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("openai: parse response: %w", err)
	}
	if resp.TotalGrantedUSD <= 0 {
		return nil, fmt.Errorf("openai: /v1/balances returned no grant credits (total_granted_usd=%.2f); this account may not support balance query via API key", resp.TotalGrantedUSD)
	}

	return &BalanceResult{
		Balance:     resp.TotalAvailableUSD,
		BalanceUsed: resp.TotalUsedUSD,
		Currency:    "USD",
	}, nil
}

// --- ChatGPT Codex 套餐 (WHAM API) ---
//
// ChatGPT Codex 订阅的套餐用量查询与 API 转发完全不同：
//   - 域名：chatgpt.com（控制台 WHAM API），非 api.openai.com
//   - 凭据：OAuth JSON（含 access_token 和 account_id），非 sk- API key
//   - 鉴权：Bearer access_token + chatgpt-account-id header
//   - 用量按百分比返回（used_percent），非绝对值
//
// 响应结构：
//
//	{
//	  "plan_type": "...",
//	  "rate_limit": {
//	    "primary_window":   { "used_percent": 42.5, "reset_at": 1234567890, "limit_window_seconds": 604800 },
//	    "secondary_window": { "used_percent": 10.0, "reset_at": 1234567890, "limit_window_seconds": 18000 }
//	  },
//	  "additional_rate_limits": [...]
//	}
//
// primary_window = 周配额（limit_window_seconds ≈ 604800 = 7 天）
// secondary_window = 5 小时配额（limit_window_seconds ≈ 18000 = 5h）
// used_percent 是 0-100 的百分比，转为 QuotaUsed/QuotaTotal 表示。
var codexWhamUsageURL = "https://chatgpt.com/backend-api/wham/usage"

func queryCodexTokenPlan(ctx context.Context, oauthKeyJSON string, proxyMode model.ProxyUsageMode, proxyConfigID *int) (*TokenPlanResult, error) {
	oauthKey, err := parseCodexOAuthKey(oauthKeyJSON)
	if err != nil {
		return nil, fmt.Errorf("codex: %w", err)
	}
	if oauthKey.AccessToken == "" {
		return nil, fmt.Errorf("codex: access_token is required")
	}
	if oauthKey.AccountID == "" {
		return nil, fmt.Errorf("codex: account_id is required")
	}

	client, err := planQueryHTTPClient(proxyMode, proxyConfigID)
	if err != nil {
		return nil, fmt.Errorf("codex: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, codexWhamUsageURL, nil)
	if err != nil {
		return nil, fmt.Errorf("codex: create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+oauthKey.AccessToken)
	req.Header.Set("chatgpt-account-id", oauthKey.AccountID)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("originator", "codex_cli_rs")

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("codex: http get: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("codex: read body: %w", err)
	}

	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return nil, fmt.Errorf("codex: authentication failed (status %d): %s", resp.StatusCode, string(body))
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("codex: http status %d: %s", resp.StatusCode, string(body))
	}

	var wham struct {
		PlanType  string `json:"plan_type"`
		RateLimit *struct {
			PrimaryWindow   *codexWhamWindow `json:"primary_window"`
			SecondaryWindow *codexWhamWindow `json:"secondary_window"`
		} `json:"rate_limit"`
	}
	if err := json.Unmarshal(body, &wham); err != nil {
		return nil, fmt.Errorf("codex: parse response: %w", err)
	}

	result := &TokenPlanResult{}

	// primary_window = 周配额（≈7 天窗口）
	if wham.RateLimit != nil && wham.RateLimit.PrimaryWindow != nil {
		w := wham.RateLimit.PrimaryWindow
		// used_percent 0-100 -> 转为 100 为总量，used_percent 为已用
		result.QuotaTotal = 100
		result.QuotaUsed = w.UsedPercent
		if w.ResetAt > 0 {
			t := time.Unix(w.ResetAt, 0)
			result.QuotaResetAt = &t
		}
	}

	// secondary_window = 5 小时配额，映射到 weekly 字段（UI 显示为「周/日配额」）
	if wham.RateLimit != nil && wham.RateLimit.SecondaryWindow != nil {
		w := wham.RateLimit.SecondaryWindow
		result.WeeklyTotal = 100
		result.WeeklyUsed = w.UsedPercent
		if w.ResetAt > 0 {
			t := time.Unix(w.ResetAt, 0)
			result.WeeklyResetAt = &t
		}
	}

	return result, nil
}

type codexWhamWindow struct {
	UsedPercent        float64 `json:"used_percent"`
	ResetAt            int64   `json:"reset_at"`
	ResetAfterSeconds  int64   `json:"reset_after_seconds"`
	LimitWindowSeconds int64   `json:"limit_window_seconds"`
}

type codexOAuthKey struct {
	AccessToken  string `json:"access_token"`
	AccountID    string `json:"account_id"`
	RefreshToken string `json:"refresh_token,omitempty"`
	IDToken      string `json:"id_token,omitempty"`
	Email        string `json:"email,omitempty"`
	Type         string `json:"type,omitempty"`
}

func parseCodexOAuthKey(raw string) (*codexOAuthKey, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, fmt.Errorf("empty oauth key")
	}
	if !strings.HasPrefix(raw, "{") {
		return nil, fmt.Errorf("key must be a JSON object containing access_token and account_id")
	}
	var key codexOAuthKey
	if err := json.Unmarshal([]byte(raw), &key); err != nil {
		return nil, fmt.Errorf("invalid oauth key json: %w", err)
	}
	return &key, nil
}

// --- 百炼 Token Plan (阿里云百炼) ---
//
// 阿里云百炼 Token Plan 的套餐用量查询通过控制台网关 API：
//   - 域名：bailian-cs.console.aliyun.com（控制台网关），非 API 端点
//   - 凭据：浏览器 Cookie（阿里云控制台会话），非 sk- API key
//   - 鉴权：Cookie 会话认证
//   - 用量按百分比返回（per5HourPercentage / per1WeekPercentage），非绝对值
//
// 需要两个 API 调用：
//   - subscription：查询订阅状态（status / remainingDays / endTime）
//   - usage：查询用量百分比（5 小时窗口 / 1 周窗口）
//
// 转发渠道使用独立的 API 端点和 API Key：
//   - 接入点：token-plan.cn-beijing.maas.aliyuncs.com/compatible-mode/v1
//   - API Key：sk-sp-... 格式
var bailianPlanGatewayURL = "https://bailian-cs.console.aliyun.com/data/api.json"

func queryBailianPlanTokenPlan(ctx context.Context, cookie string) (*TokenPlanResult, error) {
	// 1. 查询订阅状态
	subData, err := bailianGatewayPost(ctx, cookie,
		"zeldaHttp.apikeyMgr./tokenplan/personal/api/v2/subscription",
		`{"queryInstanceInfoRequest":{"commodityCode":"sfm_tokenplansolo_public_cn"}}`,
	)
	if err != nil {
		return nil, fmt.Errorf("query subscription: %w", err)
	}

	// 解析订阅状态
	var subResp struct {
		Code string `json:"code"`
		Data struct {
			DataV2 struct {
				Data struct {
					Code string `json:"code"`
					Data struct {
						Status        string `json:"status"`
						RemainingDays int    `json:"remainingDays"`
						EndTime       int64  `json:"endTime"`
					} `json:"data"`
				} `json:"data"`
			} `json:"DataV2"`
		} `json:"data"`
	}
	if err := json.Unmarshal(subData, &subResp); err != nil {
		return nil, fmt.Errorf("parse subscription response: %w", err)
	}
	if subResp.Data.DataV2.Data.Code != "SUCCESS" {
		return nil, fmt.Errorf("subscription query failed: code=%s", subResp.Data.DataV2.Data.Code)
	}
	subInfo := subResp.Data.DataV2.Data.Data
	if subInfo.Status != "VALID" {
		return nil, fmt.Errorf("subscription status is %s (not VALID)", subInfo.Status)
	}

	// 2. 查询用量百分比
	usageData, err := bailianGatewayPost(ctx, cookie,
		"zeldaHttp.apikeyMgr./tokenplan/personal/api/v2/usage",
		`{}`,
	)
	if err != nil {
		return nil, fmt.Errorf("query usage: %w", err)
	}

	var usageResp struct {
		Code string `json:"code"`
		Data struct {
			DataV2 struct {
				Data struct {
					Code string `json:"code"`
					Data struct {
						Per5HourPercentage float64 `json:"per5HourPercentage"`
						Per1WeekPercentage float64 `json:"per1WeekPercentage"`
						Per5HourResetTime  int64   `json:"per5HourResetTime"`
						Per1WeekResetTime  int64   `json:"per1WeekResetTime"`
					} `json:"data"`
				} `json:"data"`
			} `json:"DataV2"`
		} `json:"data"`
	}
	if err := json.Unmarshal(usageData, &usageResp); err != nil {
		return nil, fmt.Errorf("parse usage response: %w", err)
	}
	if usageResp.Data.DataV2.Data.Code != "SUCCESS" {
		return nil, fmt.Errorf("usage query failed: code=%s", usageResp.Data.DataV2.Data.Code)
	}
	usage := usageResp.Data.DataV2.Data.Data

	// 3. 映射到 TokenPlanResult
	// 百炼用量百分比（0-1）表示"剩余"而非"已使用"，需用 1 减去。
	// 百炼仅提供 5 小时与 1 周两档，无月配额：
	//   Per5HourPercentage -> FiveHour 槽（近5小时用量）
	//   Per1WeekPercentage -> Weekly 槽（近一周用量）
	result := &TokenPlanResult{
		FiveHourTotal: 100,
		FiveHourUsed:  (1 - usage.Per5HourPercentage) * 100,
		WeeklyTotal:   100,
		WeeklyUsed:    (1 - usage.Per1WeekPercentage) * 100,
	}
	if usage.Per5HourResetTime > 0 {
		t := time.UnixMilli(usage.Per5HourResetTime)
		result.FiveHourResetAt = &t
	}
	if usage.Per1WeekResetTime > 0 {
		t := time.UnixMilli(usage.Per1WeekResetTime)
		result.WeeklyResetAt = &t
	}

	return result, nil
}

// bailianGatewayPost 向百炼控制台网关发送 POST 请求。
// apiPath 是网关 API 路径（如 zeldaHttp.apikeyMgr./tokenplan/personal/api/v2/usage），
// dataJSON 是 Data 字段的业务参数 JSON（如 subscription 的 queryInstanceInfoRequest；usage 为 {}）。
//
// 网关请求体结构（与浏览器抓包一致）：
//
//	params={"Api":...,"V":"1.0","Data":{ <业务参数>, "cornerstoneParam":{...} }}&region=cn-beijing
//
// cornerstoneParam 嵌在 Data 内（不是 params 顶层），且必须携带 switchAgent/switchUserType/
// domain/consoleSite/xsp_lang/X-Anonymous-Id 等字段，否则网关返回 Bad Request。
// api 查询参数原样不编码（浏览器 :path 中 api 值含 / 与 . 未编码）。
func bailianGatewayPost(ctx context.Context, cookie, apiPath, dataJSON string) ([]byte, error) {
	// 构造 cornerstoneParam（字段与浏览器抓包一致；X-Anonymous-Id 为 cna cookie 值，可留空）
	cornerstone := fmt.Sprintf(
		`{"feTraceId":"%s","feURL":"https://bailian.console.aliyun.com/cn-beijing?tab=plan#/efm/subscription/token-plan/personal","protocol":"V2","console":"ONE_CONSOLE","productCode":"p_efm","switchAgent":15437370,"switchUserType":3,"domain":"bailian.console.aliyun.com","consoleSite":"BAILIAN_ALIYUN","userNickName":"","userPrincipalName":"","xsp_lang":"zh-CN","X-Anonymous-Id":""}`,
		uuid.NewString(),
	)

	// 将 cornerstoneParam 合并进 Data 对象（保留业务参数）
	var dataObj map[string]json.RawMessage
	if strings.TrimSpace(dataJSON) == "" {
		dataJSON = "{}"
	}
	if err := json.Unmarshal([]byte(dataJSON), &dataObj); err != nil {
		return nil, fmt.Errorf("parse data json: %w", err)
	}
	dataObj["cornerstoneParam"] = json.RawMessage(cornerstone)
	mergedData, err := json.Marshal(dataObj)
	if err != nil {
		return nil, fmt.Errorf("marshal data: %w", err)
	}

	// 构造完整 params JSON（cornerstoneParam 在 Data 内）
	paramsJSON := fmt.Sprintf(
		`{"Api":"%s","V":"1.0","Data":%s}`,
		apiPath, mergedData,
	)

	// URL 编码 params，追加 region 参数（与浏览器一致）
	body := "params=" + url.QueryEscape(paramsJSON) + "&region=cn-beijing"

	// api 参数原样不编码（浏览器 :path 中 api 值含 / 与 . 未编码）
	reqURL := bailianPlanGatewayURL + "?action=BroadScopeAspnGateway&product=sfm_bailian&api=" + apiPath + "&_v=undefined"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, reqURL, strings.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Cookie", strings.TrimSpace(cookie))
	req.Header.Set("Referer", "https://bailian.console.aliyun.com/")
	req.Header.Set("Origin", "https://bailian.console.aliyun.com")

	client := &http.Client{Timeout: requestTimeout}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("http post: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read body: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("http status %d: %s", resp.StatusCode, string(respBody))
	}
	return respBody, nil
}

// --- 火山方舟 Agent Plan (Volcengine Ark) ---
//
// 火山方舟 Agent Plan 的套餐用量查询通过控制台 TOP API：
//   - 域名：console.volcengine.com（控制台），非 API 端点
//   - 凭据：浏览器 Cookie + x-csrf-token 请求头，二者配对使用
//   - 鉴权：Cookie 会话认证 + CSRF Token 头校验
//   - 用量为绝对值（Quota / Used），非百分比
//
// 用户凭据格式：`Cookie值|||x-csrf-token值`（三部分竖线分隔）。
// Cookie 从 console.volcengine.com 控制台请求头复制，x-csrf-token 同页请求头获取。
//
// 用量接口返回多档配额（5h/日/周/月），取月配额为主配额、周配额为次配额、5h 为第三档。
// 转发渠道使用火山方舟 OpenAI 兼容端点 + ark- API Key：
//   - 接入点：https://ark.cn-beijing.volces.com/api/plan/v3
//   - API Key：ark-... 格式（控制台 API Key 管理页创建）
var volcenginePlanUsageURL = "https://console.volcengine.com/api/top/ark/cn-beijing/2024-01-01/GetAgentPlanAFPUsage"

// volcengineCredentialSep 是用户凭据中 Cookie 与 CSRF Token 的分隔符。
const volcengineCredentialSep = "|||"

func queryVolcenginePlanTokenPlan(ctx context.Context, credential string) (*TokenPlanResult, error) {
	cookie, csrfToken, err := parseVolcengineCredential(credential)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, volcenginePlanUsageURL, strings.NewReader("{}"))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Cookie", cookie)
	req.Header.Set("x-csrf-token", csrfToken)
	req.Header.Set("Referer", "https://console.volcengine.com/ark/region:cn-beijing/subscription/agent-plan")
	req.Header.Set("Origin", "https://console.volcengine.com")

	client := &http.Client{Timeout: requestTimeout}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("http post: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read body: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("http status %d: %s", resp.StatusCode, string(body))
	}

	var usageResp struct {
		ResponseMetadata struct {
			Error *struct {
				Code    string `json:"Code"`
				Message string `json:"Message"`
			} `json:"Error"`
		} `json:"ResponseMetadata"`
		Result struct {
			PlanType    string `json:"PlanType"`
			AFPFiveHour struct {
				Quota     float64 `json:"Quota"`
				Used      float64 `json:"Used"`
				ResetTime int64   `json:"ResetTime"`
			} `json:"AFPFiveHour"`
			AFPWeekly struct {
				Quota     float64 `json:"Quota"`
				Used      float64 `json:"Used"`
				ResetTime int64   `json:"ResetTime"`
			} `json:"AFPWeekly"`
			AFPMonthly struct {
				Quota     float64 `json:"Quota"`
				Used      float64 `json:"Used"`
				ResetTime int64   `json:"ResetTime"`
			} `json:"AFPMonthly"`
		} `json:"Result"`
	}
	if err := json.Unmarshal(body, &usageResp); err != nil {
		return nil, fmt.Errorf("parse usage response: %w", err)
	}
	if usageResp.ResponseMetadata.Error != nil {
		return nil, fmt.Errorf("volcengine api error %s: %s",
			usageResp.ResponseMetadata.Error.Code, usageResp.ResponseMetadata.Error.Message)
	}

	r := usageResp.Result
	// 火山方舟 GetAgentPlanAFPUsage 的 Used 字段即"已使用量"：官网控制台
	// "已使用 X%" 直接以 Used/Quota 计算，大数字亦为 Used。早期误判为剩余量
	// 并做 Quota-Used 取反，使前端进度条/百分比与官网颠倒。
	result := &TokenPlanResult{
		QuotaTotal:    r.AFPMonthly.Quota,
		QuotaUsed:     r.AFPMonthly.Used,
		WeeklyTotal:   r.AFPWeekly.Quota,
		WeeklyUsed:    r.AFPWeekly.Used,
		FiveHourTotal: r.AFPFiveHour.Quota,
		FiveHourUsed:  r.AFPFiveHour.Used,
	}
	if r.AFPFiveHour.ResetTime > 0 {
		t := time.UnixMilli(r.AFPFiveHour.ResetTime)
		result.FiveHourResetAt = &t
	}
	if r.AFPMonthly.ResetTime > 0 {
		t := time.UnixMilli(r.AFPMonthly.ResetTime)
		result.QuotaResetAt = &t
	}
	if r.AFPWeekly.ResetTime > 0 {
		t := time.UnixMilli(r.AFPWeekly.ResetTime)
		result.WeeklyResetAt = &t
	}
	return result, nil
}

// parseVolcengineCredential 解析用户填入的火山方舟凭据。
// 格式：`Cookie值|||x-csrf-token值`。容忍前后空白。
func parseVolcengineCredential(credential string) (cookie, csrfToken string, err error) {
	credential = strings.TrimSpace(credential)
	parts := strings.SplitN(credential, volcengineCredentialSep, 2)
	if len(parts) != 2 {
		return "", "", fmt.Errorf("凭据格式错误：需为 `Cookie值%sx-csrf-token值`（从控制台请求头复制）", volcengineCredentialSep)
	}
	cookie = strings.TrimSpace(parts[0])
	csrfToken = strings.TrimSpace(parts[1])
	if cookie == "" {
		return "", "", fmt.Errorf("Cookie 不能为空")
	}
	if csrfToken == "" {
		return "", "", fmt.Errorf("x-csrf-token 不能为空")
	}
	return cookie, csrfToken, nil
}

// --- Helpers ---

func doGet(ctx context.Context, url, apiKey string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(apiKey))
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: requestTimeout}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("http get %s: %w", url, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read body: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("http status %d: %s", resp.StatusCode, string(body))
	}
	return body, nil
}

// parseFloat 解析 API 返回的数字字符串为 float64。
// 优先使用 strconv.ParseFloat（比 fmt.Sscanf 更严格、更快、容错更好）；
// 失败时清理常见干扰字符（货币符号、千分位逗号、空白）后重试，仍失败返回 0。
func parseFloat(s string) float64 {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0
	}
	if v, err := strconv.ParseFloat(s, 64); err == nil {
		return v
	}
	// 清理常见干扰字符：货币符号、千分位逗号、空白。
	cleaned := strings.NewReplacer(
		"$", "", "€", "", "£", "", "¥", "", "￥", "",
		",", "", " ", "", "\t", "",
	).Replace(s)
	if v, err := strconv.ParseFloat(cleaned, 64); err == nil {
		return v
	}
	return 0
}
