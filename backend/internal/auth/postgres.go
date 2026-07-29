package auth

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgconn"
	"github.com/mmdash/mmdash/backend/internal/platform/outbox"
	"github.com/mmdash/mmdash/backend/internal/platform/transaction"
)

// PostgresStore persists authentication state in module-owned tables.
type PostgresStore struct {
	DB          *sql.DB
	Outbox      outbox.Writer
	Transaction transaction.Manager
}

func (store PostgresStore) CreateUser(ctx context.Context, user User, passwordHash string) error {
	return store.Transaction.Within(ctx, nil, func(tx transaction.Tx) error {
		return store.createUser(ctx, tx, user, passwordHash)
	})
}

// CreateUserAndAcceptInvitation keeps account creation and invitation
// consumption atomic, including their durable events.
func (store PostgresStore) CreateUserAndAcceptInvitation(
	ctx context.Context,
	user User,
	passwordHash string,
	accept func(transaction.Tx) error,
) error {
	err := store.Transaction.Within(ctx, nil, func(tx transaction.Tx) error {
		if err := store.createUser(ctx, tx, user, passwordHash); err != nil {
			return err
		}
		return accept(tx)
	})
	if isUniqueViolation(err) {
		return ErrConflict
	}
	return err
}

func (store PostgresStore) createUser(ctx context.Context, tx transaction.Tx, user User, passwordHash string) error {
	if _, err := tx.ExecContext(ctx, `INSERT INTO auth_users (user_id,email,display_name,password_hash,status,system_role,created_at,updated_at) VALUES ($1,$2,$3,$4,$5,$6,$7,$7)`, user.ID, user.Email, user.DisplayName, passwordHash, user.Status, user.SystemRole, user.CreatedAt); err != nil {
		if isUniqueViolation(err) {
			return ErrConflict
		}
		return err
	}
	_, err := store.Outbox.Write(ctx, tx, outbox.Event{Actor: map[string]string{"user_id": user.ID}, EventType: "user.registered", Payload: map[string]interface{}{"display_name": user.DisplayName, "email": user.Email, "user_id": user.ID}, Producer: "auth"})
	return err
}

func (store PostgresStore) DeleteUser(ctx context.Context, userID string) error {
	_, err := store.DB.ExecContext(ctx, `DELETE FROM auth_users WHERE user_id = $1`, userID)
	return err
}

func (store PostgresStore) UpdateUser(ctx context.Context, userID string, email string, displayName string, now time.Time) (User, error) {
	var user User
	err := store.DB.QueryRowContext(ctx, `
		UPDATE auth_users SET email = $2, display_name = $3, updated_at = $4
		WHERE user_id = $1
		RETURNING user_id, email, display_name, status, system_role, created_at
	`, userID, email, displayName, now).Scan(&user.ID, &user.Email, &user.DisplayName, &user.Status, &user.SystemRole, &user.CreatedAt)
	if isUniqueViolation(err) {
		return User{}, ErrConflict
	}
	return user, err
}

func (store PostgresStore) UpdatePassword(ctx context.Context, userID string, passwordHash string, now time.Time) error {
	result, err := store.DB.ExecContext(ctx, `UPDATE auth_users SET password_hash = $2, updated_at = $3 WHERE user_id = $1`, userID, passwordHash, now)
	return requireAffected(result, err)
}

func (store PostgresStore) RevokeOtherSessions(ctx context.Context, userID string, currentSessionID string, now time.Time) error {
	_, err := store.DB.ExecContext(ctx, `
		UPDATE auth_sessions SET revoked_at = $3
		WHERE user_id = $1 AND session_id <> $2 AND revoked_at IS NULL
	`, userID, currentSessionID, now)
	return err
}

func (store PostgresStore) FindUserByEmail(ctx context.Context, email string) (User, string, error) {
	var user User
	var passwordHash string
	err := store.DB.QueryRowContext(ctx, `
		SELECT user_id, email, display_name, password_hash, status,
		       system_role, created_at
		FROM auth_users
		WHERE LOWER(email) = LOWER($1)
	`, email).Scan(
		&user.ID,
		&user.Email,
		&user.DisplayName,
		&passwordHash,
		&user.Status,
		&user.SystemRole,
		&user.CreatedAt,
	)
	if err == sql.ErrNoRows {
		return User{}, "", ErrNotFound
	}
	return user, passwordHash, err
}

func (store PostgresStore) CreateSession(ctx context.Context, session Session) error {
	_, err := store.DB.ExecContext(ctx, `
		INSERT INTO auth_sessions (
			session_id, user_id, token_hash, expires_at, last_seen_at, created_at
		) VALUES ($1, $2, $3, $4, $5, $5)
	`, session.ID, session.UserID, session.TokenHash, session.ExpiresAt, session.CreatedAt)
	return err
}

func (store PostgresStore) FindSession(
	ctx context.Context,
	sessionID string,
	tokenHash string,
	now time.Time,
) (Session, User, error) {
	var session Session
	var user User
	err := store.DB.QueryRowContext(ctx, `
		UPDATE auth_sessions AS session
		SET last_seen_at = $3
		FROM auth_users AS users
		WHERE session.session_id = $1
		  AND session.token_hash = $2
		  AND session.revoked_at IS NULL
		  AND session.expires_at > $3
		  AND users.user_id = session.user_id
		  AND users.status = 'active'
		RETURNING session.session_id, session.user_id, session.token_hash,
		          session.expires_at, session.created_at,
		          users.user_id, users.email, users.display_name,
		          users.status, users.system_role, users.created_at
	`, sessionID, tokenHash, now).Scan(
		&session.ID,
		&session.UserID,
		&session.TokenHash,
		&session.ExpiresAt,
		&session.CreatedAt,
		&user.ID,
		&user.Email,
		&user.DisplayName,
		&user.Status,
		&user.SystemRole,
		&user.CreatedAt,
	)
	return session, user, err
}

func (store PostgresStore) RevokeSession(ctx context.Context, sessionID string, now time.Time) error {
	result, err := store.DB.ExecContext(ctx, `
		UPDATE auth_sessions SET revoked_at = $2
		WHERE session_id = $1 AND revoked_at IS NULL
	`, sessionID, now)
	return requireAffected(result, err)
}

func (store PostgresStore) CreateToken(ctx context.Context, token Token) error {
	_, err := store.DB.ExecContext(ctx, `
		INSERT INTO auth_tokens (
			token_id, user_id, project_id, kind, name, token_hash, expires_at, created_at
		) VALUES ($1, $2, NULLIF($3, '')::UUID, $4, $5, $6, $7, $8)
	`, token.ID, token.UserID, token.ProjectID, token.Kind, token.Name, token.TokenHash, token.ExpiresAt, token.CreatedAt)
	return err
}

func (store PostgresStore) FindToken(ctx context.Context, tokenHash string, now time.Time) (Token, User, error) {
	var token Token
	var user User
	err := store.DB.QueryRowContext(ctx, `
		SELECT token_id, token.user_id, COALESCE(project_id::TEXT, ''), kind,
		       name, token_hash, expires_at, token.created_at,
		       users.user_id, users.email, users.display_name,
		       users.status, users.system_role, users.created_at
		FROM auth_tokens AS token
		JOIN auth_users AS users ON users.user_id = token.user_id
		WHERE token_hash = $1
		  AND token.revoked_at IS NULL
		  AND (token.expires_at IS NULL OR token.expires_at > $2)
		  AND users.status = 'active'
	`, tokenHash, now).Scan(
		&token.ID,
		&token.UserID,
		&token.ProjectID,
		&token.Kind,
		&token.Name,
		&token.TokenHash,
		&token.ExpiresAt,
		&token.CreatedAt,
		&user.ID,
		&user.Email,
		&user.DisplayName,
		&user.Status,
		&user.SystemRole,
		&user.CreatedAt,
	)
	return token, user, err
}

func (store PostgresStore) ListTokens(ctx context.Context, userID string) ([]Token, error) {
	rows, err := store.DB.QueryContext(ctx, `
		SELECT token_id, user_id, COALESCE(project_id::TEXT, ''), kind,
		       name, expires_at, revoked_at, created_at
		FROM auth_tokens
		WHERE user_id = $1
		ORDER BY created_at DESC
	`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	tokens := []Token{}
	for rows.Next() {
		var token Token
		if err := rows.Scan(
			&token.ID,
			&token.UserID,
			&token.ProjectID,
			&token.Kind,
			&token.Name,
			&token.ExpiresAt,
			&token.RevokedAt,
			&token.CreatedAt,
		); err != nil {
			return nil, err
		}
		tokens = append(tokens, token)
	}
	return tokens, rows.Err()
}

func (store PostgresStore) RevokeToken(
	ctx context.Context,
	userID string,
	tokenID string,
	now time.Time,
) error {
	result, err := store.DB.ExecContext(ctx, `
		UPDATE auth_tokens SET revoked_at = $3
		WHERE token_id = $1 AND user_id = $2 AND revoked_at IS NULL
	`, tokenID, userID, now)
	return requireAffected(result, err)
}

func requireAffected(result sql.Result, err error) error {
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return ErrNotFound
	}
	return nil
}

func wrapStore(operation string, err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%s: %w", operation, err)
}

func isUniqueViolation(err error) bool {
	var postgresError *pgconn.PgError
	return errors.As(err, &postgresError) && postgresError.Code == "23505"
}
