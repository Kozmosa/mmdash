package auth

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgconn"
	"github.com/mmdash/mmdash/backend/internal/platform/outbox"
	"github.com/mmdash/mmdash/backend/internal/platform/transaction"
)

// PostgresStore persists authentication state in module-owned tables.
type PostgresStore struct {
	AgentCredentials AgentCredentialLifecycle
	DB               *sql.DB
	Outbox           outbox.Writer
	Transaction      transaction.Manager
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
			session_id, user_id, token_hash, refresh_token_hash,
			expires_at, last_seen_at, created_at
		) VALUES ($1, $2, $3, $4, $5, $6, $6)
	`, session.ID, session.UserID, session.TokenHash, nullableHash(session.RefreshTokenHash), session.ExpiresAt, session.CreatedAt)
	return err
}

func (store PostgresStore) CreateDeviceAuthorization(ctx context.Context, authorization DeviceAuthorization) error {
	if _, err := store.DB.ExecContext(ctx, `
		DELETE FROM auth_device_authorizations WHERE expires_at <= $1
	`, authorization.CreatedAt); err != nil {
		return err
	}
	_, err := store.DB.ExecContext(ctx, `
		INSERT INTO auth_device_authorizations (
			authorization_id, device_code_hash, user_code_hash, status,
			expires_at, created_at
		) VALUES ($1, $2, $3, $4, $5, $6)
	`, authorization.ID, authorization.DeviceCodeHash, authorization.UserCodeHash, authorization.Status, authorization.ExpiresAt, authorization.CreatedAt)
	return err
}

func (store PostgresStore) DecideDeviceAuthorization(ctx context.Context, userCodeHash string, userID string, approve bool, now time.Time) error {
	status := "denied"
	if approve {
		status = "approved"
	}
	result, err := store.DB.ExecContext(ctx, `
		UPDATE auth_device_authorizations
		SET status = $3, user_id = $2, approved_at = CASE WHEN $3 = 'approved' THEN $4 ELSE NULL END
		WHERE user_code_hash = $1 AND status = 'pending' AND expires_at > $4
	`, userCodeHash, userID, status, now)
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return deviceAuthorizationError(ctx, store.DB, "user_code_hash", userCodeHash, now)
	}
	return nil
}

func (store PostgresStore) ExchangeDeviceAuthorization(
	ctx context.Context,
	deviceCodeHash string,
	now time.Time,
	createSession func(User) (Session, error),
) (Session, User, error) {
	var session Session
	var user User
	err := store.Transaction.Within(ctx, nil, func(tx transaction.Tx) error {
		var userID string
		err := tx.QueryRowContext(ctx, `
			UPDATE auth_device_authorizations
			SET status = 'consumed', consumed_at = $2
			WHERE device_code_hash = $1 AND status = 'approved' AND expires_at > $2
			RETURNING user_id
		`, deviceCodeHash, now).Scan(&userID)
		if err == sql.ErrNoRows {
			return deviceAuthorizationError(ctx, tx, "device_code_hash", deviceCodeHash, now)
		}
		if err != nil {
			return err
		}
		err = tx.QueryRowContext(ctx, `
			SELECT user_id, email, display_name, status, system_role, created_at
			FROM auth_users WHERE user_id = $1 AND status = 'active'
		`, userID).Scan(&user.ID, &user.Email, &user.DisplayName, &user.Status, &user.SystemRole, &user.CreatedAt)
		if err != nil {
			return err
		}
		session, err = createSession(user)
		if err != nil {
			return err
		}
		if session.UserID != userID {
			return fmt.Errorf("device session user does not match approved user")
		}
		_, err = tx.ExecContext(ctx, `
			INSERT INTO auth_sessions (
				session_id, user_id, token_hash, refresh_token_hash,
				expires_at, last_seen_at, created_at
			) VALUES ($1, $2, $3, $4, $5, $6, $6)
		`, session.ID, session.UserID, session.TokenHash, session.RefreshTokenHash, session.ExpiresAt, session.CreatedAt)
		return err
	})
	return session, user, err
}

func (store PostgresStore) RotateSession(ctx context.Context, refreshTokenHash string, tokenHash string, newRefreshTokenHash string, now time.Time) (Session, User, error) {
	var session Session
	var user User
	err := store.DB.QueryRowContext(ctx, `
		UPDATE auth_sessions AS session
		SET token_hash = $2, refresh_token_hash = $3, last_seen_at = $4
		FROM auth_users AS users
		WHERE session.refresh_token_hash = $1
		  AND session.revoked_at IS NULL
		  AND session.expires_at > $4
		  AND users.user_id = session.user_id
		  AND users.status = 'active'
		RETURNING session.session_id, session.user_id, session.token_hash,
		          session.refresh_token_hash, session.expires_at, session.created_at,
		          users.user_id, users.email, users.display_name,
		          users.status, users.system_role, users.created_at
	`, refreshTokenHash, tokenHash, newRefreshTokenHash, now).Scan(
		&session.ID, &session.UserID, &session.TokenHash,
		&session.RefreshTokenHash, &session.ExpiresAt, &session.CreatedAt,
		&user.ID, &user.Email, &user.DisplayName,
		&user.Status, &user.SystemRole, &user.CreatedAt,
	)
	return session, user, err
}

type rowQuerier interface {
	QueryRowContext(context.Context, string, ...interface{}) *sql.Row
}

func deviceAuthorizationError(ctx context.Context, querier rowQuerier, column string, hash string, now time.Time) error {
	query := `SELECT status, expires_at FROM auth_device_authorizations WHERE device_code_hash = $1`
	if column == "user_code_hash" {
		query = `SELECT status, expires_at FROM auth_device_authorizations WHERE user_code_hash = $1`
	}
	var status string
	var expiresAt time.Time
	if err := querier.QueryRowContext(ctx, query, hash).Scan(&status, &expiresAt); err != nil {
		return ErrNotFound
	}
	if !expiresAt.After(now) {
		return ErrAuthorizationExpired
	}
	switch status {
	case "pending":
		return ErrAuthorizationPending
	case "denied":
		return ErrAuthorizationDenied
	default:
		return ErrConflict
	}
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

func (store PostgresStore) FindSessionByRefreshToken(ctx context.Context, refreshTokenHash string, now time.Time) (Session, User, error) {
	var session Session
	var user User
	err := store.DB.QueryRowContext(ctx, `
		SELECT session.session_id, session.user_id, session.token_hash,
		       session.refresh_token_hash, session.expires_at, session.created_at,
		       users.user_id, users.email, users.display_name,
		       users.status, users.system_role, users.created_at
		FROM auth_sessions AS session
		JOIN auth_users AS users ON users.user_id = session.user_id
		WHERE session.refresh_token_hash = $1
		  AND session.revoked_at IS NULL
		  AND session.expires_at > $2
		  AND users.status = 'active'
	`, refreshTokenHash, now).Scan(
		&session.ID, &session.UserID, &session.TokenHash,
		&session.RefreshTokenHash, &session.ExpiresAt, &session.CreatedAt,
		&user.ID, &user.Email, &user.DisplayName,
		&user.Status, &user.SystemRole, &user.CreatedAt,
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

func (store PostgresStore) RevokeManagedToken(
	ctx context.Context,
	tokenID string,
	projectID string,
	kind string,
	now time.Time,
) error {
	result, err := store.DB.ExecContext(ctx, `
		UPDATE auth_tokens SET revoked_at = COALESCE(revoked_at, $4)
		WHERE token_id = $1 AND project_id = $2 AND kind = $3
	`, tokenID, projectID, kind, now)
	return requireAffected(result, err)
}

func (store PostgresStore) CreateAgentToken(ctx context.Context, token AgentToken) error {
	if token.ExpiresAt != nil && !token.ExpiresAt.After(token.CreatedAt) {
		return ErrConflict
	}
	tools, err := json.Marshal(token.AllowedTools)
	if err != nil {
		return err
	}
	err = store.Transaction.Within(ctx, nil, func(tx transaction.Tx) error {
		// Serialize issuance per Grant. The partial unique index is the final
		// concurrent-insert guard; the row lock also lets a new issuance retire
		// an expired pending credential without touching the old active Token.
		var lockedGrantID string
		if err := tx.QueryRowContext(ctx, `
			SELECT grant_id FROM agent_project_grants
			WHERE grant_id=$1 AND agent_instance_id=$2 AND project_id=$3
			FOR UPDATE
		`, token.GrantID, token.AgentInstanceID, token.ProjectID).Scan(&lockedGrantID); err != nil {
			return err
		}
		rows, err := tx.QueryContext(ctx, agentTokenSelect+`
			WHERE grant_id=$1 AND status='pending' AND revoked_at IS NULL
			  AND expires_at IS NOT NULL AND expires_at <= $2
			FOR UPDATE
		`, token.GrantID, token.CreatedAt)
		if err != nil {
			return err
		}
		expired := []AgentToken{}
		for rows.Next() {
			item, scanErr := scanAgentToken(rows.Scan)
			if scanErr != nil {
				_ = rows.Close()
				return scanErr
			}
			expired = append(expired, item)
		}
		if err := rows.Err(); err != nil {
			_ = rows.Close()
			return err
		}
		if err := rows.Close(); err != nil {
			return err
		}
		for _, item := range expired {
			result, err := tx.ExecContext(ctx, `
				UPDATE auth_agent_tokens
				SET status='revoked', revoked_at=$2
				WHERE token_id=$1 AND status='pending' AND revoked_at IS NULL
			`, item.ID, token.CreatedAt)
			if err := requireAffected(result, err); err != nil {
				return err
			}
			if store.AgentCredentials != nil {
				if err := store.AgentCredentials.RevokeAgentCredential(
					ctx, tx, item, token.CreatedAt,
				); err != nil {
					return err
				}
			}
		}
		_, err = tx.ExecContext(ctx, `
			INSERT INTO auth_agent_tokens (
				token_id, agent_instance_id, grant_id, project_id, issued_by,
				name, token_hash, allowed_tools, status, expires_at,
				replaces_token_id, created_at
			) VALUES (
				$1, $2, $3, $4, $5, $6, $7, $8, $9, $10,
				NULLIF($11, '')::uuid, $12
			)
		`, token.ID, token.AgentInstanceID, token.GrantID, token.ProjectID,
			token.IssuedBy, token.Name, token.TokenHash, tools, token.Status,
			token.ExpiresAt, token.ReplacesTokenID, token.CreatedAt)
		return err
	})
	if isUniqueViolation(err) {
		return ErrConflict
	}
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	return err
}

func (store PostgresStore) FindAgentToken(
	ctx context.Context,
	tokenHash string,
	now time.Time,
) (AgentToken, error) {
	return scanAgentToken(store.DB.QueryRowContext(ctx, agentTokenSelect+`
		WHERE token_hash = $1
		  AND status IN ('pending', 'active')
		  AND revoked_at IS NULL
		  AND (expires_at IS NULL OR expires_at > $2)
	`, tokenHash, now).Scan)
}

func (store PostgresStore) GetAgentToken(
	ctx context.Context,
	tokenID string,
) (AgentToken, error) {
	return scanAgentToken(store.DB.QueryRowContext(ctx, agentTokenSelect+`
		WHERE token_id = $1
	`, tokenID).Scan)
}

func (store PostgresStore) ListAgentTokens(
	ctx context.Context,
	grantID string,
) ([]AgentToken, error) {
	rows, err := store.DB.QueryContext(ctx, agentTokenSelect+`
		WHERE grant_id = $1
		ORDER BY created_at DESC, token_id DESC
	`, grantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []AgentToken{}
	for rows.Next() {
		item, scanErr := scanAgentToken(rows.Scan)
		if scanErr != nil {
			return nil, scanErr
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (store PostgresStore) TouchAgentToken(
	ctx context.Context,
	tokenID string,
	now time.Time,
) error {
	_, err := store.DB.ExecContext(ctx, `
		UPDATE auth_agent_tokens
		SET last_used_at = CASE
			WHEN last_used_at IS NULL OR last_used_at < $2 THEN $2
			ELSE last_used_at
		END
		WHERE token_id = $1 AND revoked_at IS NULL
	`, tokenID, now)
	return err
}

func (store PostgresStore) MarkAgentTokenVerified(
	ctx context.Context,
	evidence AgentTokenVerificationEvidence,
) (AgentTokenVerificationEvidence, error) {
	var result AgentTokenVerificationEvidence
	err := store.DB.QueryRowContext(ctx, `
		UPDATE auth_agent_tokens
		SET verification_evidence_id = COALESCE(verification_evidence_id, $4),
			verification_method = COALESCE(verification_method, $5),
			verification_request_id = COALESCE(verification_request_id, $6),
			verification_session_id = COALESCE(verification_session_id, $7),
			verified_by_token_id = COALESCE(verified_by_token_id, $8),
			verified_at = COALESCE(verified_at, $9)
		WHERE token_id = $1 AND agent_instance_id = $2 AND project_id = $3
		  AND status = 'pending' AND revoked_at IS NULL
		  AND (expires_at IS NULL OR expires_at > $9)
		RETURNING verification_evidence_id, token_id, agent_instance_id,
			project_id, verification_method, verification_session_id,
			verification_request_id, verified_at, verified_by_token_id
	`, evidence.TokenID, evidence.AgentInstanceID, evidence.ProjectID,
		evidence.EvidenceID, evidence.MCPMethod, evidence.RequestID,
		evidence.MCPSessionID, evidence.VerifiedByTokenID,
		evidence.VerifiedAt).Scan(
		&result.EvidenceID, &result.TokenID, &result.AgentInstanceID,
		&result.ProjectID, &result.MCPMethod, &result.MCPSessionID,
		&result.RequestID, &result.VerifiedAt, &result.VerifiedByTokenID,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return AgentTokenVerificationEvidence{}, ErrConflict
	}
	return result, err
}

func (store PostgresStore) ActivateAgentToken(
	ctx context.Context,
	tokenID string,
	oldTokenID string,
	newRemoteAccessID string,
	now time.Time,
) (AgentToken, error) {
	var activated AgentToken
	err := store.Transaction.Within(ctx, nil, func(tx transaction.Tx) error {
		var grantID string
		if err := tx.QueryRowContext(ctx, `
			SELECT grant_id FROM auth_agent_tokens WHERE token_id=$1
		`, tokenID).Scan(&grantID); err != nil {
			return err
		}
		// Match CreateAgentToken's Grant-first lock order so a verification
		// activation cannot deadlock with concurrent rotation issuance.
		if err := tx.QueryRowContext(ctx, `
			SELECT grant_id FROM agent_project_grants WHERE grant_id=$1 FOR UPDATE
		`, grantID).Scan(&grantID); err != nil {
			return err
		}
		updated, err := scanAgentToken(tx.QueryRowContext(ctx, agentTokenSelect+`
			WHERE token_id = $1
			FOR UPDATE
		`, tokenID).Scan)
		if err != nil {
			return err
		}
		if updated.Status != "pending" || updated.Verification == nil ||
			updated.Verification.VerifiedAt.Before(updated.CreatedAt) ||
			updated.Verification.MCPMethod != AgentTokenVerificationMethod ||
			(updated.ExpiresAt != nil && !updated.ExpiresAt.After(now)) {
			return ErrConflict
		}
		if oldTokenID != "" {
			result, err := tx.ExecContext(ctx, `
				UPDATE auth_agent_tokens
				SET status = 'revoked', revoked_at = $3
				WHERE token_id = $1 AND grant_id = $2
				  AND status = 'active' AND revoked_at IS NULL
			`, oldTokenID, updated.GrantID, now)
			if err != nil {
				return err
			}
			affected, err := result.RowsAffected()
			if err != nil {
				return err
			}
			if affected != 1 {
				return ErrConflict
			}
		}
		result, err := tx.ExecContext(ctx, `
			UPDATE auth_agent_tokens
			SET status = 'active', activated_at = $2
			WHERE token_id = $1 AND status = 'pending' AND revoked_at IS NULL
			  AND (expires_at IS NULL OR expires_at > $2)
		`, tokenID, now)
		if err != nil {
			return err
		}
		affected, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if affected != 1 {
			return ErrConflict
		}
		updated.Status = "active"
		updated.ActivatedAt = &now
		if store.AgentCredentials != nil {
			if err := store.AgentCredentials.ActivateAgentCredential(
				ctx, tx, updated, oldTokenID, newRemoteAccessID, now,
			); err != nil {
				return err
			}
		}
		activated = updated
		return nil
	})
	if errors.Is(err, sql.ErrNoRows) {
		return AgentToken{}, ErrNotFound
	}
	return activated, err
}

func (store PostgresStore) RevokeAgentToken(
	ctx context.Context,
	tokenID string,
	now time.Time,
) error {
	return store.Transaction.Within(ctx, nil, func(tx transaction.Tx) error {
		token, err := scanAgentToken(tx.QueryRowContext(ctx, agentTokenSelect+`
			WHERE token_id = $1 FOR UPDATE
		`, tokenID).Scan)
		if err != nil {
			return err
		}
		result, err := tx.ExecContext(ctx, `
			UPDATE auth_agent_tokens
			SET status = 'revoked', revoked_at = $2
			WHERE token_id = $1 AND revoked_at IS NULL
		`, tokenID, now)
		if err := requireAffected(result, err); err != nil {
			return err
		}
		token.Status = "revoked"
		token.RevokedAt = &now
		if store.AgentCredentials != nil {
			return store.AgentCredentials.RevokeAgentCredential(ctx, tx, token, now)
		}
		return nil
	})
}

const agentTokenSelect = `
	SELECT token_id, agent_instance_id, grant_id, project_id, issued_by,
	       name, token_hash, allowed_tools, status, expires_at, activated_at,
	       last_used_at, revoked_at, COALESCE(replaces_token_id::text, ''),
	       COALESCE(verification_evidence_id::text, ''),
	       COALESCE(verification_method, ''),
	       COALESCE(verification_request_id, ''),
	       COALESCE(verification_session_id, ''),
	       COALESCE(verified_by_token_id::text, ''), verified_at,
	       created_at
	FROM auth_agent_tokens
`

type agentTokenScan func(...interface{}) error

func scanAgentToken(scan agentTokenScan) (AgentToken, error) {
	var token AgentToken
	var tools []byte
	var verification AgentTokenVerificationEvidence
	var verifiedAt *time.Time
	err := scan(
		&token.ID, &token.AgentInstanceID, &token.GrantID, &token.ProjectID,
		&token.IssuedBy, &token.Name, &token.TokenHash, &tools, &token.Status,
		&token.ExpiresAt, &token.ActivatedAt, &token.LastUsedAt, &token.RevokedAt,
		&token.ReplacesTokenID, &verification.EvidenceID,
		&verification.MCPMethod, &verification.RequestID,
		&verification.MCPSessionID, &verification.VerifiedByTokenID,
		&verifiedAt, &token.CreatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return AgentToken{}, ErrNotFound
	}
	if err != nil {
		return AgentToken{}, err
	}
	if err := json.Unmarshal(tools, &token.AllowedTools); err != nil {
		return AgentToken{}, fmt.Errorf("decode agent token tools: %w", err)
	}
	if verifiedAt != nil {
		verification.AgentInstanceID = token.AgentInstanceID
		verification.ProjectID = token.ProjectID
		verification.TokenID = token.ID
		verification.VerifiedAt = *verifiedAt
		token.Verification = &verification
	}
	return token, nil
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

func nullableHash(value string) interface{} {
	if value == "" {
		return nil
	}
	return value
}
