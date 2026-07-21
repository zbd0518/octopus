package task

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/lingyuins/octopus/internal/helper"
	"github.com/lingyuins/octopus/internal/model"
	"github.com/lingyuins/octopus/internal/op/alert"
	"github.com/lingyuins/octopus/internal/op/analytics"
	"github.com/lingyuins/octopus/internal/op/channel"
	"github.com/lingyuins/octopus/internal/op/notification"
	"github.com/lingyuins/octopus/internal/op/report"
	"github.com/lingyuins/octopus/internal/op/stats"
	"github.com/lingyuins/octopus/internal/utils/log"
)

// EvaluateReportSchedules checks all enabled report schedules and sends reports
// that are due based on their type (daily/weekly/monthly) and configured send time.
func EvaluateReportSchedules() {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	schedules, err := report.GetEnabledSchedules(ctx)
	if err != nil {
		log.Warnf("report: failed to load enabled schedules: %v", err)
		return
	}

	if len(schedules) == 0 {
		return
	}

	// Load notification channel map for dispatch
	notifiChannels, err := alert.NotifChannelList(ctx)
	if err != nil {
		log.Warnf("report: failed to load notification channels: %v", err)
		return
	}
	channelMap := make(map[int]*model.AlertNotifChannel, len(notifiChannels))
	for _, ch := range notifiChannels {
		channelMap[ch.ID] = &ch
	}

	loc := stats.StatsLocation()
	localNow := time.Now().In(loc)

	for _, sched := range schedules {
		if !isReportDue(sched, localNow, loc) {
			continue
		}

		log.Debugf("report: schedule %d (%s) is due, generating %s report", sched.ID, sched.Name, sched.Type)
		generateAndSendReport(ctx, sched, channelMap, loc)
	}
}

// isReportDue checks if a schedule should run at the given time.
func isReportDue(sched model.ReportSchedule, now time.Time, loc *time.Location) bool {
	// Check if we already sent today in the configured stats timezone.
	if sched.LastSentAt > 0 {
		lastSent := time.UnixMilli(sched.LastSentAt).In(loc)
		if lastSent.Year() == now.Year() && lastSent.YearDay() == now.YearDay() {
			return false // Already sent today
		}
	}

	// Check hour
	if now.Hour() != sched.SendHour {
		return false
	}

	switch sched.Type {
	case model.ReportTypeDaily:
		return true
	case model.ReportTypeWeekly:
		return int(now.Weekday()) == sched.SendDayOfWeek
	case model.ReportTypeMonthly:
		return now.Day() == sched.SendDayOfMonth
	}

	return false
}

// generateAndSendReport generates a report for the given schedule and sends it.
func generateAndSendReport(ctx context.Context, sched model.ReportSchedule, channelMap map[int]*model.AlertNotifChannel, loc *time.Location) {
	// Determine report range based on type
	var rangeStr string
	var rangeTitle string
	now := time.Now().In(loc)

	switch sched.Type {
	case model.ReportTypeDaily:
		rangeStr = "1d"
		rangeTitle = fmt.Sprintf("%s 日报", now.Format("2006-01-02"))
	case model.ReportTypeWeekly:
		rangeStr = "7d"
		rangeTitle = fmt.Sprintf("%s 周报", now.Format("2006-01-02"))
	case model.ReportTypeMonthly:
		rangeStr = "30d"
		rangeTitle = fmt.Sprintf("%s 月报", now.Format("2006-01"))
	default:
		log.Warnf("report: unknown schedule type: %s", sched.Type)
		return
	}

	// Parse metrics
	metrics := report.ParseMetrics(sched.Metrics)
	if len(metrics) == 0 {
		log.Warnf("report: schedule %d has no metrics configured", sched.ID)
		return
	}

	// Generate report content
	content, err := buildReportContent(ctx, rangeStr, rangeTitle, metrics)
	if err != nil {
		log.Warnf("report: failed to generate content for schedule %d: %v", sched.ID, err)
		recordReportHistory(sched, "", "failed", fmt.Sprintf("generate error: %v", err))
		return
	}

	// Find notification channel
	ch, ok := channelMap[sched.NotifChannelID]
	if !ok || ch == nil {
		log.Warnf("report: schedule %d references notif_channel_id=%d which was not found", sched.ID, sched.NotifChannelID)
		recordReportHistory(sched, content, "skipped", "notification channel not found")
		return
	}

	// Send notification
	title := fmt.Sprintf("%s - %s", sched.Name, rangeTitle)
	if err := helper.SendNotificationMessage(ch, title, content); err != nil {
		log.Warnf("report: failed to send for schedule %d: %v", sched.ID, err)
		recordReportHistory(sched, content, "failed", err.Error())
		return
	}

	// Update last sent time
	if err := report.ScheduleUpdateLastSent(ctx, sched.ID, time.Now().UnixMilli()); err != nil {
		log.Warnf("report: failed to update last_sent_at for schedule %d: %v", sched.ID, err)
	}

	recordReportHistory(sched, content, "sent", fmt.Sprintf("%s (%s)", ch.Name, ch.Type))
}

// buildReportContent generates the formatted report text.
//
// 报告使用纯文本排版（【】分节标题 + 每指标一行），不使用 Markdown。多数通知渠道
// （飞书/钉钉/企微/Telegram/Gotify/邮件/Ntfy）以纯文本发送，Markdown 的 ##/- /---
// 符号会被原样显示，可读性差。大数字加千分位，token 数用 k 缩写以保持紧凑。
func buildReportContent(ctx context.Context, rangeStr, rangeTitle string, metrics []model.ReportMetric) (string, error) {
	var sections []string

	// Header
	sections = append(sections, fmt.Sprintf("📊 %s", rangeTitle))
	sections = append(sections, "")

	// Parse range
	rangeEnum, err := model.ParseAnalyticsRange(rangeStr)
	if err != nil {
		return "", fmt.Errorf("invalid range: %w", err)
	}

	// 概览/成本明细/错误分析都依赖 overview，只取一次复用，避免重复查询。
	overview, overviewErr := analytics.AnalyticsOverviewGet(ctx, rangeEnum)

	// Overview section
	if containsMetric(metrics, model.ReportMetricOverview) {
		if overviewErr == nil && overview != nil {
			sections = append(sections, "【总览】")
			sections = append(sections, formatOverviewSection(overview)...)
			sections = append(sections, "")
		}
	}

	// Top models
	if containsMetric(metrics, model.ReportMetricTopModels) {
		modelBreakdown, err := analytics.AnalyticsModelBreakdownGet(ctx, rangeEnum)
		if err == nil && len(modelBreakdown) > 0 {
			sections = append(sections, "【Top 5 模型】")
			sort.Slice(modelBreakdown, func(i, j int) bool {
				return modelBreakdown[i].RequestCount > modelBreakdown[j].RequestCount
			})
			limit := 5
			if len(modelBreakdown) < limit {
				limit = len(modelBreakdown)
			}
			for i := 0; i < limit; i++ {
				m := modelBreakdown[i]
				sections = append(sections, fmt.Sprintf("%d. %s | %s 请求 | %s tokens | $%.2f",
					i+1, m.ModelName, formatCount(m.RequestCount), formatTokens(m.TotalTokens), m.TotalCost))
			}
			sections = append(sections, "")
		}
	}

	// Top channels
	if containsMetric(metrics, model.ReportMetricTopChannels) {
		providerBreakdown, err := analytics.AnalyticsProviderBreakdownGet(ctx, rangeEnum)
		if err == nil && len(providerBreakdown) > 0 {
			sections = append(sections, "【Top 5 渠道】")
			sort.Slice(providerBreakdown, func(i, j int) bool {
				return providerBreakdown[i].RequestCount > providerBreakdown[j].RequestCount
			})
			limit := 5
			if len(providerBreakdown) < limit {
				limit = len(providerBreakdown)
			}
			for i := 0; i < limit; i++ {
				p := providerBreakdown[i]
				sections = append(sections, fmt.Sprintf("%d. %s | %s 请求 | %s tokens | $%.2f",
					i+1, p.ChannelName, formatCount(p.RequestCount), formatTokens(p.TotalTokens), p.TotalCost))
			}
			sections = append(sections, "")
		}
	}

	// Top API Keys
	if containsMetric(metrics, model.ReportMetricTopAPIKeys) {
		apiKeyBreakdown, err := analytics.AnalyticsAPIKeyBreakdownGet(ctx, rangeEnum)
		if err == nil && len(apiKeyBreakdown) > 0 {
			sections = append(sections, "【Top 5 API Key】")
			sort.Slice(apiKeyBreakdown, func(i, j int) bool {
				return apiKeyBreakdown[i].RequestCount > apiKeyBreakdown[j].RequestCount
			})
			limit := 5
			if len(apiKeyBreakdown) < limit {
				limit = len(apiKeyBreakdown)
			}
			for i := 0; i < limit; i++ {
				k := apiKeyBreakdown[i]
				sections = append(sections, fmt.Sprintf("%d. %s | %s 请求 | %s tokens | $%.2f",
					i+1, k.Name, formatCount(k.RequestCount), formatTokens(k.TotalTokens), k.TotalCost))
			}
			sections = append(sections, "")
		}
	}

	// Cost breakdown
	if containsMetric(metrics, model.ReportMetricCostBreakdown) {
		if overviewErr == nil && overview != nil {
			sections = append(sections, "【成本明细】")
			sections = append(sections, formatCostBreakdownSection(overview)...)
			sections = append(sections, "")
		}
	}

	// Error analysis
	if containsMetric(metrics, model.ReportMetricErrorAnalysis) {
		if overviewErr == nil && overview != nil {
			sections = append(sections, "【错误分析】")
			sections = append(sections, formatErrorAnalysisSection(overview)...)
			sections = append(sections, "")
		}
	}

	// Footer
	sections = append(sections, "━━━━━━━━━━━━━━━━")
	sections = append(sections, fmt.Sprintf("生成时间: %s", time.Now().Format("2006-01-02 15:04:05")))

	return strings.Join(sections, "\n"), nil
}

// formatOverviewSection 格式化总览分节。SuccessRate/FallbackRate 已是 0-100 区间，
// 不再二次乘 100（修复历史 bug：曾显示 9318.18% 这种不可能值）。
func formatOverviewSection(o *model.AnalyticsOverview) []string {
	return []string{
		fmt.Sprintf("请求总数: %s", formatCount(o.RequestCount)),
		fmt.Sprintf("Token 总数: %s (输入 %s / 输出 %s)",
			formatCount(o.TotalTokens), formatCount(o.InputTokens), formatCount(o.OutputTokens)),
		fmt.Sprintf("总成本: $%.2f", o.TotalCost),
		fmt.Sprintf("成功率: %.2f%%", o.SuccessRate),
		fmt.Sprintf("活跃渠道: %d", o.ProviderCount),
		fmt.Sprintf("活跃模型: %d", o.ModelCount),
		fmt.Sprintf("API Key: %d", o.APIKeyCount),
	}
}

// formatCostBreakdownSection 格式化成本明细分节。
func formatCostBreakdownSection(o *model.AnalyticsOverview) []string {
	lines := []string{fmt.Sprintf("总成本: $%.2f", o.TotalCost)}
	if o.RequestCount > 0 {
		costPerRequest := o.TotalCost / float64(o.RequestCount)
		lines = append(lines, fmt.Sprintf("平均每请求: $%.4f", costPerRequest))
	}
	if o.TotalTokens > 0 {
		costPer1kTokens := (o.TotalCost / float64(o.TotalTokens)) * 1000
		lines = append(lines, fmt.Sprintf("每千 token: $%.4f", costPer1kTokens))
	}
	return lines
}

// formatErrorAnalysisSection 格式化错误分析分节。SuccessRate/FallbackRate 已是 0-100
// 区间，不再二次乘 100（修复历史 bug：曾显示 9318.18% 这种不可能值）。失败请求由
// RequestCount 与 SuccessRate（0-100）反推并四舍五入，而非把 SuccessRate 当 0-1
// 分数（修复历史 bug：曾得到负数）。
func formatErrorAnalysisSection(o *model.AnalyticsOverview) []string {
	failedRequests := int64(0)
	if o.RequestCount > 0 {
		success := int64(math.Round(float64(o.RequestCount) * o.SuccessRate / 100))
		failedRequests = o.RequestCount - success
		if failedRequests < 0 {
			failedRequests = 0
		}
	}
	return []string{
		fmt.Sprintf("成功率: %.2f%%", o.SuccessRate),
		fmt.Sprintf("失败请求: %s", formatCount(failedRequests)),
		fmt.Sprintf("回退率: %.2f%%", o.FallbackRate),
	}
}

// formatCount 为整数加千分位分隔符（如 673138 -> "673,138"）。
func formatCount(n int64) string {
	s := strconv.FormatInt(n, 10)
	if n < 0 {
		return "-" + insertCommas(s[1:])
	}
	return insertCommas(s)
}

// insertCommas 在纯数字字符串中每三位插入一个逗号。
func insertCommas(s string) string {
	n := len(s)
	if n <= 3 {
		return s
	}
	var b strings.Builder
	first := n % 3
	if first > 0 {
		b.WriteString(s[:first])
		if n > first {
			b.WriteByte(',')
		}
	}
	for i := first; i < n; i += 3 {
		b.WriteString(s[i : i+3])
		if i+3 < n {
			b.WriteByte(',')
		}
	}
	return b.String()
}

// formatTokens 将 token 数格式化为紧凑的 k 缩写（如 312000 -> "312k"，1500 -> "1.5k"）。
// 不足 1000 时直接显示原数。
func formatTokens(n int64) string {
	if n < 1000 {
		return strconv.FormatInt(n, 10)
	}
	kb := float64(n) / 1000
	if kb >= 100 {
		return strconv.FormatInt(int64(kb), 10) + "k"
	}
	// 保留一位小数，去掉多余的 ".0"（如 "12.0" -> "12"），再拼接 "k"。
	s := strconv.FormatFloat(kb, 'f', 1, 64)
	s = strings.TrimSuffix(s, ".0")
	return s + "k"
}

// containsMetric checks if a metric is in the list.
func containsMetric(metrics []model.ReportMetric, target model.ReportMetric) bool {
	for _, m := range metrics {
		if m == target {
			return true
		}
	}
	return false
}

// recordReportHistory records the report send result.
func recordReportHistory(sched model.ReportSchedule, content, status, detail string) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	entry := &model.ReportHistory{
		ScheduleID:   sched.ID,
		ScheduleName: sched.Name,
		Type:         sched.Type,
		Title:        sched.Name,
		Content:      content,
		SendStatus:   status,
		SendDetail:   detail,
	}

	if err := report.HistoryAdd(ctx, entry); err != nil {
		log.Warnf("report: failed to record history for schedule %d: %v", sched.ID, err)
	}
	createReportNotification(ctx, sched, content, status, detail)
}

func createReportNotification(ctx context.Context, sched model.ReportSchedule, content, status, detail string) {
	severity := model.NotificationSeverityInfo
	var titleKey, contentKey notification.NotifKey
	switch status {
	case "sent":
		severity = model.NotificationSeveritySuccess
		titleKey, contentKey = notification.KeyReportSent, notification.KeyReportSent
	case "failed":
		severity = model.NotificationSeverityError
		titleKey, contentKey = notification.KeyReportFailed, notification.KeyReportFailed
	case "skipped":
		severity = model.NotificationSeverityWarning
		titleKey, contentKey = notification.KeyReportSkipped, notification.KeyReportSkipped
	default:
		titleKey, contentKey = notification.KeyReportFailed, notification.KeyReportFailed
	}
	metadata, _ := json.Marshal(map[string]any{
		"schedule_id":   sched.ID,
		"schedule_name": sched.Name,
		"report_type":   sched.Type,
		"status":        status,
		"detail":        detail,
	})
	n := &model.Notification{
		Type:         model.NotificationTypeReport,
		Severity:     severity,
		Source:       "report_schedule",
		SourceID:     fmt.Sprintf("%d", sched.ID),
		DedupeKey:    fmt.Sprintf("report:%d:%s:%d", sched.ID, status, time.Now().UnixMilli()),
		MetadataJSON: string(metadata),
		Link:         "notification",
	}
	// 短消息方案：通知正文不再放完整报表正文（报表历史已单独存储）。
	// sent -> channel（detail 形如 "gotify (gotify)"）；failed/skipped -> detail（原因）。
	titleArgs := map[string]any{"name": sched.Name}
	if status == "sent" {
		notification.SetMessage(n, titleKey, contentKey,
			titleArgs, map[string]any{"name": sched.Name, "channel": detail},
			[]any{sched.Name}, []any{sched.Name, detail})
	} else {
		notification.SetMessage(n, titleKey, contentKey,
			titleArgs, map[string]any{"name": sched.Name, "detail": detail},
			[]any{sched.Name}, []any{sched.Name, detail})
	}
	if err := notification.Create(ctx, n); err != nil {
		log.Warnf("notification: failed to create report notification for schedule %d: %v", sched.ID, err)
	}
}

// TestSendReport generates and sends a test report immediately (for UI test button).
func TestSendReport(ctx context.Context, sched model.ReportSchedule) error {
	// Load notification channel
	notifiChannels, err := alert.NotifChannelList(ctx)
	if err != nil {
		return fmt.Errorf("failed to load notification channels: %w", err)
	}

	var targetChannel *model.AlertNotifChannel
	for _, ch := range notifiChannels {
		if ch.ID == sched.NotifChannelID {
			targetChannel = &ch
			break
		}
	}

	if targetChannel == nil {
		return fmt.Errorf("notification channel not found")
	}

	// Generate test report (1d range)
	metrics := report.ParseMetrics(sched.Metrics)
	if len(metrics) == 0 {
		metrics = []model.ReportMetric{model.ReportMetricOverview}
	}

	content, err := buildReportContent(ctx, "1d", "测试报告", metrics)
	if err != nil {
		return fmt.Errorf("failed to generate report: %w", err)
	}

	// Send
	title := fmt.Sprintf("%s - 测试报告", sched.Name)
	if err := helper.SendNotificationMessage(targetChannel, title, content); err != nil {
		return fmt.Errorf("failed to send: %w", err)
	}

	return nil
}

// ParseMetricsFromJSON parses metrics JSON string.
func ParseMetricsFromJSON(metricsJSON string) []model.ReportMetric {
	var metrics []model.ReportMetric
	if err := json.Unmarshal([]byte(metricsJSON), &metrics); err != nil {
		return nil
	}
	return metrics
}

// GetChannelsForReport returns all channels for report generation.
func GetChannelsForReport(ctx context.Context) ([]model.Channel, error) {
	return channel.List(ctx)
}
