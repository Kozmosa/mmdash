import { apiClient } from "@/lib/api-client";

import type { InboxItem, InboxListQuery, InboxPage } from "./types";

export function listInbox(query: InboxListQuery) {
  return apiClient.request<InboxPage>("/inbox", { query });
}

export function getInboxItem(inboxItemId: string) {
  return apiClient.request<InboxItem>(
    `/inbox/${encodeURIComponent(inboxItemId)}`,
  );
}

export function updateInboxItem(
  inboxItemId: string,
  body: { archived?: boolean; read_state?: "read" | "unread" },
) {
  return apiClient.request<InboxItem>(
    `/inbox/${encodeURIComponent(inboxItemId)}`,
    { body, method: "PATCH" },
  );
}

export function markAllInboxRead(body: {
  project_id?: string;
  type_key?: string;
}) {
  return apiClient.request<void>("/inbox/mark-all-read", {
    body,
    method: "POST",
  });
}

export function acceptInboxInvitation(invitationId: string) {
  return apiClient.request(
    `/projects/invitations/${encodeURIComponent(invitationId)}/accept`,
    { body: {}, method: "POST" },
  );
}

export function inboxTitle(item: InboxItem): string {
  const title = item.notification.rendered_snapshot?.title;
  return typeof title === "string" && title.trim() ? title : "需要关注的通知";
}

export function inboxBody(item: InboxItem): string {
  const body = item.notification.rendered_snapshot?.body;
  return typeof body === "string" && body.trim()
    ? body
    : "项目中有一项需要你处理的消息。";
}
