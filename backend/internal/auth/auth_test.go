package auth

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"

	"github.com/mmdash/mmdash/backend/internal/platform/clock"
	"github.com/mmdash/mmdash/backend/internal/platform/identity"
)

type memoryStore struct {
	passwordHash string
	sessions     map[string]Session
	tokens       map[string]Token
	user         User
}

type projectAuthorizerStub struct{}

func (projectAuthorizerStub) AuthorizeTokenManagement(
	context.Context,
	Identity,
	string,
) error {
	return nil
}

func newMemoryStore() *memoryStore {
	return &memoryStore{
		sessions: map[string]Session{},
		tokens:   map[string]Token{},
	}
}

func (store *memoryStore) CreateUser(
	_ context.Context,
	user User,
	passwordHash string,
) error {
	store.user = user
	store.passwordHash = passwordHash
	return nil
}

func (store *memoryStore) FindUserByEmail(
	_ context.Context,
	email string,
) (User, string, error) {
	if store.user.Email != email {
		return User{}, "", ErrNotFound
	}
	return store.user, store.passwordHash, nil
}

func TestEnsureBootstrapUserDoesNotHideStoreFailure(t *testing.T) {
	service := Service{
		Clock:     clock.Fixed{Time: time.Now()},
		Generator: identity.Generator{Reader: bytes.NewReader(make([]byte, 16))},
		Store:     failingLookupStore{memoryStore: newMemoryStore()},
	}
	if err := service.EnsureBootstrapUser(
		context.Background(),
		"admin@example.com",
		"Admin",
		"password",
	); err == nil {
		t.Fatal("expected lookup failure to stop bootstrap")
	}
}

type failingLookupStore struct {
	*memoryStore
}

func (failingLookupStore) FindUserByEmail(
	context.Context,
	string,
) (User, string, error) {
	return User{}, "", errors.New("database unavailable")
}

func (store *memoryStore) CreateSession(_ context.Context, session Session) error {
	store.sessions[session.ID] = session
	return nil
}

func (store *memoryStore) FindSession(
	_ context.Context,
	sessionID string,
	tokenHash string,
	now time.Time,
) (Session, User, error) {
	session, ok := store.sessions[sessionID]
	if !ok || session.TokenHash != tokenHash || session.RevokedAt != nil || !session.ExpiresAt.After(now) {
		return Session{}, User{}, errors.New("not found")
	}
	return session, store.user, nil
}

func (store *memoryStore) RevokeSession(
	_ context.Context,
	sessionID string,
	now time.Time,
) error {
	session := store.sessions[sessionID]
	session.RevokedAt = &now
	store.sessions[sessionID] = session
	return nil
}

func (store *memoryStore) CreateToken(_ context.Context, token Token) error {
	store.tokens[token.TokenHash] = token
	return nil
}

func (store *memoryStore) FindToken(
	_ context.Context,
	tokenHash string,
	now time.Time,
) (Token, User, error) {
	token, ok := store.tokens[tokenHash]
	if !ok || token.RevokedAt != nil || (token.ExpiresAt != nil && !token.ExpiresAt.After(now)) {
		return Token{}, User{}, errors.New("not found")
	}
	return token, store.user, nil
}

func (store *memoryStore) ListTokens(_ context.Context, userID string) ([]Token, error) {
	tokens := []Token{}
	for _, token := range store.tokens {
		if token.UserID == userID {
			tokens = append(tokens, token)
		}
	}
	return tokens, nil
}

func (store *memoryStore) RevokeToken(
	_ context.Context,
	userID string,
	tokenID string,
	now time.Time,
) error {
	for hash, token := range store.tokens {
		if token.ID == tokenID && token.UserID == userID {
			token.RevokedAt = &now
			store.tokens[hash] = token
			return nil
		}
	}
	return ErrNotFound
}

func TestSessionLoginAuthenticationAndRevocation(t *testing.T) {
	now := time.Date(2026, time.July, 28, 12, 0, 0, 0, time.UTC)
	store := newMemoryStore()
	service := Service{
		Clock:      clock.Fixed{Time: now},
		Generator:  identity.Generator{Reader: bytes.NewReader(make([]byte, 64))},
		JWTSecret:  []byte("test-jwt-secret-with-at-least-32-characters"),
		SessionTTL: time.Hour,
		Store:      store,
	}
	ctx := context.Background()
	if err := service.EnsureBootstrapUser(ctx, "ADMIN@example.com", "Admin", "correct-password"); err != nil {
		t.Fatalf("ensure user: %v", err)
	}
	result, err := service.Login(ctx, "admin@example.com", "correct-password")
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	authenticated, err := service.Authenticate(ctx, "Bearer "+result.AccessToken)
	if err != nil {
		t.Fatalf("authenticate: %v", err)
	}
	if authenticated.User.Email != "admin@example.com" || authenticated.Kind != "session" {
		t.Fatalf("unexpected identity: %#v", authenticated)
	}
	if err := service.Logout(ctx, "Bearer "+result.AccessToken); err != nil {
		t.Fatalf("logout: %v", err)
	}
	if _, err := service.Authenticate(ctx, "Bearer "+result.AccessToken); !errors.Is(err, ErrUnauthenticated) {
		t.Fatalf("expected revoked session to fail, got %v", err)
	}
}

func TestIssuedAgentTokenIsHashedAndProjectScoped(t *testing.T) {
	now := time.Date(2026, time.July, 28, 12, 0, 0, 0, time.UTC)
	store := newMemoryStore()
	store.user = User{ID: "user-1", Email: "user@example.com", Status: "active"}
	service := Service{
		Clock:         clock.Fixed{Time: now},
		Generator:     identity.Generator{Reader: bytes.NewReader(make([]byte, 32))},
		JWTSecret:     []byte("test-jwt-secret-with-at-least-32-characters"),
		ProjectTokens: projectAuthorizerStub{},
		Store:         store,
	}
	issued, err := service.IssueToken(
		context.Background(),
		Identity{Kind: "session", User: store.user},
		"agent",
		"research agent",
		"project-1",
		nil,
	)
	if err != nil {
		t.Fatalf("issue token: %v", err)
	}
	if issued.Secret == "" || issued.Token.TokenHash == issued.Secret {
		t.Fatal("token secret must be returned once and stored as a hash")
	}
	authenticated, err := service.Authenticate(context.Background(), "Bearer "+issued.Secret)
	if err != nil {
		t.Fatalf("authenticate token: %v", err)
	}
	if authenticated.Kind != "agent" || authenticated.ProjectID != "project-1" {
		t.Fatalf("unexpected token identity: %#v", authenticated)
	}
}
