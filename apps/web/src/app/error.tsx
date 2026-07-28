"use client";

import { ErrorState } from "@/components/states/error-state";

export default function ErrorPage({
  error,
  reset,
}: Readonly<{ error: Error & { digest?: string }; reset: () => void }>) {
  return (
    <main className="mx-auto w-full max-w-7xl p-6 lg:p-10">
      <ErrorState
        description={
          error.digest
            ? `页面加载失败。错误标识：${error.digest}`
            : "页面加载失败，请重试。"
        }
        onRetry={reset}
      />
    </main>
  );
}
