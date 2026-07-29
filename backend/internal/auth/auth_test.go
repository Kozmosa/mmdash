package auth

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"

	"github.com/mmdash/mmdash/backend/internal/platform/clock"
	"github.com/mmdash/mmdash/backend/internal/platform/identity"
	"github.com/mmdash/mmdash/backend/internal/platform/transaction"
)

type memoryStore struct {
	passwordHash string
	sessions     map[string]Session
	tokens       map[string]Token
	user         User
}

type projectAuthorizerStub struct{}

type registrationPolicyStub bool

func (policy registrationPolicyStub) AllowOpenRegistration(context.Context) (bool, error) {
	return bool(policy), nil
}

type invitationStub struct {
	invitation Invitation
	accepted   bool
	declined   bool
	err        error
}

func (stub *invitationStub) PreviewInvitation(context.Context, string) (Invitation, error) {
	return stub.invitation, nil
}
func (stub *invitationStub) AcceptInvitation(context.Context, Identity, string) (AcceptedMember, error) {
	stub.accepted = true
	return AcceptedMember{UserID: "user-1", Role: "viewer"}, nil
}
func (stub *invitationStub) DeclineInvitation(context.Context, string) error {
	if stub.err != nil {
		return stub.err
	}
	stub.declined = true
	return nil
}
func (stub *invitationStub) AcceptRegistration(context.Context, string, User) (AcceptedMember, error) {
	if stub.err != nil {
		return AcceptedMember{}, stub.err
	}
	stub.accepted = true
	return AcceptedMember{UserID: "user-1", Role: "viewer"}, nil
}
func (stub *invitationStub) AcceptRegistrationInTransaction(context.Context, transaction.Tx, string, User) (AcceptedMember, error) {
	if stub.err != nil {
		return AcceptedMember{}, stub.err
	}
	stub.accepted = true
	return AcceptedMember{UserID: "user-1", Role: "viewer"}, nil
}

func TestDeclineInvitationDelegatesWithoutAuthentication(t *testing.T) {
	invitations := &invitationStub{}
	service := Service{Invitations: invitations}

	if err := service.DeclineInvitation(context.Background(), "invitation-token"); err != nil {
		t.Fatalf("decline invitation: %v", err)
	}
	if !invitations.declined {
		t.Fatal("invitation service did not receive decline request")
	}
}

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

func (store *memoryStore) CreateUserAndAcceptInvitation(
	ctx context.Context,
	user User,
	passwordHash string,
	accept func(transaction.Tx) error,
) error {
	previousUser, previousHash := store.user, store.passwordHash
	if err := store.CreateUser(ctx, user, passwordHash); err != nil {
		return err
	}
	if err := accept(nil); err != nil {
		store.user, store.passwordHash = previousUser, previousHash
		return err
	}
	return nil
}

func (store *memoryStore) DeleteUser(_ context.Context, userID string) error {
	if store.user.ID == userID {
		store.user = User{}
		store.passwordHash = ""
	}
	return nil
}
func (store *memoryStore) UpdateUser(_ context.Context, userID, email, displayName string, _ time.Time) (User, error) {
	if store.user.ID != userID {
		return User{}, ErrNotFound
	}
	store.user.Email = email
	store.user.DisplayName = displayName
	return store.user, nil
}
func (store *memoryStore) UpdatePassword(_ context.Context, userID, passwordHash string, _ time.Time) error {
	if store.user.ID != userID {
		return ErrNotFound
	}
	store.passwordHash = passwordHash
	return nil
}
func (store *memoryStore) RevokeOtherSessions(_ context.Context, userID, current string, now time.Time) error {
	for id, s := range store.sessions {
		if s.UserID == userID && id != current {
			s.RevokedAt = &now
			store.sessions[id] = s
		}
	}
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

func TestRegistrationPolicyAndInvitationEmailAreEnforced(t *testing.T) {
	now := time.Date(2026, time.July, 29, 0, 0, 0, 0, time.UTC)
	service := Service{Clock: clock.Fixed{Time: now}, Generator: identity.Generator{Reader: bytes.NewReader(make([]byte, 128))}, JWTSecret: []byte("test-jwt-secret-with-at-least-32-characters"), SessionTTL: time.Hour, Store: newMemoryStore(), Policy: registrationPolicyStub(false)}
	if _, err := service.Register(context.Background(), RegisterInput{Email: "new@example.com", DisplayName: "New", Password: "password-123"}); !errors.Is(err, ErrRegistrationClosed) {
		t.Fatalf("expected closed registration, got %v", err)
	}
	invites := &invitationStub{invitation: Invitation{Email: "invited@example.com", Status: "pending", ExpiresAt: now.Add(time.Hour)}}
	service.Invitations = invites
	if _, err := service.Register(context.Background(), RegisterInput{Email: "other@example.com", DisplayName: "New", Password: "password-123", InvitationToken: "token"}); !errors.Is(err, ErrInvalidInvitation) {
		t.Fatalf("expected email mismatch, got %v", err)
	}
	service.Store = newMemoryStore()
	result, err := service.Register(context.Background(), RegisterInput{Email: "INVITED@example.com", DisplayName: "Invited", Password: "password-123", InvitationToken: "token"})
	if err != nil {
		t.Fatalf("register invited user: %v", err)
	}
	if result.User.Email != "invited@example.com" || !invites.accepted {
		t.Fatalf("unexpected registration: %#v", result)
	}
}

func TestInvitedRegistrationRollsBackUserWhenInvitationCannotBeConsumed(t *testing.T) {
	now := time.Date(2026, time.July, 29, 0, 0, 0, 0, time.UTC)
	store := newMemoryStore()
	invites := &invitationStub{
		invitation: Invitation{Email: "invited@example.com", Status: "pending", ExpiresAt: now.Add(time.Hour)},
		err:        errors.New("invitation was revoked"),
	}
	service := Service{
		Clock:       clock.Fixed{Time: now},
		Generator:   identity.Generator{Reader: bytes.NewReader(make([]byte, 64))},
		Invitations: invites,
		JWTSecret:   []byte("test-jwt-secret-with-at-least-32-characters"),
		SessionTTL:  time.Hour,
		Store:       store,
	}
	_, err := service.Register(context.Background(), RegisterInput{
		DisplayName:     "Invited",
		Email:           "invited@example.com",
		InvitationToken: "token",
		Password:        "password-123",
	})
	if !errors.Is(err, invites.err) {
		t.Fatalf("expected invitation failure, got %v", err)
	}
	if store.user.ID != "" {
		t.Fatalf("failed invited registration left a user: %#v", store.user)
	}
}

func TestProfileEmailAndPasswordChangesRequireCurrentPassword(t *testing.T) {
	now := time.Date(2026, time.July, 29, 0, 0, 0, 0, time.UTC)
	store := newMemoryStore()
	service := Service{Clock: clock.Fixed{Time: now}, Generator: identity.Generator{Reader: bytes.NewReader(make([]byte, 128))}, JWTSecret: []byte("test-jwt-secret-with-at-least-32-characters"), SessionTTL: time.Hour, Store: store}
	if err := service.EnsureBootstrapUser(context.Background(), "admin@example.com", "Admin", "old-password"); err != nil {
		t.Fatal(err)
	}
	login, err := service.Login(context.Background(), "admin@example.com", "old-password")
	if err != nil {
		t.Fatal(err)
	}
	identity, err := service.Authenticate(context.Background(), "Bearer "+login.AccessToken)
	if err != nil {
		t.Fatal(err)
	}
	newEmail := "next@example.com"
	if _, err := service.UpdateProfile(context.Background(), identity, UpdateProfileInput{Email: &newEmail, CurrentPassword: "wrong"}); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("expected credential failure, got %v", err)
	}
	if _, err := service.UpdateProfile(context.Background(), identity, UpdateProfileInput{Email: &newEmail, CurrentPassword: "old-password"}); err != nil {
		t.Fatal(err)
	}
	identity.User.Email = newEmail
	if err := service.ChangePassword(context.Background(), identity, "old-password", "new-password"); err != nil {
		t.Fatal(err)
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
