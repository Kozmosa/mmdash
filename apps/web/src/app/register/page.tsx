"use client";

import Link from "next/link";
import { useRouter, useSearchParams } from "next/navigation";
import { type FormEvent, Suspense, useState } from "react";
import { toast } from "sonner";

import { Button } from "@/components/ui/button";
import {
  Card,
  CardContent,
  CardDescription,
  CardFooter,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { apiClient } from "@/lib/api-client";

function RegisterForm() {
  const router = useRouter();
  const search = useSearchParams();
  const [submitting, setSubmitting] = useState(false);
  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const form = new FormData(event.currentTarget);
    setSubmitting(true);
    try {
      await apiClient.request("/auth/register", {
        method: "POST",
        body: {
          email: String(form.get("email") ?? ""),
          display_name: String(form.get("display_name") ?? ""),
          password: String(form.get("password") ?? ""),
          invitation_token: search.get("token") || undefined,
        },
      });
      toast.success("注册成功");
      router.replace(search.get("returnTo") || "/projects");
      router.refresh();
    } catch (error) {
      toast.error(error instanceof Error ? error.message : "注册失败");
    } finally {
      setSubmitting(false);
    }
  }
  return (
    <main className="grid min-h-screen place-items-center bg-muted/30 p-6">
      <Card className="w-full max-w-sm">
        <CardHeader>
          <CardTitle>创建 mmdash 账号</CardTitle>
          <CardDescription>注册后即可创建项目或接受团队邀请</CardDescription>
        </CardHeader>
        <form onSubmit={submit}>
          <CardContent className="space-y-4">
            <label className="grid gap-2 text-sm font-medium">
              显示名称
              <Input name="display_name" required />
            </label>
            <label className="grid gap-2 text-sm font-medium">
              邮箱
              <Input name="email" type="email" required />
            </label>
            <label className="grid gap-2 text-sm font-medium">
              密码
              <Input name="password" type="password" minLength={8} required />
            </label>
          </CardContent>
          <CardFooter className="flex-col gap-3">
            <Button className="w-full" disabled={submitting}>
              {submitting ? "注册中…" : "注册"}
            </Button>
            <Link
              className="text-sm text-muted-foreground hover:text-foreground"
              href="/login"
            >
              已有账号？登录
            </Link>
          </CardFooter>
        </form>
      </Card>
    </main>
  );
}

export default function RegisterPage() {
  return (
    <Suspense>
      <RegisterForm />
    </Suspense>
  );
}
