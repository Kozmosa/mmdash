import { GlobalPageShell } from "@/components/layout/global-page-shell";
import { NotificationInboxDetail } from "@/features/notification/notification-inbox-detail";

export default async function InboxDetailPage({
  params,
}: Readonly<{ params: Promise<{ inboxItemId: string }> }>) {
  const { inboxItemId } = await params;
  return (
    <GlobalPageShell>
      <NotificationInboxDetail inboxItemId={inboxItemId} />
    </GlobalPageShell>
  );
}
