package model

import "time"

// ModelPriceCategoryRule 分类规则类型。
// 命中语义：
//   - exact:    模型名与 rule_value 完全相等（忽略大小写，model 查询时已转为小写）
//   - prefix:   模型名以 rule_value 为前缀
//   - contains: 模型名包含 rule_value 子串
type ModelPriceCategoryRule string

const (
	ModelPriceCategoryRuleExact    ModelPriceCategoryRule = "exact"
	ModelPriceCategoryRulePrefix   ModelPriceCategoryRule = "prefix"
	ModelPriceCategoryRuleContains ModelPriceCategoryRule = "contains"
)

// ModelPriceCategory 模型价格分类：为未匹配到精确价格的模型提供按规则的兜底定价。
// 当模型名在 DB(LLMInfo) 和内置 presets 都未命中时，price.GetLLMPrice 会按
// sort_order 升序遍历分类，命中第一条 rule 的分类并用其 LLMPrice 作为兜底价。
type ModelPriceCategory struct {
	ID        uint   `json:"id" gorm:"primaryKey;autoIncrement"`
	Name      string `json:"name" gorm:"size:191;not null;uniqueIndex"`
	RuleType  string `json:"rule_type" gorm:"size:32;not null;default:'contains'"`
	RuleValue string `json:"rule_value" gorm:"size:191;not null"`
	LLMPrice
	// SortOrder 越小越优先匹配。
	SortOrder int       `json:"sort_order" gorm:"not null;default:0;index"`
	Enabled   bool      `json:"enabled" gorm:"not null"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}
