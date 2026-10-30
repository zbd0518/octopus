package airoute

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/lingyuins/octopus/internal/model"
	"github.com/lingyuins/octopus/internal/utils/xurl"
)

const (
	aiRouteModelsHTTPTimeout = 30 * time.Second
	aiRouteModelsMaxSize     = 2 << 20
)

// FetchModels lists models from an unsaved AI route service configuration.
func FetchModels(ctx context.Context, baseURL, apiKey string) ([]string, error) {
	baseURL = strings.TrimSpace(baseURL)
	apiKey = strings.TrimSpace(apiKey)
	if baseURL == "" || apiKey == "" {
		return nil, fmt.Errorf("base url and api key are required")
	}
	if err := xurl.AssertSafeURL(baseURL); err != nil {
		return nil, err
	}

	endpoint, err := joinAIRouteModelsURL(baseURL)
	if err != nil {
		return nil, err
	}
	client, err := getAIRouteHTTPClient(aiRouteModelsHTTPTimeout)
	if err != nil {
		return nil, fmt.Errorf("initialize model request client: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("create model request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey)

	resp, err := doAIRouteRequest(client, req)
	if err != nil {
		return nil, fmt.Errorf("fetch models: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("fetch models failed with upstream status %d", resp.StatusCode)
	}

	raw, err := io.ReadAll(io.LimitReader(resp.Body, aiRouteModelsMaxSize+1))
	if err != nil {
		return nil, fmt.Errorf("read model response: %w", err)
	}
	if len(raw) > aiRouteModelsMaxSize {
		return nil, fmt.Errorf("model response exceeds size limit")
	}
	var result model.OpenAIModelList
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil, fmt.Errorf("decode model response: %w", err)
	}

	unique := make(map[string]struct{}, len(result.Data))
	for _, item := range result.Data {
		if id := strings.TrimSpace(item.ID); id != "" {
			unique[id] = struct{}{}
		}
	}
	models := make([]string, 0, len(unique))
	for id := range unique {
		models = append(models, id)
	}
	sort.Strings(models)
	return models, nil
}

func doAIRouteRequest(client *http.Client, req *http.Request) (*http.Response, error) {
	requestCtx, err := xurl.AssertSafeRequestWithPin(req)
	if err != nil {
		return nil, err
	}
	return client.Do(req.WithContext(requestCtx))
}

func joinAIRouteModelsURL(baseURL string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(baseURL))
	if err != nil {
		return "", fmt.Errorf("invalid base url: %w", err)
	}
	if (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return "", fmt.Errorf("base url must be a valid http or https url")
	}
	if parsed.User != nil {
		return "", fmt.Errorf("base url must not contain credentials")
	}
	parsed.RawQuery = ""
	parsed.Fragment = ""
	parsed.Path = strings.TrimRight(parsed.Path, "/")
	for _, suffix := range []string{"/chat/completions", "/responses", "/messages", "/models"} {
		parsed.Path = strings.TrimSuffix(parsed.Path, suffix)
	}
	if !strings.HasSuffix(parsed.Path, "/v1") {
		parsed.Path += "/v1"
	}
	parsed.Path += "/models"
	return parsed.String(), nil
}
