package pool

import (
	"strings"
	"testing"

	"github.com/lingyuins/octopus/internal/model"
)

func TestParseImportedAccountsAcceptsObjectExtra(t *testing.T) {
	raw := `[{"name":"account-1","platform":"openai","type":"apikey","extra":{"project_id":"project-1","header_overrides_enabled":true},"credentials":{"api_key":"sk-test"}}]`

	accounts, err := ParseImportedAccounts(raw, 7)
	if err != nil {
		t.Fatalf("ParseImportedAccounts() error = %v", err)
	}
	if len(accounts) != 1 {
		t.Fatalf("ParseImportedAccounts() returned %d accounts, want 1", len(accounts))
	}
	if got, want := accounts[0].Extra, `{"project_id":"project-1","header_overrides_enabled":true}`; got != want {
		t.Fatalf("Extra = %q, want %q", got, want)
	}
	if got := accounts[0].GetExtra(); got.ProjectID != "project-1" || !got.HeaderOverridesEnabled {
		t.Fatalf("parsed Extra = %+v, want project_id and header_overrides_enabled", got)
	}
	if accounts[0].PoolID != 7 || accounts[0].Platform != model.PoolPlatformOpenAI {
		t.Fatalf("account defaults/fields were not preserved: %+v", accounts[0])
	}
}

func TestParseImportedAccountsPreservesSub2APIOpenAIAccountID(t *testing.T) {
	raw := `[{"name":"account-1","platform":"openai","type":"oauth","credentials":{"access_token":"access","chatgpt_account_id":"chatgpt-acct-1"}}]`

	accounts, err := ParseImportedAccounts(raw, 7)
	if err != nil {
		t.Fatalf("ParseImportedAccounts() error = %v", err)
	}
	cred := model.ParsePoolCredential(accounts[0].Credentials)
	if cred.AccountID != "chatgpt-acct-1" {
		t.Fatalf("AccountID = %q, want chatgpt-acct-1", cred.AccountID)
	}
	if cred.ChatGPTAccountID != "chatgpt-acct-1" {
		t.Fatalf("ChatGPTAccountID = %q, want chatgpt-acct-1", cred.ChatGPTAccountID)
	}
}

func TestParseImportedAccountsAcceptsStringExtra(t *testing.T) {
	raw := `[{"name":"account-1","extra":"{\"project_id\":\"project-1\"}","credentials":{"api_key":"sk-test"}}]`

	accounts, err := ParseImportedAccounts(raw, 7)
	if err != nil {
		t.Fatalf("ParseImportedAccounts() error = %v", err)
	}
	if len(accounts) != 1 {
		t.Fatalf("ParseImportedAccounts() returned %d accounts, want 1", len(accounts))
	}
	if got, want := accounts[0].Extra, `{"project_id":"project-1"}`; got != want {
		t.Fatalf("Extra = %q, want %q", got, want)
	}
}

func TestParseImportedAccountsPreservesNonObjectExtra(t *testing.T) {
	raw := `[{"name":"account-1","extra":123,"credentials":{"api_key":"sk-test"}}]`

	accounts, err := ParseImportedAccounts(raw, 7)
	if err != nil {
		t.Fatalf("ParseImportedAccounts() error = %v", err)
	}
	if got, want := accounts[0].Extra, "123"; got != want {
		t.Fatalf("Extra = %q, want %q", got, want)
	}
	if strings.TrimSpace(accounts[0].Extra) == "" {
		t.Fatal("Extra should not be discarded")
	}
}
