import type { ReactNode } from "react";

import { GlobalHeader } from "./global-header";

export function GlobalPageShell({
  children,
  headerActions,
}: Readonly<{ children: ReactNode; headerActions?: ReactNode }>) {
  return (
    <div className="min-h-screen bg-muted/20">
      <GlobalHeader actions={headerActions} />
      <main className="mx-auto w-full max-w-6xl p-6 lg:p-10">{children}</main>
    </div>
  );
}
