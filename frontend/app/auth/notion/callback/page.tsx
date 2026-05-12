"use client";

import { Suspense, useEffect, useState } from "react";
import { useRouter, useSearchParams } from "next/navigation";
import api from "@/lib/api";

const PENDING_PROJECT_CREATION_KEY = "pending_project_creation";

function NotionCallbackContent() {
  const router = useRouter();
  const searchParams = useSearchParams();
  const [status, setStatus] = useState("正在完成 Notion 授权...");
  const [error, setError] = useState("");

  useEffect(() => {
    const code = searchParams.get("code");
    if (!code) {
      setError("缺少授权回调 code");
      return;
    }

    let cancelled = false;

    const completeAuth = async () => {
      try {
        await api.post("/auth/provider/callback", { code });
        if (cancelled) return;
        setStatus("授权成功，正在返回项目创建流程...");
        if (sessionStorage.getItem(PENDING_PROJECT_CREATION_KEY)) {
          router.replace("/home");
          return;
        }
        router.replace("/settings");
      } catch (err: any) {
        if (cancelled) return;
        setError(err.response?.data?.detail || "Notion 授权失败");
      }
    };

    void completeAuth();

    return () => {
      cancelled = true;
    };
  }, [router, searchParams]);

  return (
    <div className="w-full max-w-md rounded-xl border bg-card p-6 shadow-sm">
      <h1 className="text-xl font-semibold">Notion 授权回调</h1>
      <p className="mt-3 text-sm text-muted-foreground">{status}</p>
      {error && <p className="mt-3 text-sm text-destructive">{error}</p>}
    </div>
  );
}

export default function NotionCallbackPage() {
  return (
    <div className="min-h-screen flex items-center justify-center bg-muted/40 p-4">
      <Suspense
        fallback={
          <div className="w-full max-w-md rounded-xl border bg-card p-6 shadow-sm">
            <h1 className="text-xl font-semibold">Notion 授权回调</h1>
            <p className="mt-3 text-sm text-muted-foreground">正在加载授权结果...</p>
          </div>
        }
      >
        <NotionCallbackContent />
      </Suspense>
    </div>
  );
}
