// Package settings owns typed system/project configuration and encrypted secrets.
package settings

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/mmdash/mmdash/backend/internal/auth"
	"github.com/mmdash/mmdash/backend/internal/platform/clock"
)

// Scope identifies the owner boundary of a setting.
type Scope string

const (
	ScopeProject Scope = "project"
	ScopeSystem  Scope = "system"
)

// FieldKind describes the JSON value accepted by a registered field.
type FieldKind string

const (
	FieldBoolean FieldKind = "boolean"
	FieldNumber  FieldKind = "number"
	FieldSecret  FieldKind = "secret"
	FieldSelect  FieldKind = "select"
	FieldString  FieldKind = "string"
	FieldURL     FieldKind = "url"
)

const RedactedSecret = "********"

// FieldDefinition is the UI and validation contract for one setting field.
type FieldDefinition struct {
	Description string    `json:"description,omitempty"`
	Key         string    `json:"key"`
	Kind        FieldKind `json:"kind"`
	Label       string    `json:"label"`
	Options     []string  `json:"options,omitempty"`
	Required    bool      `json:"required"`
}

// TypeDefinition is registered in code by the module that owns the setting.
type TypeDefinition struct {
	Description string            `json:"description"`
	Fields      []FieldDefinition `json:"fields"`
	Key         string            `json:"key"`
	Order       int               `json:"order"`
	Owner       string            `json:"owner"`
	Scopes      []Scope           `json:"scopes"`
	Tester      ConnectionTester  `json:"-"`
	Title       string            `json:"title"`
	Validator   ConfigValidator   `json:"-"`
}

// ConfigValidator checks one complete resolved candidate before it is saved.
type ConfigValidator interface {
	ValidateConfig(map[string]interface{}) error
}

// TypeDescriptor is the serializable type-registry projection.
type TypeDescriptor struct {
	Description   string            `json:"description"`
	Fields        []FieldDefinition `json:"fields"`
	Key           string            `json:"key"`
	Order         int               `json:"order"`
	Owner         string            `json:"owner"`
	Scopes        []Scope           `json:"scopes"`
	TestSupported bool              `json:"test_supported"`
	Title         string            `json:"title"`
}

// Registry stores reviewed module-owned configuration contracts.
type Registry struct {
	definitions map[string]TypeDefinition
	mutex       sync.RWMutex
}

func NewRegistry() *Registry {
	return &Registry{definitions: map[string]TypeDefinition{}}
}

// Register adds one immutable config type and rejects ambiguous contracts.
func (registry *Registry) Register(definition TypeDefinition) error {
	if err := validateDefinition(definition); err != nil {
		return err
	}
	registry.mutex.Lock()
	defer registry.mutex.Unlock()
	if _, exists := registry.definitions[definition.Key]; exists {
		return fmt.Errorf("%w: %s", ErrTypeConflict, definition.Key)
	}
	registry.definitions[definition.Key] = cloneDefinition(definition)
	return nil
}

func (registry *Registry) Get(key string) (TypeDefinition, error) {
	registry.mutex.RLock()
	defer registry.mutex.RUnlock()
	definition, exists := registry.definitions[key]
	if !exists {
		return TypeDefinition{}, ErrTypeNotFound
	}
	return cloneDefinition(definition), nil
}

func (registry *Registry) List(scope Scope) []TypeDescriptor {
	registry.mutex.RLock()
	defer registry.mutex.RUnlock()
	items := make([]TypeDescriptor, 0, len(registry.definitions))
	for _, definition := range registry.definitions {
		if !supportsScope(definition, scope) {
			continue
		}
		items = append(items, descriptor(definition))
	}
	sort.Slice(items, func(left, right int) bool {
		return items[left].Order < items[right].Order ||
			(items[left].Order == items[right].Order && items[left].Key < items[right].Key)
	})
	return items
}

// EncryptedSecret is the persisted AES-GCM envelope.
type EncryptedSecret struct {
	Algorithm  string `json:"algorithm"`
	Ciphertext string `json:"ciphertext"`
	Nonce      string `json:"nonce"`
}

// SecretCodec encrypts module secrets with authenticated encryption.
type SecretCodec struct {
	key    []byte
	random io.Reader
}

func NewSecretCodec(keyMaterial string) (SecretCodec, error) {
	if len(keyMaterial) < 32 {
		return SecretCodec{}, fmt.Errorf("settings encryption key is too short")
	}
	sum := sha256.Sum256([]byte(keyMaterial))
	return SecretCodec{key: sum[:], random: rand.Reader}, nil
}

func (codec SecretCodec) Encrypt(plaintext string) (EncryptedSecret, error) {
	block, err := aes.NewCipher(codec.key)
	if err != nil {
		return EncryptedSecret{}, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return EncryptedSecret{}, err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(codec.random, nonce); err != nil {
		return EncryptedSecret{}, fmt.Errorf("generate settings nonce: %w", err)
	}
	ciphertext := gcm.Seal(nil, nonce, []byte(plaintext), nil)
	return EncryptedSecret{
		Algorithm:  "aes-256-gcm",
		Ciphertext: base64.RawStdEncoding.EncodeToString(ciphertext),
		Nonce:      base64.RawStdEncoding.EncodeToString(nonce),
	}, nil
}

func (codec SecretCodec) Decrypt(secret EncryptedSecret) (string, error) {
	if secret.Algorithm != "aes-256-gcm" {
		return "", fmt.Errorf("unsupported settings encryption algorithm")
	}
	block, err := aes.NewCipher(codec.key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce, err := base64.RawStdEncoding.DecodeString(secret.Nonce)
	if err != nil {
		return "", fmt.Errorf("decode settings nonce: %w", err)
	}
	ciphertext, err := base64.RawStdEncoding.DecodeString(secret.Ciphertext)
	if err != nil {
		return "", fmt.Errorf("decode settings ciphertext: %w", err)
	}
	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return "", fmt.Errorf("decrypt settings secret: %w", err)
	}
	return string(plaintext), nil
}

// StoredSetting is the persistence representation. It never leaves Core.
type StoredSetting struct {
	CreatedAt        time.Time
	EncryptedSecrets map[string]EncryptedSecret
	PublicValues     map[string]interface{}
	ResourceID       string
	Scope            Scope
	ScopeID          string
	TypeKey          string
	UpdatedAt        time.Time
	UpdatedBy        string
	Version          int64
}

// Setting is the public projection with secrets redacted.
type Setting struct {
	ResourceID string                 `json:"resource_id,omitempty"`
	Scope      Scope                  `json:"scope"`
	ScopeID    string                 `json:"scope_id"`
	TypeKey    string                 `json:"type_key"`
	UpdatedAt  time.Time              `json:"updated_at"`
	UpdatedBy  string                 `json:"updated_by"`
	Values     map[string]interface{} `json:"values"`
	Version    int64                  `json:"version"`
}

// ResolvedSetting is passed only to trusted in-process module adapters.
type ResolvedSetting struct {
	ResourceID string
	Scope      Scope
	ScopeID    string
	TypeKey    string
	Values     map[string]interface{}
	Version    int64
}

// Store is the persistence boundary for encrypted settings.
type Store interface {
	Delete(context.Context, Scope, string, string, string) error
	Get(context.Context, Scope, string, string) (StoredSetting, error)
	RotateSecrets(context.Context, string, StoredSetting) (StoredSetting, error)
	Upsert(context.Context, string, StoredSetting) (StoredSetting, error)
}

// ResourceStore extends Store for module-owned settings attached to a stable
// resource such as one Agent instance. Generic Settings routes always use the
// empty resource ID and cannot enumerate or overwrite these values.
type ResourceStore interface {
	DeleteResource(context.Context, Scope, string, string, string, string) error
	GetResource(context.Context, Scope, string, string, string) (StoredSetting, error)
	UpsertResource(context.Context, string, StoredSetting) (StoredSetting, error)
}

// Authorizer enforces system-admin and project configuration permissions.
type Authorizer interface {
	Authorize(context.Context, auth.Identity, Scope, string, bool) error
}

// ProjectAccess is implemented by Project without exposing its persistence.
type ProjectAccess interface {
	AuthorizeSettings(context.Context, auth.Identity, string, bool) error
}

// AccessPolicy maps setting scope to the authoritative permission owner.
type AccessPolicy struct {
	Projects ProjectAccess
}

func (policy AccessPolicy) Authorize(
	ctx context.Context,
	identity auth.Identity,
	scope Scope,
	scopeID string,
	manage bool,
) error {
	switch scope {
	case ScopeSystem:
		if identity.User.SystemRole != "admin" ||
			(identity.Kind != "session" && identity.Kind != "api") {
			return ErrForbidden
		}
		return nil
	case ScopeProject:
		if policy.Projects == nil ||
			policy.Projects.AuthorizeSettings(ctx, identity, scopeID, manage) != nil {
			return ErrForbidden
		}
		return nil
	default:
		return ErrInvalid
	}
}

// ConnectionCheck is one safe, named connectivity assertion.
type ConnectionCheck struct {
	Message string `json:"message,omitempty"`
	Name    string `json:"name"`
	Status  string `json:"status"`
}

// ConnectionTestResult is the shared response convention for every module.
type ConnectionTestResult struct {
	CheckedAt time.Time         `json:"checked_at"`
	Checks    []ConnectionCheck `json:"checks"`
	Status    string            `json:"status"`
}

// ConnectionTester is registered by a module alongside its config type.
type ConnectionTester interface {
	Test(context.Context, ResolvedSetting) ([]ConnectionCheck, error)
}

// Service coordinates registry validation, authorization, encryption, and tests.
type Service struct {
	Access   Authorizer
	Clock    clock.Clock
	Codec    SecretCodec
	Registry *Registry
	Store    Store
}

func (service Service) ListTypes(
	ctx context.Context,
	identity auth.Identity,
	scope Scope,
	scopeID string,
) ([]TypeDescriptor, error) {
	if err := service.Access.Authorize(ctx, identity, scope, scopeID, false); err != nil {
		return nil, err
	}
	return service.Registry.List(scope), nil
}

func (service Service) Get(
	ctx context.Context,
	identity auth.Identity,
	scope Scope,
	scopeID string,
	typeKey string,
) (Setting, error) {
	if err := service.Access.Authorize(ctx, identity, scope, scopeID, false); err != nil {
		return Setting{}, err
	}
	if _, err := service.definition(typeKey, scope); err != nil {
		return Setting{}, err
	}
	stored, err := service.Store.Get(ctx, scope, normalizeScopeID(scope, scopeID), typeKey)
	if err != nil {
		return Setting{}, err
	}
	return redact(stored), nil
}

func (service Service) Update(
	ctx context.Context,
	identity auth.Identity,
	scope Scope,
	scopeID string,
	typeKey string,
	patch map[string]interface{},
) (Setting, error) {
	if err := service.Access.Authorize(ctx, identity, scope, scopeID, true); err != nil {
		return Setting{}, err
	}
	definition, err := service.definition(typeKey, scope)
	if err != nil {
		return Setting{}, err
	}
	scopeID = normalizeScopeID(scope, scopeID)
	stored, err := service.Store.Get(ctx, scope, scopeID, typeKey)
	if errors.Is(err, ErrNotFound) {
		stored = StoredSetting{
			EncryptedSecrets: map[string]EncryptedSecret{},
			PublicValues:     map[string]interface{}{},
			Scope:            scope,
			ScopeID:          scopeID,
			TypeKey:          typeKey,
		}
	} else if err != nil {
		return Setting{}, err
	}
	if err := service.applyPatch(definition, &stored, patch); err != nil {
		return Setting{}, err
	}
	if err := service.validateStoredConfig(definition, stored); err != nil {
		return Setting{}, err
	}
	updated, err := service.Store.Upsert(ctx, identity.User.ID, stored)
	if err != nil {
		return Setting{}, err
	}
	return redact(updated), nil
}

func (service Service) Delete(
	ctx context.Context,
	identity auth.Identity,
	scope Scope,
	scopeID string,
	typeKey string,
) error {
	if err := service.Access.Authorize(ctx, identity, scope, scopeID, true); err != nil {
		return err
	}
	if _, err := service.definition(typeKey, scope); err != nil {
		return err
	}
	return service.Store.Delete(
		ctx,
		scope,
		normalizeScopeID(scope, scopeID),
		typeKey,
		identity.User.ID,
	)
}

func (service Service) TestConnection(
	ctx context.Context,
	identity auth.Identity,
	scope Scope,
	scopeID string,
	typeKey string,
) (ConnectionTestResult, error) {
	result := ConnectionTestResult{
		CheckedAt: service.Clock.Now().UTC(),
		Checks:    []ConnectionCheck{},
		Status:    "failed",
	}
	if err := service.Access.Authorize(ctx, identity, scope, scopeID, true); err != nil {
		return ConnectionTestResult{}, err
	}
	definition, err := service.definition(typeKey, scope)
	if err != nil {
		return ConnectionTestResult{}, err
	}
	if definition.Tester == nil {
		result.Status = "unsupported"
		return result, nil
	}
	resolved, err := service.Resolve(ctx, scope, scopeID, typeKey)
	if err != nil {
		return ConnectionTestResult{}, err
	}
	checks, err := definition.Tester.Test(ctx, resolved)
	result.Checks = nonNilChecks(checks)
	if err != nil {
		result.Checks = append(result.Checks, ConnectionCheck{
			Message: "Connection test failed",
			Name:    "connection",
			Status:  "failed",
		})
		return result, nil
	}
	result.Status = "passed"
	for _, check := range result.Checks {
		if check.Status != "passed" {
			result.Status = "failed"
			break
		}
	}
	return result, nil
}

// Resolve decrypts values for trusted in-process module code only.
func (service Service) Resolve(
	ctx context.Context,
	scope Scope,
	scopeID string,
	typeKey string,
) (ResolvedSetting, error) {
	if _, err := service.definition(typeKey, scope); err != nil {
		return ResolvedSetting{}, err
	}
	stored, err := service.Store.Get(
		ctx,
		scope,
		normalizeScopeID(scope, scopeID),
		typeKey,
	)
	if err != nil {
		return ResolvedSetting{}, err
	}
	values := cloneValues(stored.PublicValues)
	for key, encrypted := range stored.EncryptedSecrets {
		value, err := service.Codec.Decrypt(encrypted)
		if err != nil {
			return ResolvedSetting{}, fmt.Errorf("decrypt setting %s: %w", key, err)
		}
		values[key] = value
	}
	return ResolvedSetting{
		Scope:   stored.Scope,
		ScopeID: stored.ScopeID,
		TypeKey: stored.TypeKey,
		Values:  values,
		Version: stored.Version,
	}, nil
}

// RotateSecrets atomically replaces trusted module credentials without
// publishing settings.updated. Public configuration did not change, so
// consumers must not restart domain workflows merely because an OAuth
// provider rotated its access and refresh tokens.
func (service Service) RotateSecrets(
	ctx context.Context,
	actorID string,
	scope Scope,
	scopeID string,
	typeKey string,
	secrets map[string]string,
) error {
	definition, err := service.definition(typeKey, scope)
	if err != nil {
		return err
	}
	if strings.TrimSpace(actorID) == "" || len(secrets) == 0 {
		return ErrInvalid
	}
	secretFields := map[string]bool{}
	for _, field := range definition.Fields {
		if field.Kind == FieldSecret {
			secretFields[field.Key] = true
		}
	}
	stored, err := service.Store.Get(ctx, scope, normalizeScopeID(scope, scopeID), typeKey)
	if err != nil {
		return err
	}
	for key, plaintext := range secrets {
		if !secretFields[key] || strings.TrimSpace(plaintext) == "" {
			return fmt.Errorf("%w: invalid secret rotation field %s", ErrInvalid, key)
		}
		encrypted, err := service.Codec.Encrypt(plaintext)
		if err != nil {
			return err
		}
		stored.EncryptedSecrets[key] = encrypted
	}
	_, err = service.Store.RotateSecrets(ctx, actorID, stored)
	return err
}

// GetResource returns a redacted module-owned instance setting after applying
// the same project authorization and registry validation as generic Settings.
func (service Service) GetResource(
	ctx context.Context,
	identity auth.Identity,
	scope Scope,
	scopeID string,
	typeKey string,
	resourceID string,
) (Setting, error) {
	resourceID = strings.TrimSpace(resourceID)
	if resourceID == "" {
		return Setting{}, ErrInvalid
	}
	if err := service.Access.Authorize(ctx, identity, scope, scopeID, false); err != nil {
		return Setting{}, err
	}
	if _, err := service.definition(typeKey, scope); err != nil {
		return Setting{}, err
	}
	store, ok := service.Store.(ResourceStore)
	if !ok {
		return Setting{}, fmt.Errorf("resource settings store is not configured")
	}
	stored, err := store.GetResource(
		ctx, scope, normalizeScopeID(scope, scopeID), typeKey, resourceID,
	)
	if err != nil {
		return Setting{}, err
	}
	return redact(stored), nil
}

// UpdateResource stores encrypted module secrets without exposing them through
// the generic Settings collection.
func (service Service) UpdateResource(
	ctx context.Context,
	identity auth.Identity,
	scope Scope,
	scopeID string,
	typeKey string,
	resourceID string,
	patch map[string]interface{},
) (Setting, error) {
	resourceID = strings.TrimSpace(resourceID)
	if resourceID == "" {
		return Setting{}, ErrInvalid
	}
	if err := service.Access.Authorize(ctx, identity, scope, scopeID, true); err != nil {
		return Setting{}, err
	}
	definition, err := service.definition(typeKey, scope)
	if err != nil {
		return Setting{}, err
	}
	store, ok := service.Store.(ResourceStore)
	if !ok {
		return Setting{}, fmt.Errorf("resource settings store is not configured")
	}
	scopeID = normalizeScopeID(scope, scopeID)
	stored, err := store.GetResource(ctx, scope, scopeID, typeKey, resourceID)
	if errors.Is(err, ErrNotFound) {
		stored = StoredSetting{
			EncryptedSecrets: map[string]EncryptedSecret{},
			PublicValues:     map[string]interface{}{},
			ResourceID:       resourceID,
			Scope:            scope,
			ScopeID:          scopeID,
			TypeKey:          typeKey,
		}
	} else if err != nil {
		return Setting{}, err
	}
	if err := service.applyPatch(definition, &stored, patch); err != nil {
		return Setting{}, err
	}
	updated, err := store.UpsertResource(ctx, identity.User.ID, stored)
	if err != nil {
		return Setting{}, err
	}
	return redact(updated), nil
}

// ResolveResource decrypts one module-owned instance setting for trusted
// in-process adapters only.
func (service Service) ResolveResource(
	ctx context.Context,
	scope Scope,
	scopeID string,
	typeKey string,
	resourceID string,
) (ResolvedSetting, error) {
	resourceID = strings.TrimSpace(resourceID)
	if resourceID == "" {
		return ResolvedSetting{}, ErrInvalid
	}
	if _, err := service.definition(typeKey, scope); err != nil {
		return ResolvedSetting{}, err
	}
	store, ok := service.Store.(ResourceStore)
	if !ok {
		return ResolvedSetting{}, fmt.Errorf("resource settings store is not configured")
	}
	stored, err := store.GetResource(
		ctx, scope, normalizeScopeID(scope, scopeID), typeKey, resourceID,
	)
	if err != nil {
		return ResolvedSetting{}, err
	}
	values := cloneValues(stored.PublicValues)
	for key, encrypted := range stored.EncryptedSecrets {
		value, err := service.Codec.Decrypt(encrypted)
		if err != nil {
			return ResolvedSetting{}, fmt.Errorf("decrypt setting %s: %w", key, err)
		}
		values[key] = value
	}
	return ResolvedSetting{
		ResourceID: stored.ResourceID,
		Scope:      stored.Scope, ScopeID: stored.ScopeID, TypeKey: stored.TypeKey,
		Values: values, Version: stored.Version,
	}, nil
}

func (service Service) definition(typeKey string, scope Scope) (TypeDefinition, error) {
	definition, err := service.Registry.Get(typeKey)
	if err != nil {
		return TypeDefinition{}, err
	}
	if !supportsScope(definition, scope) {
		return TypeDefinition{}, ErrTypeNotFound
	}
	return definition, nil
}

func (service Service) applyPatch(
	definition TypeDefinition,
	stored *StoredSetting,
	patch map[string]interface{},
) error {
	fields := map[string]FieldDefinition{}
	for _, field := range definition.Fields {
		fields[field.Key] = field
	}
	for key, value := range patch {
		field, exists := fields[key]
		if !exists {
			return fmt.Errorf("%w: unknown field %s", ErrInvalid, key)
		}
		if field.Kind == FieldSecret {
			if value == nil {
				delete(stored.EncryptedSecrets, key)
				continue
			}
			plaintext, ok := value.(string)
			if !ok {
				return fmt.Errorf("%w: %s must be a string", ErrInvalid, key)
			}
			if plaintext == RedactedSecret {
				continue
			}
			if plaintext == "" {
				return fmt.Errorf("%w: %s must not be empty", ErrInvalid, key)
			}
			encrypted, err := service.Codec.Encrypt(plaintext)
			if err != nil {
				return err
			}
			stored.EncryptedSecrets[key] = encrypted
			continue
		}
		if value == nil {
			delete(stored.PublicValues, key)
			continue
		}
		if err := validateValue(field, value); err != nil {
			return err
		}
		stored.PublicValues[key] = value
	}
	for _, field := range definition.Fields {
		if !field.Required {
			continue
		}
		if field.Kind == FieldSecret {
			if _, exists := stored.EncryptedSecrets[field.Key]; !exists {
				return fmt.Errorf("%w: %s is required", ErrInvalid, field.Key)
			}
		} else if _, exists := stored.PublicValues[field.Key]; !exists {
			return fmt.Errorf("%w: %s is required", ErrInvalid, field.Key)
		}
	}
	return nil
}

func (service Service) validateStoredConfig(
	definition TypeDefinition,
	stored StoredSetting,
) error {
	if definition.Validator == nil {
		return nil
	}
	values := cloneValues(stored.PublicValues)
	for key, encrypted := range stored.EncryptedSecrets {
		plaintext, err := service.Codec.Decrypt(encrypted)
		if err != nil {
			return fmt.Errorf("decrypt setting %s for validation: %w", key, err)
		}
		values[key] = plaintext
	}
	if err := definition.Validator.ValidateConfig(values); err != nil {
		return fmt.Errorf("%w: configuration is invalid", ErrInvalid)
	}
	return nil
}

func validateDefinition(definition TypeDefinition) error {
	if strings.TrimSpace(definition.Key) == "" ||
		strings.TrimSpace(definition.Owner) == "" ||
		strings.TrimSpace(definition.Title) == "" ||
		len(definition.Scopes) == 0 {
		return ErrInvalid
	}
	scopes := map[Scope]bool{}
	for _, scope := range definition.Scopes {
		if scope != ScopeProject && scope != ScopeSystem {
			return ErrInvalid
		}
		if scopes[scope] {
			return ErrInvalid
		}
		scopes[scope] = true
	}
	fields := map[string]bool{}
	for _, field := range definition.Fields {
		if strings.TrimSpace(field.Key) == "" || strings.TrimSpace(field.Label) == "" || fields[field.Key] {
			return ErrInvalid
		}
		fields[field.Key] = true
		switch field.Kind {
		case FieldBoolean, FieldNumber, FieldSecret, FieldString, FieldURL:
			if len(field.Options) != 0 {
				return ErrInvalid
			}
		case FieldSelect:
			if len(field.Options) == 0 {
				return ErrInvalid
			}
		default:
			return ErrInvalid
		}
	}
	return nil
}

func validateValue(field FieldDefinition, value interface{}) error {
	valid := false
	switch field.Kind {
	case FieldBoolean:
		_, valid = value.(bool)
	case FieldNumber:
		_, valid = value.(float64)
	case FieldString:
		text, ok := value.(string)
		valid = ok && strings.TrimSpace(text) != ""
	case FieldURL:
		text, ok := value.(string)
		parsed, err := url.Parse(text)
		valid = ok && err == nil && parsed.Host != "" &&
			(parsed.Scheme == "http" || parsed.Scheme == "https")
	case FieldSelect:
		text, ok := value.(string)
		valid = ok && contains(field.Options, text)
	}
	if !valid {
		return fmt.Errorf("%w: invalid value for %s", ErrInvalid, field.Key)
	}
	return nil
}

func redact(stored StoredSetting) Setting {
	values := cloneValues(stored.PublicValues)
	for key := range stored.EncryptedSecrets {
		values[key] = RedactedSecret
	}
	return Setting{
		ResourceID: stored.ResourceID,
		Scope:      stored.Scope,
		ScopeID:    stored.ScopeID,
		TypeKey:    stored.TypeKey,
		UpdatedAt:  stored.UpdatedAt,
		UpdatedBy:  stored.UpdatedBy,
		Values:     values,
		Version:    stored.Version,
	}
}

func descriptor(definition TypeDefinition) TypeDescriptor {
	return TypeDescriptor{
		Description:   definition.Description,
		Fields:        append([]FieldDefinition(nil), definition.Fields...),
		Key:           definition.Key,
		Order:         definition.Order,
		Owner:         definition.Owner,
		Scopes:        append([]Scope(nil), definition.Scopes...),
		TestSupported: definition.Tester != nil,
		Title:         definition.Title,
	}
}

func cloneDefinition(definition TypeDefinition) TypeDefinition {
	definition.Fields = append([]FieldDefinition(nil), definition.Fields...)
	definition.Scopes = append([]Scope(nil), definition.Scopes...)
	return definition
}

func cloneValues(values map[string]interface{}) map[string]interface{} {
	cloned := make(map[string]interface{}, len(values))
	for key, value := range values {
		cloned[key] = value
	}
	return cloned
}

func supportsScope(definition TypeDefinition, scope Scope) bool {
	for _, candidate := range definition.Scopes {
		if candidate == scope {
			return true
		}
	}
	return false
}

func normalizeScopeID(scope Scope, scopeID string) string {
	if scope == ScopeSystem {
		return "system"
	}
	return scopeID
}

func contains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func nonNilChecks(checks []ConnectionCheck) []ConnectionCheck {
	if checks == nil {
		return []ConnectionCheck{}
	}
	return checks
}

var (
	ErrForbidden    = errors.New("settings permission denied")
	ErrInvalid      = errors.New("invalid settings input")
	ErrNotFound     = errors.New("setting not found")
	ErrTypeConflict = errors.New("settings type already registered")
	ErrTypeNotFound = errors.New("settings type not found")
)
