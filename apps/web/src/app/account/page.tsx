"use client";

import { useQueryClient } from "@tanstack/react-query";
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
import { useCurrentUser } from "@/components/providers/user-provider";
import { apiClient } from "@/lib/api-client";

export default function AccountPage() {
  const user = useCurrentUser();
  const client = useQueryClient();
  const [saving, setSaving] = useState(false);
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
    }
  }
  return (
    <main className="mx-auto max-w-3xl space-y-6 p-6 lg:p-10">
      <header>
        <h1 className="text-2xl font-semibold">个人中心</h1>
        <p className="text-sm text-muted-foreground">
          管理个人资料、邮箱和账号安全
        </p>
      </header>
      <Card>
        <CardHeader>
          <CardTitle>个人资料</CardTitle>
          <CardDescription>
            头像由 Gravatar 根据邮箱生成，失败时显示默认首字母。
          </CardDescription>
        </CardHeader>
        <form onSubmit={profile}>
          <CardContent className="space-y-4">
            <label className="grid gap-2 text-sm font-medium">
              显示名称
              <Input
                defaultValue={user?.displayName}
                name="display_name"
                required
              />
            </label>
            <label className="grid gap-2 text-sm font-medium">
              邮箱
              <Input
                defaultValue={user?.email}
                name="email"
                type="email"
                required
              />
            </label>
            <label className="grid gap-2 text-sm font-medium">
              当前密码（修改邮箱时必填）
              <Input name="current_password" type="password" />
            </label>
          </CardContent>
          <CardFooter>
            <Button disabled={saving}>{saving ? "保存中…" : "保存资料"}</Button>
          </CardFooter>
        </form>
      </Card>
      <Card>
        <CardHeader>
          <CardTitle>修改密码</CardTitle>
        </CardHeader>
        <form onSubmit={password}>
          <CardContent className="grid gap-4 md:grid-cols-2">
            <label className="grid gap-2 text-sm font-medium">
              当前密码
              <Input name="current_password" type="password" required />
            </label>
            <label className="grid gap-2 text-sm font-medium">
              新密码
              <Input
                name="new_password"
                type="password"
                minLength={8}
                required
              />
            </label>
          </CardContent>
          <CardFooter>
            <Button>修改密码</Button>
          </CardFooter>
        </form>
      </Card>
    </main>
  );
}
