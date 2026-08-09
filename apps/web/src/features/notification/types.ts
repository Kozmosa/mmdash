export type InboxAction = {
  action_type: string;
  action_resource_id: string;
  route?: string;
};

export type InboxNotification = {
  notification_id: string;
  type_key: string;
  template_version: number;
  source_event_id: string;
  project_id?: string;
  resource_type: string;
  resource_id: string;
  priority: "low" | "normal" | "high" | "urgent";
  data: Record<string, unknown>;
  rendered_snapshot?: Record<string, unknown>;
  action?: InboxAction;
  occurred_at: string;
  created_at: string;
};

export type InboxItem = {
  inbox_item_id: string;
  notification: InboxNotification;
  read_state: "read" | "unread";
  archived_at?: string;
  outcome: "active" | "resolved" | "revoked" | "expired";
  created_at: string;
  updated_at: string;
};

export type InboxPage = {
  items: InboxItem[];
  has_more: boolean;
  next_cursor?: string;
};

export type InboxListQuery = {
  archived: "true" | "false";
  cursor?: string;
  limit?: number;
  occurred_from?: string;
  occurred_to?: string;
  outcome_group?: "processed";
  project_id?: string;
  read_state?: "read" | "unread";
  type_key?: string;
};

export type ProjectOption = {
  id: string;
  name: string;
};

export type NotificationRule = {
  project_id: string;
  type_key: string;
  external_enabled: boolean;
  channel_keys: string[];
  minimum_priority: "low" | "normal" | "high" | "urgent";
  version: number;
};
