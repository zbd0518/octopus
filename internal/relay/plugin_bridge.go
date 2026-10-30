package relay

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	dbmodel "github.com/lingyuins/octopus/internal/model"
	"github.com/lingyuins/octopus/internal/plugin"
	transmodel "github.com/lingyuins/octopus/internal/transformer/model"
	"github.com/lingyuins/octopus/internal/transformer/outbound"
)

// Raw wire data and adapter metadata remain host-owned when a plugin rewrites
// the internal DTO. Re-encoding must use the original protocol, not the DTO JSON.
func applyBeforeRoutePlugin(ctx context.Context, c *gin.Context, request *transmodel.InternalLLMRequest) (bool, error) {
	if request == nil || !plugin.Handles("relay.request.before_route") {
		return false, nil
	}
	before, err := json.Marshal(request)
	if err != nil {
		return false, err
	}
	payload := struct {
		Request  *transmodel.InternalLLMRequest `json:"request"`
		APIKeyID int                            `json:"api_key_id"`
	}{Request: request, APIKeyID: c.GetInt("api_key_id")}
	result, err := plugin.Invoke(ctx, "relay.request.before_route", payload)
	if err != nil {
		return false, err
	}
	if len(result.Payload) == 0 {
		return false, nil
	}
	var response struct {
		Request *transmodel.InternalLLMRequest `json:"request"`
	}
	if err := json.Unmarshal(result.Payload, &response); err != nil {
		return false, fmt.Errorf("invalid before_route plugin payload: %w", err)
	}
	if response.Request == nil {
		return false, nil
	}
	updated := response.Request
	updated.RawRequest, updated.RawAPIFormat = append([]byte(nil), request.RawRequest...), request.RawAPIFormat
	updated.TransformerMetadata, updated.TransformOptions, updated.Include = request.TransformerMetadata, request.TransformOptions, request.Include
	updated.Query, updated.RawPath = request.Query, request.RawPath
	updated.ConversationID, updated.ResumeFromEventID = request.ConversationID, request.ResumeFromEventID
	updated.Model = strings.TrimSpace(updated.Model)
	after, _ := json.Marshal(updated)
	if bytes.Equal(before, after) {
		return false, nil
	}
	if err := updated.Validate(); err != nil {
		return false, fmt.Errorf("plugin returned invalid request: %w", err)
	}
	raw, err := encodePluginRequestWire(ctx, request, updated)
	if err != nil {
		return false, fmt.Errorf("failed to re-encode plugin request: %w", err)
	}
	updated.RawRequest = raw
	*request = *updated
	return true, nil
}

func encodePluginRequestWire(ctx context.Context, original, updated *transmodel.InternalLLMRequest) ([]byte, error) {
	modelOnly := *updated
	modelOnly.Model = original.Model
	before, err := json.Marshal(original)
	if err != nil {
		return nil, err
	}
	after, err := json.Marshal(&modelOnly)
	if err != nil {
		return nil, err
	}
	if bytes.Equal(before, after) && len(original.RawRequest) > 0 {
		// Model-only edits preserve opaque provider fields and passthrough bodies.
		var wire map[string]json.RawMessage
		if err := json.Unmarshal(original.RawRequest, &wire); err != nil {
			return nil, err
		}
		wire["model"], err = json.Marshal(updated.Model)
		if err != nil {
			return nil, err
		}
		return json.Marshal(wire)
	}
	var kind outbound.OutboundType
	switch original.RawAPIFormat {
	case transmodel.APIFormatOpenAIChatCompletion:
		kind = outbound.OutboundTypeOpenAIChat
	case transmodel.APIFormatOpenAIResponse:
		kind = outbound.OutboundTypeOpenAIResponse
	case transmodel.APIFormatAnthropicMessage:
		kind = outbound.OutboundTypeAnthropic
	case transmodel.APIFormatOpenAIEmbedding:
		kind = outbound.OutboundTypeOpenAIEmbedding
	default:
		return nil, fmt.Errorf("unsupported plugin request format %q", original.RawAPIFormat)
	}
	wire, err := outbound.Get(kind).TransformRequest(ctx, updated, "https://plugin.invalid", "")
	if err != nil {
		return nil, err
	}
	defer wire.Body.Close()
	return io.ReadAll(wire.Body)
}

func applyRouteFilterPlugin(ctx context.Context, group dbmodel.Group, requestModel string, attemptID string) (dbmodel.Group, error) {
	if !plugin.Handles("relay.route.filter") {
		return group, nil
	}
	candidates := make([]int, 0, len(group.Items))
	for _, item := range group.Items {
		candidates = append(candidates, item.ChannelID)
	}
	result, err := plugin.Invoke(ctx, "relay.route.filter", map[string]any{"group_id": group.ID, "model": requestModel, "candidates": candidates})
	if err != nil {
		return group, err
	}
	if len(result.Payload) == 0 {
		return group, nil
	}
	var response struct {
		ExcludeChannelIDs []int `json:"exclude_channel_ids"`
	}
	if err := json.Unmarshal(result.Payload, &response); err != nil {
		return group, fmt.Errorf("invalid route.filter plugin payload: %w", err)
	}
	allowed := make(map[int]bool, len(candidates))
	for _, id := range candidates {
		allowed[id] = true
	}
	for _, id := range response.ExcludeChannelIDs {
		if !allowed[id] {
			return group, fmt.Errorf("plugin attempted to filter an unknown channel")
		}
		allowed[id] = false
	}
	filtered := make([]dbmodel.GroupItem, 0, len(group.Items))
	for _, item := range group.Items {
		if allowed[item.ChannelID] {
			filtered = append(filtered, item)
		}
	}
	group.Items = filtered
	return group, nil
}

var pluginBlockedUpstreamHeaders = map[string]bool{
	"host": true, "content-length": true, "authorization": true, "x-api-key": true, "api-key": true,
	"cookie": true, "proxy-authorization": true, "chatgpt-account-id": true, "connection": true,
	"transfer-encoding": true, "x-forwarded-for": true, "x-real-ip": true,
}

func applyBeforeUpstreamPlugin(ctx context.Context, request *http.Request, channelID int, attemptID string) error {
	if request == nil || !plugin.Handles("relay.request.before_upstream") {
		return nil
	}
	originalBody, err := io.ReadAll(request.Body)
	if err != nil {
		return err
	}
	request.Body = io.NopCloser(bytes.NewReader(originalBody))
	headers := map[string]string{}
	for name := range request.Header {
		if !pluginBlockedUpstreamHeaders[strings.ToLower(name)] {
			headers[name] = request.Header.Get(name)
		}
	}
	result, err := plugin.Invoke(ctx, "relay.request.before_upstream", map[string]any{"channel_id": channelID, "headers": headers, "body": originalBody})
	if err != nil {
		request.Body = io.NopCloser(bytes.NewReader(originalBody))
		return err
	}
	if len(result.Payload) == 0 {
		request.Body = io.NopCloser(bytes.NewReader(originalBody))
		return nil
	}
	var response struct {
		Headers map[string]string `json:"headers"`
		Body    json.RawMessage   `json:"body"`
	}
	if err := json.Unmarshal(result.Payload, &response); err != nil {
		return err
	}
	body := originalBody
	if len(response.Body) > 0 {
		if err := json.Unmarshal(response.Body, &body); err != nil {
			return fmt.Errorf("plugin body must be base64 encoded: %w", err)
		}
		if len(body) > plugin.MaxPayloadBytes {
			return plugin.ErrPayloadTooLarge
		}
	}
	applyPluginUpstreamHeaders(request, response.Headers)
	request.Body = io.NopCloser(bytes.NewReader(body))
	request.ContentLength = int64(len(body))
	return nil
}

func applyPluginUpstreamHeaders(request *http.Request, headers map[string]string) {
	for name, value := range headers {
		lower := strings.ToLower(name)
		if pluginBlockedUpstreamHeaders[lower] || (!strings.HasPrefix(lower, "x-octopus-") && lower != "content-type" && lower != "accept") {
			continue
		}
		request.Header.Set(name, value)
	}
}

var errPluginProcessing = errors.New("plugin processing failed")

func applyBeforeSendPlugin(ctx context.Context, c *gin.Context, body []byte, attemptID string) (resultBody []byte, resultErr error) {
	defer func() {
		if resultErr != nil {
			resultErr = fmt.Errorf("%w: %w", errPluginProcessing, resultErr)
		}
	}()
	if !plugin.Handles("relay.response.before_send") || c.Writer.Written() {
		return body, nil
	}
	result, err := plugin.Invoke(ctx, "relay.response.before_send", map[string]any{"body": body})
	if err != nil {
		return nil, err
	}
	if len(result.Payload) == 0 {
		return body, nil
	}
	var response struct {
		Body json.RawMessage `json:"body"`
	}
	if err := json.Unmarshal(result.Payload, &response); err != nil {
		return nil, err
	}
	if len(response.Body) == 0 {
		return body, nil
	}
	var encoded string
	if err := json.Unmarshal(response.Body, &encoded); err != nil {
		return nil, fmt.Errorf("invalid response body encoding: %w", err)
	}
	body, err = base64.StdEncoding.DecodeString(encoded)
	if err != nil || len(body) > plugin.MaxPayloadBytes || !json.Valid(body) {
		return nil, fmt.Errorf("plugin returned an invalid JSON response")
	}
	if c.Writer.Written() {
		return nil, errors.New("plugin response attempted after response was written")
	}
	return body, nil
}

func publishRelayCompleted(metrics *RelayMetrics, success bool, relayErr error, attempts []dbmodel.ChannelAttempt) {
	if plugin.DefaultManager == nil || metrics == nil {
		return
	}
	message := ""
	if relayErr != nil {
		message = relayErr.Error()
	}
	plugin.Publish("relay.completed", map[string]any{"model": metrics.RequestModel, "api_key_id": metrics.APIKeyID, "success": success, "error": message, "response": metrics.InternalResponse, "attempt_count": len(attempts)})
}
