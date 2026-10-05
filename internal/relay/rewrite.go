package relay

import (
	"fmt"
	"strings"

	appmodel "github.com/lingyuins/octopus/internal/model"
	transmodel "github.com/lingyuins/octopus/internal/transformer/model"
	"github.com/lingyuins/octopus/internal/transformer/rewrite"
	"github.com/lingyuins/octopus/internal/utils/log"
)

func prepareInternalRequestForOutbound(channel *appmodel.Channel, request *transmodel.InternalLLMRequest, group *appmodel.Group) (*transmodel.InternalLLMRequest, *rewrite.EffectiveConfig, error) {
	if channel == nil {
		return nil, nil, fmt.Errorf("channel is nil")
	}
	if request == nil {
		return nil, nil, fmt.Errorf("request is nil")
	}

	effectiveRewrite, enabled, err := rewrite.Resolve(channel.Type, channel.RequestRewrite)
	if err != nil {
		return nil, nil, err
	}

	cloned := *request
	cloned.TransformerMetadata = make(map[string]string, len(request.TransformerMetadata))
	for key, value := range request.TransformerMetadata {
		cloned.TransformerMetadata[key] = value
	}
	request = &cloned
	var target *transmodel.InternalLLMRequest
	if !enabled {
		target = cloneRequestForOutbound(request)
	} else {
		rewritten, applyErr := rewrite.Apply(request, effectiveRewrite)
		if applyErr != nil {
			return nil, nil, applyErr
		}
		target = rewritten
	}

	applyParamOverride(channel, target)
	if group != nil {
		// 分组强制思考模式（upstream）：auto 保留客户端控制。
		target = transmodel.WithThinkingMode(target, group.ThinkingMode)
		// 本仓库附加：端点类型 + 提供方元数据，供 deepseek/mimo 兼容层区分上游
		// wire-format（provider 非 auto 时才落库，见 attachRelayGroupEndpointMetadata）。
		attachRelayGroupEndpointMetadata(target, group.EndpointType, group.EndpointProvider)
	}
	return target, effectiveRewrite, nil
}

// applyParamOverride merges channel-level param_override JSON into the outbound request.
// Only overrides fields that are not already set by the client request (client takes precedence).
func cloneRequestForOutbound(request *transmodel.InternalLLMRequest) *transmodel.InternalLLMRequest {
	if request == nil {
		return nil
	}

	cloned := *request
	if len(request.Messages) > 0 {
		cloned.Messages = append([]transmodel.Message(nil), request.Messages...)
	}
	if request.StreamOptions != nil {
		streamOptions := *request.StreamOptions
		cloned.StreamOptions = &streamOptions
	}
	if request.TransformerMetadata != nil {
		cloned.TransformerMetadata = make(map[string]string, len(request.TransformerMetadata))
		for key, value := range request.TransformerMetadata {
			cloned.TransformerMetadata[key] = value
		}
	}
	return &cloned
}

func applyParamOverride(channel *appmodel.Channel, request *transmodel.InternalLLMRequest) {
	if channel == nil || channel.ParamOverride == nil || *channel.ParamOverride == "" {
		return
	}
	if request == nil {
		return
	}

	var overrides map[string]RawMessage
	if err := jsonAPI.Unmarshal([]byte(*channel.ParamOverride), &overrides); err != nil {
		log.Warnf("param_override: invalid JSON for channel %d: %v", channel.ID, err)
		return
	}

	if v, ok := overrides["max_tokens"]; ok && request.MaxTokens == nil {
		var val int64
		if err := jsonAPI.Unmarshal(v, &val); err == nil {
			request.MaxTokens = &val
		}
	}
	if v, ok := overrides["max_completion_tokens"]; ok && request.MaxCompletionTokens == nil {
		var val int64
		if err := jsonAPI.Unmarshal(v, &val); err == nil {
			request.MaxCompletionTokens = &val
		}
	}
	if v, ok := overrides["temperature"]; ok && request.Temperature == nil {
		var val float64
		if err := jsonAPI.Unmarshal(v, &val); err == nil {
			request.Temperature = &val
		}
	}
	if v, ok := overrides["top_p"]; ok && request.TopP == nil {
		var val float64
		if err := jsonAPI.Unmarshal(v, &val); err == nil {
			request.TopP = &val
		}
	}
}

func attachRelayGroupEndpointMetadata(request *transmodel.InternalLLMRequest, groupEndpointType, groupEndpointProvider string) {
	if request == nil {
		return
	}

	normalizedEndpointType := appmodel.NormalizeEndpointType(groupEndpointType)
	normalizedEndpointProvider := strings.ToLower(strings.TrimSpace(groupEndpointProvider))
	if normalizedEndpointType == "" && normalizedEndpointProvider == "" {
		return
	}

	if request.TransformerMetadata == nil {
		request.TransformerMetadata = make(map[string]string)
	}
	if normalizedEndpointType != "" {
		request.TransformerMetadata[transmodel.TransformerMetadataGroupEndpointType] = normalizedEndpointType
	}
	if normalizedEndpointProvider != "" && normalizedEndpointProvider != "auto" {
		request.TransformerMetadata[transmodel.TransformerMetadataGroupEndpointProvider] = normalizedEndpointProvider
	}
}
