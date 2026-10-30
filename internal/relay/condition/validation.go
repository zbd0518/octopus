package condition

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/dlclark/regexp2"
	"github.com/lingyuins/octopus/internal/utils/json"
)

func Validate(raw string) error {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	var rules []ConditionRule
	if err := json.Unmarshal([]byte(raw), &rules); err != nil {
		return fmt.Errorf("invalid routing condition")
	}
	for _, rule := range rules {
		switch rule.Key {
		case "model", "api_key_id", "api_key_name", "content_type", "hour":
		default:
			if !strings.HasPrefix(rule.Key, "header_") || len(rule.Key) <= 7 {
				return fmt.Errorf("invalid routing condition field")
			}
		}
		switch rule.Op {
		case "equals", "not_equals", "contains", "not_contains", "starts_with", "ends_with", "in_list":
		case "gt", "lt":
			if _, err := strconv.Atoi(rule.Value); err != nil {
				return fmt.Errorf("invalid numeric routing condition")
			}
		case "regex":
			if _, err := regexp2.Compile(rule.Value, regexp2.ECMAScript); err != nil {
				return fmt.Errorf("invalid routing condition regex")
			}
		default:
			return fmt.Errorf("invalid routing condition operator")
		}
	}
	return nil
}
