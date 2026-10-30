package condition

import "testing"

func TestConditionValidationAndRealContext(t *testing.T) {
	raw := `[{"key":"api_key_name","op":"equals","value":"named"},{"key":"content_type","op":"equals","value":"application/json"},{"key":"header_x-client","op":"equals","value":"mobile"}]`
	if err := Validate(raw); err != nil {
		t.Fatal(err)
	}
	matched, err := Evaluate(raw, RequestContext{APIKeyName: "named", ContentType: "application/json", Headers: map[string]string{"x-client": "mobile"}})
	if err != nil || !matched {
		t.Fatal(matched, err)
	}
	for _, invalid := range []string{`not json`, `[{"key":"unknown","op":"equals","value":"a"}]`, `[{"key":"model","op":"unknown","value":"a"}]`, `[{"key":"hour","op":"gt","value":"bad"}]`, `[{"key":"model","op":"regex","value":"["}]`} {
		if Validate(invalid) == nil {
			t.Fatal("invalid condition accepted", invalid)
		}
	}
}
