package llm

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/lingyuins/octopus/internal/db"
	"github.com/lingyuins/octopus/internal/model"
)

// priceCategoryCache 缓存启用且按 sort_order 排序的分类，供 price.GetLLMPrice
// 兜底匹配热点路径使用，避免每次查询都访问 DB。与 modelCache 类似的全局内存缓存，
// 由 Create/Update/Delete/RefreshPriceCategoryCache 维护。
var priceCategoryCache = modelPriceCategoryList(nil)

// modelPriceCategoryList 是只读的快照切片类型，便于并发安全地整体替换。
type modelPriceCategoryList []model.ModelPriceCategory

// ListPriceCategories 返回全部分类（含禁用），按 sort_order 升序。
func ListPriceCategories(ctx context.Context) ([]model.ModelPriceCategory, error) {
	rows := []model.ModelPriceCategory{}
	if err := db.GetDB().WithContext(ctx).Order("sort_order ASC, id ASC").Find(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

// listEnabledPriceCategories 返回启用的分类快照（复制，避免调用方持有全局切片引用）。
func listEnabledPriceCategories() []model.ModelPriceCategory {
	c := priceCategoryCache
	out := make([]model.ModelPriceCategory, len(c))
	copy(out, c)
	return out
}

// RefreshPriceCategoryCache 从 DB 重载启用分类进内存缓存。
func RefreshPriceCategoryCache(ctx context.Context) error {
	rows := []model.ModelPriceCategory{}
	if err := db.GetDB().WithContext(ctx).
		Where("enabled = ?", true).
		Order("sort_order ASC, id ASC").
		Find(&rows).Error; err != nil {
		return err
	}
	priceCategoryCache = modelPriceCategoryList(rows)
	return nil
}

func getPriceCategoryByName(name string) (model.ModelPriceCategory, error) {
	var row model.ModelPriceCategory
	err := db.GetDB().Where("name = ?", strings.ToLower(strings.TrimSpace(name))).First(&row).Error
	return row, err
}

// CreatePriceCategory 创建分类，name 转小写，rule_type 校验。
func CreatePriceCategory(c model.ModelPriceCategory, ctx context.Context) (model.ModelPriceCategory, error) {
	c.Name = strings.ToLower(strings.TrimSpace(c.Name))
	if c.Name == "" {
		return c, fmt.Errorf("name is required")
	}
	if err := validatePriceCategory(c); err != nil {
		return c, err
	}
	if err := db.GetDB().WithContext(ctx).Create(&c).Error; err != nil {
		return c, err
	}
	if err := RefreshPriceCategoryCache(ctx); err != nil {
		return c, err
	}
	return c, nil
}

// UpdatePriceCategory 更新分类。不存在的 ID 返回 error。
func UpdatePriceCategory(c model.ModelPriceCategory, ctx context.Context) (model.ModelPriceCategory, error) {
	var existing model.ModelPriceCategory
	if err := db.GetDB().WithContext(ctx).First(&existing, c.ID).Error; err != nil {
		return c, err
	}
	c.Name = strings.ToLower(strings.TrimSpace(c.Name))
	if c.Name == "" {
		return c, fmt.Errorf("name is required")
	}
	if err := validatePriceCategory(c); err != nil {
		return c, err
	}
	if err := db.GetDB().WithContext(ctx).Save(&c).Error; err != nil {
		return c, err
	}
	if err := RefreshPriceCategoryCache(ctx); err != nil {
		return c, err
	}
	return c, nil
}

// DeletePriceCategory 删除分类。
func DeletePriceCategory(id uint, ctx context.Context) error {
	if err := db.GetDB().WithContext(ctx).Delete(&model.ModelPriceCategory{}, id).Error; err != nil {
		return err
	}
	return RefreshPriceCategoryCache(ctx)
}

func validatePriceCategory(c model.ModelPriceCategory) error {
	switch model.ModelPriceCategoryRule(c.RuleType) {
	case model.ModelPriceCategoryRuleExact,
		model.ModelPriceCategoryRulePrefix,
		model.ModelPriceCategoryRuleContains:
	default:
		return fmt.Errorf("invalid rule_type: %s", c.RuleType)
	}
	if strings.TrimSpace(c.RuleValue) == "" {
		return fmt.Errorf("rule_value is required")
	}
	if c.Input < 0 || c.Output < 0 || c.CacheRead < 0 || c.CacheWrite < 0 {
		return fmt.Errorf("price must be non-negative")
	}
	return nil
}

// categoryMatches 判断 modelName（小写后）是否命中分类规则。
func categoryMatches(c model.ModelPriceCategory, modelName string) bool {
	rule := model.ModelPriceCategoryRule(c.RuleType)
	v := c.RuleValue
	switch rule {
	case model.ModelPriceCategoryRuleExact:
		return modelName == v
	case model.ModelPriceCategoryRulePrefix:
		return strings.HasPrefix(modelName, v)
	default:
		return strings.Contains(modelName, v)
	}
}

// PriceCategoryMatch 在启用分类快照中按 sort_order 找第一个命中的分类，
// 命中则返回其兜底价 LLMPrice，否则返回 nil。供 price.GetLLMPrice 兜底链调用。
func PriceCategoryMatch(modelName string) *model.LLMPrice {
	rows := listEnabledPriceCategories()
	for i := range rows {
		if categoryMatches(rows[i], modelName) {
			p := rows[i].LLMPrice
			return &p
		}
	}
	return nil
}

// SortPriceCategoriesForDisplay 按 sort_order 升序排序（供管理端稳定展示，DB 查询已有序，此处冗余防御）。
func SortPriceCategoriesForDisplay(rows []model.ModelPriceCategory) {
	sort.SliceStable(rows, func(i, j int) bool {
		if rows[i].SortOrder != rows[j].SortOrder {
			return rows[i].SortOrder < rows[j].SortOrder
		}
		return rows[i].ID < rows[j].ID
	})
}
