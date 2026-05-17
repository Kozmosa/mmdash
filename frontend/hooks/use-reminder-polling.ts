"use client";

import { useEffect, useRef } from "react";
import { toast } from "sonner";
import { reminderApi } from "@/lib/api";

const POLL_INTERVAL = 30_000;

function notify(title: string, body: string) {
  if (typeof document !== "undefined" && document.visibilityState === "hidden") {
    if (typeof window !== "undefined" && "Notification" in window) {
      if (Notification.permission === "granted") {
        new Notification(title, { body, icon: "/favicon.ico" });
        return;
      }
    }
  }
  toast(title, { description: body });
}

export function useReminderPolling(projectId: string | null) {
  const pollingRef = useRef<ReturnType<typeof setInterval> | null>(null);

  useEffect(() => {
    if (!projectId) return;

    const poll = async () => {
      try {
        const { events, todos } = await reminderApi.getPending(projectId);

        const eventIds: string[] = [];
        for (const e of events) {
          notify("日程提醒", `${e.title} 即将开始`);
          eventIds.push(e.id);
        }

        const todoIds: string[] = [];
        for (const t of todos) {
          const label = t.due_date
            ? `截止: ${new Date(t.due_date).toLocaleDateString("zh-CN")}`
            : "";
          notify("待办提醒", `${t.content}${label ? " — " + label : ""}`);
          todoIds.push(t.id);
        }

        if (eventIds.length > 0) await reminderApi.ack(projectId, "event", eventIds);
        if (todoIds.length > 0) await reminderApi.ack(projectId, "todo", todoIds);
      } catch {
        // Silently retry next interval
      }
    };

    poll();
    pollingRef.current = setInterval(poll, POLL_INTERVAL);

    return () => {
      if (pollingRef.current) clearInterval(pollingRef.current);
    };
  }, [projectId]);
}
