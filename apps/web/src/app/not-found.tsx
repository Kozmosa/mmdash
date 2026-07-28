import Link from "next/link";

import { EmptyState } from "@/components/states/empty-state";

export default function NotFound() {
  return (
    <main className="mx-auto w-full max-w-3xl p-6 lg:p-10">
      <EmptyState
        action={
          <Link
            className="text-sm font-medium underline underline-offset-4"
            href="/projects"
          >
            返回项目列表
          </Link>
        }
        description="请求的页面不存在，或尚未由功能模块注册。"
        title="没有找到页面"
      />
    </main>
  );
}
