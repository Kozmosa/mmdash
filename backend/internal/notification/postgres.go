package notification

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/mmdash/mmdash/backend/internal/platform/pagination"
	"github.com/mmdash/mmdash/backend/internal/platform/transaction"
)

func (store PostgresStore) ClaimEmailRecipients(ctx context.Context, email, userID string) error {
	if strings.TrimSpace(email) == "" || strings.TrimSpace(userID) == "" {
		return ErrInvalid
	}
	_, err := store.DB.ExecContext(ctx, `UPDATE notification_recipients SET user_id=$2 WHERE LOWER(normalized_email)=LOWER($1) AND (expires_at IS NULL OR expires_at > NOW())`, email, userID)
	return err
}

func (store PostgresStore) ApplyInvitationOutcome(ctx context.Context, invitationID, outcome string) error {
	if invitationID == "" || (outcome != OutcomeResolved && outcome != OutcomeRevoked && outcome != OutcomeExpired) {
		return ErrInvalid
	}
	if store.Transaction.DB == nil {
		return ErrNotReady
	}
	return store.Transaction.Within(ctx, nil, func(tx transaction.Tx) error {
		if _, err := tx.ExecContext(ctx, `UPDATE notification_inbox_items AS item SET outcome=$2, updated_at=NOW() FROM notification_notifications AS notification WHERE item.notification_id=notification.notification_id AND notification.type_key=$3 AND notification.data->>'invitation_id'=$1 AND item.outcome='active'`, invitationID, outcome, TypeInvitationReceived); err != nil {
			return err
		}
		if outcome == OutcomeRevoked || outcome == OutcomeExpired {
			_, err := tx.ExecContext(ctx, `UPDATE notification_deliveries AS delivery SET status='cancelled',locked_by=NULL,lease_expires_at=NULL,last_error_code='notification_outcome',last_error_message=$2,updated_at=NOW() FROM notification_notifications AS notification WHERE delivery.notification_id=notification.notification_id AND notification.type_key=$3 AND notification.data->>'invitation_id'=$1 AND delivery.status IN ('pending','retrying')`, invitationID, "Invitation is no longer active", TypeInvitationReceived)
			return err
		}
		return nil
	})
}

func (store PostgresStore) ListInbox(ctx context.Context, userID string, filter Filter, page pagination.Request) (Page, error) {
	where := []string{"recipient.user_id = $1"}
	args := []interface{}{userID}
	next := 2
	cursorTime, cursorID, err := decodeCursor(page.Cursor)
	if err != nil {
		return Page{}, ErrInvalid
	}
	if filter.ProjectID != "" {
		where = append(where, fmt.Sprintf("notification.project_id = $%d", next))
		args = append(args, filter.ProjectID)
		next++
	}
	if filter.TypeKey != "" {
		where = append(where, fmt.Sprintf("notification.type_key = $%d", next))
		args = append(args, filter.TypeKey)
		next++
	}
	if filter.ReadState == "read" || filter.ReadState == "unread" {
		where = append(where, fmt.Sprintf("item.read_state = $%d", next))
		args = append(args, filter.ReadState)
		next++
	}
	if filter.Archived == "true" {
		where = append(where, "item.archived_at IS NOT NULL")
	} else if filter.Archived == "false" {
		where = append(where, "item.archived_at IS NULL")
	}
	if filter.Outcome != "" {
		where = append(where, fmt.Sprintf("item.outcome = $%d", next))
		args = append(args, filter.Outcome)
		next++
	}
	if filter.OutcomeGroup == "processed" {
		where = append(where, "item.outcome IN ('resolved','revoked','expired')")
	}
	if filter.OccurredFrom != nil {
		where = append(where, fmt.Sprintf("notification.occurred_at >= $%d", next))
		args = append(args, *filter.OccurredFrom)
		next++
	}
	if filter.OccurredTo != nil {
		where = append(where, fmt.Sprintf("notification.occurred_at <= $%d", next))
		args = append(args, *filter.OccurredTo)
		next++
	}
	if cursorTime != "" {
		where = append(where, fmt.Sprintf("(item.created_at, item.inbox_item_id) < ($%d::timestamptz, $%d::uuid)", next, next+1))
		args = append(args, cursorTime, cursorID)
		next += 2
	}
	args = append(args, page.Limit+1)
	query := `SELECT item.inbox_item_id,item.read_state,item.archived_at,item.outcome,item.created_at,item.updated_at,
	 notification.notification_id,notification.type_key,notification.template_version,notification.source_event_id,COALESCE(notification.project_id::text,''),COALESCE(notification.actor_id::text,''),notification.resource_type,notification.resource_id,notification.priority,notification.data,notification.rendered_snapshot,notification.action_type,notification.action_resource_id,notification.action_route,notification.occurred_at,notification.created_at,
 recipient.recipient_id,recipient.notification_id,recipient.recipient_key,COALESCE(recipient.user_id::text,''),COALESCE(recipient.normalized_email,''),recipient.expires_at
 FROM notification_inbox_items item JOIN notification_notifications notification USING(notification_id) JOIN notification_recipients recipient USING(recipient_id)
 WHERE ` + strings.Join(where, " AND ") + ` ORDER BY item.created_at DESC,item.inbox_item_id DESC LIMIT $` + fmt.Sprint(next)
	rows, err := store.DB.QueryContext(ctx, query, args...)
	if err != nil {
		return Page{}, err
	}
	defer rows.Close()
	items := []InboxItem{}
	for rows.Next() {
		item, err := scanInbox(rows.Scan)
		if err != nil {
			return Page{}, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return Page{}, err
	}
	pageResult := Page{Items: items}
	if len(items) > page.Limit {
		pageResult.HasMore = true
		pageResult.Items = items[:page.Limit]
		last := pageResult.Items[len(pageResult.Items)-1]
		pageResult.NextCursor, err = pagination.Encode(pagination.Cursor{ID: last.ID, SortValue: last.CreatedAt.Format(time.RFC3339Nano)})
		if err != nil {
			return Page{}, err
		}
	}
	return pageResult, nil
}

func (store PostgresStore) GetInbox(ctx context.Context, userID, id string) (InboxItem, error) {
	row := store.DB.QueryRowContext(ctx, `SELECT item.inbox_item_id,item.read_state,item.archived_at,item.outcome,item.created_at,item.updated_at,
	 notification.notification_id,notification.type_key,notification.template_version,notification.source_event_id,COALESCE(notification.project_id::text,''),COALESCE(notification.actor_id::text,''),notification.resource_type,notification.resource_id,notification.priority,notification.data,notification.rendered_snapshot,notification.action_type,notification.action_resource_id,notification.action_route,notification.occurred_at,notification.created_at,
 recipient.recipient_id,recipient.notification_id,recipient.recipient_key,COALESCE(recipient.user_id::text,''),COALESCE(recipient.normalized_email,''),recipient.expires_at
 FROM notification_inbox_items item JOIN notification_notifications notification USING(notification_id) JOIN notification_recipients recipient USING(recipient_id)
 WHERE item.inbox_item_id=$1 AND recipient.user_id=$2`, id, userID)
	item, err := scanInbox(row.Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return InboxItem{}, ErrNotFound
	}
	return item, err
}

func (store PostgresStore) UpdateInbox(ctx context.Context, userID, id string, readState *string, archived *bool) (InboxItem, error) {
	result, err := store.DB.ExecContext(ctx, `UPDATE notification_inbox_items AS item SET read_state=COALESCE($3,item.read_state), archived_at=CASE WHEN $4::boolean IS NULL THEN item.archived_at WHEN $4 THEN COALESCE(item.archived_at,NOW()) ELSE NULL END, updated_at=NOW() FROM notification_recipients recipient WHERE item.recipient_id=recipient.recipient_id AND item.inbox_item_id=$1 AND recipient.user_id=$2`, id, userID, readState, archived)
	if err != nil {
		return InboxItem{}, err
	}
	if affected, affectedErr := result.RowsAffected(); affectedErr != nil {
		return InboxItem{}, affectedErr
	} else if affected == 0 {
		return InboxItem{}, ErrNotFound
	}
	return store.GetInbox(ctx, userID, id)
}

func (store PostgresStore) MarkAllRead(ctx context.Context, userID string, filter Filter) error {
	where := []string{"recipient.user_id=$1", "item.read_state='unread'"}
	args := []interface{}{userID}
	next := 2
	if filter.ProjectID != "" {
		where = append(where, fmt.Sprintf("notification.project_id=$%d", next))
		args = append(args, filter.ProjectID)
		next++
	}
	if filter.TypeKey != "" {
		where = append(where, fmt.Sprintf("notification.type_key=$%d", next))
		args = append(args, filter.TypeKey)
		next++
	}
	_, err := store.DB.ExecContext(ctx, `UPDATE notification_inbox_items item SET read_state='read',updated_at=NOW() FROM notification_recipients recipient JOIN notification_notifications notification USING(notification_id) WHERE item.recipient_id=recipient.recipient_id AND `+strings.Join(where, " AND "), args...)
	return err
}

func (store PostgresStore) UnreadCount(ctx context.Context, userID, projectID string) (int64, error) {
	query := `SELECT COUNT(*) FROM notification_inbox_items item JOIN notification_recipients recipient USING(recipient_id) JOIN notification_notifications notification USING(notification_id) WHERE recipient.user_id=$1 AND item.read_state='unread' AND item.archived_at IS NULL`
	args := []interface{}{userID}
	if projectID != "" {
		query += ` AND notification.project_id=$2`
		args = append(args, projectID)
	}
	var count int64
	err := store.DB.QueryRowContext(ctx, query, args...).Scan(&count)
	return count, err
}

func (store PostgresStore) GetRule(ctx context.Context, projectID, typeKey string) (Rule, error) {
	var rule Rule
	var channelKeys []byte
	err := store.DB.QueryRowContext(ctx, `SELECT project_id,type_key,external_enabled,channel_keys,minimum_priority,version,updated_by,updated_at FROM notification_rules WHERE project_id=$1 AND type_key=$2`, projectID, typeKey).Scan(&rule.ProjectID, &rule.TypeKey, &rule.ExternalEnabled, &channelKeys, &rule.MinimumPriority, &rule.Version, &rule.UpdatedBy, &rule.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Rule{ProjectID: projectID, TypeKey: typeKey, ChannelKeys: []string{}, MinimumPriority: "normal"}, nil
	}
	if err != nil {
		return Rule{}, err
	}
	rule.ChannelKeys, err = decodeRuleChannelKeys(channelKeys)
	return rule, err
}

func (store PostgresStore) UpsertRule(ctx context.Context, rule Rule) (Rule, error) {
	if rule.UpdatedAt.IsZero() {
		rule.UpdatedAt = store.now()
	}
	ruleID, err := store.Generator.New()
	if err != nil {
		return Rule{}, err
	}
	if rule.MinimumPriority == "" {
		rule.MinimumPriority = "normal"
	}
	expectedVersion := rule.Version
	channelKeys, marshalErr := json.Marshal(nonNilRuleChannelKeys(rule.ChannelKeys))
	if marshalErr != nil {
		return Rule{}, marshalErr
	}
	var returnedChannelKeys []byte
	err = store.DB.QueryRowContext(ctx, `INSERT INTO notification_rules(rule_id,project_id,type_key,external_enabled,channel_keys,minimum_priority,updated_by,version,created_at,updated_at) VALUES($1,$2,$3,$4,$5::jsonb,$6,$7,1,$8,$8) ON CONFLICT(project_id,type_key) DO UPDATE SET external_enabled=EXCLUDED.external_enabled,channel_keys=EXCLUDED.channel_keys,minimum_priority=EXCLUDED.minimum_priority,updated_by=EXCLUDED.updated_by,version=notification_rules.version+1,updated_at=EXCLUDED.updated_at WHERE notification_rules.version=$9 RETURNING project_id,type_key,external_enabled,channel_keys,minimum_priority,version,updated_by,updated_at`, ruleID, rule.ProjectID, rule.TypeKey, rule.ExternalEnabled, channelKeys, rule.MinimumPriority, rule.UpdatedBy, rule.UpdatedAt, expectedVersion).Scan(&rule.ProjectID, &rule.TypeKey, &rule.ExternalEnabled, &returnedChannelKeys, &rule.MinimumPriority, &rule.Version, &rule.UpdatedBy, &rule.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Rule{}, ErrConflict
	}
	if err != nil {
		return Rule{}, err
	}
	rule.ChannelKeys, err = decodeRuleChannelKeys(returnedChannelKeys)
	return rule, err
}

func (store PostgresStore) ListDeliveries(ctx context.Context, projectID, channelKey string, page pagination.Request) (DeliveryPage, error) {
	query := `SELECT delivery_id,notification_id,COALESCE(recipient_id::text,''),project_id,channel_key,target_key,rule_version,settings_version,delivery_key,status,attempts,max_attempts,COALESCE(provider_message_id,''),last_error_code,last_error_message,COALESCE(response_summary,''),available_at,delivered_at,created_at,updated_at FROM notification_deliveries WHERE project_id=$1`
	args := []interface{}{projectID}
	cursorTime, cursorID, err := decodeCursor(page.Cursor)
	if err != nil {
		return DeliveryPage{}, ErrInvalid
	}
	if channelKey != "" {
		query += ` AND channel_key=$2`
		args = append(args, channelKey)
	}
	if cursorTime != "" {
		position := len(args) + 1
		query += fmt.Sprintf(` AND (created_at, delivery_id) < ($%d::timestamptz, $%d::uuid)`, position, position+1)
		args = append(args, cursorTime, cursorID)
	}
	query += ` ORDER BY created_at DESC,delivery_id DESC LIMIT $` + fmt.Sprint(len(args)+1)
	args = append(args, page.Limit+1)
	rows, err := store.DB.QueryContext(ctx, query, args...)
	if err != nil {
		return DeliveryPage{}, err
	}
	defer rows.Close()
	items := []Delivery{}
	for rows.Next() {
		var d Delivery
		if err := rows.Scan(&d.ID, &d.NotificationID, &d.RecipientID, &d.ProjectID, &d.ChannelKey, &d.TargetKey, &d.RuleVersion, &d.SettingsVersion, &d.DeliveryKey, &d.Status, &d.Attempts, &d.MaxAttempts, &d.ProviderMessage, &d.LastErrorCode, &d.LastError, &d.ResponseSummary, &d.AvailableAt, &d.DeliveredAt, &d.CreatedAt, &d.UpdatedAt); err != nil {
			return DeliveryPage{}, err
		}
		items = append(items, d)
	}
	result := DeliveryPage{Items: items}
	if len(items) > page.Limit {
		result.HasMore = true
		result.Items = items[:page.Limit]
		last := result.Items[len(result.Items)-1]
		result.NextCursor, err = pagination.Encode(pagination.Cursor{ID: last.ID, SortValue: last.CreatedAt.Format(time.RFC3339Nano)})
		if err != nil {
			return DeliveryPage{}, err
		}
	}
	return result, rows.Err()
}

func (store PostgresStore) CreateRetry(ctx context.Context, projectID, deliveryID, reason string) (Delivery, error) {
	newID, err := store.Generator.New()
	if err != nil {
		return Delivery{}, err
	}
	key := "retry:" + newID
	var d Delivery
	err = store.DB.QueryRowContext(ctx, `INSERT INTO notification_deliveries(delivery_id,notification_id,recipient_id,project_id,channel_key,target_key,rule_version,settings_version,delivery_key,status,max_attempts,available_at,created_at,updated_at) SELECT $1,notification_id,recipient_id,project_id,channel_key,target_key,rule_version,settings_version,$2,'pending',max_attempts,NOW(),NOW(),NOW() FROM notification_deliveries WHERE delivery_id=$3 AND project_id=$4 AND status IN ('failed','retrying') RETURNING delivery_id,notification_id,COALESCE(recipient_id::text,''),project_id,channel_key,target_key,rule_version,settings_version,delivery_key,status,attempts,max_attempts,COALESCE(provider_message_id,''),last_error_code,last_error_message,COALESCE(response_summary,''),available_at,delivered_at,created_at,updated_at`, newID, key, deliveryID, projectID).Scan(&d.ID, &d.NotificationID, &d.RecipientID, &d.ProjectID, &d.ChannelKey, &d.TargetKey, &d.RuleVersion, &d.SettingsVersion, &d.DeliveryKey, &d.Status, &d.Attempts, &d.MaxAttempts, &d.ProviderMessage, &d.LastErrorCode, &d.LastError, &d.ResponseSummary, &d.AvailableAt, &d.DeliveredAt, &d.CreatedAt, &d.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Delivery{}, ErrNotFound
	}
	return d, err
}

func (store PostgresStore) EnqueueDelivery(ctx context.Context, notification Notification, channelKey string, settingsVersion int64) (Delivery, error) {
	id, err := store.Generator.New()
	if err != nil {
		return Delivery{}, err
	}
	now := store.now()
	var delivery Delivery
	targetKey := "project-channel:" + channelKey
	err = store.DB.QueryRowContext(ctx, `INSERT INTO notification_deliveries(delivery_id,notification_id,project_id,channel_key,target_key,settings_version,delivery_key,status,max_attempts,available_at,created_at,updated_at) VALUES($1,$2,$3,$4,$5,$6,'live','pending',5,$7,$7,$7) ON CONFLICT(notification_id,channel_key,target_key,delivery_key) DO NOTHING RETURNING delivery_id,notification_id,COALESCE(recipient_id::text,''),project_id,channel_key,target_key,rule_version,settings_version,delivery_key,status,attempts,max_attempts,COALESCE(provider_message_id,''),last_error_code,last_error_message,COALESCE(response_summary,''),available_at,delivered_at,created_at,updated_at`, id, notification.ID, notification.ProjectID, channelKey, targetKey, settingsVersion, now).Scan(&delivery.ID, &delivery.NotificationID, &delivery.RecipientID, &delivery.ProjectID, &delivery.ChannelKey, &delivery.TargetKey, &delivery.RuleVersion, &delivery.SettingsVersion, &delivery.DeliveryKey, &delivery.Status, &delivery.Attempts, &delivery.MaxAttempts, &delivery.ProviderMessage, &delivery.LastErrorCode, &delivery.LastError, &delivery.ResponseSummary, &delivery.AvailableAt, &delivery.DeliveredAt, &delivery.CreatedAt, &delivery.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Delivery{}, ErrNotFound
	}
	return delivery, err
}

func (store PostgresStore) ClaimDelivery(ctx context.Context, owner string, lease time.Duration) (*Delivery, Notification, error) {
	if store.Transaction.DB == nil {
		return nil, Notification{}, ErrNotReady
	}
	var delivery *Delivery
	var notification Notification
	var data []byte
	now := store.now()
	if lease <= 0 {
		lease = time.Minute
	}
	err := store.Transaction.Within(ctx, nil, func(tx transaction.Tx) error {
		_, err := tx.ExecContext(ctx, `UPDATE notification_delivery_attempts AS attempt SET outcome=CASE WHEN delivery.attempts < delivery.max_attempts THEN 'retrying' ELSE 'failed' END,finished_at=$1,error_code='lease_expired',error_message='Delivery lease expired' FROM notification_deliveries AS delivery WHERE attempt.delivery_id=delivery.delivery_id AND attempt.outcome='sending' AND delivery.status='sending' AND delivery.lease_expires_at <= $1`, now)
		if err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `UPDATE notification_deliveries SET status=CASE WHEN attempts < max_attempts THEN 'retrying' ELSE 'failed' END,locked_by=NULL,lease_expires_at=NULL,updated_at=$1 WHERE status='sending' AND lease_expires_at <= $1`, now)
		if err != nil {
			return err
		}
		var d Delivery
		var actionType, actionResourceID, actionRoute sql.NullString
		err = tx.QueryRowContext(ctx, `SELECT delivery.delivery_id,delivery.notification_id,COALESCE(delivery.recipient_id::text,''),delivery.project_id,delivery.channel_key,delivery.target_key,delivery.rule_version,delivery.settings_version,delivery.delivery_key,delivery.status,delivery.attempts,delivery.max_attempts,COALESCE(delivery.provider_message_id,''),delivery.last_error_code,delivery.last_error_message,COALESCE(delivery.response_summary,''),delivery.available_at,delivery.delivered_at,delivery.created_at,delivery.updated_at,notification.type_key,notification.template_version,notification.source_event_id,COALESCE(notification.project_id::text,''),COALESCE(notification.actor_id::text,''),notification.resource_type,notification.resource_id,notification.priority,notification.data,notification.action_type,notification.action_resource_id,notification.action_route,notification.occurred_at,notification.created_at FROM notification_deliveries delivery JOIN notification_notifications notification USING(notification_id) WHERE delivery.status IN ('pending','retrying') AND delivery.available_at <= $1 ORDER BY delivery.available_at,delivery.created_at,delivery.delivery_id FOR UPDATE OF delivery SKIP LOCKED LIMIT 1`, now).Scan(&d.ID, &d.NotificationID, &d.RecipientID, &d.ProjectID, &d.ChannelKey, &d.TargetKey, &d.RuleVersion, &d.SettingsVersion, &d.DeliveryKey, &d.Status, &d.Attempts, &d.MaxAttempts, &d.ProviderMessage, &d.LastErrorCode, &d.LastError, &d.ResponseSummary, &d.AvailableAt, &d.DeliveredAt, &d.CreatedAt, &d.UpdatedAt, &notification.TypeKey, &notification.TemplateVersion, &notification.SourceEventID, &notification.ProjectID, &notification.ActorID, &notification.ResourceType, &notification.ResourceID, &notification.Priority, &data, &actionType, &actionResourceID, &actionRoute, &notification.OccurredAt, &notification.CreatedAt)
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		expires := now.Add(lease)
		if _, err = tx.ExecContext(ctx, `UPDATE notification_deliveries SET status='sending',attempts=attempts+1,locked_by=$2,lease_expires_at=$3,updated_at=$1 WHERE delivery_id=$4`, now, owner, expires, d.ID); err != nil {
			return err
		}
		attemptID, err := store.Generator.New()
		if err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO notification_delivery_attempts(attempt_id,delivery_id,attempt_number,started_at,outcome) VALUES($1,$2,$3,$4,'sending')`, attemptID, d.ID, d.Attempts+1, now); err != nil {
			return err
		}
		if err = json.Unmarshal(data, &notification.Data); err != nil {
			return err
		}
		if actionType.Valid && actionResourceID.Valid {
			notification.Action = &Action{Type: actionType.String, ResourceID: actionResourceID.String}
			if actionRoute.Valid {
				notification.Action.Route = actionRoute.String
			}
		}
		d.Status = "sending"
		d.Attempts++
		delivery = &d
		return nil
	})
	if err != nil || delivery == nil {
		return delivery, notification, err
	}
	return delivery, notification, nil
}

func (store PostgresStore) CompleteDelivery(ctx context.Context, deliveryID, owner string) error {
	if store.Transaction.DB == nil {
		return ErrNotReady
	}
	now := store.now()
	return store.Transaction.Within(ctx, nil, func(tx transaction.Tx) error {
		result, err := tx.ExecContext(ctx, `UPDATE notification_deliveries SET status='delivered',delivered_at=$1,locked_by=NULL,lease_expires_at=NULL,updated_at=$1 WHERE delivery_id=$2 AND status='sending' AND locked_by=$3`, now, deliveryID, owner)
		if err != nil {
			return err
		}
		if affected, affectedErr := result.RowsAffected(); affectedErr != nil {
			return affectedErr
		} else if affected == 0 {
			return ErrNotFound
		}
		_, err = tx.ExecContext(ctx, `UPDATE notification_delivery_attempts SET outcome='delivered',finished_at=$1 WHERE delivery_id=$2 AND outcome='sending'`, now, deliveryID)
		return err
	})
}

func (store PostgresStore) FailDelivery(ctx context.Context, deliveryID, owner, code string, providerStatus int, message string, retryable bool, retryAfter time.Duration) error {
	if store.Transaction.DB == nil {
		return ErrNotReady
	}
	now := store.now()
	next := now
	if retryable {
		if retryAfter <= 0 {
			retryAfter = time.Minute
		}
		next = now.Add(retryAfter)
	}
	if code == "" {
		code = "provider_error"
	}
	return store.Transaction.Within(ctx, nil, func(tx transaction.Tx) error {
		var status string
		err := tx.QueryRowContext(ctx, `UPDATE notification_deliveries SET status=CASE WHEN $1 AND attempts < max_attempts THEN 'retrying' ELSE 'failed' END,next_retry_at=$2,available_at=$2,locked_by=NULL,lease_expires_at=NULL,last_error_code=$3,last_error_message=$4,updated_at=$5 WHERE delivery_id=$6 AND status='sending' AND locked_by=$7 RETURNING status`, retryable, next, code, safeDeliveryError(message), now, deliveryID, owner).Scan(&status)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return ErrNotFound
			}
			return err
		}
		_, err = tx.ExecContext(ctx, `UPDATE notification_delivery_attempts SET outcome=$1,finished_at=$2,error_code=$3,error_message=$4,provider_status=NULLIF($5,0) WHERE delivery_id=$6 AND outcome='sending'`, status, now, code, safeDeliveryError(message), providerStatus, deliveryID)
		return err
	})
}

func (store PostgresStore) CancelPending(ctx context.Context, projectID, channelKey string) error {
	_, err := store.DB.ExecContext(ctx, `UPDATE notification_deliveries SET status='cancelled',updated_at=NOW(),last_error_code='channel_disabled',last_error_message='Notification channel is disabled' WHERE project_id=$1 AND channel_key=$2 AND status IN ('pending','retrying')`, projectID, channelKey)
	return err
}

func (store PostgresStore) CancelDelivery(ctx context.Context, deliveryID, owner, message string) error {
	if store.Transaction.DB == nil {
		return ErrNotReady
	}
	now := store.now()
	return store.Transaction.Within(ctx, nil, func(tx transaction.Tx) error {
		result, err := tx.ExecContext(ctx, `UPDATE notification_deliveries SET status='cancelled',locked_by=NULL,lease_expires_at=NULL,last_error_code='channel_disabled',last_error_message=$1,updated_at=$2 WHERE delivery_id=$3 AND status='sending' AND locked_by=$4`, safeDeliveryError(message), now, deliveryID, owner)
		if err != nil {
			return err
		}
		if affected, affectedErr := result.RowsAffected(); affectedErr != nil {
			return affectedErr
		} else if affected == 0 {
			return ErrNotFound
		}
		_, err = tx.ExecContext(ctx, `UPDATE notification_delivery_attempts SET outcome='cancelled',finished_at=$1,error_code='channel_disabled',error_message=$2 WHERE delivery_id=$3 AND outcome='sending'`, now, safeDeliveryError(message), deliveryID)
		return err
	})
}

func safeDeliveryError(message string) string {
	message = strings.TrimSpace(message)
	if len(message) > 500 {
		message = message[:500]
	}
	return message
}

func scanInbox(scan func(...interface{}) error) (InboxItem, error) {
	var item InboxItem
	var notificationData []byte
	var renderedSnapshot []byte
	var actionType, actionResourceID, actionRoute sql.NullString
	err := scan(&item.ID, &item.ReadState, &item.ArchivedAt, &item.Outcome, &item.CreatedAt, &item.UpdatedAt, &item.Notification.ID, &item.Notification.TypeKey, &item.Notification.TemplateVersion, &item.Notification.SourceEventID, &item.Notification.ProjectID, &item.Notification.ActorID, &item.Notification.ResourceType, &item.Notification.ResourceID, &item.Notification.Priority, &notificationData, &renderedSnapshot, &actionType, &actionResourceID, &actionRoute, &item.Notification.OccurredAt, &item.Notification.CreatedAt, &item.Recipient.ID, &item.Recipient.NotificationID, &item.Recipient.RecipientKey, &item.Recipient.UserID, &item.Recipient.NormalizedEmail, &item.Recipient.ExpiresAt)
	if err != nil {
		return InboxItem{}, err
	}
	if err = json.Unmarshal(notificationData, &item.Notification.Data); err != nil {
		return InboxItem{}, err
	}
	if err = json.Unmarshal(renderedSnapshot, &item.Notification.RenderedSnapshot); err != nil {
		return InboxItem{}, err
	}
	if actionType.Valid && actionResourceID.Valid {
		item.Notification.Action = &Action{Type: actionType.String, ResourceID: actionResourceID.String}
		if actionRoute.Valid {
			item.Notification.Action.Route = actionRoute.String
		}
	}
	return item, nil
}

func decodeCursor(value string) (string, string, error) {
	if value == "" {
		return "", "", nil
	}
	cursor, err := pagination.Decode(value)
	if err != nil {
		return "", "", err
	}
	if _, err := time.Parse(time.RFC3339Nano, cursor.SortValue); err != nil {
		return "", "", err
	}
	return cursor.SortValue, cursor.ID, nil
}

func nonNilRuleChannelKeys(values []string) []string {
	if values == nil {
		return []string{}
	}
	return values
}

func decodeRuleChannelKeys(raw []byte) ([]string, error) {
	if len(raw) == 0 {
		return []string{}, nil
	}
	var values []string
	if err := json.Unmarshal(raw, &values); err != nil {
		return nil, fmt.Errorf("decode notification rule channel keys: %w", err)
	}
	return nonNilRuleChannelKeys(values), nil
}
