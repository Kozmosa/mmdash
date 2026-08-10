import { FlaskConical } from "lucide-react";
import Link from "next/link";
import type { ReactNode } from "react";

import { UserMenu } from "@/components/user-menu";

import { InboxNavLink } from "./inbox-nav-link";

export function GlobalHeader({ actions }: Readonly<{ actions?: ReactNode }>) {
  return (
    <header className="border-b border-border bg-background">
      <div className="mx-auto flex h-16 w-full max-w-6xl items-center gap-3 px-6 lg:px-10">
        <Link
          aria-label="mmdash 项目首页"
          className="flex items-center gap-2.5 font-semibold tracking-tight"
          href="/projects"
        >
          <span className="flex size-9 items-center justify-center rounded-lg bg-primary text-primary-foreground shadow-sm">
            <FlaskConical aria-hidden="true" className="size-4" />
          </span>
          <span>mmdash</span>
        </Link>
        <div className="ml-auto flex items-center gap-2 sm:gap-3">
          {actions}
          <InboxNavLink />
          <UserMenu />
        </div>
      </div>
    </header>
  );
}
