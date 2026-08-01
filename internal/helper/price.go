package helper

import (
	"context"

	"github.com/lingyuins/octopus/internal/model"
	"github.com/lingyuins/octopus/internal/op/llm"
	"github.com/lingyuins/octopus/internal/price"
)

func LLMPriceAddToDB(modelNames []string, ctx context.Context) error {
	newLLMInfos := make([]model.LLMInfo, 0, len(modelNames))
	newLLMNames := make([]string, 0, len(modelNames))
	for _, modelName := range modelNames {
		if modelName == "" {
			continue
		}
		// 仅从同步价格源（外部价格文件 + 托底价格）查找，外部命中用外部价，
		// 未命中回落托底价，仍未命中写 0（model.LLMPrice 零值）。
		modelPrice := price.GetLLMPriceFromUpstream(modelName)
		if modelPrice != nil {
			newLLMInfos = append(newLLMInfos, model.LLMInfo{
				Name:     modelName,
				LLMPrice: *modelPrice,
			})
		} else {
			newLLMInfos = append(newLLMInfos, model.LLMInfo{Name: modelName})
		}
		newLLMNames = append(newLLMNames, modelName)
	}
	if len(newLLMInfos) > 0 {
		return llm.BatchCreate(newLLMInfos, ctx)
	}
	return nil
}

func LLMPriceDeleteFromDBWithNoPrice(modelNames []string, ctx context.Context) error {
	if len(modelNames) == 0 {
		return nil
	}
	needDeleteModelNames := make([]string, 0, len(modelNames))
	for _, modelName := range modelNames {
		if modelName == "" {
			continue
		}
		modelPrice, err := llm.Get(modelName)
		if err != nil {
			return err
		}
		if modelPrice.Input != 0 || modelPrice.Output != 0 || modelPrice.CacheRead != 0 || modelPrice.CacheWrite != 0 {
			continue
		}
		needDeleteModelNames = append(needDeleteModelNames, modelName)
	}
	if len(needDeleteModelNames) > 0 {
		return llm.BatchDelete(needDeleteModelNames, ctx)
	}
	return nil
}

func LLMPriceRefreshExistingModels(ctx context.Context) error {
	models, err := llm.List(ctx)
	if err != nil {
		return err
	}

	updates := make([]model.LLMInfo, 0, len(models))
	for _, existing := range models {
		// 仅从同步价格源（外部价格文件 + 托底价格）查找，跳过 DB 旧值，
		// 确保"同步价格"真正生效：外部命中用外部价，未命中回落托底价。
		modelPrice := price.GetLLMPriceFromUpstream(existing.Name)
		if modelPrice == nil {
			// 外部与托底价格均未命中：写 0（已为 0 则跳过，避免无谓更新）。
			if existing.Input == 0 && existing.Output == 0 &&
				existing.CacheRead == 0 && existing.CacheWrite == 0 {
				continue
			}
			updates = append(updates, model.LLMInfo{Name: existing.Name})
			continue
		}
		if existing.Input == modelPrice.Input &&
			existing.Output == modelPrice.Output &&
			existing.CacheRead == modelPrice.CacheRead &&
			existing.CacheWrite == modelPrice.CacheWrite {
			continue
		}

		updates = append(updates, model.LLMInfo{
			Name:     existing.Name,
			LLMPrice: *modelPrice,
		})
	}

	return llm.BatchUpdate(updates, ctx)
}
