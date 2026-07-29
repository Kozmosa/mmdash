"use client";

import { useQueryClient } from "@tanstack/react-query";
import { FlaskConical } from "lucide-react";
import { useRouter } from "next/navigation";
import Link from "next/link";
import { useSearchParams } from "next/navigation";
import { type FormEvent, useState } from "react";
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

export default function LoginPage() {
  const router = useRouter();
  const queryClient = useQueryClient();
  const search = useSearchParams();
  const [submitting, setSubmitting] = useState(false);

  async function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const form = new FormData(event.currentTarget);
    setSubmitting(true);
    try {
      const result = await apiClient.request<{
        user: {
          display_name: string;
          email: string;
          id: string;
        };
      }>("/auth/login", {
        body: {
          email: String(form.get("email") ?? ""),
          password: String(form.get("password") ?? ""),
        },
        method: "POST",
      });
      queryClient.setQueryData(["current-user"], {
        displayName: result.user.display_name,
        email: result.user.email,
        id: result.user.id,
      });
      toast.success("登录成功");
      const returnTo = search.get("returnTo");
      router.replace(
        returnTo?.startsWith("/") && !returnTo.startsWith("//")
          ? returnTo
          : "/projects",
      );
      router.refresh();
    } catch (error) {
      toast.error(error instanceof Error ? error.message : "登录失败");
    } finally {
      setSubmitting(false);
    }
  }

  return (
    <main className="grid min-h-screen place-items-center bg-muted/30 p-6">
      <Card className="w-full max-w-sm">
        <CardHeader>
          <div className="mb-4 flex size-10 items-center justify-center rounded-lg bg-primary text-primary-foreground">
            <FlaskConical aria-hidden="true" className="size-5" />
          </div>
          <CardTitle>登录 mmdash</CardTitle>
          <CardDescription>进入数学建模与研究项目协作工作台</CardDescription>
        </CardHeader>
        <form onSubmit={handleSubmit}>
          <CardContent className="space-y-4">
            <label className="grid gap-2 text-sm font-medium">
              邮箱
              <Input
                autoComplete="email"
                name="email"
                placeholder="name@example.com"
                required
                type="email"
              />
            </label>
            <label className="grid gap-2 text-sm font-medium">
              密码
              <Input
                autoComplete="current-password"
                name="password"
                required
                type="password"
              />
            </label>
          </CardContent>
          <CardFooter className="flex-col gap-3">
            <Button className="w-full" disabled={submitting} type="submit">
              {submitting ? "登录中…" : "登录"}
            </Button>
            <Link
              className="text-sm text-muted-foreground hover:text-foreground"
              href="/register"
            >
              没有账号？注册
            </Link>
          </CardFooter>
        </form>
      </Card>
    </main>
  );
}
