"use client";

import { useQuery } from "@tanstack/react-query";
import { Inbox } from "lucide-react";
import Link from "next/link";

import { apiClient } from "@/lib/api-client";

export function InboxNavLink() {
  const unread = useQuery({
    queryFn: () => apiClient.request<{ count: number }>("/inbox/unread-count"),
    queryKey: ["inbox", "unread-count"],
    refetchInterval: 30_000,
  });
  const count = unread.data?.count ?? 0;

  return (
    <Link
      aria-label={count > 0 ? `收件箱，${count} 条未读消息` : "收件箱"}
      className="relative flex size-9 items-center justify-center rounded-lg text-muted-foreground outline-none transition-colors hover:bg-muted hover:text-foreground focus-visible:ring-2 focus-visible:ring-ring"
      href="/inbox"
      title="收件箱"
    >
      <Inbox aria-hidden="true" className="size-5" />
      {count > 0 ? (
        <span className="absolute -right-1.5 -top-1.5 min-w-5 rounded-full bg-primary px-1 text-center text-[10px] font-semibold leading-5 text-primary-foreground">
          {count > 99 ? "99+" : count}
        </span>
      ) : null}
    </Link>
  );
}
