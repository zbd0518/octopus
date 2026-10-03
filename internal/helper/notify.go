package helper

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/mail"
	"net/smtp"
	"net/url"
	"strings"
	"time"

	"github.com/lingyuins/octopus/internal/model"
	"github.com/lingyuins/octopus/internal/op/setting"
	"github.com/lingyuins/octopus/internal/utils/httpx"
)

func notifyHTTPClient() *http.Client {
	timeout := 10 * time.Second
	if v, err := setting.GetInt(model.SettingKeyNotifyHTTPTimeoutSeconds); err == nil && v > 0 {
		timeout = time.Duration(v) * time.Second
	}
	return &http.Client{Timeout: timeout, Transport: httpx.WithUserAgent(nil)}
}

// AlertWebhookPayload is the JSON body sent to webhook endpoints on alert state changes.
type AlertWebhookPayload struct {
	RuleID        int                          `json:"rule_id"`
	RuleName      string                       `json:"rule_name"`
	ConditionType model.AlertRuleConditionType `json:"condition_type"`
	State         string                       `json:"state"`
	Message       string                       `json:"message"`
	Threshold     float64                      `json:"threshold"`
	CurrentValue  float64                      `json:"current_value"`
	Time          string                       `json:"time"`
}

// buildAlertText builds a plain-text alert message body from the payload.
func buildAlertText(payload AlertWebhookPayload) string {
	return fmt.Sprintf("%s\n\nRule: %s\nCondition: %s\nState: %s\nThreshold: %.2f\nCurrent: %.2f\nTime: %s",
		payload.Message, payload.RuleName, payload.ConditionType, payload.State, payload.Threshold, payload.CurrentValue, payload.Time)
}

// needsMimeEncoding returns true if s contains non-ASCII characters.
func needsMimeEncoding(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] > 127 {
			return true
		}
	}
	return false
}

// ----------------------------------------------------------------------------
// Public API
// ----------------------------------------------------------------------------

// SendNotification dispatches an alert notification to the appropriate channel
// based on type. Webhook channels receive the full structured AlertWebhookPayload
// JSON (backward compatible); all other channel types receive a formatted
// title + message derived from the payload.
func SendNotification(channel *model.AlertNotifChannel, payload AlertWebhookPayload) error {
	if channel == nil {
		return fmt.Errorf("notification channel is required")
	}
	// Webhook keeps its structured payload format for backward compatibility
	// (external webhook receivers may depend on the AlertWebhookPayload schema).
	if model.AlertNotifChannelType(channel.Type) == model.AlertNotifWebhook {
		return sendWebhookPayload(channel, payload)
	}
	title := payload.RuleName
	message := buildAlertText(payload)
	return dispatch(channel, title, message)
}

// SendNotificationMessage sends a plain title+message notification to the given
// channel, reusing the same per-type HTTP dispatch as SendNotification but
// without alert-specific payload formatting. Used by the disposable-channel
// expiry task to notify users when a one-time channel is auto-deleted.
func SendNotificationMessage(channel *model.AlertNotifChannel, title, message string) error {
	if channel == nil {
		return fmt.Errorf("notification channel is required")
	}
	return dispatch(channel, title, message)
}

// TestNotification sends a test message to the given channel to verify its configuration.
// It performs no persistence and is intended for the management UI "Test" action so
// misconfigurations surface before (or after) a channel is saved.
func TestNotification(channel *model.AlertNotifChannel) error {
	if channel == nil {
		return fmt.Errorf("notification channel is required")
	}
	payload := AlertWebhookPayload{
		RuleName:      channel.Name,
		ConditionType: model.AlertRuleConditionType("test"),
		State:         "test",
		Message:       buildTestNotificationMessage(),
		Time:          time.Now().Format(time.RFC3339),
	}
	return SendNotification(channel, payload)
}

// testNotifyLanguageDefault is the fallback language for test notification text.
const testNotifyLanguageDefault = "en"

// buildTestNotificationMessage returns a localized test notification message
// based on the alert notification language setting.
func buildTestNotificationMessage() string {
	language := testNotifyLanguageDefault
	if v, err := setting.GetString(model.SettingKeyAlertNotifyLanguage); err == nil && v != "" {
		language = v
	}
	switch language {
	case "zh-Hans":
		return "这是来自 Octopus 的测试通知。如果你收到了这条消息，说明通知渠道配置正确。"
	case "zh-Hant":
		return "這是來自 Octopus 的測試通知。如果你收到了這條訊息，說明通知管道設定正確。"
	default:
		return "This is a test notification from Octopus. If you received this message, the notification channel is configured correctly."
	}
}

// ----------------------------------------------------------------------------
// Internal dispatch — single implementation per channel type
// ----------------------------------------------------------------------------

// dispatch routes a (title, message) pair to the channel-type-specific sender.
// This is the single dispatch path shared by SendNotification (for non-webhook
// types) and SendNotificationMessage, eliminating the previous duplicate
// SendXxx / sendXxxMessage function pairs (~400 lines of repeated HTTP code).
func dispatch(channel *model.AlertNotifChannel, title, message string) error {
	switch model.AlertNotifChannelType(channel.Type) {
	case model.AlertNotifGotify:
		return sendGotify(channel, title, message)
	case model.AlertNotifEmail:
		return sendEmail(channel, title, message)
	case model.AlertNotifTelegram:
		return sendTelegram(channel, title, message)
	case model.AlertNotifFeishu:
		return sendFeishu(channel, title, message)
	case model.AlertNotifDingTalk:
		return sendDingTalk(channel, title, message)
	case model.AlertNotifWeCom:
		return sendWeCom(channel, title, message)
	case model.AlertNotifNtfy:
		return sendNtfy(channel, title, message)
	case model.AlertNotifWebhook:
		fallthrough
	default:
		return sendWebhookMessage(channel, title, message)
	}
}

// --- Webhook ---

// sendWebhookPayload sends the structured AlertWebhookPayload JSON to a webhook
// endpoint. Used only by SendNotification for webhook channels to preserve the
// structured payload format that external receivers may depend on.
func sendWebhookPayload(channel *model.AlertNotifChannel, payload AlertWebhookPayload) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal webhook payload: %w", err)
	}
	req, err := http.NewRequest("POST", channel.URL, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create webhook request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if channel.Secret != "" {
		req.Header.Set("Authorization", "Bearer "+channel.Secret)
	}
	if channel.Headers != "" {
		var headers map[string]string
		if err := json.Unmarshal([]byte(channel.Headers), &headers); err == nil {
			for k, v := range headers {
				req.Header.Set(k, v)
			}
		}
	}
	client := notifyHTTPClient()
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("send webhook: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return fmt.Errorf("webhook responded %d", resp.StatusCode)
	}
	return nil
}

// sendWebhookMessage sends a plain {title, message, time} JSON to a webhook
// endpoint. Used by SendNotificationMessage for non-alert notifications.
func sendWebhookMessage(channel *model.AlertNotifChannel, title, message string) error {
	body, err := json.Marshal(map[string]string{
		"title":   title,
		"message": message,
		"time":    time.Now().Format(time.RFC3339),
	})
	if err != nil {
		return fmt.Errorf("marshal webhook payload: %w", err)
	}
	req, err := http.NewRequest("POST", channel.URL, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create webhook request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if channel.Secret != "" {
		req.Header.Set("Authorization", "Bearer "+channel.Secret)
	}
	if channel.Headers != "" {
		var headers map[string]string
		if err := json.Unmarshal([]byte(channel.Headers), &headers); err == nil {
			for k, v := range headers {
				req.Header.Set(k, v)
			}
		}
	}
	client := notifyHTTPClient()
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("send webhook: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return fmt.Errorf("webhook responded %d", resp.StatusCode)
	}
	return nil
}

// --- Gotify ---

func sendGotify(channel *model.AlertNotifChannel, title, message string) error {
	var cfg model.GotifyConfig
	if channel.Config != "" {
		if err := json.Unmarshal([]byte(channel.Config), &cfg); err != nil {
			return fmt.Errorf("parse gotify config: %w", err)
		}
	}
	serverURL := cfg.ServerURL
	if serverURL == "" {
		serverURL = strings.TrimRight(channel.URL, "/")
	}
	token := cfg.Token
	if token == "" {
		token = channel.Secret
	}
	if serverURL == "" || token == "" {
		return fmt.Errorf("gotify: server_url and token are required")
	}
	priority := cfg.Priority
	if priority <= 0 {
		priority = 5
	}
	msgBody, err := json.Marshal(map[string]interface{}{
		"title":    fmt.Sprintf("Octopus: %s", title),
		"message":  message,
		"priority": priority,
	})
	if err != nil {
		return fmt.Errorf("marshal gotify message: %w", err)
	}
	endpoint := fmt.Sprintf("%s/message?token=%s", strings.TrimRight(serverURL, "/"), token)
	req, err := http.NewRequest("POST", endpoint, bytes.NewReader(msgBody))
	if err != nil {
		return fmt.Errorf("create gotify request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	client := notifyHTTPClient()
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("send gotify: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return fmt.Errorf("gotify responded %d", resp.StatusCode)
	}
	return nil
}

// --- Email ---

func sendEmail(channel *model.AlertNotifChannel, title, message string) error {
	var cfg model.EmailConfig
	if channel.Config != "" {
		if err := json.Unmarshal([]byte(channel.Config), &cfg); err != nil {
			return fmt.Errorf("parse email config: %w", err)
		}
	}
	if cfg.SMTPHost == "" || cfg.From == "" || cfg.To == "" {
		return fmt.Errorf("email: smtp_host, from, and to are required")
	}
	port := cfg.SMTPPort
	if port == 0 {
		port = 587
	}
	subject := fmt.Sprintf("Octopus: %s", title)
	fromHeader := cfg.From
	toAddrs := strings.Split(cfg.To, ",")
	for i, a := range toAddrs {
		toAddrs[i] = strings.TrimSpace(a)
	}
	var msg strings.Builder
	msg.WriteString("From: " + fromHeader + "\r\n")
	msg.WriteString("To: " + cfg.To + "\r\n")
	msg.WriteString("Subject: " + subject + "\r\n")
	msg.WriteString("MIME-Version: 1.0\r\n")
	msg.WriteString("Content-Type: text/plain; charset=UTF-8\r\n")
	msg.WriteString("\r\n")
	msg.WriteString(message)
	addr := fmt.Sprintf("%s:%d", cfg.SMTPHost, port)
	useTLS := cfg.UseTLS
	if !useTLS && port == 465 {
		useTLS = true
	}
	if cfg.Username == "" && cfg.Password == "" {
		if err := smtp.SendMail(addr, nil, fromHeader, toAddrs, []byte(msg.String())); err != nil {
			return fmt.Errorf("send email (no auth): %w", err)
		}
		return nil
	}
	auth := smtp.PlainAuth("", cfg.Username, cfg.Password, cfg.SMTPHost)
	if err := smtp.SendMail(addr, auth, fromHeader, toAddrs, []byte(msg.String())); err != nil {
		return fmt.Errorf("send email: %w", err)
	}
	return nil
}

// --- Telegram ---

func sendTelegram(channel *model.AlertNotifChannel, title, message string) error {
	var cfg model.TelegramConfig
	if channel.Config != "" {
		if err := json.Unmarshal([]byte(channel.Config), &cfg); err != nil {
			return fmt.Errorf("parse telegram config: %w", err)
		}
	}
	if cfg.BotToken == "" || cfg.ChatID == "" {
		return fmt.Errorf("telegram: bot_token and chat_id are required")
	}
	text := fmt.Sprintf("🐙 %s\n%s", title, message)
	if len(text) > 4096 {
		text = text[:4096]
	}
	body, _ := json.Marshal(map[string]interface{}{
		"chat_id":                  cfg.ChatID,
		"text":                     text,
		"disable_web_page_preview": true,
	})
	endpoint := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", cfg.BotToken)
	req, err := http.NewRequest("POST", endpoint, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create telegram request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	client := notifyHTTPClient()
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("send telegram: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("telegram responded %d: %s", resp.StatusCode, string(respBody))
	}
	var result struct {
		OK          bool   `json:"ok"`
		Description string `json:"description"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err == nil && !result.OK {
		return fmt.Errorf("telegram API error: %s", result.Description)
	}
	return nil
}

// --- Feishu (Lark) ---

func sendFeishu(channel *model.AlertNotifChannel, title, message string) error {
	var cfg model.FeishuConfig
	if channel.Config != "" {
		if err := json.Unmarshal([]byte(channel.Config), &cfg); err != nil {
			return fmt.Errorf("parse feishu config: %w", err)
		}
	}
	if cfg.WebhookKey == "" {
		return fmt.Errorf("feishu: webhook_key is required")
	}
	text := fmt.Sprintf("🐙 %s\n%s", title, message)
	body, _ := json.Marshal(map[string]interface{}{
		"msg_type": "text",
		"content":  map[string]string{"text": text},
	})
	endpoint := fmt.Sprintf("https://open.feishu.cn/open-apis/bot/v2/hook/%s", cfg.WebhookKey)
	req, err := http.NewRequest("POST", endpoint, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create feishu request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	client := notifyHTTPClient()
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("send feishu: %w", err)
	}
	defer resp.Body.Close()
	var result struct {
		Code       int `json:"code"`
		StatusCode int `json:"StatusCode"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err == nil {
		if result.Code != 0 || result.StatusCode != 0 {
			return fmt.Errorf("feishu API error: code=%d statusCode=%d", result.Code, result.StatusCode)
		}
	}
	return nil
}

// --- DingTalk ---

func sendDingTalk(channel *model.AlertNotifChannel, title, message string) error {
	var cfg model.DingTalkConfig
	if channel.Config != "" {
		if err := json.Unmarshal([]byte(channel.Config), &cfg); err != nil {
			return fmt.Errorf("parse dingtalk config: %w", err)
		}
	}
	if cfg.WebhookKey == "" {
		return fmt.Errorf("dingtalk: webhook_key is required")
	}
	endpoint := fmt.Sprintf("https://oapi.dingtalk.com/robot/send?access_token=%s", cfg.WebhookKey)
	if cfg.Secret != "" {
		timestamp := fmt.Sprintf("%d", time.Now().UnixMilli())
		stringToSign := timestamp + "\n" + cfg.Secret
		mac := hmac.New(sha256.New, []byte(cfg.Secret))
		mac.Write([]byte(stringToSign))
		sign := url.QueryEscape(base64.StdEncoding.EncodeToString(mac.Sum(nil)))
		endpoint += "&timestamp=" + timestamp + "&sign=" + sign
	}
	text := fmt.Sprintf("🐙 %s\n%s", title, message)
	body, _ := json.Marshal(map[string]interface{}{
		"msgtype": "text",
		"text":    map[string]string{"content": text},
		"at":      map[string]interface{}{"isAtAll": false},
	})
	req, err := http.NewRequest("POST", endpoint, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create dingtalk request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json;charset=utf-8")
	client := notifyHTTPClient()
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("send dingtalk: %w", err)
	}
	defer resp.Body.Close()
	var result struct {
		ErrCode interface{} `json:"errcode"` // can be number or string
		ErrMsg  string      `json:"errmsg"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err == nil {
		switch v := result.ErrCode.(type) {
		case float64:
			if v != 0 {
				return fmt.Errorf("dingtalk API error: errcode=%v errmsg=%s", v, result.ErrMsg)
			}
		case string:
			if v != "0" && v != "" {
				return fmt.Errorf("dingtalk API error: errcode=%s errmsg=%s", v, result.ErrMsg)
			}
		}
	}
	return nil
}

// --- WeCom (Enterprise WeChat) ---

func sendWeCom(channel *model.AlertNotifChannel, title, message string) error {
	var cfg model.WeComConfig
	if channel.Config != "" {
		if err := json.Unmarshal([]byte(channel.Config), &cfg); err != nil {
			return fmt.Errorf("parse wecom config: %w", err)
		}
	}
	if cfg.WebhookKey == "" {
		return fmt.Errorf("wecom: webhook_key is required")
	}
	text := fmt.Sprintf("🐙 %s\n%s", title, message)
	body, _ := json.Marshal(map[string]interface{}{
		"msgtype": "text",
		"text":    map[string]string{"content": text},
	})
	endpoint := fmt.Sprintf("https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=%s", cfg.WebhookKey)
	req, err := http.NewRequest("POST", endpoint, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create wecom request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	client := notifyHTTPClient()
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("send wecom: %w", err)
	}
	defer resp.Body.Close()
	var result struct {
		ErrCode interface{} `json:"errcode"` // can be number or string
		ErrMsg  string      `json:"errmsg"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err == nil {
		switch v := result.ErrCode.(type) {
		case float64:
			if v != 0 {
				return fmt.Errorf("wecom API error: errcode=%v errmsg=%s", v, result.ErrMsg)
			}
		case string:
			if v != "0" && v != "" {
				return fmt.Errorf("wecom API error: errcode=%s errmsg=%s", v, result.ErrMsg)
			}
		}
	}
	return nil
}

// --- ntfy ---

func sendNtfy(channel *model.AlertNotifChannel, title, message string) error {
	var cfg model.NtfyConfig
	if channel.Config != "" {
		if err := json.Unmarshal([]byte(channel.Config), &cfg); err != nil {
			return fmt.Errorf("parse ntfy config: %w", err)
		}
	}
	if cfg.TopicURL == "" {
		return fmt.Errorf("ntfy: topic_url is required")
	}
	topicURL := cfg.TopicURL
	if !strings.HasPrefix(topicURL, "http://") && !strings.HasPrefix(topicURL, "https://") {
		if strings.Contains(topicURL, "/") || strings.Contains(topicURL, ".") {
			topicURL = "https://" + topicURL
		} else {
			topicURL = "https://ntfy.sh/" + topicURL
		}
	}
	req, err := http.NewRequest("POST", topicURL, strings.NewReader(message))
	if err != nil {
		return fmt.Errorf("create ntfy request: %w", err)
	}
	ntfyTitle := fmt.Sprintf("🐙 Octopus: %s", title)
	if needsMimeEncoding(ntfyTitle) {
		ntfyTitle = mime.QEncoding.Encode("utf-8", ntfyTitle)
	}
	req.Header.Set("Title", ntfyTitle)
	req.Header.Set("Priority", "default")
	req.Header.Set("Tags", "bell")
	if cfg.AccessToken != "" {
		req.Header.Set("Authorization", "Bearer "+cfg.AccessToken)
	}
	client := notifyHTTPClient()
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("send ntfy: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("ntfy responded %d: %s", resp.StatusCode, string(respBody))
	}
	return nil
}

// FormatEmailAddress validates and formats an email address.
func FormatEmailAddress(addr string) (string, error) {
	a, err := mail.ParseAddress(addr)
	if err != nil {
		return "", fmt.Errorf("invalid email address %q: %w", addr, err)
	}
	return a.String(), nil
}
