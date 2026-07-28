"use client";

import { useEffect, useState } from "react";

type ExampleResponse = {
  status: string;
  storage: string;
  checked_at: string;
};

export default function EngineeringBaselinePage() {
  const [result, setResult] = useState<ExampleResponse | null>(null);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    const controller = new AbortController();

    fetch("/api/example", { signal: controller.signal })
      .then(async (response) => {
        if (!response.ok) {
          throw new Error(`HTTP ${response.status}`);
        }
        return (await response.json()) as ExampleResponse;
      })
      .then(setResult)
      .catch((reason: unknown) => {
        if (!controller.signal.aborted) {
          setError(reason instanceof Error ? reason.message : "Unknown error");
        }
      });

    return () => controller.abort();
  }, []);

  return (
    <main>
      <p className="eyebrow">mmdash engineering baseline</p>
      <h1>Monorepo 示例调用链</h1>
      <p>Web → BFF → Core → PostgreSQL</p>
      <output aria-live="polite">
        {error
          ? `连接失败：${error}`
          : result
            ? `状态：${result.status}；存储：${result.storage}`
            : "正在检查调用链…"}
      </output>
    </main>
  );
}
