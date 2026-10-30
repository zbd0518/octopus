package airoute

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// ChatService is the public, credential-bearing subset used by other
// background AI workflows that share the AI route service pool.
type ChatService struct {
	Index   int
	Name    string
	BaseURL string
	APIKey  string
	Model   string
}

// LoadChatServices returns the enabled AI route services using the same
// configuration and legacy fallback as AI route generation.
func LoadChatServices() ([]ChatService, error) {
	services, err := loadAIRouteServicesSnapshot()
	if err != nil {
		return nil, err
	}
	result := make([]ChatService, 0, len(services))
	for _, service := range services {
		result = append(result, ChatService{
			Index:   service.Index,
			Name:    service.Name,
			BaseURL: service.BaseURL,
			APIKey:  service.APIKey,
			Model:   service.Model,
		})
	}
	return result, nil
}

// CallChatCompletion reuses the hardened OpenAI-compatible client used by
// AI route generation, including JSON-mode fallback and SSRF protections.
func CallChatCompletion(ctx context.Context, service ChatService, systemPrompt, userPrompt string) (string, error) {
	return callAIRouteChatCompletion(ctx, aiRouteService{
		Index:   service.Index,
		Name:    service.Name,
		BaseURL: service.BaseURL,
		APIKey:  service.APIKey,
		Model:   service.Model,
	}, systemPrompt, userPrompt)
}

// ChatServicePool is the shared failover/concurrency-aware service pool used
// by background AI workflows. Keeping the pool here makes normalization and
// AI route generation use the same cooldown and retry semantics.
type ChatServicePool struct {
	pool  aiRouteServicePool
	count int
}

func NewChatServicePool(services []ChatService) (*ChatServicePool, error) {
	if len(services) == 0 {
		return nil, fmt.Errorf("没有可用的 AI 路由分析服务")
	}
	internal := make([]aiRouteService, 0, len(services))
	for _, service := range services {
		internal = append(internal, aiRouteService{
			Index:   service.Index,
			Name:    service.Name,
			BaseURL: service.BaseURL,
			APIKey:  service.APIKey,
			Model:   service.Model,
		})
	}
	return &ChatServicePool{
		pool:  newAIRouteServicePool(internal),
		count: len(internal),
	}, nil
}

func (p *ChatServicePool) Call(ctx context.Context, batchIndex int, systemPrompt, userPrompt string) (string, error) {
	if p == nil || p.pool == nil || p.count == 0 {
		return "", fmt.Errorf("没有可用的 AI 路由分析服务")
	}

	hint := aiRouteServiceHint{BucketIndex: batchIndex}
	exclude := make(map[int]struct{}, p.count)
	lease, err := acquireChatLease(ctx, p.pool, hint, nil)
	if err != nil {
		return "", err
	}

	for {
		content, callErr := callAIRouteChatCompletion(ctx, lease.Service, systemPrompt, userPrompt)
		if callErr == nil {
			p.pool.Release(lease, aiRouteServiceOutcome{Success: true})
			return content, nil
		}

		outcome := aiRouteServiceOutcome{Err: callErr}
		var routeErr *aiRouteCallError
		if errors.As(callErr, &routeErr) && routeErr.Retryable && ctx.Err() == nil {
			outcome.Retryable = true
			outcome.Cooldown = routeErr.Cooldown
		}
		p.pool.Release(lease, outcome)
		if !outcome.Retryable {
			return "", callErr
		}

		exclude[lease.Index] = struct{}{}
		if len(exclude) >= p.count {
			lease, err = acquireChatLease(ctx, p.pool, hint, nil)
			if err != nil {
				return "", err
			}
			exclude = make(map[int]struct{}, p.count)
			continue
		}
		lease, err = acquireChatLease(ctx, p.pool, hint, exclude)
		if err != nil {
			return "", err
		}
	}
}

func acquireChatLease(
	ctx context.Context,
	pool aiRouteServicePool,
	hint aiRouteServiceHint,
	exclude map[int]struct{},
) (*aiRouteServiceLease, error) {
	for {
		lease, err := pool.Acquire(ctx, hint, exclude)
		if err == nil {
			return lease, nil
		}
		var cooldownErr *aiRouteCooldownError
		if !errors.As(err, &cooldownErr) || cooldownErr.RetryAfter <= 0 {
			return nil, err
		}
		timer := time.NewTimer(cooldownErr.RetryAfter)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				<-timer.C
			}
			return nil, ctx.Err()
		case <-timer.C:
		}
	}
}

func ChatParallelism() int {
	return loadAIRouteParallelism()
}

func ChatMaxModelsPerRequest() int {
	return getAIRouteMaxModelsPerRequest()
}

func ClampChatParallelism(value, batchCount int) int {
	return clampAIRouteParallelism(value, batchCount)
}
