"use client";

import { ErrorState } from "@/components/states/error-state";

export default function GlobalError({
  reset,
}: Readonly<{ error: Error & { digest?: string }; reset: () => void }>) {
  return (
    <html lang="zh-CN">
      <body>
        <main className="mx-auto w-full max-w-3xl p-6 lg:p-10">
          <ErrorState
            description="应用外壳无法继续渲染，请重新加载。"
            onRetry={reset}
            title="mmdash 遇到严重错误"
          />
        </main>
      </body>
    </html>
  );
}
