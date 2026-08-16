import { ArrowLeft, FlaskConical } from "lucide-react";
import Link from "next/link";

import { BoxManagement } from "@/features/experiment/box-management";

export default function BoxesPage() {
  return (
    <div className="min-h-screen bg-muted/20">
      <header className="border-b border-border bg-background">
        <div className="mx-auto flex h-16 w-full max-w-5xl items-center justify-between px-6 lg:px-10">
          <Link aria-label="返回项目列表" className="flex items-center gap-2.5 font-semibold tracking-tight" href="/projects"><span className="flex size-9 items-center justify-center rounded-lg bg-primary text-primary-foreground shadow-sm"><FlaskConical className="size-4" /></span><span>mmdash</span></Link>
          <Link className="inline-flex h-9 items-center justify-center gap-2 rounded-md px-4 py-2 text-sm font-medium hover:bg-accent" href="/projects"><ArrowLeft className="size-4" />返回项目</Link>
        </div>
      </header>
      <main className="mx-auto w-full max-w-5xl p-6 lg:p-10"><BoxManagement /></main>
    </div>
  );
}
