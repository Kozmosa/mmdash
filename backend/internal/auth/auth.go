// Package auth owns users, sessions, access tokens, and request identities.
package auth

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"

	"github.com/mmdash/mmdash/backend/internal/platform/clock"
	"github.com/mmdash/mmdash/backend/internal/platform/identity"
	"github.com/mmdash/mmdash/backend/internal/platform/requestctx"
	"github.com/mmdash/mmdash/backend/internal/platform/transaction"
)

// User is the public account projection.
type User struct {
	CreatedAt   time.Time `json:"created_at"`
	DisplayName string    `json:"display_name"`
	Email       string    `json:"email"`
	ID          string    `json:"id"`
	Status      string    `json:"status"`
	SystemRole  string    `json:"system_role"`
}

// Session is a persisted browser login.
type Session struct {
	CreatedAt        time.Time
	ExpiresAt        time.Time
	ID               string
	RefreshTokenHash string
	RevokedAt        *time.Time
	TokenHash        string
	UserID           string
}

// DeviceAuthorization is a short-lived user-approved CLI login request.
type DeviceAuthorization struct {
	CreatedAt      time.Time
	DeviceCodeHash string
	ExpiresAt      time.Time
	ID             string
	Status         string
	UserCodeHash   string
	UserID         string
}

// DeviceAuthorizationResult returns the two one-time codes to the CLI.
type DeviceAuthorizationResult struct {
	DeviceCode              string    `json:"device_code"`
	ExpiresAt               time.Time `json:"expires_at"`
	Interval                int       `json:"interval"`
	UserCode                string    `json:"user_code"`
	VerificationURI         string    `json:"verification_uri"`
	VerificationURIComplete string    `json:"verification_uri_complete"`
}

// Token is a persisted API, Agent, or Box credential.
type Token struct {
	CreatedAt time.Time  `json:"created_at"`
	ExpiresAt *time.Time `json:"expires_at,omitempty"`
	ID        string     `json:"id"`
	Kind      string     `json:"kind"`
	Name      string     `json:"name"`
	ProjectID string     `json:"project_id,omitempty"`
	RevokedAt *time.Time `json:"revoked_at,omitempty"`
	TokenHash string     `json:"-"`
	UserID    string     `json:"user_id"`
}

// Identity is the authenticated caller used by domain authorization.
type Identity struct {
	Kind      string `json:"kind"`
	ProjectID string `json:"project_id,omitempty"`
	SessionID string `json:"session_id,omitempty"`
	TokenID   string `json:"token_id,omitempty"`
	User      User   `json:"user"`
}

// LoginResult returns the JWT only at login time.
type LoginResult struct {
	AccessToken  string    `json:"access_token"`
	ExpiresAt    time.Time `json:"expires_at"`
	RefreshToken string    `json:"refresh_token"`
	SessionID    string    `json:"session_id"`
	User         User      `json:"user"`
}

// RegisterInput contains public account registration fields.
type RegisterInput struct {
	DisplayName     string
	Email           string
	InvitationToken string
	Password        string
}

// UpdateProfileInput contains mutable account fields.
type UpdateProfileInput struct {
	CurrentPassword string
	DisplayName     *string
	Email           *string
}

// Invitation is the public invitation projection needed by Auth.
type Invitation struct {
	CreatedAt   time.Time `json:"created_at"`
	Email       string    `json:"email"`
	ExpiresAt   time.Time `json:"expires_at"`
	ID          string    `json:"id"`
	InvitedBy   string    `json:"invited_by"`
	ProjectID   string    `json:"project_id"`
	ProjectName string    `json:"project_name"`
	Role        string    `json:"role"`
	Status      string    `json:"status"`
}

// AcceptedMember is returned after invitation acceptance.
type AcceptedMember struct {
	DisplayName string    `json:"display_name"`
	Email       string    `json:"email"`
	JoinedAt    time.Time `json:"joined_at"`
	Role        string    `json:"role"`
	UserID      string    `json:"user_id"`
}

// IssuedToken returns the opaque secret only once.
type IssuedToken struct {
	Secret string `json:"token"`
	Token  Token  `json:"credential"`
}

// Store is the persistence boundary for the auth domain.
type Store interface {
	CreateDeviceAuthorization(context.Context, DeviceAuthorization) error
	CreateSession(context.Context, Session) error
	CreateToken(context.Context, Token) error
	CreateUser(context.Context, User, string) error
	FindSession(context.Context, string, string, time.Time) (Session, User, error)
	FindSessionByRefreshToken(context.Context, string, time.Time) (Session, User, error)
	FindToken(context.Context, string, time.Time) (Token, User, error)
	FindUserByEmail(context.Context, string) (User, string, error)
	DecideDeviceAuthorization(context.Context, string, string, bool, time.Time) error
	ExchangeDeviceAuthorization(context.Context, string, time.Time, func(User) (Session, error)) (Session, User, error)
	ListTokens(context.Context, string) ([]Token, error)
	RotateSession(context.Context, string, string, string, time.Time) (Session, User, error)
	RevokeSession(context.Context, string, time.Time) error
	RevokeOtherSessions(context.Context, string, string, time.Time) error
	RevokeToken(context.Context, string, string, time.Time) error
	UpdatePassword(context.Context, string, string, time.Time) error
	UpdateUser(context.Context, string, string, string, time.Time) (User, error)
	DeleteUser(context.Context, string) error
}

// RegistrationPolicy resolves whether users may register without an invitation.
type RegistrationPolicy interface {
	AllowOpenRegistration(context.Context) (bool, error)
}

// StaticRegistrationPolicy is the bootstrap/default registration policy.
type StaticRegistrationPolicy bool

func (policy StaticRegistrationPolicy) AllowOpenRegistration(context.Context) (bool, error) {
	return bool(policy), nil
}

// InvitationService bridges Auth registration to Project-owned collaboration.
type InvitationService interface {
	AcceptInvitation(context.Context, Identity, string) (AcceptedMember, error)
	AcceptRegistration(context.Context, string, User) (AcceptedMember, error)
	DeclineInvitation(context.Context, string) error
	PreviewInvitation(context.Context, string) (Invitation, error)
}

// TransactionalInvitationService consumes an invitation in the caller's
// registration transaction so user and membership state cannot diverge.
type TransactionalInvitationService interface {
	AcceptRegistrationInTransaction(context.Context, transaction.Tx, string, User) (AcceptedMember, error)
}

// TransactionalRegistrationStore creates an account and invokes invitation
// acceptance inside the same database transaction.
type TransactionalRegistrationStore interface {
	CreateUserAndAcceptInvitation(context.Context, User, string, func(transaction.Tx) error) error
}

// ProjectTokenAuthorizer verifies project-scoped token management.
type ProjectTokenAuthorizer interface {
	AuthorizeTokenManagement(context.Context, Identity, string) error
}

// Service contains credential policy and token cryptography.
type Service struct {
	AccessTokenTTL         time.Duration
	Clock                  clock.Clock
	DeviceAuthorizationTTL time.Duration
	DevicePollInterval     time.Duration
	DeviceVerificationURI  string
	Generator              identity.Generator
	JWTSecret              []byte
	Invitations            InvitationService
	Policy                 RegistrationPolicy
	ProjectTokens          ProjectTokenAuthorizer
	SessionTTL             time.Duration
	Store                  Store
}

// Register creates an active account and immediately creates a session.
func (service Service) Register(ctx context.Context, input RegisterInput) (LoginResult, error) {
	input.Email = normalizeEmail(input.Email)
	input.DisplayName = strings.TrimSpace(input.DisplayName)
	if input.Email == "" || input.DisplayName == "" || len(input.Password) < 8 {
		return LoginResult{}, ErrInvalid
	}
	if _, _, err := service.Store.FindUserByEmail(ctx, input.Email); err == nil {
		return LoginResult{}, ErrConflict
	} else if !errors.Is(err, ErrNotFound) {
		return LoginResult{}, err
	}
	if input.InvitationToken == "" {
		allowed := false
		if service.Policy != nil {
			var err error
			allowed, err = service.Policy.AllowOpenRegistration(ctx)
			if err != nil {
				return LoginResult{}, err
			}
		}
		if !allowed {
			return LoginResult{}, ErrRegistrationClosed
		}
	} else {
		if service.Invitations == nil {
			return LoginResult{}, ErrInvalidInvitation
		}
		invitation, err := service.Invitations.PreviewInvitation(ctx, input.InvitationToken)
		if err != nil || normalizeEmail(invitation.Email) != input.Email {
			return LoginResult{}, ErrInvalidInvitation
		}
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(input.Password), bcrypt.DefaultCost)
	if err != nil {
		return LoginResult{}, err
	}
	userID, err := service.Generator.New()
	if err != nil {
		return LoginResult{}, err
	}
	user := User{CreatedAt: service.Clock.Now().UTC(), DisplayName: input.DisplayName, Email: input.Email, ID: userID, Status: "active", SystemRole: "member"}
	if input.InvitationToken != "" {
		store, storeOK := service.Store.(TransactionalRegistrationStore)
		invitations, invitationsOK := service.Invitations.(TransactionalInvitationService)
		if !storeOK || !invitationsOK {
			return LoginResult{}, fmt.Errorf("invitation registration transaction is not configured")
		}
		if err := store.CreateUserAndAcceptInvitation(ctx, user, string(hash), func(tx transaction.Tx) error {
			_, err := invitations.AcceptRegistrationInTransaction(ctx, tx, input.InvitationToken, user)
			return err
		}); err != nil {
			return LoginResult{}, err
		}
	} else if err := service.Store.CreateUser(ctx, user, string(hash)); err != nil {
		return LoginResult{}, err
	}
	return service.Login(ctx, user.Email, input.Password)
}

// UpdateProfile changes display name or email after credential verification.
func (service Service) UpdateProfile(ctx context.Context, identity Identity, input UpdateProfileInput) (User, error) {
	user, passwordHash, err := service.Store.FindUserByEmail(ctx, identity.User.Email)
	if err != nil {
		return User{}, err
	}
	displayName := user.DisplayName
	email := user.Email
	if input.DisplayName != nil {
		displayName = strings.TrimSpace(*input.DisplayName)
		if displayName == "" {
			return User{}, ErrInvalid
		}
	}
	if input.Email != nil {
		email = normalizeEmail(*input.Email)
		if email == "" || bcrypt.CompareHashAndPassword([]byte(passwordHash), []byte(input.CurrentPassword)) != nil {
			return User{}, ErrInvalidCredentials
		}
	}
	updated, err := service.Store.UpdateUser(ctx, user.ID, email, displayName, service.Clock.Now().UTC())
	if err != nil {
		return User{}, err
	}
	return updated, nil
}

// ChangePassword verifies the old password and revokes other sessions.
func (service Service) ChangePassword(ctx context.Context, identity Identity, currentPassword string, newPassword string) error {
	if len(newPassword) < 8 {
		return ErrInvalid
	}
	_, passwordHash, err := service.Store.FindUserByEmail(ctx, identity.User.Email)
	if err != nil || bcrypt.CompareHashAndPassword([]byte(passwordHash), []byte(currentPassword)) != nil {
		return ErrInvalidCredentials
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(newPassword), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	now := service.Clock.Now().UTC()
	if err := service.Store.UpdatePassword(ctx, identity.User.ID, string(hash), now); err != nil {
		return err
	}
	return service.Store.RevokeOtherSessions(ctx, identity.User.ID, identity.SessionID, now)
}

func (service Service) PreviewInvitation(ctx context.Context, token string) (Invitation, error) {
	if service.Invitations == nil || strings.TrimSpace(token) == "" {
		return Invitation{}, ErrInvalidInvitation
	}
	return service.Invitations.PreviewInvitation(ctx, token)
}

func (service Service) AcceptInvitation(ctx context.Context, identity Identity, token string) (AcceptedMember, error) {
	if service.Invitations == nil {
		return AcceptedMember{}, ErrInvalidInvitation
	}
	return service.Invitations.AcceptInvitation(ctx, identity, token)
}

func (service Service) DeclineInvitation(ctx context.Context, token string) error {
	if service.Invitations == nil || strings.TrimSpace(token) == "" {
		return ErrInvalidInvitation
	}
	return service.Invitations.DeclineInvitation(ctx, token)
}

type jwtClaims struct {
	ExpiresAt int64  `json:"exp"`
	ID        string `json:"jti"`
	IssuedAt  int64  `json:"iat"`
	SessionID string `json:"sid"`
	Subject   string `json:"sub"`
	Type      string `json:"typ"`
}

// EnsureBootstrapUser creates the configured first account when absent.
func (service Service) EnsureBootstrapUser(
	ctx context.Context,
	email string,
	displayName string,
	password string,
) error {
	if _, _, err := service.Store.FindUserByEmail(ctx, normalizeEmail(email)); err == nil {
		return nil
	} else if !errors.Is(err, ErrNotFound) {
		return fmt.Errorf("find bootstrap user: %w", err)
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("hash bootstrap password: %w", err)
	}
	userID, err := service.Generator.New()
	if err != nil {
		return err
	}
	now := service.Clock.Now().UTC()
	return service.Store.CreateUser(ctx, User{
		CreatedAt:   now,
		DisplayName: strings.TrimSpace(displayName),
		Email:       normalizeEmail(email),
		ID:          userID,
		Status:      "active",
		SystemRole:  "admin",
	}, string(hash))
}

// Login verifies credentials and creates a revocable database session.
func (service Service) Login(ctx context.Context, email string, password string) (LoginResult, error) {
	user, passwordHash, err := service.Store.FindUserByEmail(ctx, normalizeEmail(email))
	if err != nil || user.Status != "active" ||
		bcrypt.CompareHashAndPassword([]byte(passwordHash), []byte(password)) != nil {
		return LoginResult{}, ErrInvalidCredentials
	}
	now := service.Clock.Now().UTC()
	sessionID, err := service.Generator.New()
	if err != nil {
		return LoginResult{}, err
	}
	sessionExpiresAt := now.Add(service.SessionTTL)
	accessToken, refreshToken, accessExpiresAt, err := service.createSessionTokens(sessionID, user.ID, now, sessionExpiresAt)
	if err != nil {
		return LoginResult{}, err
	}
	if err := service.Store.CreateSession(ctx, Session{
		CreatedAt:        now,
		ExpiresAt:        sessionExpiresAt,
		ID:               sessionID,
		RefreshTokenHash: hashToken(refreshToken),
		TokenHash:        hashToken(accessToken),
		UserID:           user.ID,
	}); err != nil {
		return LoginResult{}, fmt.Errorf("create session: %w", err)
	}
	requestctx.SetActor(ctx, user.ID, "session")
	return LoginResult{
		AccessToken:  accessToken,
		ExpiresAt:    accessExpiresAt,
		RefreshToken: refreshToken,
		SessionID:    sessionID,
		User:         user,
	}, nil
}

// Refresh rotates both session secrets and immediately invalidates the old pair.
func (service Service) Refresh(ctx context.Context, refreshToken string) (LoginResult, error) {
	if !strings.HasPrefix(refreshToken, "mmdash_refresh_") {
		return LoginResult{}, ErrUnauthenticated
	}
	now := service.Clock.Now().UTC()
	session, user, err := service.Store.FindSessionByRefreshToken(ctx, hashToken(refreshToken), now)
	if err != nil {
		return LoginResult{}, ErrUnauthenticated
	}
	newRefresh, err := randomSecret("mmdash_refresh_")
	if err != nil {
		return LoginResult{}, err
	}
	accessExpiresAt := service.accessExpiresAt(now, session.ExpiresAt)
	jwtID, err := randomSecret("jwt_")
	if err != nil {
		return LoginResult{}, err
	}
	accessToken, err := service.signJWT(jwtClaims{ExpiresAt: accessExpiresAt.Unix(), ID: jwtID, IssuedAt: now.Unix(), SessionID: session.ID, Subject: user.ID, Type: "session"})
	if err != nil {
		return LoginResult{}, err
	}
	session, user, err = service.Store.RotateSession(ctx, hashToken(refreshToken), hashToken(accessToken), hashToken(newRefresh), now)
	if err != nil {
		return LoginResult{}, ErrUnauthenticated
	}
	requestctx.SetActor(ctx, user.ID, "session")
	return LoginResult{AccessToken: accessToken, ExpiresAt: accessExpiresAt, RefreshToken: newRefresh, SessionID: session.ID, User: user}, nil
}

// StartDeviceAuthorization creates one short-lived device login challenge.
func (service Service) StartDeviceAuthorization(ctx context.Context) (DeviceAuthorizationResult, error) {
	now := service.Clock.Now().UTC()
	id, err := service.Generator.New()
	if err != nil {
		return DeviceAuthorizationResult{}, err
	}
	deviceCode, err := randomSecret("mmdash_device_")
	if err != nil {
		return DeviceAuthorizationResult{}, err
	}
	userCode, err := randomUserCode()
	if err != nil {
		return DeviceAuthorizationResult{}, err
	}
	ttl := service.DeviceAuthorizationTTL
	if ttl <= 0 {
		ttl = 10 * time.Minute
	}
	interval := service.DevicePollInterval
	if interval <= 0 {
		interval = 5 * time.Second
	}
	verificationURI := strings.TrimRight(service.DeviceVerificationURI, "/")
	if verificationURI == "" {
		return DeviceAuthorizationResult{}, fmt.Errorf("device verification URI is not configured")
	}
	authorization := DeviceAuthorization{CreatedAt: now, DeviceCodeHash: hashToken(deviceCode), ExpiresAt: now.Add(ttl), ID: id, Status: "pending", UserCodeHash: hashToken(normalizeUserCode(userCode))}
	if err := service.Store.CreateDeviceAuthorization(ctx, authorization); err != nil {
		return DeviceAuthorizationResult{}, err
	}
	return DeviceAuthorizationResult{DeviceCode: deviceCode, ExpiresAt: authorization.ExpiresAt, Interval: int(interval / time.Second), UserCode: userCode, VerificationURI: verificationURI, VerificationURIComplete: verificationURI + "?user_code=" + userCode}, nil
}

// DecideDeviceAuthorization records the authenticated browser user's decision.
func (service Service) DecideDeviceAuthorization(ctx context.Context, identity Identity, userCode string, approve bool) error {
	if identity.User.ID == "" {
		return ErrUnauthenticated
	}
	return service.Store.DecideDeviceAuthorization(ctx, hashToken(normalizeUserCode(userCode)), identity.User.ID, approve, service.Clock.Now().UTC())
}

// ExchangeDeviceAuthorization creates a refreshable CLI session exactly once.
func (service Service) ExchangeDeviceAuthorization(ctx context.Context, deviceCode string) (LoginResult, error) {
	if !strings.HasPrefix(deviceCode, "mmdash_device_") {
		return LoginResult{}, ErrInvalid
	}
	now := service.Clock.Now().UTC()
	sessionID, err := service.Generator.New()
	if err != nil {
		return LoginResult{}, err
	}
	refreshToken, err := randomSecret("mmdash_refresh_")
	if err != nil {
		return LoginResult{}, err
	}
	sessionExpiresAt := now.Add(service.SessionTTL)
	accessExpiresAt := service.accessExpiresAt(now, sessionExpiresAt)
	var accessToken string
	session, user, err := service.Store.ExchangeDeviceAuthorization(ctx, hashToken(deviceCode), now, func(user User) (Session, error) {
		jwtID, err := randomSecret("jwt_")
		if err != nil {
			return Session{}, err
		}
		accessToken, err = service.signJWT(jwtClaims{ExpiresAt: accessExpiresAt.Unix(), ID: jwtID, IssuedAt: now.Unix(), SessionID: sessionID, Subject: user.ID, Type: "session"})
		if err != nil {
			return Session{}, err
		}
		return Session{
			CreatedAt:        now,
			ExpiresAt:        sessionExpiresAt,
			ID:               sessionID,
			RefreshTokenHash: hashToken(refreshToken),
			TokenHash:        hashToken(accessToken),
			UserID:           user.ID,
		}, nil
	})
	if err != nil {
		return LoginResult{}, err
	}
	requestctx.SetActor(ctx, user.ID, "session")
	return LoginResult{AccessToken: accessToken, ExpiresAt: accessExpiresAt, RefreshToken: refreshToken, SessionID: session.ID, User: user}, nil
}

func (service Service) createSessionTokens(sessionID string, userID string, now time.Time, sessionExpiresAt time.Time) (string, string, time.Time, error) {
	accessExpiresAt := service.accessExpiresAt(now, sessionExpiresAt)
	jwtID, err := randomSecret("jwt_")
	if err != nil {
		return "", "", time.Time{}, err
	}
	accessToken, err := service.signJWT(jwtClaims{ExpiresAt: accessExpiresAt.Unix(), ID: jwtID, IssuedAt: now.Unix(), SessionID: sessionID, Subject: userID, Type: "session"})
	if err != nil {
		return "", "", time.Time{}, err
	}
	refreshToken, err := randomSecret("mmdash_refresh_")
	return accessToken, refreshToken, accessExpiresAt, err
}

func (service Service) accessExpiresAt(now time.Time, sessionExpiresAt time.Time) time.Time {
	ttl := service.AccessTokenTTL
	if ttl <= 0 {
		ttl = service.SessionTTL
	}
	expiresAt := now.Add(ttl)
	if expiresAt.After(sessionExpiresAt) {
		return sessionExpiresAt
	}
	return expiresAt
}

// Authenticate validates a session JWT or opaque service token.
func (service Service) Authenticate(ctx context.Context, authorization string) (Identity, error) {
	secret := strings.TrimSpace(strings.TrimPrefix(authorization, "Bearer "))
	if secret == "" || secret == authorization {
		return Identity{}, ErrUnauthenticated
	}
	now := service.Clock.Now().UTC()
	if strings.Count(secret, ".") == 2 {
		claims, err := service.verifyJWT(secret, now)
		if err != nil {
			return Identity{}, ErrUnauthenticated
		}
		session, user, err := service.Store.FindSession(ctx, claims.SessionID, hashToken(secret), now)
		if err != nil || session.UserID != claims.Subject {
			return Identity{}, ErrUnauthenticated
		}
		identity := Identity{Kind: "session", SessionID: session.ID, User: user}
		requestctx.SetActor(ctx, identity.User.ID, identity.Kind)
		return identity, nil
	}
	token, user, err := service.Store.FindToken(ctx, hashToken(secret), now)
	if err != nil {
		return Identity{}, ErrUnauthenticated
	}
	identity := Identity{
		Kind:      token.Kind,
		ProjectID: token.ProjectID,
		TokenID:   token.ID,
		User:      user,
	}
	requestctx.SetActor(ctx, identity.User.ID, identity.Kind)
	if identity.ProjectID != "" {
		requestctx.SetProject(ctx, identity.ProjectID)
	}
	return identity, nil
}

// Logout revokes the current browser session.
func (service Service) Logout(ctx context.Context, authorization string) error {
	identity, err := service.Authenticate(ctx, authorization)
	if err != nil || identity.SessionID == "" {
		return ErrUnauthenticated
	}
	return service.Store.RevokeSession(ctx, identity.SessionID, service.Clock.Now().UTC())
}

// IssueToken creates an API, Agent, or Box token.
func (service Service) IssueToken(
	ctx context.Context,
	identity Identity,
	kind string,
	name string,
	projectID string,
	expiresAt *time.Time,
) (IssuedToken, error) {
	if identity.Kind != "session" && identity.Kind != "api" {
		return IssuedToken{}, ErrForbidden
	}
	if kind != "api" && kind != "agent" && kind != "box" {
		return IssuedToken{}, ErrInvalid
	}
	if (kind == "agent" || kind == "box") && projectID == "" {
		return IssuedToken{}, ErrInvalid
	}
	if projectID != "" {
		if service.ProjectTokens == nil ||
			service.ProjectTokens.AuthorizeTokenManagement(ctx, identity, projectID) != nil {
			return IssuedToken{}, ErrForbidden
		}
	}
	tokenID, err := service.Generator.New()
	if err != nil {
		return IssuedToken{}, err
	}
	secret, err := randomSecret("mmdash_" + kind + "_")
	if err != nil {
		return IssuedToken{}, err
	}
	now := service.Clock.Now().UTC()
	if expiresAt != nil && !expiresAt.After(now) {
		return IssuedToken{}, ErrInvalid
	}
	token := Token{
		CreatedAt: now,
		ExpiresAt: expiresAt,
		ID:        tokenID,
		Kind:      kind,
		Name:      strings.TrimSpace(name),
		ProjectID: projectID,
		TokenHash: hashToken(secret),
		UserID:    identity.User.ID,
	}
	if token.Name == "" {
		return IssuedToken{}, ErrInvalid
	}
	if err := service.Store.CreateToken(ctx, token); err != nil {
		return IssuedToken{}, fmt.Errorf("create token: %w", err)
	}
	return IssuedToken{Secret: secret, Token: token}, nil
}

// ListTokens returns metadata without token secrets.
func (service Service) ListTokens(ctx context.Context, identity Identity) ([]Token, error) {
	return service.Store.ListTokens(ctx, identity.User.ID)
}

// RevokeToken revokes one token owned by the caller.
func (service Service) RevokeToken(ctx context.Context, identity Identity, tokenID string) error {
	return service.Store.RevokeToken(ctx, identity.User.ID, tokenID, service.Clock.Now().UTC())
}

func (service Service) signJWT(claims jwtClaims) (string, error) {
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"HS256","typ":"JWT"}`))
	payloadBytes, err := json.Marshal(claims)
	if err != nil {
		return "", err
	}
	payload := base64.RawURLEncoding.EncodeToString(payloadBytes)
	unsigned := header + "." + payload
	mac := hmac.New(sha256.New, service.JWTSecret)
	_, _ = mac.Write([]byte(unsigned))
	return unsigned + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil)), nil
}

func (service Service) verifyJWT(token string, now time.Time) (jwtClaims, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return jwtClaims{}, ErrUnauthenticated
	}
	mac := hmac.New(sha256.New, service.JWTSecret)
	_, _ = mac.Write([]byte(parts[0] + "." + parts[1]))
	signature, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil || !hmac.Equal(signature, mac.Sum(nil)) {
		return jwtClaims{}, ErrUnauthenticated
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return jwtClaims{}, ErrUnauthenticated
	}
	var claims jwtClaims
	if json.Unmarshal(payload, &claims) != nil ||
		claims.Type != "session" ||
		claims.Subject == "" ||
		claims.SessionID == "" ||
		claims.ExpiresAt <= now.Unix() {
		return jwtClaims{}, ErrUnauthenticated
	}
	return claims, nil
}

func randomSecret(prefix string) (string, error) {
	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		return "", fmt.Errorf("generate token: %w", err)
	}
	return prefix + base64.RawURLEncoding.EncodeToString(bytes), nil
}

func randomUserCode() (string, error) {
	const alphabet = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"
	value := make([]byte, 8)
	random := make([]byte, len(value))
	if _, err := rand.Read(random); err != nil {
		return "", fmt.Errorf("generate user code: %w", err)
	}
	for index := range value {
		value[index] = alphabet[int(random[index])%len(alphabet)]
	}
	return string(value[:4]) + "-" + string(value[4:]), nil
}

func normalizeUserCode(value string) string {
	return strings.ToUpper(strings.TrimSpace(value))
}

func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func normalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

// Domain errors are mapped to stable HTTP errors by the module.
var (
	ErrConflict             = fmt.Errorf("auth conflict")
	ErrForbidden            = fmt.Errorf("forbidden")
	ErrInvalidCredentials   = fmt.Errorf("invalid credentials")
	ErrInvalid              = fmt.Errorf("invalid auth input")
	ErrNotFound             = fmt.Errorf("not found")
	ErrInvalidInvitation    = fmt.Errorf("invalid invitation")
	ErrAuthorizationPending = fmt.Errorf("authorization pending")
	ErrAuthorizationDenied  = fmt.Errorf("authorization denied")
	ErrAuthorizationExpired = fmt.Errorf("authorization expired")
	ErrRegistrationClosed   = fmt.Errorf("registration closed")
	ErrUnauthenticated      = fmt.Errorf("unauthenticated")
)
