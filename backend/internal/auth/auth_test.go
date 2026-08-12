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
	activatedRemoteAccessID string
	agentTokens             map[string]AgentToken
	devices                 map[string]DeviceAuthorization
	passwordHash            string
	sessions                map[string]Session
	tokens                  map[string]Token
	user                    User
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
		agentTokens: map[string]AgentToken{},
		devices:     map[string]DeviceAuthorization{},
		sessions:    map[string]Session{},
		tokens:      map[string]Token{},
	}
}

func (store *memoryStore) CreateAgentToken(_ context.Context, token AgentToken) error {
	store.agentTokens[token.ID] = token
	return nil
}

func (store *memoryStore) FindAgentToken(_ context.Context, tokenHash string, now time.Time) (AgentToken, error) {
	for _, token := range store.agentTokens {
		if token.TokenHash == tokenHash && token.RevokedAt == nil &&
			(token.ExpiresAt == nil || token.ExpiresAt.After(now)) {
			return token, nil
		}
	}
	return AgentToken{}, ErrNotFound
}

func (store *memoryStore) GetAgentToken(_ context.Context, tokenID string) (AgentToken, error) {
	token, ok := store.agentTokens[tokenID]
	if !ok {
		return AgentToken{}, ErrNotFound
	}
	return token, nil
}

func (store *memoryStore) ListAgentTokens(_ context.Context, grantID string) ([]AgentToken, error) {
	items := []AgentToken{}
	for _, token := range store.agentTokens {
		if token.GrantID == grantID {
			items = append(items, token)
		}
	}
	return items, nil
}

func (store *memoryStore) TouchAgentToken(_ context.Context, tokenID string, now time.Time) error {
	token, ok := store.agentTokens[tokenID]
	if !ok {
		return ErrNotFound
	}
	token.LastUsedAt = &now
	store.agentTokens[tokenID] = token
	return nil
}

func (store *memoryStore) MarkAgentTokenVerified(
	_ context.Context,
	evidence AgentTokenVerificationEvidence,
) (AgentTokenVerificationEvidence, error) {
	token, ok := store.agentTokens[evidence.TokenID]
	if !ok || token.Status != "pending" || token.RevokedAt != nil ||
		token.AgentInstanceID != evidence.AgentInstanceID ||
		token.ProjectID != evidence.ProjectID {
		return AgentTokenVerificationEvidence{}, ErrConflict
	}
	if token.Verification != nil {
		return *token.Verification, nil
	}
	if token.VerificationChallengeHash == "" ||
		token.VerificationChallengeHash != evidence.ChallengeHash {
		return AgentTokenVerificationEvidence{}, ErrForbidden
	}
	token.Verification = &evidence
	token.VerificationChallengeHash = ""
	store.agentTokens[evidence.TokenID] = token
	return evidence, nil
}

func (store *memoryStore) ActivateAgentToken(
	_ context.Context,
	tokenID string,
	oldTokenID string,
	newRemoteAccessID string,
	now time.Time,
) (AgentToken, error) {
	store.activatedRemoteAccessID = newRemoteAccessID
	token, ok := store.agentTokens[tokenID]
	if !ok || token.Status != "pending" || token.Verification == nil ||
		token.Verification.MCPMethod != AgentTokenVerificationMethod {
		return AgentToken{}, ErrNotFound
	}
	if oldTokenID != "" {
		old, exists := store.agentTokens[oldTokenID]
		if !exists || old.Status != "active" {
			return AgentToken{}, ErrConflict
		}
		old.Status, old.RevokedAt = "revoked", &now
		store.agentTokens[oldTokenID] = old
	}
	token.Status, token.ActivatedAt = "active", &now
	store.agentTokens[tokenID] = token
	return token, nil
}

func (store *memoryStore) RevokeAgentToken(_ context.Context, tokenID string, now time.Time) error {
	token, ok := store.agentTokens[tokenID]
	if !ok {
		return ErrNotFound
	}
	token.Status, token.RevokedAt = "revoked", &now
	store.agentTokens[tokenID] = token
	return nil
}

func (store *memoryStore) CreateDeviceAuthorization(_ context.Context, authorization DeviceAuthorization) error {
	store.devices[authorization.DeviceCodeHash] = authorization
	return nil
}

func (store *memoryStore) DecideDeviceAuthorization(_ context.Context, userCodeHash string, userID string, approve bool, now time.Time) error {
	for key, authorization := range store.devices {
		if authorization.UserCodeHash != userCodeHash {
			continue
		}
		if authorization.Status != "pending" {
			return ErrConflict
		}
		if !authorization.ExpiresAt.After(now) {
			return ErrAuthorizationExpired
		}
		authorization.UserID = userID
		if approve {
			authorization.Status = "approved"
		} else {
			authorization.Status = "denied"
		}
		store.devices[key] = authorization
		return nil
	}
	return ErrNotFound
}

func (store *memoryStore) ExchangeDeviceAuthorization(_ context.Context, deviceCodeHash string, now time.Time, createSession func(User) (Session, error)) (Session, User, error) {
	authorization, ok := store.devices[deviceCodeHash]
	if !ok {
		return Session{}, User{}, ErrNotFound
	}
	if !authorization.ExpiresAt.After(now) {
		return Session{}, User{}, ErrAuthorizationExpired
	}
	if authorization.Status == "pending" {
		return Session{}, User{}, ErrAuthorizationPending
	}
	if authorization.Status == "denied" {
		return Session{}, User{}, ErrAuthorizationDenied
	}
	if authorization.Status != "approved" {
		return Session{}, User{}, ErrConflict
	}
	session, err := createSession(store.user)
	if err != nil {
		return Session{}, User{}, err
	}
	authorization.Status = "consumed"
	store.devices[deviceCodeHash] = authorization
	store.sessions[session.ID] = session
	return session, store.user, nil
}

func (store *memoryStore) RotateSession(_ context.Context, refreshTokenHash string, tokenHash string, newRefreshTokenHash string, now time.Time) (Session, User, error) {
	for id, session := range store.sessions {
		if session.RefreshTokenHash == refreshTokenHash && session.RevokedAt == nil && session.ExpiresAt.After(now) {
			session.TokenHash = tokenHash
			session.RefreshTokenHash = newRefreshTokenHash
			store.sessions[id] = session
			return session, store.user, nil
		}
	}
	return Session{}, User{}, ErrNotFound
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

func (store *memoryStore) FindSessionByRefreshToken(_ context.Context, refreshTokenHash string, now time.Time) (Session, User, error) {
	for _, session := range store.sessions {
		if session.RefreshTokenHash == refreshTokenHash && session.RevokedAt == nil && session.ExpiresAt.After(now) {
			return session, store.user, nil
		}
	}
	return Session{}, User{}, ErrNotFound
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

func (store *memoryStore) RevokeManagedToken(
	_ context.Context,
	tokenID string,
	projectID string,
	kind string,
	now time.Time,
) error {
	for hash, token := range store.tokens {
		if token.ID == tokenID && token.ProjectID == projectID && token.Kind == kind {
			token.RevokedAt = &now
			store.tokens[hash] = token
			return nil
		}
	}
	return ErrNotFound
}

func TestRevokeManagedTokenUsesProjectScopedAuthLifecycle(t *testing.T) {
	now := time.Date(2026, time.August, 11, 10, 0, 0, 0, time.UTC)
	store := newMemoryStore()
	store.tokens["hash"] = Token{ID: "token-1", UserID: "creator", ProjectID: "project-1", Kind: "box"}
	service := Service{Clock: clock.Fixed{Time: now}, ProjectTokens: projectAuthorizerStub{}, Store: store}

	if err := service.RevokeManagedToken(context.Background(), Identity{Kind: "session", User: User{ID: "manager"}}, "project-1", "box", "token-1"); err != nil {
		t.Fatalf("revoke managed token: %v", err)
	}
	if token := store.tokens["hash"]; token.RevokedAt == nil || !token.RevokedAt.Equal(now) {
		t.Fatalf("managed token was not revoked: %#v", token)
	}
	if err := service.RevokeManagedToken(context.Background(), Identity{}, "project-1", "api", "token-1"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("unexpected invalid-kind result: %v", err)
	}
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
	issued, err := service.IssueAgentToken(
		context.Background(),
		Identity{Kind: "session", User: store.user},
		IssueAgentTokenInput{
			AgentInstanceID: "agent-1", AllowedTools: []string{"project.get"},
			GrantID: "grant-1", Name: "research agent", ProjectID: "project-1",
		},
	)
	if err != nil {
		t.Fatalf("issue token: %v", err)
	}
	if issued.Secret == "" || issued.Challenge == "" ||
		issued.Token.TokenHash == issued.Secret ||
		issued.Token.VerificationChallengeHash == issued.Challenge {
		t.Fatal("token and challenge must be returned once and stored only as hashes")
	}
	authenticated, err := service.Authenticate(context.Background(), "Bearer "+issued.Secret)
	if err != nil {
		t.Fatalf("authenticate token: %v", err)
	}
	if authenticated.Kind != "agent" || authenticated.ProjectID != "project-1" ||
		authenticated.AgentInstanceID != "agent-1" || authenticated.CredentialStatus != "pending" ||
		authenticated.User.ID != "user-1" {
		t.Fatalf("unexpected token identity: %#v", authenticated)
	}
	stored := store.agentTokens[issued.Token.ID]
	if stored.LastUsedAt != nil || stored.Verification != nil {
		t.Fatalf("ordinary authentication created pending verification evidence: %#v", stored)
	}
}

func TestGenericTokenIssueRejectsCompatibilityAgentKind(t *testing.T) {
	service := Service{}
	_, err := service.IssueToken(
		context.Background(),
		Identity{Kind: "session", User: User{ID: "user-1"}},
		"agent",
		"legacy generic agent",
		"project-1",
		nil,
	)
	if !errors.Is(err, ErrInvalid) {
		t.Fatalf("generic agent token issue: got %v want ErrInvalid", err)
	}
}

func TestPendingAgentChallengeVerificationIsRequiredBeforeActivation(t *testing.T) {
	now := time.Date(2026, time.August, 6, 12, 0, 0, 0, time.UTC)
	const challenge = "mmdash_challenge_pending-token"
	store := newMemoryStore()
	store.agentTokens["old-token"] = AgentToken{
		AgentInstanceID: "agent-1", CreatedAt: now.Add(-time.Hour), GrantID: "grant-1",
		ID: "old-token", ProjectID: "project-1", Status: "active",
	}
	store.agentTokens["pending-token"] = AgentToken{
		AgentInstanceID: "agent-1", CreatedAt: now.Add(-time.Minute), GrantID: "grant-1",
		ID: "pending-token", ProjectID: "project-1", Status: "pending",
		VerificationChallengeHash: hashToken(challenge),
	}
	service := Service{
		Clock:         clock.Fixed{Time: now},
		Generator:     identity.Generator{Reader: bytes.NewReader(make([]byte, 16))},
		ProjectTokens: projectAuthorizerStub{},
		Store:         store,
	}
	operator := Identity{Kind: "session", User: User{ID: "user-1", SystemRole: "admin"}}
	pendingAgent := Identity{
		AgentInstanceID: "agent-1", CredentialStatus: "pending", Kind: "agent",
		ProjectID: "project-1", TokenID: "pending-token",
	}

	if _, err := service.ActivateAgentToken(
		context.Background(), operator, "project-1", "pending-token", "old-token", "",
	); !errors.Is(err, ErrNotFound) && !errors.Is(err, ErrConflict) {
		t.Fatalf("activation without challenge evidence: %v", err)
	}
	if store.agentTokens["old-token"].Status != "active" {
		t.Fatal("old token was revoked before challenge verification")
	}

	evidence, err := service.RecordAgentTokenVerification(
		context.Background(), pendingAgent, "pending-token",
		RecordAgentTokenVerificationInput{
			AgentInstanceID: "agent-1", Challenge: challenge, MCPMethod: AgentTokenVerificationMethod,
			MCPSessionID: "mcp-session-1", ProjectID: "project-1", RequestID: "request-1",
		},
	)
	if err != nil {
		t.Fatalf("record challenge verification: %v", err)
	}
	if evidence.MCPMethod != "tools/list" || evidence.EvidenceID == "" ||
		!evidence.VerifiedAt.Equal(now) {
		t.Fatalf("unexpected verification evidence: %#v", evidence)
	}
	if store.agentTokens["pending-token"].VerificationChallengeHash != "" {
		t.Fatal("one-time challenge was not consumed")
	}
	const remoteAccessID = " remote/access::v2 "
	activated, err := service.ActivateAgentToken(
		context.Background(), operator, "project-1", "pending-token", "old-token", remoteAccessID,
	)
	if err != nil {
		t.Fatalf("activate verified token: %v", err)
	}
	if activated.Status != "active" || store.agentTokens["old-token"].Status != "revoked" {
		t.Fatalf("unexpected activation outcome: new=%#v old=%#v", activated, store.agentTokens["old-token"])
	}
	if store.activatedRemoteAccessID != remoteAccessID {
		t.Fatalf("remote access ID was interpreted by Auth: got %q want %q",
			store.activatedRemoteAccessID, remoteAccessID)
	}
}

func TestAgentTokenVerificationRejectsWrongIdentityChallengeOrMethod(t *testing.T) {
	now := time.Date(2026, time.August, 6, 12, 0, 0, 0, time.UTC)
	const challenge = "mmdash_challenge_expected"
	store := newMemoryStore()
	store.agentTokens["pending-token"] = AgentToken{
		AgentInstanceID: "agent-1", CreatedAt: now, GrantID: "grant-1",
		ID: "pending-token", ProjectID: "project-1", Status: "pending",
		VerificationChallengeHash: hashToken(challenge),
	}
	service := Service{
		Clock:     clock.Fixed{Time: now},
		Generator: identity.Generator{Reader: bytes.NewReader(make([]byte, 48))},
		Store:     store,
	}
	input := RecordAgentTokenVerificationInput{
		AgentInstanceID: "agent-1", Challenge: challenge, MCPMethod: AgentTokenVerificationMethod,
		MCPSessionID: "mcp-session-1", ProjectID: "project-1", RequestID: "request-1",
	}
	if _, err := service.RecordAgentTokenVerification(
		context.Background(), Identity{Kind: "session", User: User{SystemRole: "admin"}},
		"pending-token", input,
	); !errors.Is(err, ErrForbidden) {
		t.Fatalf("browser session recorded evidence: %v", err)
	}
	wrongAgent := Identity{AgentInstanceID: "agent-2", CredentialStatus: "pending", Kind: "agent", ProjectID: "project-1", TokenID: "pending-token"}
	if _, err := service.RecordAgentTokenVerification(
		context.Background(), wrongAgent, "pending-token", input,
	); !errors.Is(err, ErrForbidden) {
		t.Fatalf("wrong Agent recorded evidence: %v", err)
	}
	pendingAgent := Identity{AgentInstanceID: "agent-1", CredentialStatus: "pending", Kind: "agent", ProjectID: "project-1", TokenID: "pending-token"}
	input.Challenge = "mmdash_challenge_wrong"
	if _, err := service.RecordAgentTokenVerification(
		context.Background(), pendingAgent, "pending-token", input,
	); !errors.Is(err, ErrForbidden) {
		t.Fatalf("wrong challenge recorded evidence: %v", err)
	}
	input.Challenge = challenge
	input.MCPMethod = "initialize"
	if _, err := service.RecordAgentTokenVerification(
		context.Background(), pendingAgent, "pending-token", input,
	); !errors.Is(err, ErrInvalid) {
		t.Fatalf("initialize created evidence: %v", err)
	}
}

func TestExpiredOrCrossProjectPendingAgentTokenCannotVerifyOrActivate(t *testing.T) {
	now := time.Date(2026, time.August, 6, 12, 0, 0, 0, time.UTC)
	expiresAt := now.Add(-time.Second)
	store := newMemoryStore()
	store.agentTokens["old-token"] = AgentToken{
		AgentInstanceID: "agent-1", CreatedAt: now.Add(-time.Hour), GrantID: "grant-1",
		ID: "old-token", ProjectID: "project-1", Status: "active",
	}
	store.agentTokens["expired-token"] = AgentToken{
		AgentInstanceID: "agent-1", CreatedAt: now.Add(-time.Minute),
		ExpiresAt: &expiresAt, GrantID: "grant-1", ID: "expired-token",
		ProjectID: "project-1", Status: "pending",
		VerificationChallengeHash: hashToken("mmdash_challenge_expired"),
	}
	service := Service{
		Clock:         clock.Fixed{Time: now},
		Generator:     identity.Generator{Reader: bytes.NewReader(make([]byte, 16))},
		ProjectTokens: projectAuthorizerStub{},
		Store:         store,
	}
	operator := Identity{Kind: "session", User: User{ID: "user-1", SystemRole: "admin"}}
	pendingAgent := Identity{AgentInstanceID: "agent-1", CredentialStatus: "pending", Kind: "agent", ProjectID: "project-1", TokenID: "expired-token"}
	input := RecordAgentTokenVerificationInput{
		AgentInstanceID: "agent-1", Challenge: "mmdash_challenge_expired", MCPMethod: AgentTokenVerificationMethod,
		MCPSessionID: "mcp-session-1", ProjectID: "project-1", RequestID: "request-1",
	}
	if _, err := service.RecordAgentTokenVerification(
		context.Background(), pendingAgent, "expired-token", input,
	); !errors.Is(err, ErrConflict) {
		t.Fatalf("expired token verification: got %v", err)
	}
	expired := store.agentTokens["expired-token"]
	expired.Verification = &AgentTokenVerificationEvidence{
		AgentInstanceID: "agent-1", EvidenceID: "evidence-1",
		MCPMethod: AgentTokenVerificationMethod, MCPSessionID: "mcp-session-1",
		ProjectID: "project-1", RequestID: "request-1", TokenID: "expired-token",
		VerifiedAt: now.Add(-2 * time.Second),
	}
	store.agentTokens["expired-token"] = expired
	if _, err := service.ActivateAgentToken(
		context.Background(), operator, "project-1", "expired-token", "old-token", "",
	); !errors.Is(err, ErrConflict) {
		t.Fatalf("expired token activation: got %v", err)
	}
	if store.agentTokens["old-token"].Status != "active" {
		t.Fatal("expired replacement revoked the old token")
	}
	if _, err := service.ActivateAgentToken(
		context.Background(), operator, "project-2", "expired-token", "old-token", "",
	); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-project token activation: got %v", err)
	}
	if err := service.RevokeAgentToken(
		context.Background(), operator, "project-2", "expired-token",
	); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-project token revoke: got %v", err)
	}
	if store.agentTokens["expired-token"].Status != "pending" {
		t.Fatal("cross-project revoke changed the target token")
	}
}

func TestDeviceAuthorizationAndRefreshRotation(t *testing.T) {
	now := time.Date(2026, time.August, 1, 12, 0, 0, 0, time.UTC)
	store := newMemoryStore()
	store.user = User{ID: "user-1", Email: "user@example.com", DisplayName: "User", Status: "active", SystemRole: "member", CreatedAt: now}
	service := Service{
		AccessTokenTTL:         time.Hour,
		Clock:                  clock.Fixed{Time: now},
		DeviceAuthorizationTTL: 10 * time.Minute,
		DevicePollInterval:     2 * time.Second,
		DeviceVerificationURI:  "https://mmdash.example/cli/authorize",
		Generator:              identity.Generator{Reader: bytes.NewReader(make([]byte, 64))},
		JWTSecret:              []byte("test-jwt-secret-with-at-least-32-characters"),
		SessionTTL:             24 * time.Hour,
		Store:                  store,
	}

	authorization, err := service.StartDeviceAuthorization(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if authorization.Interval != 2 || authorization.UserCode == "" || authorization.DeviceCode == "" {
		t.Fatalf("unexpected authorization: %#v", authorization)
	}
	if _, err := service.ExchangeDeviceAuthorization(context.Background(), authorization.DeviceCode); !errors.Is(err, ErrAuthorizationPending) {
		t.Fatalf("expected pending exchange, got %v", err)
	}
	if err := service.DecideDeviceAuthorization(context.Background(), Identity{Kind: "session", User: store.user}, authorization.UserCode, true); err != nil {
		t.Fatal(err)
	}
	login, err := service.ExchangeDeviceAuthorization(context.Background(), authorization.DeviceCode)
	if err != nil {
		t.Fatal(err)
	}
	if login.RefreshToken == "" || login.AccessToken == "" {
		t.Fatal("device exchange did not return both session secrets")
	}
	if _, err := service.Authenticate(context.Background(), "Bearer "+login.AccessToken); err != nil {
		t.Fatalf("authenticate device session: %v", err)
	}
	if _, err := service.ExchangeDeviceAuthorization(context.Background(), authorization.DeviceCode); !errors.Is(err, ErrConflict) {
		t.Fatalf("device code was not single-use: %v", err)
	}
	refreshed, err := service.Refresh(context.Background(), login.RefreshToken)
	if err != nil {
		t.Fatal(err)
	}
	if refreshed.AccessToken == login.AccessToken || refreshed.RefreshToken == login.RefreshToken {
		t.Fatal("refresh did not rotate both secrets")
	}
	if _, err := service.Authenticate(context.Background(), "Bearer "+login.AccessToken); !errors.Is(err, ErrUnauthenticated) {
		t.Fatalf("old access token remained valid: %v", err)
	}
	if _, err := service.Refresh(context.Background(), login.RefreshToken); !errors.Is(err, ErrUnauthenticated) {
		t.Fatalf("old refresh token remained valid: %v", err)
	}
}
