"use client";

import dynamic from "next/dynamic";

import { LoadingState } from "@/components/states/loading-state";
import { cn } from "@/lib/cn";

const MonacoEditor = dynamic(() => import("@monaco-editor/react"), {
  loading: () => <LoadingState label="正在加载编辑器…" />,
  ssr: false,
});

export function CodeEditor({
  className,
  language = "text",
  onChange,
  value,
}: Readonly<{
  className?: string;
  language?: string;
  onChange?: (value: string) => void;
  value: string;
}>) {
  return (
    <div
      className={cn(
        "h-96 overflow-hidden rounded-xl border border-border",
        className,
      )}
    >
      <MonacoEditor
        language={language}
        onChange={(nextValue) => onChange?.(nextValue ?? "")}
        options={{
          automaticLayout: true,
          fontSize: 13,
          minimap: { enabled: false },
          padding: { top: 12 },
          readOnly: !onChange,
          scrollBeyondLastLine: false,
        }}
        theme="vs-dark"
        value={value}
      />
    </div>
  );
}
