import { RefreshCw } from "lucide-react";

export function RefreshIcon({ spinning = false }: { spinning?: boolean }) {
  return (
    <RefreshCw
      aria-hidden="true"
      className={spinning ? "size-4 animate-spin" : "size-4"}
    />
  );
}
