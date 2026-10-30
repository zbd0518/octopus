package pool

import (
	"strings"
	"testing"

	"github.com/lingyuins/octopus/internal/model"
	"github.com/lingyuins/octopus/internal/utils/crypto"
)

func TestCredentialsForRelayUsesPrivateDecryptedCopy(t *testing.T) {
	crypto.Init("octopus-test-encryption-key")
	plain := `{"type":"apikey","api_key":"relay-secret"}`
	encrypted, err := crypto.Encrypt(plain)
	if err != nil {
		t.Fatal(err)
	}
	for _, raw := range []string{plain, encrypted} {
		account := &model.PoolAccount{Credentials: raw}
		copy, credential, err := CredentialsForRelay(account)
		if err != nil || credential.APIKey != "relay-secret" || copy.Credentials != plain {
			t.Fatalf("credential decode failed: %v", err)
		}
		if account.Credentials != raw || copy == account {
			t.Fatal("modified original account")
		}
	}
	for _, raw := range []string{"", "enc:broken", "not-json", `{"type":"apikey"}`} {
		_, _, err := CredentialsForRelay(&model.PoolAccount{Credentials: raw})
		if err == nil || strings.Contains(err.Error(), raw) && len(raw) > 10 {
			t.Fatalf("invalid credentials not safely rejected: %v", err)
		}
	}
}
