import { ArrowLeft } from "lucide-react";
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
          <Link
            className="inline-flex items-center gap-2 text-sm text-muted-foreground hover:text-foreground"
            href="/projects"
          >
            <ArrowLeft className="size-4" />
            进入工作台
          </Link>
        </div>
      </header>
      <main className="mx-auto w-full max-w-6xl p-6 lg:p-10">
        <DownloadCenter />
      </main>
    </div>
  );
}
