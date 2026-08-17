import Link from "next/link";

export default function RootPage() {
  return (
    <main className="min-h-screen bg-background">
      <header className="mx-auto flex h-20 w-full max-w-6xl items-center justify-between px-6 lg:px-10">
        <Link className="text-lg font-semibold tracking-tight" href="/">
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
      </header>
      <section className="mx-auto flex min-h-[calc(100vh-5rem)] w-full max-w-6xl flex-col justify-center px-6 py-20 lg:px-10">
        <p className="text-sm font-medium text-primary">mmdash</p>
        <h1 className="mt-4 max-w-3xl text-4xl font-semibold tracking-tight sm:text-6xl">
          数学建模与研究协作工作台
        </h1>
        <p className="mt-6 max-w-2xl text-lg text-muted-foreground">
          介绍性主页位置已预留。项目工作台、Box 和 CLI
          的发行入口从这里统一进入。
        </p>
        <div className="mt-10 flex flex-wrap gap-3">
          <Link
            className="inline-flex h-10 items-center rounded-md bg-primary px-5 text-sm font-medium text-primary-foreground"
            href="/projects"
          >
            进入工作台
          </Link>
          <Link
            className="inline-flex h-10 items-center rounded-md border border-border px-5 text-sm font-medium hover:bg-muted"
            href="/downloads"
          >
            下载 Box / CLI
          </Link>
        </div>
      </section>
    </main>
  );
}
