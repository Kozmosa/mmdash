import { Inbox } from "lucide-react";

import { GlobalPageShell } from "@/components/layout/global-page-shell";
import { NotificationInbox } from "@/features/notification/notification-inbox";

export default function InboxPage() {
  return (
    <GlobalPageShell>
      <section aria-labelledby="inbox-title">
        <header>
          <div className="mb-3 flex size-10 items-center justify-center rounded-lg border border-border bg-card shadow-xs">
            <Inbox aria-hidden="true" className="size-5" />
          </div>
          <h1
            className="text-2xl font-semibold tracking-tight"
            id="inbox-title"
          >
            收件箱
          </h1>
          <p className="mt-1 max-w-2xl text-sm leading-6 text-muted-foreground">
            集中查看需要你关注或处理的消息。站内阅读、业务结果和外部投递状态彼此独立。
          </p>
        </header>
        <div className="mt-8">
          <NotificationInbox />
        </div>
      </section>
    </GlobalPageShell>
  );
}
