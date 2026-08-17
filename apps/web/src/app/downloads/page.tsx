import Link from "next/link";

import { DownloadCenter } from "@/features/experiment/download-center";

export default function DownloadsPage() {
  return (
    <div className="min-h-screen bg-muted/20">
      <header className="border-b border-border bg-background">
        <div className="mx-auto flex h-16 w-full max-w-6xl items-center justify-between px-6 lg:px-10">
          <Link className="font-semibold tracking-tight" href="/">
            mmdash
          </Link>
          <nav aria-label="账户" className="flex items-center gap-2">
            <Link
              className="inline-flex h-9 items-center rounded-md px-4 text-sm font-medium text-muted-foreground hover:bg-muted hover:text-foreground"
              href="/login"
            >
              Login
            </Link>
            <Link
              className="inline-flex h-9 items-center rounded-md bg-primary px-4 text-sm font-medium text-primary-foreground hover:bg-primary/90"
              href="/register"
            >
              Signup
            </Link>
          </nav>
        </div>
      </header>
      <main className="mx-auto w-full max-w-6xl p-6 lg:p-10">
        <DownloadCenter />
      </main>
    </div>
  );
}
