package backup

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	internaldb "github.com/lingyuins/octopus/internal/db"
	"github.com/lingyuins/octopus/internal/transformer/outbound"
	"github.com/lingyuins/octopus/internal/model"
	"github.com/lingyuins/octopus/internal/utils/apikeyhash"
	"github.com/lingyuins/octopus/internal/utils/crypto"
)

// TestMain initializes the process-wide crypto key for this package's tests.
// Uses the repo-wide shared test key (same constant as other packages' TestMain).
func TestMain(m *testing.M) {
	crypto.Init("octopus-test-encryption-key")
	os.Exit(m.Run())
}

// resetCryptoKeyForTest switches the process crypto key mid-test (simulating a
// different instance with a different encryption_key). t.Cleanup restores the
// shared test key so later tests in the same process are unaffected.
func resetCryptoKeyForTest(t *testing.T, key string) {
	t.Helper()
	crypto.ResetForTest()
	crypto.Init(key)
	t.Cleanup(func() {
		crypto.ResetForTest()
		crypto.Init("octopus-test-encryption-key")
	})
}

func mustEncrypt(t *testing.T, plain string) string {
	t.Helper()
	enc, err := crypto.Encrypt(plain)
	if err != nil {
		t.Fatalf("encrypt %q: %v", plain, err)
	}
	if !crypto.IsEncrypted(enc) {
		t.Fatalf("encrypted value %q lost enc: prefix", enc)
	}
	return enc
}

// TestExportDecryptsSensitiveFields verifies issue #247: sensitive fields that
// are stored as enc: ciphertext are exported as plaintext, making the backup
// portable across instances with different encryption keys.
func TestExportDecryptsSensitiveFields(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "enc_export.db")
	if err := internaldb.InitDB("sqlite", dbPath, false); err != nil {
		t.Fatalf("init db: %v", err)
	}
	t.Cleanup(func() { _ = internaldb.Close() })
	conn := internaldb.GetDB()

	const (
		plainAPIKey     = "sk-plain-api-key-1"
		plainChanKey    = "sk-upstream-channel-key-1"
		plainCredKey    = "sk-credential-profile-key-1"
		plainSiteToken  = "sk-remote-site-access-token-1"
		plainSitePass   = "remote-site-password-1"
		plainRSTokenKey = "sk-remote-site-token-key-1"
		plainAccPass    = "account-password-1"
		plainAccToken   = "account-access-token-1"
		plainAccKey     = "sk-account-api-key-1"
		plainAccRefresh = "account-refresh-token-1"
		plainSiteTok    = "sk-site-token-1"
	)

	if err := conn.Create(&model.Channel{ID: 1, Name: "ch1", Type: outbound.OutboundTypeOpenAIChat}).Error; err != nil {
		t.Fatalf("seed channels: %v", err)
	}
	if err := conn.Create(&model.APIKey{ID: 1, Name: "k1", APIKey: mustEncrypt(t, plainAPIKey), APIKeyHash: apikeyhash.Sum(plainAPIKey)}).Error; err != nil {
		t.Fatalf("seed api_keys: %v", err)
	}
	if err := conn.Create(&model.ChannelKey{ID: 1, ChannelID: 1, ChannelKey: mustEncrypt(t, plainChanKey)}).Error; err != nil {
		t.Fatalf("seed channel_keys: %v", err)
	}
	if err := conn.Create(&model.APICredentialProfile{ID: 1, Name: "p1", BaseURL: "https://api.example.com", APIKey: mustEncrypt(t, plainCredKey)}).Error; err != nil {
		t.Fatalf("seed api_credential_profiles: %v", err)
	}
	if err := conn.Create(&model.RemoteSite{ID: 1, Name: "rs1", BaseURL: "https://remote.example.com", SiteType: model.SiteTypeNewAPI, AuthType: model.AuthTypeAccessToken, AccessToken: mustEncrypt(t, plainSiteToken), Password: mustEncrypt(t, plainSitePass)}).Error; err != nil {
		t.Fatalf("seed remote_sites: %v", err)
	}
	if err := conn.Create(&model.RemoteSiteToken{ID: 1, RemoteSiteID: 1, Key: mustEncrypt(t, plainRSTokenKey)}).Error; err != nil {
		t.Fatalf("seed remote_site_tokens: %v", err)
	}
	if err := conn.Create(&model.Site{ID: 1, Name: "s1", Platform: model.SitePlatformNewAPI, BaseURL: "https://site.example.com", ProxyMode: model.ProxyUsageModeDirect}).Error; err != nil {
		t.Fatalf("seed sites: %v", err)
	}
	if err := conn.Create(&model.SiteAccount{ID: 1, SiteID: 1, Name: "acc1", CredentialType: model.SiteCredentialTypeAccessToken, Password: mustEncrypt(t, plainAccPass), AccessToken: mustEncrypt(t, plainAccToken), APIKey: mustEncrypt(t, plainAccKey), RefreshToken: mustEncrypt(t, plainAccRefresh)}).Error; err != nil {
		t.Fatalf("seed site_accounts: %v", err)
	}
	// Seed a second account (ID 2) so the token row has a valid parent;
	// account 1 above already exercises the site_accounts sensitive fields.
	if err := conn.Create(&model.SiteAccount{ID: 2, SiteID: 1, Name: "acc2", CredentialType: model.SiteCredentialTypeAccessToken}).Error; err != nil {
		t.Fatalf("seed site_account 2: %v", err)
	}
	if err := conn.Create(&model.SiteToken{ID: 1, SiteAccountID: 2, Token: mustEncrypt(t, plainSiteTok), ValueStatus: model.SiteTokenValueStatusReady}).Error; err != nil {
		t.Fatalf("seed site_tokens: %v", err)
	}

	dump, err := ExportAll(context.Background(), false, false)
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	if dump.Version != 2 {
		t.Fatalf("dump version = %d, want 2", dump.Version)
	}

	assertPlain := func(field, got, want string) {
		t.Helper()
		if got != want {
			t.Fatalf("%s = %q, want plaintext %q", field, got, want)
		}
		if crypto.IsEncrypted(got) {
			t.Fatalf("%s still carries enc: prefix", field)
		}
	}
	assertPlain("api_keys.api_key", dump.APIKeys[0].APIKey, plainAPIKey)
	assertPlain("channel_keys.channel_key", dump.ChannelKeys[0].ChannelKey, plainChanKey)
	assertPlain("api_credential_profiles.api_key", dump.APICredentialProfiles[0].APIKey, plainCredKey)
	assertPlain("remote_sites.access_token", dump.RemoteSites[0].AccessToken, plainSiteToken)
	assertPlain("remote_sites.password", dump.RemoteSites[0].Password, plainSitePass)
	assertPlain("remote_site_tokens.key", dump.RemoteSiteTokens[0].Key, plainRSTokenKey)
	assertPlain("site_accounts.password", dump.SiteAccounts[0].Password, plainAccPass)
	assertPlain("site_accounts.access_token", dump.SiteAccounts[0].AccessToken, plainAccToken)
	assertPlain("site_accounts.api_key", dump.SiteAccounts[0].APIKey, plainAccKey)
	assertPlain("site_accounts.refresh_token", dump.SiteAccounts[0].RefreshToken, plainAccRefresh)
	assertPlain("site_tokens.token", dump.SiteTokens[0].Token, plainSiteTok)
}

// TestExportKeepsUndecryptableValues verifies that a row whose ciphertext
// cannot be decrypted (corrupt payload / key mismatch) does not fail the
// export — the original value is kept as-is.
func TestExportKeepsUndecryptableValues(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "enc_undecryptable.db")
	if err := internaldb.InitDB("sqlite", dbPath, false); err != nil {
		t.Fatalf("init db: %v", err)
	}
	t.Cleanup(func() { _ = internaldb.Close() })
	conn := internaldb.GetDB()

	const broken = "enc:!!!not-valid-base64!!!"
	if err := conn.Create(&model.Site{ID: 1, Name: "s1", Platform: model.SitePlatformNewAPI, BaseURL: "https://site.example.com", ProxyMode: model.ProxyUsageModeDirect}).Error; err != nil {
		t.Fatalf("seed sites: %v", err)
	}
	if err := conn.Create(&model.SiteAccount{ID: 1, SiteID: 1, Name: "acc1", CredentialType: model.SiteCredentialTypeAccessToken}).Error; err != nil {
		t.Fatalf("seed site_accounts: %v", err)
	}
	if err := conn.Create(&model.SiteToken{ID: 1, SiteAccountID: 1, Token: broken, ValueStatus: model.SiteTokenValueStatusReady}).Error; err != nil {
		t.Fatalf("seed site_tokens: %v", err)
	}

	dump, err := ExportAll(context.Background(), false, false)
	if err != nil {
		t.Fatalf("export should tolerate undecryptable values: %v", err)
	}
	if len(dump.SiteTokens) != 1 {
		t.Fatalf("exported site_tokens = %d, want 1", len(dump.SiteTokens))
	}
	if dump.SiteTokens[0].Token != broken {
		t.Fatalf("undecryptable token = %q, want original %q", dump.SiteTokens[0].Token, broken)
	}
}

// TestImportEncryptsPlaintextAndComputesHash verifies the import side of
// issue #247: a v2 dump (plaintext secrets) is re-encrypted on import and
// api_keys rows get their deterministic api_key_hash filled.
func TestImportEncryptsPlaintextAndComputesHash(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "enc_import.db")
	if err := internaldb.InitDB("sqlite", dbPath, false); err != nil {
		t.Fatalf("init db: %v", err)
	}
	t.Cleanup(func() { _ = internaldb.Close() })
	conn := internaldb.GetDB()

	const plainKey = "sk-import-plain-key-1"
	dump := &model.DBDump{
		Version: 2,
		APIKeys: []model.APIKey{
			{ID: 1, Name: "k1", APIKey: plainKey},
		},
		SiteTokens: []model.SiteToken{
			{ID: 1, SiteAccountID: 1, Token: "sk-import-site-token-1", ValueStatus: model.SiteTokenValueStatusReady},
		},
	}
	if _, err := ImportWithModeToDB(context.Background(), conn, dump, model.ImportModeIncremental); err != nil {
		t.Fatalf("import: %v", err)
	}

	var key model.APIKey
	if err := conn.First(&key, 1).Error; err != nil {
		t.Fatalf("query api_keys: %v", err)
	}
	if !crypto.IsEncrypted(key.APIKey) {
		t.Fatalf("stored api_key = %q, want enc: ciphertext", key.APIKey)
	}
	if want := apikeyhash.Sum(plainKey); key.APIKeyHash != want {
		t.Fatalf("api_key_hash = %q, want %q", key.APIKeyHash, want)
	}
	dec, err := crypto.Decrypt(key.APIKey)
	if err != nil || dec != plainKey {
		t.Fatalf("decrypt stored api_key = %q, %v; want %q", dec, err, plainKey)
	}

	var tok model.SiteToken
	if err := conn.First(&tok, 1).Error; err != nil {
		t.Fatalf("query site_tokens: %v", err)
	}
	if !crypto.IsEncrypted(tok.Token) {
		t.Fatalf("stored site token = %q, want enc: ciphertext", tok.Token)
	}
}

// TestImportIdempotentOnEncryptedValues verifies that a dump whose sensitive
// fields already carry enc: ciphertext (e.g. a v1 dump restored into the same
// instance) is imported unchanged — no double encryption, hash untouched.
func TestImportIdempotentOnEncryptedValues(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "enc_idempotent.db")
	if err := internaldb.InitDB("sqlite", dbPath, false); err != nil {
		t.Fatalf("init db: %v", err)
	}
	t.Cleanup(func() { _ = internaldb.Close() })
	conn := internaldb.GetDB()

	const plainKey = "sk-idempotent-key-1"
	encKey := mustEncrypt(t, plainKey)
	encHash := apikeyhash.Sum(plainKey)

	dump := &model.DBDump{
		Version: 1, // v1 dump: fields carry the (same-instance) ciphertext.
		APIKeys: []model.APIKey{
			{ID: 1, Name: "k1", APIKey: encKey, APIKeyHash: encHash},
		},
	}
	if _, err := ImportWithModeToDB(context.Background(), conn, dump, model.ImportModeIncremental); err != nil {
		t.Fatalf("import: %v", err)
	}

	var key model.APIKey
	if err := conn.First(&key, 1).Error; err != nil {
		t.Fatalf("query api_keys: %v", err)
	}
	if key.APIKey != encKey {
		t.Fatalf("encrypted api_key was re-encrypted: got %q, want unchanged %q", key.APIKey, encKey)
	}
	if key.APIKeyHash != encHash {
		t.Fatalf("api_key_hash of encrypted row changed: got %q, want %q", key.APIKeyHash, encHash)
	}
}

// TestCrossKeyRoundTrip is the core regression for issue #247: export from an
// instance with key A, switch the process key to key B (simulating a different
// instance), import, and verify the stored ciphertext is decryptable under
// key B back to the original plaintext.
func TestCrossKeyRoundTrip(t *testing.T) {
	const plainSiteToken = "sk-cross-key-site-token-1"
	const plainAPIKey = "sk-cross-key-api-key-1"

	// --- Instance A: seed encrypted data and export. ---
	sourcePath := filepath.Join(t.TempDir(), "source.db")
	if err := internaldb.InitDB("sqlite", sourcePath, false); err != nil {
		t.Fatalf("init source db: %v", err)
	}
	srcConn := internaldb.GetDB()
	if err := srcConn.Create(&model.Site{ID: 1, Name: "s1", Platform: model.SitePlatformNewAPI, BaseURL: "https://site.example.com", ProxyMode: model.ProxyUsageModeDirect}).Error; err != nil {
		t.Fatalf("seed source sites: %v", err)
	}
	if err := srcConn.Create(&model.SiteAccount{ID: 1, SiteID: 1, Name: "acc1", CredentialType: model.SiteCredentialTypeAccessToken}).Error; err != nil {
		t.Fatalf("seed source site_accounts: %v", err)
	}
	if err := srcConn.Create(&model.SiteToken{ID: 1, SiteAccountID: 1, Token: mustEncrypt(t, plainSiteToken), ValueStatus: model.SiteTokenValueStatusReady}).Error; err != nil {
		t.Fatalf("seed source site_tokens: %v", err)
	}
	if err := srcConn.Create(&model.APIKey{ID: 1, Name: "k1", APIKey: mustEncrypt(t, plainAPIKey), APIKeyHash: apikeyhash.Sum(plainAPIKey)}).Error; err != nil {
		t.Fatalf("seed source api_keys: %v", err)
	}
	dump, err := ExportAll(context.Background(), false, false)
	if err != nil {
		t.Fatalf("export from instance A: %v", err)
	}
	if dump.SiteTokens[0].Token != plainSiteToken || dump.APIKeys[0].APIKey != plainAPIKey {
		t.Fatalf("dump should carry plaintext secrets: %+v / %+v", dump.SiteTokens[0], dump.APIKeys[0])
	}
	// Release the source DB file handle (Windows: TempDir cleanup fails on
	// open files). db.Close leaves the global pointer stale, which is fine
	// here — the rest of this test only uses the standalone target handle.
	if sqlDB, err := srcConn.DB(); err == nil {
		_ = sqlDB.Close()
	}

	// --- Switch to instance B's key (cross-instance import). ---
	resetCryptoKeyForTest(t, "octopus-test-encryption-key-instance-b")

	// --- Instance B: import into a fresh database. ---
	targetPath := filepath.Join(t.TempDir(), "target.db")
	target, err := internaldb.OpenStandalone("sqlite", targetPath, false)
	if err != nil {
		t.Fatalf("open target db: %v", err)
	}
	if err := internaldb.Migrate(target); err != nil {
		t.Fatalf("migrate target db: %v", err)
	}
	t.Cleanup(func() {
		if sqlDB, e := target.DB(); e == nil {
			_ = sqlDB.Close()
		}
	})

	if _, err := ImportWithModeToDB(context.Background(), target, dump, model.ImportModeFull); err != nil {
		t.Fatalf("import into instance B: %v", err)
	}

	var tok model.SiteToken
	if err := target.First(&tok, 1).Error; err != nil {
		t.Fatalf("query target site_tokens: %v", err)
	}
	if !crypto.IsEncrypted(tok.Token) {
		t.Fatalf("target site token = %q, want ciphertext under instance B key", tok.Token)
	}
	dec, err := crypto.Decrypt(tok.Token)
	if err != nil {
		t.Fatalf("target site token not decryptable under instance B key: %v", err)
	}
	if dec != plainSiteToken {
		t.Fatalf("decrypted site token = %q, want %q", dec, plainSiteToken)
	}

	var key model.APIKey
	if err := target.First(&key, 1).Error; err != nil {
		t.Fatalf("query target api_keys: %v", err)
	}
	decKey, err := crypto.Decrypt(key.APIKey)
	if err != nil || decKey != plainAPIKey {
		t.Fatalf("target api_key decrypt = %q, %v; want %q", decKey, err, plainAPIKey)
	}
	if want := apikeyhash.Sum(plainAPIKey); key.APIKeyHash != want {
		t.Fatalf("target api_key_hash = %q, want %q", key.APIKeyHash, want)
	}
}
