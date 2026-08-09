package settings

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/mmdash/mmdash/backend/internal/platform/clock"
	"github.com/mmdash/mmdash/backend/internal/platform/identity"
	"github.com/mmdash/mmdash/backend/internal/platform/outbox"
	"github.com/mmdash/mmdash/backend/internal/platform/transaction"
)

// PostgresStore persists encrypted settings and emits secret-free outbox events.
type PostgresStore struct {
	Clock       clock.Clock
	DB          *sql.DB
	Generator   identity.Generator
	Outbox      outbox.Writer
	Transaction transaction.Manager
}

func (store PostgresStore) Get(
	ctx context.Context,
	scope Scope,
	scopeID string,
	typeKey string,
) (StoredSetting, error) {
	return scanStored(store.DB.QueryRowContext(ctx, `
		SELECT scope_type, scope_id, type_key, public_values,
		       encrypted_secrets, version, updated_by, created_at, updated_at
		FROM settings_values
		WHERE scope_type = $1 AND scope_id = $2 AND type_key = $3
	`, scope, scopeID, typeKey).Scan)
}

func (store PostgresStore) Upsert(
	ctx context.Context,
	actorID string,
	setting StoredSetting,
) (StoredSetting, error) {
	settingID, err := store.Generator.New()
	if err != nil {
		return StoredSetting{}, err
	}
	publicValues, err := json.Marshal(setting.PublicValues)
	if err != nil {
		return StoredSetting{}, err
	}
	encryptedSecrets, err := json.Marshal(setting.EncryptedSecrets)
	if err != nil {
		return StoredSetting{}, err
	}
	now := store.Clock.Now().UTC()
	var updated StoredSetting
	err = store.Transaction.Within(ctx, nil, func(tx transaction.Tx) error {
		updated, err = scanStored(tx.QueryRowContext(ctx, `
			INSERT INTO settings_values (
				setting_id, scope_type, scope_id, type_key,
				public_values, encrypted_secrets, version,
				updated_by, created_at, updated_at
			) VALUES ($1, $2, $3, $4, $5, $6, 1, $7, $8, $8)
			ON CONFLICT (scope_type, scope_id, type_key)
			DO UPDATE SET
				public_values = EXCLUDED.public_values,
				encrypted_secrets = EXCLUDED.encrypted_secrets,
				version = settings_values.version + 1,
				updated_by = EXCLUDED.updated_by,
				updated_at = EXCLUDED.updated_at
			RETURNING scope_type, scope_id, type_key, public_values,
			          encrypted_secrets, version, updated_by, created_at, updated_at
		`, settingID, setting.Scope, setting.ScopeID, setting.TypeKey,
			publicValues, encryptedSecrets, actorID, now).Scan)
		if err != nil {
			return err
		}
		_, err = store.Outbox.Write(ctx, tx, outbox.Event{
			Actor:     map[string]string{"user_id": actorID},
			EventType: "settings.updated",
			Payload: map[string]interface{}{
				"scope":    setting.Scope,
				"scope_id": setting.ScopeID,
				"type_key": setting.TypeKey,
				"version":  updated.Version,
			},
			Producer:  "settings",
			ProjectID: projectID(setting.Scope, setting.ScopeID),
		})
		return err
	})
	if err != nil {
		return StoredSetting{}, fmt.Errorf("upsert setting: %w", err)
	}
	return updated, nil
}

func (store PostgresStore) RotateSecrets(
	ctx context.Context,
	actorID string,
	setting StoredSetting,
) (StoredSetting, error) {
	encryptedSecrets, err := json.Marshal(setting.EncryptedSecrets)
	if err != nil {
		return StoredSetting{}, err
	}
	now := store.Clock.Now().UTC()
	updated, err := scanStored(store.DB.QueryRowContext(ctx, `
		UPDATE settings_values
		SET encrypted_secrets=$4,version=version+1,updated_by=$5,updated_at=$6
		WHERE scope_type=$1 AND scope_id=$2 AND type_key=$3
		RETURNING scope_type,scope_id,type_key,public_values,
		          encrypted_secrets,version,updated_by,created_at,updated_at
	`, setting.Scope, setting.ScopeID, setting.TypeKey, encryptedSecrets, actorID, now).Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return StoredSetting{}, ErrNotFound
	}
	return updated, err
}

func (store PostgresStore) Delete(
	ctx context.Context,
	scope Scope,
	scopeID string,
	typeKey string,
	actorID string,
) error {
	return store.Transaction.Within(ctx, nil, func(tx transaction.Tx) error {
		result, err := tx.ExecContext(ctx, `
			DELETE FROM settings_values
			WHERE scope_type = $1 AND scope_id = $2 AND type_key = $3
		`, scope, scopeID, typeKey)
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
		_, err = store.Outbox.Write(ctx, tx, outbox.Event{
			Actor:     map[string]string{"user_id": actorID},
			EventType: "settings.deleted",
			Payload: map[string]interface{}{
				"scope":    scope,
				"scope_id": scopeID,
				"type_key": typeKey,
			},
			Producer:  "settings",
			ProjectID: projectID(scope, scopeID),
		})
		return err
	})
}

type settingScan func(...interface{}) error

func scanStored(scan settingScan) (StoredSetting, error) {
	var setting StoredSetting
	var publicValues []byte
	var encryptedSecrets []byte
	err := scan(
		&setting.Scope,
		&setting.ScopeID,
		&setting.TypeKey,
		&publicValues,
		&encryptedSecrets,
		&setting.Version,
		&setting.UpdatedBy,
		&setting.CreatedAt,
		&setting.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return StoredSetting{}, ErrNotFound
	}
	if err != nil {
		return StoredSetting{}, err
	}
	if err := json.Unmarshal(publicValues, &setting.PublicValues); err != nil {
		return StoredSetting{}, fmt.Errorf("decode setting values: %w", err)
	}
	if err := json.Unmarshal(encryptedSecrets, &setting.EncryptedSecrets); err != nil {
		return StoredSetting{}, fmt.Errorf("decode encrypted settings: %w", err)
	}
	return setting, nil
}

func projectID(scope Scope, scopeID string) string {
	if scope == ScopeProject {
		return scopeID
	}
	return ""
}
