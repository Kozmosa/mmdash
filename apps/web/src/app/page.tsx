import Link from "next/link";

export default function RootPage() {
  return (
    <main className="min-h-screen bg-background">
      <section className="mx-auto flex min-h-screen w-full max-w-6xl flex-col justify-center px-6 py-20 lg:px-10">
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
