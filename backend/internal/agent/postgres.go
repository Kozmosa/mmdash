package agent

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgconn"
	"github.com/mmdash/mmdash/backend/internal/audit"
	"github.com/mmdash/mmdash/backend/internal/auth"
	"github.com/mmdash/mmdash/backend/internal/platform/clock"
	"github.com/mmdash/mmdash/backend/internal/platform/identity"
	"github.com/mmdash/mmdash/backend/internal/platform/outbox"
	"github.com/mmdash/mmdash/backend/internal/platform/transaction"
	"github.com/mmdash/mmdash/backend/internal/project"
)

// Store is the Agent domain persistence boundary. Runtime message bodies and
// complete Tool output deliberately do not cross this boundary.
type Store interface {
	CreateInstance(context.Context, string, string, Instance, ProjectGrant) (Instance, error)
	DisableInstance(context.Context, string, string, string, string, time.Time) error
	GetGrant(context.Context, string, string) (ProjectGrant, error)
	GetInstance(context.Context, string, string) (Instance, error)
	ListInstances(context.Context, string) ([]Instance, error)
	SaveChecks(context.Context, string, CheckSnapshot, CheckSnapshot, CheckSnapshot, string, map[string]interface{}, string, time.Time) error
	SetProjectAccess(context.Context, string, string, time.Time) error
	UpdateInstance(context.Context, string, string, Instance, time.Time) (Instance, error)

	GetPromptOverride(context.Context, string) (string, time.Time, int64, error)
	ResetPrompt(context.Context, string, string, time.Time) error
	UpdatePrompt(context.Context, string, string, string, time.Time) error

	CreateSession(context.Context, string, SessionRecord, string) (SessionRecord, error)
	GetSession(context.Context, string, string, string) (SessionRecord, error)
	ListSessions(context.Context, string, string) ([]SessionRecord, error)
	SetDefaultSession(context.Context, string, string, string, string, time.Time) error
	UpdateSession(context.Context, string, SessionRecord, string, time.Time) (SessionRecord, error)

	ActivateRun(context.Context, string, RunRecord, time.Time) (RunRecord, error)
	ApplyNextRunApprovalResponse(context.Context, string, time.Time) (RunRecord, string, error)
	ApplyRunApprovalResponse(context.Context, string, string, time.Time) (RunRecord, error)
	ClaimRunApproval(context.Context, string, string, string, time.Time) (RunRecord, error)
	CompleteRunApproval(context.Context, string, string, string, time.Time) (RunRecord, error)
	FailRunReservation(context.Context, string, string, string, time.Time) error
	GetRun(context.Context, string, string) (RunRecord, error)
	RecordRunApproval(context.Context, string, string, time.Time) (RunRecord, error)
	ReleaseRunApprovalClaim(context.Context, string, string, string, time.Time) (RunRecord, error)
	ReserveRun(context.Context, RunRecord) (RunRecord, error)
	UpdateRun(context.Context, string, string, string, time.Time) (RunRecord, error)
	UpsertToolCall(context.Context, ToolCallRecord) (ToolCallRecord, error)
	ValidateProvenance(context.Context, string, string, string, string) error

	CreateRotation(context.Context, string, TokenRotation) (TokenRotation, error)
	FindRotationByToken(context.Context, string, string) (TokenRotation, error)
	UpdateRotation(context.Context, string, string, string, time.Time) (TokenRotation, error)
}

type PostgresStore struct {
	Audit       audit.Recorder
	Clock       clock.Clock
	DB          *sql.DB
	Generator   identity.Generator
	Outbox      outbox.Writer
	Transaction transaction.Manager
}

func (store PostgresStore) ActivateAgentCredential(
	ctx context.Context,
	tx transaction.Tx,
	token auth.AgentToken,
	oldTokenID string,
	newRemoteAccessID string,
	now time.Time,
) error {
	tools, _ := json.Marshal(nonNilStrings(token.AllowedTools))
	result, err := tx.ExecContext(ctx, `
		UPDATE agent_project_grants
		SET allowed_tools=$2,
			remote_access_id=COALESCE(NULLIF($6,''), remote_access_id),
			updated_at=$3, version=version+1
		WHERE grant_id=$1 AND agent_instance_id=$4 AND project_id=$5
		  AND status='active'
	`, token.GrantID, tools, now, token.AgentInstanceID, token.ProjectID,
		strings.TrimSpace(newRemoteAccessID))
	if err := requireAffected(result, err); err != nil {
		return err
	}
	var rotationID string
	err = tx.QueryRowContext(ctx, `
		UPDATE agent_token_rotations
		SET status='completed', safe_error_code=NULL, completed_at=$2,
			updated_at=$2
		WHERE new_token_id=$1 AND status IN (
			'pending','awaiting_user','configuring','verifying'
		)
		RETURNING rotation_id
	`, token.ID, now).Scan(&rotationID)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrConflict
	}
	if err != nil {
		return err
	}
	payload := map[string]interface{}{
		"rotation_id": rotationID,
		"status":      "active",
	}
	if oldTokenID != "" {
		payload["replaces_token_id"] = oldTokenID
	}
	if err := store.event(ctx, tx, token.IssuedBy, "session", token.ProjectID,
		"agent.token.activated", token.ID, payload); err != nil {
		return err
	}
	if err := store.record(ctx, tx, token.IssuedBy, "session", token.ProjectID,
		"agent.token.activate", "agent-token", token.ID, "success", ""); err != nil {
		return err
	}
	if oldTokenID != "" {
		if err := store.event(ctx, tx, token.IssuedBy, "session", token.ProjectID,
			"agent.token.revoked", oldTokenID,
			map[string]interface{}{"status": "revoked"}); err != nil {
			return err
		}
	}
	return nil
}

func (store PostgresStore) RevokeAgentCredential(
	ctx context.Context,
	tx transaction.Tx,
	token auth.AgentToken,
	now time.Time,
) error {
	if err := store.event(ctx, tx, token.IssuedBy, "session", token.ProjectID,
		"agent.token.revoked", token.ID,
		map[string]interface{}{"status": "revoked"}); err != nil {
		return err
	}
	return store.record(ctx, tx, token.IssuedBy, "session", token.ProjectID,
		"agent.token.revoke", "agent-token", token.ID, "success", "")
}

func (store PostgresStore) ResolveAgentRole(
	ctx context.Context,
	agentInstanceID string,
	projectID string,
) (project.Role, error) {
	var role project.Role
	err := store.DB.QueryRowContext(ctx, `
		SELECT grant_row.role
		FROM agent_project_grants AS grant_row
		JOIN agent_instances AS instance USING (agent_instance_id)
		WHERE grant_row.agent_instance_id = $1 AND grant_row.project_id = $2
		  AND grant_row.status = 'active' AND instance.status <> 'disabled'
	`, agentInstanceID, projectID).Scan(&role)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrForbidden
	}
	return role, err
}

func (store PostgresStore) ValidateProvenance(
	ctx context.Context,
	agentInstanceID string,
	projectID string,
	sessionID string,
	runID string,
) error {
	var exists bool
	err := store.DB.QueryRowContext(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM agent_sessions AS session
			JOIN agent_runs AS run ON run.session_id = session.session_id
			JOIN agent_project_grants AS grant_row ON grant_row.grant_id = session.grant_id
			JOIN agent_instances AS instance
			  ON instance.agent_instance_id = session.agent_instance_id
			WHERE session.session_id=$1 AND run.run_id=$2
			  AND session.project_id=$3 AND session.agent_instance_id=$4
			  AND grant_row.status='active' AND instance.status <> 'disabled'
		)
	`, sessionID, runID, projectID, agentInstanceID).Scan(&exists)
	if err != nil {
		return err
	}
	if !exists {
		return ErrForbidden
	}
	return nil
}

func (store PostgresStore) CreateInstance(
	ctx context.Context,
	actorID string,
	actorKind string,
	instance Instance,
	grant ProjectGrant,
) (Instance, error) {
	capabilities, _ := json.Marshal(nonNilMap(instance.Capabilities))
	runtimeCheck, _ := json.Marshal(instance.RuntimeCheck)
	managementCheck, _ := json.Marshal(instance.ManagementCheck)
	projectAccessCheck, _ := json.Marshal(instance.ProjectAccessCheck)
	tools, _ := json.Marshal(nonNilStrings(grant.AllowedTools))
	err := store.Transaction.Within(ctx, nil, func(tx transaction.Tx) error {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO agent_instances (
				agent_instance_id, adapter_type, display_name, management_mode,
				runtime_url, dashboard_url, status, management_path,
				capabilities, runtime_check, management_check, project_access_check,
				created_by, created_at, updated_at, version
			) VALUES (
				$1,$2,$3,$4,$5,NULLIF($6,''),$7,$8,$9,$10,$11,$12,$13,$14,$14,1
			)
		`, instance.ID, instance.AdapterType, instance.DisplayName,
			instance.ManagementMode, instance.RuntimeURL, instance.DashboardURL,
			instance.Status, instance.ManagementPath, capabilities, runtimeCheck,
			managementCheck, projectAccessCheck, instance.CreatedBy,
			instance.CreatedAt); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO agent_project_grants (
				grant_id, agent_instance_id, project_id, role, status,
				allowed_tools, created_by, created_at, updated_at, version
			) VALUES ($1,$2,$3,'agent','active',$4,$5,$6,$6,1)
		`, grant.GrantID, grant.AgentInstanceID, grant.ProjectID, tools,
			grant.CreatedBy, grant.CreatedAt); err != nil {
			return err
		}
		if err := store.event(ctx, tx, actorID, actorKind, grant.ProjectID,
			"agent.instance.created", instance.ID, map[string]interface{}{
				"adapter_type":    instance.AdapterType,
				"management_mode": instance.ManagementMode,
				"status":          instance.Status,
			}); err != nil {
			return err
		}
		return store.record(ctx, tx, actorID, actorKind, grant.ProjectID,
			"agent.instance.create", "agent-instance", instance.ID, "success", "")
	})
	if isUniqueViolation(err) {
		return Instance{}, ErrConflict
	}
	if err != nil {
		return Instance{}, fmt.Errorf("create agent instance: %w", err)
	}
	instance.Grant = &grant
	return instance, nil
}

func (store PostgresStore) ListInstances(ctx context.Context, projectID string) ([]Instance, error) {
	rows, err := store.DB.QueryContext(ctx, instanceSelect+`
		WHERE grant_row.project_id = $1
		ORDER BY instance.created_at, instance.agent_instance_id
	`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []Instance{}
	for rows.Next() {
		item, scanErr := scanInstance(rows.Scan)
		if scanErr != nil {
			return nil, scanErr
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (store PostgresStore) GetInstance(ctx context.Context, projectID, instanceID string) (Instance, error) {
	item, err := scanInstance(store.DB.QueryRowContext(ctx, instanceSelect+`
		WHERE grant_row.project_id = $1 AND instance.agent_instance_id = $2
	`, projectID, instanceID).Scan)
	return item, mapNotFound(err)
}

func (store PostgresStore) GetGrant(ctx context.Context, projectID, instanceID string) (ProjectGrant, error) {
	grant, err := scanGrant(store.DB.QueryRowContext(ctx, grantSelect+`
		WHERE project_id = $1 AND agent_instance_id = $2
	`, projectID, instanceID).Scan)
	return grant, mapNotFound(err)
}

func (store PostgresStore) UpdateInstance(
	ctx context.Context,
	actorID string,
	actorKind string,
	instance Instance,
	now time.Time,
) (Instance, error) {
	capabilities, _ := json.Marshal(nonNilMap(instance.Capabilities))
	err := store.Transaction.Within(ctx, nil, func(tx transaction.Tx) error {
		updated, err := scanInstance(tx.QueryRowContext(ctx, instanceSelect+`
			WHERE grant_row.project_id = $1 AND instance.agent_instance_id = $2
			FOR UPDATE OF instance
		`, instance.Grant.ProjectID, instance.ID).Scan)
		if err != nil {
			return mapNotFound(err)
		}
		if updated.Status == InstanceDisabled {
			return ErrConflict
		}
		if _, err := tx.ExecContext(ctx, `
			UPDATE agent_instances SET
				display_name=$3, management_mode=$4, runtime_url=$5,
				dashboard_url=NULLIF($6,''), status=$7, management_path=$8,
				capabilities=$9, updated_at=$10, version=version+1
			WHERE agent_instance_id=$1 AND EXISTS (
				SELECT 1 FROM agent_project_grants
				WHERE agent_instance_id=$1 AND project_id=$2
			)
		`, instance.ID, instance.Grant.ProjectID, instance.DisplayName,
			instance.ManagementMode, instance.RuntimeURL, instance.DashboardURL,
			instance.Status, instance.ManagementPath, capabilities, now); err != nil {
			return err
		}
		if err := store.event(ctx, tx, actorID, actorKind, instance.Grant.ProjectID,
			"agent.instance.updated", instance.ID, map[string]interface{}{
				"management_mode": instance.ManagementMode, "status": instance.Status,
			}); err != nil {
			return err
		}
		return store.record(ctx, tx, actorID, actorKind, instance.Grant.ProjectID,
			"agent.instance.update", "agent-instance", instance.ID, "success", "")
	})
	if err != nil {
		return Instance{}, err
	}
	return store.GetInstance(ctx, instance.Grant.ProjectID, instance.ID)
}

func (store PostgresStore) DisableInstance(
	ctx context.Context,
	actorID string,
	actorKind string,
	projectID string,
	instanceID string,
	now time.Time,
) error {
	return store.Transaction.Within(ctx, nil, func(tx transaction.Tx) error {
		result, err := tx.ExecContext(ctx, `
			UPDATE agent_instances
			SET status='disabled', disabled_at=$3, updated_at=$3, version=version+1
			WHERE agent_instance_id=$2 AND EXISTS (
				SELECT 1 FROM agent_project_grants
				WHERE project_id=$1 AND agent_instance_id=$2
			) AND status <> 'disabled'
		`, projectID, instanceID, now)
		if err := requireAffected(result, err); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `
			UPDATE agent_project_grants
			SET status='revoked', revoked_at=$3, updated_at=$3, version=version+1
			WHERE project_id=$1 AND agent_instance_id=$2 AND status='active'
		`, projectID, instanceID, now); err != nil {
			return err
		}
		if err := store.event(ctx, tx, actorID, actorKind, projectID,
			"agent.instance.revoked", instanceID, map[string]interface{}{
				"status": InstanceDisabled,
			}); err != nil {
			return err
		}
		return store.record(ctx, tx, actorID, actorKind, projectID,
			"agent.instance.disable", "agent-instance", instanceID, "success", "")
	})
}

func (store PostgresStore) SaveChecks(
	ctx context.Context,
	instanceID string,
	runtimeCheck CheckSnapshot,
	managementCheck CheckSnapshot,
	projectAccessCheck CheckSnapshot,
	managementPath string,
	capabilities map[string]interface{},
	status string,
	now time.Time,
) error {
	runtimeJSON, _ := json.Marshal(runtimeCheck)
	managementJSON, _ := json.Marshal(managementCheck)
	accessJSON, _ := json.Marshal(projectAccessCheck)
	capabilityJSON, _ := json.Marshal(nonNilMap(capabilities))
	result, err := store.DB.ExecContext(ctx, `
		UPDATE agent_instances SET runtime_check=$2, management_check=$3,
			project_access_check=$4, management_path=$5, capabilities=$6,
			status=$7, updated_at=$8, version=version+1
		WHERE agent_instance_id=$1 AND status <> 'disabled'
	`, instanceID, runtimeJSON, managementJSON, accessJSON, managementPath,
		capabilityJSON, status, now)
	return requireAffected(result, err)
}

func (store PostgresStore) SetProjectAccess(
	ctx context.Context,
	grantID string,
	remoteAccessID string,
	now time.Time,
) error {
	result, err := store.DB.ExecContext(ctx, `
		UPDATE agent_project_grants
		SET remote_access_id=NULLIF($2,''), last_access_at=$3,
			updated_at=$3, version=version+1
		WHERE grant_id=$1 AND status='active'
	`, grantID, remoteAccessID, now)
	return requireAffected(result, err)
}

func (store PostgresStore) GetPromptOverride(ctx context.Context, grantID string) (string, time.Time, int64, error) {
	var override sql.NullString
	var updated time.Time
	var version int64
	err := store.DB.QueryRowContext(ctx, `
		SELECT prompt_override, updated_at, version
		FROM agent_project_grants WHERE grant_id=$1 AND status='active'
	`, grantID).Scan(&override, &updated, &version)
	if errors.Is(err, sql.ErrNoRows) {
		return "", time.Time{}, 0, ErrNotFound
	}
	return override.String, updated, version, err
}

func (store PostgresStore) UpdatePrompt(ctx context.Context, actorID, grantID, override string, now time.Time) error {
	return store.updatePrompt(ctx, actorID, grantID, &override, "agent.prompt.updated", now)
}

func (store PostgresStore) ResetPrompt(ctx context.Context, actorID, grantID string, now time.Time) error {
	return store.updatePrompt(ctx, actorID, grantID, nil, "agent.prompt.reset", now)
}

func (store PostgresStore) updatePrompt(
	ctx context.Context,
	actorID string,
	grantID string,
	override *string,
	eventType string,
	now time.Time,
) error {
	return store.Transaction.Within(ctx, nil, func(tx transaction.Tx) error {
		var projectID string
		var instanceID string
		err := tx.QueryRowContext(ctx, `
			UPDATE agent_project_grants
			SET prompt_override=$2, updated_at=$3, version=version+1
			WHERE grant_id=$1 AND status='active'
			RETURNING project_id, agent_instance_id
		`, grantID, override, now).Scan(&projectID, &instanceID)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		if err := store.event(ctx, tx, actorID, "session", projectID,
			eventType, instanceID, map[string]interface{}{"version_changed": true}); err != nil {
			return err
		}
		action := "agent.prompt.update"
		if eventType == "agent.prompt.reset" {
			action = "agent.prompt.reset"
		}
		return store.record(ctx, tx, actorID, "session", projectID,
			action, "agent-prompt", instanceID,
			"success", "")
	})
}

func (store PostgresStore) CreateSession(
	ctx context.Context,
	actorID string,
	item SessionRecord,
	eventType string,
) (SessionRecord, error) {
	if eventType == "" {
		eventType = "agent.session.created"
	}
	err := store.Transaction.Within(ctx, nil, func(tx transaction.Tx) error {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO agent_sessions (
				session_id, grant_id, agent_instance_id, project_id,
				remote_session_id, session_type, title, status,
				parent_session_id, created_by, created_at, updated_at, version
			) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,NULLIF($9,'')::uuid,$10,$11,$11,1)
		`, item.ID, item.GrantID, item.AgentInstanceID, item.ProjectID,
			item.RemoteSessionID, item.SessionType, item.Title, item.Status,
			item.ParentSessionID, item.CreatedBy, item.CreatedAt); err != nil {
			return err
		}
		payload := map[string]interface{}{
			"agent_instance_id": item.AgentInstanceID,
			"session_type":      item.SessionType,
		}
		if item.ParentSessionID != "" {
			payload["parent_session_id"] = item.ParentSessionID
		}
		if err := store.event(ctx, tx, actorID, "session", item.ProjectID,
			eventType, item.ID, payload); err != nil {
			return err
		}
		action := "agent.session.create"
		if eventType == "agent.session.forked" {
			action = "agent.session.fork"
		}
		return store.record(ctx, tx, actorID, "session", item.ProjectID,
			action, "agent-session", item.ID, "success", "")
	})
	if isUniqueViolation(err) {
		return SessionRecord{}, ErrConflict
	}
	return item, err
}

func (store PostgresStore) ListSessions(ctx context.Context, projectID, instanceID string) ([]SessionRecord, error) {
	rows, err := store.DB.QueryContext(ctx, sessionSelect+`
		WHERE project_id=$1 AND agent_instance_id=$2
		ORDER BY updated_at DESC, session_id DESC
	`, projectID, instanceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []SessionRecord{}
	for rows.Next() {
		item, scanErr := scanSession(rows.Scan)
		if scanErr != nil {
			return nil, scanErr
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (store PostgresStore) GetSession(ctx context.Context, projectID, instanceID, sessionID string) (SessionRecord, error) {
	item, err := scanSession(store.DB.QueryRowContext(ctx, sessionSelect+`
		WHERE project_id=$1 AND agent_instance_id=$2 AND session_id=$3
	`, projectID, instanceID, sessionID).Scan)
	return item, mapNotFound(err)
}

func (store PostgresStore) UpdateSession(
	ctx context.Context,
	actorID string,
	item SessionRecord,
	eventType string,
	now time.Time,
) (SessionRecord, error) {
	err := store.Transaction.Within(ctx, nil, func(tx transaction.Tx) error {
		result, err := tx.ExecContext(ctx, `
			UPDATE agent_sessions SET title=$2, status=$3, end_reason=NULLIF($4,''),
				ended_at=$5, last_message_at=$6, last_run_at=$7,
				updated_at=$8, version=version+1
			WHERE session_id=$1
		`, item.ID, item.Title, item.Status, item.EndReason, item.EndedAt,
			item.LastMessageAt, item.LastRunAt, now)
		if err := requireAffected(result, err); err != nil {
			return err
		}
		if eventType != "" {
			if err := store.event(ctx, tx, actorID, "session", item.ProjectID,
				eventType, item.ID, map[string]interface{}{
					"status": item.Status, "session_type": item.SessionType,
				}); err != nil {
				return err
			}
			if err := store.record(ctx, tx, actorID, "session", item.ProjectID,
				sessionAuditAction(eventType), "agent-session", item.ID,
				"success", ""); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return SessionRecord{}, err
	}
	return store.GetSession(ctx, item.ProjectID, item.AgentInstanceID, item.ID)
}

func (store PostgresStore) SetDefaultSession(
	ctx context.Context,
	actorID string,
	projectID string,
	instanceID string,
	sessionID string,
	now time.Time,
) error {
	return store.Transaction.Within(ctx, nil, func(tx transaction.Tx) error {
		result, err := tx.ExecContext(ctx, `
			UPDATE agent_project_grants AS grant_row
			SET default_session_id=$3, updated_at=$4, version=version+1
			WHERE project_id=$1 AND agent_instance_id=$2 AND status='active'
			  AND EXISTS (
				SELECT 1 FROM agent_sessions
				WHERE session_id=$3 AND grant_id=grant_row.grant_id
			  )
		`, projectID, instanceID, sessionID, now)
		if err := requireAffected(result, err); err != nil {
			return err
		}
		if err := store.event(ctx, tx, actorID, "session", projectID,
			"agent.session.default_changed", sessionID,
			map[string]interface{}{"agent_instance_id": instanceID}); err != nil {
			return err
		}
		return store.record(ctx, tx, actorID, "session", projectID,
			"agent.session.default", "agent-session", sessionID,
			"success", "")
	})
}

func (store PostgresStore) ReserveRun(ctx context.Context, item RunRecord) (RunRecord, error) {
	_, err := store.DB.ExecContext(ctx, `
		INSERT INTO agent_runs (
			run_id, session_id, remote_run_id, status, source,
			source_run_id, source_evaluation_id, created_by, created_at,
			started_at, updated_at, version
		) VALUES ($1,$2,$3,'queued',$4,NULLIF($5,'')::uuid,
			NULLIF($6,'')::uuid,$7,$8,NULL,$8,1)
	`, item.ID, item.SessionID, item.RemoteRunID, item.Source,
		item.SourceRunID, item.SourceEvaluationID, item.CreatedBy, item.CreatedAt)
	if isUniqueViolation(err) {
		return RunRecord{}, ErrConflict
	}
	if err != nil {
		return RunRecord{}, err
	}
	item.Status = RunRecordQueued
	item.StartedAt = nil
	item.ToolCalls = []ToolCallRecord{}
	return item, nil
}

func (store PostgresStore) ActivateRun(
	ctx context.Context,
	actorID string,
	item RunRecord,
	now time.Time,
) (RunRecord, error) {
	err := store.Transaction.Within(ctx, nil, func(tx transaction.Tx) error {
		result, err := tx.ExecContext(ctx, `
			UPDATE agent_runs SET remote_run_id=$2, status=$3, started_at=$4,
				updated_at=$4, version=version+1
			WHERE run_id=$1 AND status='queued'
		`, item.ID, item.RemoteRunID, item.Status, now)
		if err := requireAffected(result, err); err != nil {
			return err
		}
		var projectID string
		if err := tx.QueryRowContext(ctx, `
			UPDATE agent_sessions
			SET last_run_at=$2, last_message_at=$2, updated_at=$2, version=version+1
			WHERE session_id=$1 RETURNING project_id
		`, item.SessionID, now).Scan(&projectID); err != nil {
			return err
		}
		payload := map[string]interface{}{
			"session_id": item.SessionID, "source": item.Source,
		}
		if item.SourceRunID != "" {
			payload["source_run_id"] = item.SourceRunID
		}
		if item.SourceEvaluationID != "" {
			payload["source_evaluation_id"] = item.SourceEvaluationID
		}
		if err := store.event(ctx, tx, actorID, "session", projectID,
			"agent.run.started", item.ID, payload); err != nil {
			return err
		}
		return store.record(ctx, tx, actorID, "session", projectID,
			runStartAuditAction(item.Source), "agent-run", item.ID, "success", "")
	})
	if err != nil {
		return RunRecord{}, err
	}
	return store.GetRun(ctx, item.SessionID, item.ID)
}

func (store PostgresStore) FailRunReservation(
	ctx context.Context,
	actorID string,
	runID string,
	safeErrorCode string,
	now time.Time,
) error {
	return store.Transaction.Within(ctx, nil, func(tx transaction.Tx) error {
		var sessionID, projectID, source, sourceRunID, sourceEvaluationID string
		if err := tx.QueryRowContext(ctx, `
			UPDATE agent_runs SET status='failed', safe_error_code=$2,
				completed_at=$3, updated_at=$3, version=version+1
			WHERE run_id=$1 AND status='queued'
			RETURNING session_id,source,COALESCE(source_run_id::text,''),
				COALESCE(source_evaluation_id::text,'')
		`, runID, safeErrorCode, now).Scan(
			&sessionID, &source, &sourceRunID, &sourceEvaluationID,
		); err != nil {
			return mapNotFound(err)
		}
		if err := tx.QueryRowContext(ctx,
			`SELECT project_id FROM agent_sessions WHERE session_id=$1`,
			sessionID).Scan(&projectID); err != nil {
			return err
		}
		payload := map[string]interface{}{
			"session_id": sessionID, "status": RunRecordFailed,
			"safe_error_code": safeErrorCode, "source": source,
		}
		if sourceRunID != "" {
			payload["source_run_id"] = sourceRunID
		}
		if sourceEvaluationID != "" {
			payload["source_evaluation_id"] = sourceEvaluationID
		}
		if err := store.event(ctx, tx, actorID, "session", projectID,
			"agent.run.failed", runID, payload); err != nil {
			return err
		}
		return store.record(ctx, tx, actorID, "session", projectID,
			"agent.run.start", "agent-run", runID, "error", safeErrorCode)
	})
}

func (store PostgresStore) GetRun(ctx context.Context, sessionID, runID string) (RunRecord, error) {
	item, err := scanRun(store.DB.QueryRowContext(ctx, runSelect+`
		WHERE session_id=$1 AND run_id=$2
	`, sessionID, runID).Scan)
	if err != nil {
		return RunRecord{}, mapNotFound(err)
	}
	rows, err := store.DB.QueryContext(ctx, toolCallSelect+`
		WHERE run_id=$1 ORDER BY started_at, tool_call_id
	`, runID)
	if err != nil {
		return RunRecord{}, err
	}
	item.ToolCalls = []ToolCallRecord{}
	for rows.Next() {
		call, scanErr := scanToolCall(rows.Scan)
		if scanErr != nil {
			_ = rows.Close()
			return RunRecord{}, scanErr
		}
		item.ToolCalls = append(item.ToolCalls, call)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return RunRecord{}, err
	}
	if err := rows.Close(); err != nil {
		return RunRecord{}, err
	}
	approvalRows, err := store.DB.QueryContext(ctx, `
		SELECT approval_id
		FROM agent_run_approvals
		WHERE run_id=$1 AND status IN ('pending','responding')
		ORDER BY approval_order
	`, runID)
	if err != nil {
		return RunRecord{}, err
	}
	defer approvalRows.Close()
	item.PendingApprovalIDs = []string{}
	for approvalRows.Next() {
		var approvalID string
		if err := approvalRows.Scan(&approvalID); err != nil {
			return RunRecord{}, err
		}
		item.PendingApprovalIDs = append(item.PendingApprovalIDs, approvalID)
	}
	return item, approvalRows.Err()
}

func (store PostgresStore) RecordRunApproval(
	ctx context.Context,
	runID string,
	approvalID string,
	now time.Time,
) (RunRecord, error) {
	var sessionID string
	err := store.Transaction.Within(ctx, nil, func(tx transaction.Tx) error {
		var runStatus string
		if err := tx.QueryRowContext(ctx, `
			SELECT session_id,status FROM agent_runs WHERE run_id=$1 FOR UPDATE
		`, runID).Scan(&sessionID, &runStatus); err != nil {
			return mapNotFound(err)
		}
		if terminalRunStatus(runStatus) || runStatus == RunRecordStopping {
			return ErrConflict
		}
		var approvalStatus string
		if err := tx.QueryRowContext(ctx, `
			INSERT INTO agent_run_approvals(
				run_id,approval_id,status,claim_id,requested_at,resolved_at,updated_at
			) VALUES($1,$2,'pending',NULL,$3,NULL,$3)
			ON CONFLICT (run_id,approval_id) DO UPDATE SET
				updated_at=agent_run_approvals.updated_at
			RETURNING status
		`, runID, approvalID, now).Scan(&approvalStatus); err != nil {
			return err
		}
		if (approvalStatus != "pending" && approvalStatus != "responding") ||
			runStatus == RunRecordWaitingForApproval {
			return nil
		}
		result, err := tx.ExecContext(ctx, `
			UPDATE agent_runs
			SET status='waiting_for_approval',safe_error_code=NULL,
				updated_at=$2,version=version+1
			WHERE run_id=$1 AND status IN ('queued','running')
		`, runID, now)
		if err := requireAffected(result, err); err != nil {
			if errors.Is(err, ErrNotFound) {
				return ErrConflict
			}
			return err
		}
		return nil
	})
	if err != nil {
		return RunRecord{}, err
	}
	return store.GetRun(ctx, sessionID, runID)
}

func (store PostgresStore) ClaimRunApproval(
	ctx context.Context,
	runID string,
	approvalID string,
	claimID string,
	now time.Time,
) (RunRecord, error) {
	var sessionID string
	err := store.Transaction.Within(ctx, nil, func(tx transaction.Tx) error {
		var runStatus string
		if err := tx.QueryRowContext(ctx, `
			SELECT session_id,status FROM agent_runs WHERE run_id=$1 FOR UPDATE
		`, runID).Scan(&sessionID, &runStatus); err != nil {
			return mapNotFound(err)
		}
		if runStatus != RunRecordWaitingForApproval {
			return ErrConflict
		}
		result, err := tx.ExecContext(ctx, `
			UPDATE agent_run_approvals
			SET status='responding',claim_id=$3,updated_at=$4
			WHERE run_id=$1 AND approval_id=$2
			  AND (
				status='pending'
				OR (status='responding' AND updated_at <= $5)
			  )
			  AND approval_order = (
				SELECT MIN(approval_order) FROM agent_run_approvals
				WHERE run_id=$1 AND status IN ('pending','responding')
			  )
		`, runID, approvalID, claimID, now, now.Add(-runApprovalClaimLease))
		if err := requireAffected(result, err); err != nil {
			if errors.Is(err, ErrNotFound) {
				return ErrConflict
			}
			return err
		}
		return nil
	})
	if err != nil {
		return RunRecord{}, err
	}
	return store.GetRun(ctx, sessionID, runID)
}

func (store PostgresStore) ReleaseRunApprovalClaim(
	ctx context.Context,
	runID string,
	approvalID string,
	claimID string,
	now time.Time,
) (RunRecord, error) {
	var sessionID string
	err := store.Transaction.Within(ctx, nil, func(tx transaction.Tx) error {
		if err := tx.QueryRowContext(ctx, `
			SELECT session_id FROM agent_runs WHERE run_id=$1 FOR UPDATE
		`, runID).Scan(&sessionID); err != nil {
			return mapNotFound(err)
		}
		result, err := tx.ExecContext(ctx, `
			UPDATE agent_run_approvals
			SET status='pending',claim_id=NULL,updated_at=$4
			WHERE run_id=$1 AND approval_id=$2
			  AND status='responding' AND claim_id=$3
		`, runID, approvalID, claimID, now)
		if err := requireAffected(result, err); err != nil {
			if errors.Is(err, ErrNotFound) {
				return ErrConflict
			}
			return err
		}
		return nil
	})
	if err != nil {
		return RunRecord{}, err
	}
	return store.GetRun(ctx, sessionID, runID)
}

func (store PostgresStore) CompleteRunApproval(
	ctx context.Context,
	runID string,
	approvalID string,
	claimID string,
	now time.Time,
) (RunRecord, error) {
	var sessionID string
	err := store.Transaction.Within(ctx, nil, func(tx transaction.Tx) error {
		var runStatus string
		if err := tx.QueryRowContext(ctx, `
			SELECT session_id,status FROM agent_runs WHERE run_id=$1 FOR UPDATE
		`, runID).Scan(&sessionID, &runStatus); err != nil {
			return mapNotFound(err)
		}
		var approvalStatus, currentClaimID string
		if err := tx.QueryRowContext(ctx, `
			SELECT status,COALESCE(claim_id::text,'') FROM agent_run_approvals
			WHERE run_id=$1 AND approval_id=$2 FOR UPDATE
		`, runID, approvalID).Scan(&approvalStatus, &currentClaimID); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return ErrConflict
			}
			return err
		}
		if approvalStatus == "resolved" && currentClaimID == claimID {
			return nil
		}
		if approvalStatus != "responding" || currentClaimID != claimID ||
			runStatus != RunRecordWaitingForApproval {
			return ErrConflict
		}
		result, err := tx.ExecContext(ctx, `
			UPDATE agent_run_approvals
			SET status='resolved',resolved_at=$3,updated_at=$3
			WHERE run_id=$1 AND approval_id=$2
			  AND status='responding' AND claim_id=$4
		`, runID, approvalID, now, claimID)
		if err := requireAffected(result, err); err != nil {
			if errors.Is(err, ErrNotFound) {
				return ErrConflict
			}
			return err
		}
		return updateRunAfterApprovalResolution(ctx, tx, runID, now)
	})
	if err != nil {
		return RunRecord{}, err
	}
	return store.GetRun(ctx, sessionID, runID)
}

func (store PostgresStore) ApplyRunApprovalResponse(
	ctx context.Context,
	runID string,
	approvalID string,
	now time.Time,
) (RunRecord, error) {
	var sessionID string
	err := store.Transaction.Within(ctx, nil, func(tx transaction.Tx) error {
		var runStatus, approvalStatus string
		if err := tx.QueryRowContext(ctx, `
			SELECT session_id,status FROM agent_runs WHERE run_id=$1 FOR UPDATE
		`, runID).Scan(&sessionID, &runStatus); err != nil {
			return mapNotFound(err)
		}
		if err := tx.QueryRowContext(ctx, `
			SELECT status FROM agent_run_approvals
			WHERE run_id=$1 AND approval_id=$2 FOR UPDATE
		`, runID, approvalID).Scan(&approvalStatus); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return ErrConflict
			}
			return err
		}
		if approvalStatus == "resolved" {
			return nil
		}
		if (approvalStatus != "pending" && approvalStatus != "responding") ||
			runStatus != RunRecordWaitingForApproval {
			return ErrConflict
		}
		result, err := tx.ExecContext(ctx, `
			UPDATE agent_run_approvals
			SET status='resolved',resolved_at=$3,updated_at=$3
			WHERE run_id=$1 AND approval_id=$2
			  AND status IN ('pending','responding')
		`, runID, approvalID, now)
		if err := requireAffected(result, err); err != nil {
			if errors.Is(err, ErrNotFound) {
				return ErrConflict
			}
			return err
		}
		return updateRunAfterApprovalResolution(ctx, tx, runID, now)
	})
	if err != nil {
		return RunRecord{}, err
	}
	return store.GetRun(ctx, sessionID, runID)
}

func (store PostgresStore) ApplyNextRunApprovalResponse(
	ctx context.Context,
	runID string,
	now time.Time,
) (RunRecord, string, error) {
	var sessionID, approvalID string
	err := store.Transaction.Within(ctx, nil, func(tx transaction.Tx) error {
		var runStatus string
		if err := tx.QueryRowContext(ctx, `
			SELECT session_id,status FROM agent_runs WHERE run_id=$1 FOR UPDATE
		`, runID).Scan(&sessionID, &runStatus); err != nil {
			return mapNotFound(err)
		}
		if runStatus != RunRecordWaitingForApproval {
			return ErrConflict
		}
		if err := tx.QueryRowContext(ctx, `
			SELECT approval_id FROM agent_run_approvals
			WHERE run_id=$1 AND status IN ('pending','responding')
			ORDER BY approval_order
			LIMIT 1 FOR UPDATE
		`, runID).Scan(&approvalID); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return ErrConflict
			}
			return err
		}
		result, err := tx.ExecContext(ctx, `
			UPDATE agent_run_approvals
			SET status='resolved',resolved_at=$3,updated_at=$3
			WHERE run_id=$1 AND approval_id=$2
			  AND status IN ('pending','responding')
		`, runID, approvalID, now)
		if err := requireAffected(result, err); err != nil {
			if errors.Is(err, ErrNotFound) {
				return ErrConflict
			}
			return err
		}
		return updateRunAfterApprovalResolution(ctx, tx, runID, now)
	})
	if err != nil {
		return RunRecord{}, "", err
	}
	item, err := store.GetRun(ctx, sessionID, runID)
	return item, approvalID, err
}

func updateRunAfterApprovalResolution(
	ctx context.Context,
	tx transaction.Tx,
	runID string,
	now time.Time,
) error {
	var hasPending bool
	if err := tx.QueryRowContext(ctx, `
		SELECT EXISTS(
			SELECT 1 FROM agent_run_approvals
			WHERE run_id=$1 AND status IN ('pending','responding')
		)
	`, runID).Scan(&hasPending); err != nil {
		return err
	}
	nextStatus := RunRecordRunning
	if hasPending {
		nextStatus = RunRecordWaitingForApproval
	}
	result, err := tx.ExecContext(ctx, `
		UPDATE agent_runs
		SET status=$2,safe_error_code=NULL,updated_at=$3,version=version+1
		WHERE run_id=$1 AND status='waiting_for_approval'
	`, runID, nextStatus, now)
	if err := requireAffected(result, err); err != nil {
		if errors.Is(err, ErrNotFound) {
			return ErrConflict
		}
		return err
	}
	return nil
}

func (store PostgresStore) UpdateRun(
	ctx context.Context,
	runID string,
	status string,
	safeErrorCode string,
	now time.Time,
) (RunRecord, error) {
	completed := status == RunRecordCompleted || status == RunRecordFailed || status == RunRecordStopped
	var item RunRecord
	err := store.Transaction.Within(ctx, nil, func(tx transaction.Tx) error {
		current, err := scanRun(tx.QueryRowContext(ctx, runSelect+`
			WHERE run_id=$1 FOR UPDATE
		`, runID).Scan)
		if err != nil {
			return mapNotFound(err)
		}
		item = current
		if status == RunRecordRunning {
			var hasPending bool
			if err := tx.QueryRowContext(ctx, `
				SELECT EXISTS(
					SELECT 1 FROM agent_run_approvals
					WHERE run_id=$1 AND status IN ('pending','responding')
				)
			`, runID).Scan(&hasPending); err != nil {
				return err
			}
			if hasPending {
				status = RunRecordWaitingForApproval
			}
		}
		completedAt := item.CompletedAt
		if completed {
			completedAt = &now
			if _, err := tx.ExecContext(ctx, `
				UPDATE agent_run_approvals
				SET status='expired',claim_id=NULL,resolved_at=$2,updated_at=$2
				WHERE run_id=$1 AND status IN ('pending','responding')
			`, runID, now); err != nil {
				return err
			}
		}
		if _, err := tx.ExecContext(ctx, `
			UPDATE agent_runs SET status=$2, safe_error_code=NULLIF($3,''),
				completed_at=$4, updated_at=$5, version=version+1
			WHERE run_id=$1
		`, runID, status, safeErrorCode, completedAt, now); err != nil {
			return err
		}
		if completed {
			var projectID string
			if err := tx.QueryRowContext(ctx, `
				SELECT project_id FROM agent_sessions WHERE session_id=$1
			`, item.SessionID).Scan(&projectID); err != nil {
				return err
			}
			eventType := "agent.run." + status
			if status == RunRecordStopped {
				eventType = "agent.run.stopped"
			}
			payload := map[string]interface{}{
				"session_id": item.SessionID, "status": status,
				"safe_error_code": safeErrorCode, "source": item.Source,
			}
			if item.SourceRunID != "" {
				payload["source_run_id"] = item.SourceRunID
			}
			if item.SourceEvaluationID != "" {
				payload["source_evaluation_id"] = item.SourceEvaluationID
			}
			if err := store.event(ctx, tx, item.CreatedBy, "session", projectID,
				eventType, runID, payload); err != nil {
				return err
			}
			outcome := "success"
			if status == RunRecordFailed {
				outcome = "error"
			}
			if err := store.record(ctx, tx, item.CreatedBy, "session", projectID,
				runAuditAction(status), "agent-run", runID,
				outcome, safeErrorCode); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return RunRecord{}, err
	}
	return store.GetRun(ctx, item.SessionID, runID)
}

func (store PostgresStore) UpsertToolCall(ctx context.Context, item ToolCallRecord) (ToolCallRecord, error) {
	if len(item.SafePreview) > 500 {
		item.SafePreview = item.SafePreview[:500]
	}
	err := store.DB.QueryRowContext(ctx, `
		INSERT INTO agent_tool_calls (
			tool_call_id, run_id, remote_tool_call_id, tool_name, status,
			safe_preview, started_at, completed_at, updated_at
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)
		ON CONFLICT (run_id, remote_tool_call_id) DO UPDATE SET
			tool_name=EXCLUDED.tool_name, status=EXCLUDED.status,
			safe_preview=EXCLUDED.safe_preview,
			completed_at=EXCLUDED.completed_at, updated_at=EXCLUDED.updated_at
		RETURNING tool_call_id
	`, item.ID, item.RunID, item.RemoteToolCallID, item.ToolName, item.Status,
		item.SafePreview, item.StartedAt, item.CompletedAt, item.UpdatedAt).Scan(&item.ID)
	return item, err
}

func (store PostgresStore) CreateRotation(ctx context.Context, actorID string, item TokenRotation) (TokenRotation, error) {
	err := store.Transaction.Within(ctx, nil, func(tx transaction.Tx) error {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO agent_token_rotations (
				rotation_id, grant_id, old_token_id, new_token_id,
				management_mode, status, created_by, created_at, updated_at
			) VALUES ($1,$2,NULLIF($3,'')::uuid,$4,$5,$6,$7,$8,$8)
		`, item.RotationID, item.GrantID, item.OldTokenID, item.NewTokenID,
			item.ManagementMode, item.Status, item.CreatedBy, item.CreatedAt); err != nil {
			return err
		}
		var projectID string
		if err := tx.QueryRowContext(ctx, `
			SELECT project_id FROM agent_project_grants WHERE grant_id=$1
		`, item.GrantID).Scan(&projectID); err != nil {
			return err
		}
		if err := store.event(ctx, tx, actorID, "session", projectID,
			"agent.token.issued", item.NewTokenID,
			map[string]interface{}{"rotation_id": item.RotationID, "status": item.Status}); err != nil {
			return err
		}
		return store.record(ctx, tx, actorID, "session", projectID,
			"agent.token.issue", "agent-token", item.NewTokenID,
			"success", "")
	})
	return item, err
}

func (store PostgresStore) FindRotationByToken(ctx context.Context, grantID, tokenID string) (TokenRotation, error) {
	item, err := scanRotation(store.DB.QueryRowContext(ctx, rotationSelect+`
		WHERE grant_id=$1 AND new_token_id=$2
		ORDER BY created_at DESC LIMIT 1
	`, grantID, tokenID).Scan)
	return item, mapNotFound(err)
}

func (store PostgresStore) UpdateRotation(
	ctx context.Context,
	rotationID string,
	status string,
	safeErrorCode string,
	now time.Time,
) (TokenRotation, error) {
	completed := status == "completed" || status == "failed" || status == "cancelled"
	var completedAt interface{}
	if completed {
		completedAt = now
	}
	var item TokenRotation
	err := store.Transaction.Within(ctx, nil, func(tx transaction.Tx) error {
		updated, err := scanRotation(tx.QueryRowContext(ctx, `
			UPDATE agent_token_rotations SET status=$2,
				safe_error_code=NULLIF($3,''), updated_at=$4,
				completed_at=$5
			WHERE rotation_id=$1
			RETURNING rotation_id, grant_id, COALESCE(old_token_id::text,''),
				new_token_id, management_mode, status, COALESCE(safe_error_code,''),
				created_by, created_at, updated_at, completed_at
		`, rotationID, status, safeErrorCode, now, completedAt).Scan)
		if err != nil {
			return mapNotFound(err)
		}
		item = updated
		var projectID string
		if status == "failed" || status == "cancelled" {
			if err := tx.QueryRowContext(ctx, `
				SELECT project_id FROM agent_project_grants WHERE grant_id=$1
			`, item.GrantID).Scan(&projectID); err != nil {
				return err
			}
		}
		if status == "cancelled" {
			return store.record(ctx, tx, item.CreatedBy, "session", projectID,
				"agent.token.abort", "agent-token", item.NewTokenID,
				"success", "")
		}
		if status != "failed" {
			return nil
		}
		if item.SafeErrorCode == "" {
			item.SafeErrorCode = "rotation_failed"
		}
		if err := store.event(ctx, tx, item.CreatedBy, "session", projectID,
			"agent.token.rotation_failed", item.NewTokenID,
			map[string]interface{}{
				"rotation_id": item.RotationID, "status": "failed",
				"safe_error_code":          item.SafeErrorCode,
				"old_token_remains_active": item.OldTokenID != "",
			}); err != nil {
			return err
		}
		return store.record(ctx, tx, item.CreatedBy, "session", projectID,
			"agent.token.rotate", "agent-token", item.NewTokenID,
			"error", item.SafeErrorCode)
	})
	return item, err
}

func (store PostgresStore) event(
	ctx context.Context,
	tx transaction.Tx,
	actorID string,
	actorKind string,
	projectID string,
	eventType string,
	resourceID string,
	payload map[string]interface{},
) error {
	payload["project_id"] = projectID
	payload["resource_id"] = resourceID
	_, err := store.Outbox.Write(ctx, tx, outbox.Event{
		Actor:     map[string]string{"actor_id": actorID, "actor_kind": actorKind},
		EventType: eventType, Payload: payload, Producer: "agent", ProjectID: projectID,
	})
	return err
}

func (store PostgresStore) record(
	ctx context.Context,
	tx transaction.Tx,
	actorID string,
	actorKind string,
	projectID string,
	action string,
	resourceType string,
	resourceID string,
	outcome string,
	errorCode string,
) error {
	if store.Audit.Store == nil {
		return nil
	}
	return store.Audit.RecordInTransaction(ctx, tx, audit.Event{
		Action: action, ActorID: actorID, ActorKind: actorKind, Category: "agent",
		ErrorCode: errorCode, Outcome: outcome, ProjectID: projectID,
		ResourceID: resourceID, ResourceType: resourceType, Source: "core",
		Metadata: map[string]interface{}{},
	})
}

const instanceSelect = `
	SELECT instance.agent_instance_id, instance.adapter_type, instance.display_name,
	       instance.management_mode, instance.runtime_url,
	       COALESCE(instance.dashboard_url,''), instance.status,
	       instance.management_path, instance.capabilities,
	       instance.runtime_check, instance.management_check,
	       instance.project_access_check, instance.created_by,
	       instance.created_at, instance.updated_at, instance.disabled_at,
	       instance.version,
	       grant_row.grant_id, grant_row.agent_instance_id, grant_row.project_id, grant_row.role,
	       grant_row.status, grant_row.allowed_tools, COALESCE(grant_row.remote_access_id,''),
	       COALESCE(grant_row.default_session_id::text,''), grant_row.last_access_at,
	       grant_row.created_by, grant_row.created_at, grant_row.updated_at, grant_row.version
	FROM agent_instances AS instance
	JOIN agent_project_grants AS grant_row USING (agent_instance_id)
`

const grantSelect = `
	SELECT grant_id, agent_instance_id, project_id, role, status, allowed_tools,
	       COALESCE(remote_access_id,''),
	       COALESCE(default_session_id::text,''), last_access_at,
	       created_by, created_at, updated_at, version
	FROM agent_project_grants
`

const sessionSelect = `
	SELECT session_id, grant_id, agent_instance_id, project_id,
	       remote_session_id, session_type, title, status,
	       COALESCE(parent_session_id::text,''), COALESCE(end_reason,''),
	       created_by, created_at, updated_at, ended_at,
	       last_message_at, last_run_at, version
	FROM agent_sessions
`

const runSelect = `
	SELECT run_id, session_id, remote_run_id, status, source,
	       COALESCE(source_run_id::text,''),
	       COALESCE(source_evaluation_id::text,''), COALESCE(safe_error_code,''),
	       created_by, created_at, started_at, completed_at, updated_at, version
	FROM agent_runs
`

const toolCallSelect = `
	SELECT tool_call_id, run_id, remote_tool_call_id, tool_name, status,
	       safe_preview, started_at, completed_at, updated_at
	FROM agent_tool_calls
`

const rotationSelect = `
	SELECT rotation_id, grant_id, COALESCE(old_token_id::text,''),
	       new_token_id, management_mode, status, COALESCE(safe_error_code,''),
	       created_by, created_at, updated_at, completed_at
	FROM agent_token_rotations
`

type scanFunc func(...interface{}) error

func scanInstance(scan scanFunc) (Instance, error) {
	var item Instance
	var capabilities, runtimeCheck, managementCheck, projectAccessCheck []byte
	var grant ProjectGrant
	var tools []byte
	err := scan(
		&item.ID, &item.AdapterType, &item.DisplayName, &item.ManagementMode,
		&item.RuntimeURL, &item.DashboardURL, &item.Status, &item.ManagementPath,
		&capabilities, &runtimeCheck, &managementCheck, &projectAccessCheck,
		&item.CreatedBy, &item.CreatedAt, &item.UpdatedAt, &item.DisabledAt,
		&item.Version,
		&grant.GrantID, &grant.AgentInstanceID, &grant.ProjectID, &grant.Role,
		&grant.Status, &tools, &grant.RemoteAccessID, &grant.DefaultSessionID, &grant.LastAccessAt,
		&grant.CreatedBy, &grant.CreatedAt, &grant.UpdatedAt, &grant.Version,
	)
	if err != nil {
		return Instance{}, err
	}
	if err := json.Unmarshal(capabilities, &item.Capabilities); err != nil {
		return Instance{}, err
	}
	if err := json.Unmarshal(runtimeCheck, &item.RuntimeCheck); err != nil {
		return Instance{}, err
	}
	if err := json.Unmarshal(managementCheck, &item.ManagementCheck); err != nil {
		return Instance{}, err
	}
	if err := json.Unmarshal(projectAccessCheck, &item.ProjectAccessCheck); err != nil {
		return Instance{}, err
	}
	if err := json.Unmarshal(tools, &grant.AllowedTools); err != nil {
		return Instance{}, err
	}
	item.Grant = &grant
	return item, nil
}

func scanGrant(scan scanFunc) (ProjectGrant, error) {
	var item ProjectGrant
	var tools []byte
	err := scan(&item.GrantID, &item.AgentInstanceID, &item.ProjectID,
		&item.Role, &item.Status, &tools, &item.RemoteAccessID, &item.DefaultSessionID,
		&item.LastAccessAt, &item.CreatedBy, &item.CreatedAt, &item.UpdatedAt,
		&item.Version)
	if err != nil {
		return ProjectGrant{}, err
	}
	if err := json.Unmarshal(tools, &item.AllowedTools); err != nil {
		return ProjectGrant{}, err
	}
	return item, nil
}

func scanSession(scan scanFunc) (SessionRecord, error) {
	var item SessionRecord
	err := scan(&item.ID, &item.GrantID, &item.AgentInstanceID, &item.ProjectID,
		&item.RemoteSessionID, &item.SessionType, &item.Title, &item.Status,
		&item.ParentSessionID, &item.EndReason, &item.CreatedBy, &item.CreatedAt,
		&item.UpdatedAt, &item.EndedAt, &item.LastMessageAt, &item.LastRunAt,
		&item.Version)
	return item, err
}

func scanRun(scan scanFunc) (RunRecord, error) {
	var item RunRecord
	err := scan(&item.ID, &item.SessionID, &item.RemoteRunID, &item.Status,
		&item.Source, &item.SourceRunID, &item.SourceEvaluationID,
		&item.SafeErrorCode, &item.CreatedBy,
		&item.CreatedAt, &item.StartedAt, &item.CompletedAt, &item.UpdatedAt,
		&item.Version)
	return item, err
}

func scanToolCall(scan scanFunc) (ToolCallRecord, error) {
	var item ToolCallRecord
	err := scan(&item.ID, &item.RunID, &item.RemoteToolCallID, &item.ToolName,
		&item.Status, &item.SafePreview, &item.StartedAt, &item.CompletedAt,
		&item.UpdatedAt)
	return item, err
}

func scanRotation(scan scanFunc) (TokenRotation, error) {
	var item TokenRotation
	err := scan(&item.RotationID, &item.GrantID, &item.OldTokenID,
		&item.NewTokenID, &item.ManagementMode, &item.Status,
		&item.SafeErrorCode, &item.CreatedBy, &item.CreatedAt, &item.UpdatedAt,
		&item.CompletedAt)
	return item, err
}

func sessionAuditAction(eventType string) string {
	switch eventType {
	case "agent.session.renamed":
		return "agent.session.rename"
	case "agent.session.ended":
		return "agent.session.end"
	case "agent.session.continued":
		return "agent.session.continue"
	default:
		return "agent.session.update"
	}
}

func runAuditAction(status string) string {
	switch status {
	case RunRecordCompleted:
		return "agent.run.complete"
	case RunRecordFailed:
		return "agent.run.fail"
	case RunRecordStopped:
		return "agent.run.stop"
	default:
		return "agent.run.update"
	}
}

func runStartAuditAction(source string) string {
	switch source {
	case "regenerate":
		return "agent.run.regenerate"
	case "rerun":
		return "agent.run.rerun"
	default:
		return "agent.run.start"
	}
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

func mapNotFound(err error) error {
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	return err
}

func isUniqueViolation(err error) bool {
	var postgresError *pgconn.PgError
	return errors.As(err, &postgresError) && postgresError.Code == "23505"
}

func nonNilStrings(values []string) []string {
	if values == nil {
		return []string{}
	}
	return values
}

func nonNilMap(value map[string]interface{}) map[string]interface{} {
	if value == nil {
		return map[string]interface{}{}
	}
	return value
}
