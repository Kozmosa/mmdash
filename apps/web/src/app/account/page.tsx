"use client";

import { useQueryClient } from "@tanstack/react-query";
import { ArrowLeft, FlaskConical, Mail, ShieldCheck } from "lucide-react";
import Link from "next/link";
import { type FormEvent, useState } from "react";
import { toast } from "sonner";

import { useCurrentUser } from "@/components/providers/user-provider";
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
import { UserAvatar } from "@/components/user-avatar";
import { apiClient } from "@/lib/api-client";

export default function AccountPage() {
  const user = useCurrentUser();
  const client = useQueryClient();
  const [saving, setSaving] = useState(false);
  const [changingPassword, setChangingPassword] = useState(false);

  async function profile(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const f = new FormData(event.currentTarget);
    setSaving(true);
    try {
      await apiClient.request("/auth/me", {
        method: "PATCH",
        body: {
          display_name: String(f.get("display_name") ?? ""),
          email: String(f.get("email") ?? ""),
          current_password: String(f.get("current_password") ?? ""),
        },
      });
      await client.invalidateQueries({ queryKey: ["current-user"] });
      toast.success("个人资料已更新");
    } catch (e) {
      toast.error(e instanceof Error ? e.message : "更新失败");
    } finally {
      setSaving(false);
    }
  }
  async function password(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const f = new FormData(event.currentTarget);
    setChangingPassword(true);
    try {
      await apiClient.request("/auth/me/password", {
        method: "POST",
        body: {
          current_password: String(f.get("current_password") ?? ""),
          new_password: String(f.get("new_password") ?? ""),
        },
      });
      toast.success("密码已更新，其他会话已退出");
      event.currentTarget.reset();
    } catch (e) {
      toast.error(e instanceof Error ? e.message : "密码更新失败");
    } finally {
      setChangingPassword(false);
    }
  }

  return (
    <div className="min-h-screen bg-muted/20">
      <header className="border-b border-border bg-background">
        <div className="mx-auto flex h-16 w-full max-w-5xl items-center justify-between px-6 lg:px-10">
          <Link
            aria-label="返回项目列表"
            className="flex items-center gap-2.5 font-semibold tracking-tight"
            href="/projects"
          >
            <span className="flex size-9 items-center justify-center rounded-lg bg-primary text-primary-foreground shadow-sm">
              <FlaskConical aria-hidden="true" className="size-4" />
            </span>
            <span>mmdash</span>
          </Link>
          <Link
            className="inline-flex h-9 items-center justify-center gap-2 rounded-md px-4 py-2 text-sm font-medium transition-colors hover:bg-accent hover:text-accent-foreground"
            href="/projects"
          >
            <ArrowLeft aria-hidden="true" className="size-4" />
            返回项目
          </Link>
        </div>
      </header>

      <main className="mx-auto w-full max-w-5xl space-y-6 p-6 lg:p-10">
        <section className="flex flex-col gap-5 rounded-2xl border border-border bg-card p-6 shadow-sm sm:flex-row sm:items-center">
          <UserAvatar
            className="size-24 rounded-2xl text-3xl"
            displayName={user?.displayName}
            email={user?.email}
          />
          <div className="min-w-0">
            <p className="text-sm font-medium text-muted-foreground">
              个人中心
            </p>
            <h1 className="mt-1 truncate text-3xl font-semibold tracking-tight">
              {user?.displayName ?? "正在加载…"}
            </h1>
            <p className="mt-2 flex items-center gap-2 text-sm text-muted-foreground">
              <Mail aria-hidden="true" className="size-4" />
              {user?.email ?? "正在读取账户信息"}
            </p>
            <p className="mt-3 text-xs text-muted-foreground">
              头像由 Gravatar 根据邮箱生成，加载失败时显示姓名首字母。
            </p>
          </div>
        </section>

        <div className="grid items-start gap-6 lg:grid-cols-5">
          <Card className="lg:col-span-3">
            <CardHeader>
              <CardTitle>个人资料</CardTitle>
              <CardDescription>
                更新你的显示名称和用于登录、生成头像的邮箱。
              </CardDescription>
            </CardHeader>
            <form
              key={`${user?.id ?? "loading"}:${user?.displayName ?? ""}:${user?.email ?? ""}`}
              onSubmit={profile}
            >
              <CardContent className="space-y-4">
                <label className="grid gap-2 text-sm font-medium">
                  显示名称
                  <Input
                    autoComplete="name"
                    defaultValue={user?.displayName}
                    maxLength={120}
                    name="display_name"
                    required
                  />
                </label>
                <label className="grid gap-2 text-sm font-medium">
                  邮箱
                  <Input
                    autoComplete="email"
                    defaultValue={user?.email}
                    name="email"
                    required
                    type="email"
                  />
                </label>
                <label className="grid gap-2 text-sm font-medium">
                  当前密码（修改邮箱时必填）
                  <Input
                    autoComplete="current-password"
                    name="current_password"
                    type="password"
                  />
                </label>
              </CardContent>
              <CardFooter>
                <Button disabled={saving} type="submit">
                  {saving ? "保存中…" : "保存资料"}
                </Button>
              </CardFooter>
            </form>
          </Card>

          <Card className="lg:col-span-2">
            <CardHeader>
              <span className="mb-2 flex size-10 items-center justify-center rounded-lg bg-secondary text-secondary-foreground">
                <ShieldCheck aria-hidden="true" className="size-5" />
              </span>
              <CardTitle>账号安全</CardTitle>
              <CardDescription>
                修改密码后，除当前浏览器外的其他会话将退出。
              </CardDescription>
            </CardHeader>
            <form onSubmit={password}>
              <CardContent className="space-y-4">
                <label className="grid gap-2 text-sm font-medium">
                  当前密码
                  <Input
                    autoComplete="current-password"
                    name="current_password"
                    required
                    type="password"
                  />
                </label>
                <label className="grid gap-2 text-sm font-medium">
                  新密码
                  <Input
                    autoComplete="new-password"
                    minLength={8}
                    name="new_password"
                    required
                    type="password"
                  />
                </label>
              </CardContent>
              <CardFooter>
                <Button disabled={changingPassword} type="submit">
                  {changingPassword ? "更新中…" : "修改密码"}
                </Button>
              </CardFooter>
            </form>
          </Card>
        </div>
      </main>
    </div>
  );
}
