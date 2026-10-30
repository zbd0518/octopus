package relay

import (
	"fmt"
	"net/http"
	"time"

	"github.com/lingyuins/octopus/internal/transformer/inbound"
	tm "github.com/lingyuins/octopus/internal/transformer/model"
	"github.com/lingyuins/octopus/internal/utils/semantic_cache"
)

type canonicalCacheResponse struct {
	Version     int                     `json:"version"`
	Response    *tm.InternalLLMResponse `json:"response"`
	ActualModel string                  `json:"actual_model"`
}

func (ra *relayAttempt) storeCanonicalCacheResponse(response *tm.InternalLLMResponse) {
	namespace, text, ok := semanticCacheStoreMetadata(ra.internalRequest)
	if !ok || ra.retryCache == nil || response == nil {
		return
	}
	ra.retryCache.mu.Lock()
	embedding, exists := ra.retryCache.embeddings[namespace]
	ra.retryCache.mu.Unlock()
	if !exists || embedding.err != nil || len(embedding.embedding) == 0 {
		return
	}
	data, err := jsonAPI.Marshal(canonicalCacheResponse{Version: 2, Response: response, ActualModel: ra.internalRequest.Model})
	if err != nil {
		return
	}
	semantic_cache.Store(namespace, text, data, embedding.embedding)
	semantic_cache.RecordStored()
}

func decodeCanonicalCache(payload []byte) (*canonicalCacheResponse, error) {
	var entry canonicalCacheResponse
	if err := jsonAPI.Unmarshal(payload, &entry); err != nil {
		return nil, err
	}
	if entry.Version != 2 || entry.Response == nil {
		return nil, fmt.Errorf("unsupported cached response")
	}
	return &entry, nil
}

func (req *relayRequest) serveCanonicalCache(entry *canonicalCacheResponse) error {
	response := cloneInternalResponse(entry.Response)
	if response == nil {
		return fmt.Errorf("invalid cached response")
	}
	if req.internalRequest.Stream == nil || !*req.internalRequest.Stream {
		data, err := req.inAdapter.TransformResponse(req.clientCtx, response)
		if err != nil {
			return err
		}
		req.c.Data(http.StatusOK, "application/json", semanticCacheHitPayload(data, req.internalRequest))
		return nil
	}
	adapter := req.newInAdapter
	var in tm.Inbound
	if adapter != nil {
		in = adapter()
	} else {
		kind := inbound.InboundTypeOpenAIChat
		if req.internalRequest.RawAPIFormat == tm.APIFormatOpenAIResponse {
			kind = inbound.InboundTypeOpenAIResponse
		}
		in = inbound.Get(kind)
	}
	if in == nil {
		return fmt.Errorf("missing cache replay adapter")
	}
	if len(req.internalRequest.RawRequest) > 0 {
		if _, err := in.TransformRequest(req.clientCtx, req.internalRequest.RawRequest); err != nil {
			return err
		}
	}
	req.c.Header("Content-Type", "text/event-stream")
	req.c.Header("Cache-Control", "no-cache")
	write := func(data []byte) error {
		if req.streamSession != nil {
			for _, event := range req.streamSession.AddPayload(data) {
				if _, err := req.c.Writer.Write(formatRelaySSEEvent(event.Sequence, event.Payload)); err != nil {
					return err
				}
			}
		} else if _, err := req.c.Writer.Write(data); err != nil {
			return err
		}
		req.c.Writer.Flush()
		return nil
	}
	for _, choice := range response.Choices {
		if choice.Message == nil {
			continue
		}
		chunk := &tm.InternalLLMResponse{ID: response.ID, Model: response.Model, Object: "chat.completion.chunk", Created: time.Now().Unix(), Choices: []tm.Choice{{Index: choice.Index, Delta: choice.Message}}}
		data, err := in.TransformStream(req.clientCtx, chunk)
		if err != nil {
			return err
		}
		if err = write(data); err != nil {
			return err
		}
		finish := "stop"
		if choice.FinishReason != nil {
			finish = *choice.FinishReason
		}
		chunk.Choices = []tm.Choice{{Index: choice.Index, Delta: &tm.Message{}, FinishReason: &finish}}
		chunk.Usage = response.Usage
		data, err = in.TransformStream(req.clientCtx, chunk)
		if err != nil {
			return err
		}
		if err = write(data); err != nil {
			return err
		}
	}
	data, err := in.TransformStream(req.clientCtx, &tm.InternalLLMResponse{Object: "[DONE]"})
	if err != nil {
		return err
	}
	if err = write(data); err != nil {
		return err
	}
	if req.streamSession != nil {
		req.streamSession.Finish(nil)
	}
	return nil
}
