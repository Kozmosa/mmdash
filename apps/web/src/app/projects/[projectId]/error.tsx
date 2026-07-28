"use client";

import { ErrorState } from "@/components/states/error-state";

export default function WorkspaceError({
  error,
  reset,
}: Readonly<{ error: Error & { digest?: string }; reset: () => void }>) {
  return (
    <ErrorState
      description={
        error.digest
          ? `项目内容加载失败。错误标识：${error.digest}`
          : "项目内容加载失败，请重试。"
      }
      onRetry={reset}
      title="无法加载项目"
    />
  );
}
