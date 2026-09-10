"use client";

import { useEffect, useState } from "react";

type PDFReaderProps = Readonly<{
  className?: string;
  title: string;
  transfer?: {
    headers: Record<string, string>;
    url: string;
  };
}>;

function isAbortError(reason: unknown): boolean {
  if (
    typeof reason === "object" &&
    reason !== null &&
    "name" in reason &&
    reason.name === "AbortError"
  ) {
    return true;
  }
  return reason instanceof Error && /aborted/i.test(reason.message);
}

export function PDFReader({ className, title, transfer }: PDFReaderProps) {
  const [objectURL, setObjectURL] = useState<string>();
  const [error, setError] = useState<string>();
  const headersKey = JSON.stringify(transfer?.headers ?? {});

  useEffect(() => {
    let active = true;
    let nextObjectURL = "";
    const controller = new AbortController();
    const dispose = () => {
      active = false;
      if (!controller.signal.aborted) {
        try {
          controller.abort("PDFReader effect disposed");
        } catch {
          // Cleanup must not turn a normal unmount into a runtime error.
        }
      }
      if (nextObjectURL) URL.revokeObjectURL(nextObjectURL);
    };
    setObjectURL(undefined);
    setError(undefined);
    if (!transfer) {
      return dispose;
    }
    void fetch(transfer.url, {
      headers: transfer.headers,
      signal: controller.signal,
    })
      .then(async (response) => {
        if (!response.ok)
          throw new Error(`PDF 读取失败（HTTP ${response.status}）`);
        return URL.createObjectURL(await response.blob());
      })
      .then((url) => {
        if (!active) {
          URL.revokeObjectURL(url);
          return;
        }
        nextObjectURL = url;
        setObjectURL(url);
      })
      .catch((reason: unknown) => {
        if (active && !isAbortError(reason))
          setError(reason instanceof Error ? reason.message : "PDF 读取失败");
      });
    return dispose;
  }, [headersKey, transfer?.url]);

  if (error)
    return (
      <p className={className} role="alert">
        {error}
      </p>
    );
  if (!objectURL)
    return (
      <div className={className} role="status">
        正在读取 PDF 预览…
      </div>
    );
  return (
    <iframe
      className={className}
      src={`${objectURL}#page=1&zoom=page-width`}
      title={title}
    />
  );
}
