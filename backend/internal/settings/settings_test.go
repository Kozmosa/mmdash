package settings

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"

	"github.com/mmdash/mmdash/backend/internal/auth"
	"github.com/mmdash/mmdash/backend/internal/platform/clock"
)

type memoryStore struct {
	setting StoredSetting
}

func (store *memoryStore) Get(
	_ context.Context,
	scope Scope,
	scopeID string,
	typeKey string,
) (StoredSetting, error) {
	if store.setting.TypeKey == "" ||
		store.setting.Scope != scope ||
		store.setting.ScopeID != scopeID ||
		store.setting.TypeKey != typeKey {
		return StoredSetting{}, ErrNotFound
	}
	return store.setting, nil
}

func (store *memoryStore) Upsert(
	_ context.Context,
	actorID string,
	setting StoredSetting,
) (StoredSetting, error) {
	setting.Version++
	setting.UpdatedBy = actorID
	setting.UpdatedAt = time.Date(2026, time.July, 28, 12, 0, 0, 0, time.UTC)
	store.setting = setting
	return setting, nil
}

func (store *memoryStore) RotateSecrets(
	_ context.Context,
	actorID string,
	setting StoredSetting,
) (StoredSetting, error) {
	setting.Version++
	setting.UpdatedBy = actorID
	store.setting = setting
	return setting, nil
}

func (store *memoryStore) Delete(
	_ context.Context,
	scope Scope,
	scopeID string,
	typeKey string,
	_ string,
) error {
	if _, err := store.Get(context.Background(), scope, scopeID, typeKey); err != nil {
		return err
	}
	store.setting = StoredSetting{}
	return nil
}

type allowAccess struct{}

func (allowAccess) Authorize(
	context.Context,
	auth.Identity,
	Scope,
	string,
	bool,
) error {
	return nil
}

type secretCheckingTester struct {
	expectedSecret string
}

func (tester secretCheckingTester) Test(
	_ context.Context,
	setting ResolvedSetting,
) ([]ConnectionCheck, error) {
	if setting.Values["token"] != tester.expectedSecret {
		return nil, errors.New("secret was not decrypted")
	}
	return []ConnectionCheck{{Name: "authentication", Status: "passed"}}, nil
}

type projectAccessStub struct {
	err error
}

func (stub projectAccessStub) AuthorizeSettings(
	context.Context,
	auth.Identity,
	string,
	bool,
) error {
	return stub.err
}

func TestRegistryRejectsDuplicatesAndFiltersScopes(t *testing.T) {
	registry := NewRegistry()
	definition := fixtureDefinition(nil)
	if err := registry.Register(definition); err != nil {
		t.Fatalf("register: %v", err)
	}
	if err := registry.Register(definition); !errors.Is(err, ErrTypeConflict) {
		t.Fatalf("expected duplicate conflict, got %v", err)
	}
	if got := registry.List(ScopeSystem); len(got) != 0 {
		t.Fatalf("expected no system definitions, got %#v", got)
	}
	items := registry.List(ScopeProject)
	if len(items) != 1 || items[0].Key != definition.Key {
		t.Fatalf("unexpected descriptors: %#v", items)
	}
}

func TestAccessPolicySeparatesSystemAndProjectAuthority(t *testing.T) {
	policy := AccessPolicy{Projects: projectAccessStub{}}
	admin := auth.Identity{
		Kind: "session",
		User: auth.User{SystemRole: "admin"},
	}
	if err := policy.Authorize(
		context.Background(),
		admin,
		ScopeSystem,
		"",
		true,
	); err != nil {
		t.Fatalf("system admin should manage system settings: %v", err)
	}
	member := admin
	member.User.SystemRole = "member"
	if err := policy.Authorize(
		context.Background(),
		member,
		ScopeSystem,
		"",
		false,
	); !errors.Is(err, ErrForbidden) {
		t.Fatalf("member should not read system settings, got %v", err)
	}
	if err := policy.Authorize(
		context.Background(),
		member,
		ScopeProject,
		"project-1",
		false,
	); err != nil {
		t.Fatalf("project access should delegate to Project: %v", err)
	}
}

func TestSecretCodecUsesAuthenticatedEncryption(t *testing.T) {
	codec, err := NewSecretCodec("test-settings-encryption-key-with-32-characters")
	if err != nil {
		t.Fatalf("codec: %v", err)
	}
	codec.random = bytes.NewReader(make([]byte, 32))
	encrypted, err := codec.Encrypt("plain-secret")
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	if encrypted.Ciphertext == "plain-secret" || encrypted.Algorithm != "aes-256-gcm" {
		t.Fatalf("secret was not encrypted: %#v", encrypted)
	}
	decrypted, err := codec.Decrypt(encrypted)
	if err != nil {
		t.Fatalf("decrypt: %v", err)
	}
	if decrypted != "plain-secret" {
		t.Fatalf("unexpected plaintext: %q", decrypted)
	}
	encrypted.Ciphertext = encrypted.Ciphertext[:len(encrypted.Ciphertext)-1] + "A"
	if _, err := codec.Decrypt(encrypted); err == nil {
		t.Fatal("expected tampered ciphertext to fail authentication")
	}
}

func TestRotateSecretsPreservesPublicValues(t *testing.T) {
	registry := NewRegistry()
	if err := registry.Register(fixtureDefinition(nil)); err != nil {
		t.Fatalf("register: %v", err)
	}
	codec, err := NewSecretCodec("test-settings-encryption-key-with-32-characters")
	if err != nil {
		t.Fatalf("codec: %v", err)
	}
	oldSecret, err := codec.Encrypt("old-secret")
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	store := &memoryStore{setting: StoredSetting{
		EncryptedSecrets: map[string]EncryptedSecret{"token": oldSecret},
		PublicValues:     map[string]interface{}{"enabled": true, "endpoint": "https://example.test/hook"},
		Scope:            ScopeProject, ScopeID: "project-1", TypeKey: "fixture.provider", Version: 2,
	}}
	service := Service{Codec: codec, Registry: registry, Store: store}
	if err := service.RotateSecrets(context.Background(), "user-1", ScopeProject, "project-1", "fixture.provider", map[string]string{"token": "new-secret"}); err != nil {
		t.Fatalf("rotate: %v", err)
	}
	resolved, err := service.Resolve(context.Background(), ScopeProject, "project-1", "fixture.provider")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if resolved.Values["token"] != "new-secret" || resolved.Values["endpoint"] != "https://example.test/hook" {
		t.Fatalf("unexpected rotated setting: %#v", resolved.Values)
	}
}

func TestServiceRedactsHTTPProjectionAndResolvesForModules(t *testing.T) {
	registry := NewRegistry()
	if err := registry.Register(fixtureDefinition(secretCheckingTester{expectedSecret: "replacement-secret"})); err != nil {
		t.Fatalf("register: %v", err)
	}
	codec, err := NewSecretCodec("test-settings-encryption-key-with-32-characters")
	if err != nil {
		t.Fatalf("codec: %v", err)
	}
	codec.random = bytes.NewReader(make([]byte, 64))
	store := &memoryStore{}
	service := Service{
		Access:   allowAccess{},
		Clock:    clock.Fixed{Time: time.Date(2026, time.July, 28, 12, 0, 0, 0, time.UTC)},
		Codec:    codec,
		Registry: registry,
		Store:    store,
	}
	identity := auth.Identity{Kind: "session", User: auth.User{ID: "user-1"}}
	setting, err := service.Update(
		context.Background(),
		identity,
		ScopeProject,
		"project-1",
		"fixture.provider",
		map[string]interface{}{
			"endpoint": "https://example.com",
			"token":    "plain-secret",
		},
	)
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if setting.Values["token"] != RedactedSecret {
		t.Fatalf("public setting leaked or omitted secret state: %#v", setting.Values)
	}
	if encoded := store.setting.EncryptedSecrets["token"]; encoded.Ciphertext == "plain-secret" {
		t.Fatal("store received plaintext secret")
	}
	originalSecret := store.setting.EncryptedSecrets["token"]
	if _, err := service.Update(
		context.Background(),
		identity,
		ScopeProject,
		"project-1",
		"fixture.provider",
		map[string]interface{}{"token": RedactedSecret},
	); err != nil {
		t.Fatalf("preserve redacted secret: %v", err)
	}
	if store.setting.EncryptedSecrets["token"] != originalSecret {
		t.Fatal("redacted secret placeholder replaced the stored secret")
	}
	if _, err := service.Update(
		context.Background(),
		identity,
		ScopeProject,
		"project-1",
		"fixture.provider",
		map[string]interface{}{"token": "replacement-secret"},
	); err != nil {
		t.Fatalf("replace secret: %v", err)
	}
	if store.setting.EncryptedSecrets["token"] == originalSecret {
		t.Fatal("new secret did not replace the encrypted value")
	}
	resolved, err := service.Resolve(
		context.Background(),
		ScopeProject,
		"project-1",
		"fixture.provider",
	)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if resolved.Values["token"] != "replacement-secret" {
		t.Fatalf("trusted resolution did not decrypt secret: %#v", resolved.Values)
	}
	testResult, err := service.TestConnection(
		context.Background(),
		identity,
		ScopeProject,
		"project-1",
		"fixture.provider",
	)
	if err != nil {
		t.Fatalf("test connection: %v", err)
	}
	if testResult.Status != "passed" ||
		len(testResult.Checks) != 1 ||
		testResult.Checks[0].Name != "authentication" {
		t.Fatalf("unexpected test result: %#v", testResult)
	}
}

func fixtureDefinition(tester ConnectionTester) TypeDefinition {
	return TypeDefinition{
		Description: "Test-only provider contract",
		Fields: []FieldDefinition{
			{
				Key:      "endpoint",
				Kind:     FieldURL,
				Label:    "Endpoint",
				Required: true,
			},
			{
				Key:      "token",
				Kind:     FieldSecret,
				Label:    "Token",
				Required: true,
			},
		},
		Key:    "fixture.provider",
		Order:  10,
		Owner:  "fixture",
		Scopes: []Scope{ScopeProject},
		Tester: tester,
		Title:  "Fixture provider",
	}
}
