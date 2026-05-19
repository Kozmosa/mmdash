"use client";

import { Braces } from "lucide-react";

export function LogoMark() {
  return (
    <span className="flex h-8 w-8 items-center justify-center rounded-full border bg-muted">
      <Braces className="h-4 w-4 text-primary" />
    </span>
  );
}
