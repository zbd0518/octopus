package passthrough

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/lingyuins/octopus/internal/transformer/model"
	"github.com/lingyuins/octopus/internal/transformer/outbound/anthropic"
	"github.com/lingyuins/octopus/internal/transformer/outbound/openai"
)

// Outbound forwards the original inbound JSON body to the upstream endpoint.
// Response parsing is delegated to the adapter that matches the original API format.
type Outbound struct {
	delegate model.Outbound
}

func (o *Outbound) TransformRequest(ctx context.Context, request *model.InternalLLMRequest, baseUrl, key string) (*http.Request, error) {
	if request == nil {
		return nil, fmt.Errorf("request is nil")
	}
	if len(request.RawRequest) == 0 {
		return nil, fmt.Errorf("raw request is empty")
	}

	delegate, endpointPath, err := delegateAndEndpointForFormat(request.RawAPIFormat)
	if err != nil {
		return nil, err
	}
	o.delegate = delegate

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "", bytes.NewReader(request.RawRequest))
	if err != nil {
		return nil, fmt.Errorf("failed to create passthrough request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	if request.Stream != nil && *request.Stream {
		req.Header.Set("Accept", "text/event-stream")
	} else {
		req.Header.Set("Accept", "application/json")
	}

	if key != "" {
		if request.RawAPIFormat == model.APIFormatAnthropicMessage {
			req.Header.Set("Anthropic-Version", "2023-06-01")
			req.Header.Set("X-API-Key", key)
		} else {
			req.Header.Set("Authorization", "Bearer "+key)
			req.Header.Set("api-key", key)
		}
	}

	upstreamURL, err := buildPassthroughURL(baseUrl, endpointPath, request.Query, request.RawAPIFormat)
	if err != nil {
		return nil, err
	}
	parsedURL, err := url.Parse(upstreamURL)
	if err != nil {
		return nil, fmt.Errorf("failed to parse passthrough upstream url: %w", err)
	}
	req.URL = parsedURL
	return req, nil
}

func (o *Outbound) TransformResponse(ctx context.Context, response *http.Response) (*model.InternalLLMResponse, error) {
	delegate, err := o.responseDelegate()
	if err != nil {
		return nil, err
	}
	return delegate.TransformResponse(ctx, response)
}

func (o *Outbound) TransformStream(ctx context.Context, eventData []byte) (*model.InternalLLMResponse, error) {
	delegate, err := o.responseDelegate()
	if err != nil {
		return nil, err
	}
	return delegate.TransformStream(ctx, eventData)
}

func (o *Outbound) responseDelegate() (model.Outbound, error) {
	if o.delegate == nil {
		return nil, fmt.Errorf("passthrough response delegate is not initialized")
	}
	return o.delegate, nil
}

func delegateAndEndpointForFormat(format model.APIFormat) (model.Outbound, string, error) {
	switch format {
	case model.APIFormatOpenAIChatCompletion:
		return &openai.ChatOutbound{}, "/v1/chat/completions", nil
	case model.APIFormatOpenAIResponse:
		return &openai.ResponseOutbound{}, "/v1/responses", nil
	case model.APIFormatAnthropicMessage:
		return &anthropic.MessageOutbound{}, "/messages", nil
	default:
		return nil, "", fmt.Errorf("passthrough does not support raw api format %q", format)
	}
}

func buildPassthroughURL(baseURL, endpointPath string, query url.Values, format model.APIFormat) (string, error) {
	if format == model.APIFormatAnthropicMessage {
		parsed, err := url.Parse(strings.TrimSuffix(baseURL, "/"))
		if err != nil {
			return "", fmt.Errorf("failed to parse base url: %w", err)
		}
		parsed.Path = strings.TrimSuffix(parsed.Path, "/") + endpointPath
		if query != nil {
			parsed.RawQuery = query.Encode()
		}
		return parsed.String(), nil
	}

	upstreamURL, err := openai.BuildOpenAIUpstreamURL(baseURL, endpointPath)
	if err != nil {
		return "", err
	}
	parsed, err := url.Parse(upstreamURL)
	if err != nil {
		return "", fmt.Errorf("failed to parse built upstream url: %w", err)
	}
	if query != nil {
		parsed.RawQuery = query.Encode()
	}
	return parsed.String(), nil
}
