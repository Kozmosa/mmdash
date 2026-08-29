"use client";

import { useQueryClient } from "@tanstack/react-query";
import { ArrowLeft, FlaskConical, Lock, Mail, Moon, Sun, User } from "lucide-react";
import Link from "next/link";
import { type FormEvent, useEffect, useState } from "react";
import { toast } from "sonner";

import { useCurrentUser } from "@/components/providers/user-provider";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { UserAvatar } from "@/components/user-avatar";
import { apiClient } from "@/lib/api-client";

export default function AccountPage() {
  const user = useCurrentUser();
  const client = useQueryClient();
  const [saving, setSaving] = useState(false);
  const [changingPassword, setChangingPassword] = useState(false);
  const [currentTheme, setCurrentTheme] = useState<string>("system");

  useEffect(() => {
    setCurrentTheme(localStorage.getItem("theme") ?? "system");
    
    function handleThemeChange() {
      setCurrentTheme(localStorage.getItem("theme") ?? "system");
    }
    window.addEventListener("theme-change", handleThemeChange);
    return () => {
      window.removeEventListener("theme-change", handleThemeChange);
    };
  }, []);

  function handleThemeChange(val: string) {
    setCurrentTheme(val);
    localStorage.setItem("theme", val);
    
    if (val === "dark") {
      document.documentElement.classList.add("dark");
    } else if (val === "light") {
      document.documentElement.classList.remove("dark");
    } else {
      // system theme
      const systemDark = window.matchMedia("(prefers-color-scheme: dark)").matches;
      if (systemDark) {
        document.documentElement.classList.add("dark");
      } else {
        document.documentElement.classList.remove("dark");
      }
    }
    window.dispatchEvent(new Event("theme-change"));
  }

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
    <div className="min-h-screen bg-background">
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

      <main className="mx-auto w-full max-w-5xl p-6 lg:p-10">
        <div className="flex flex-col-reverse gap-10 md:flex-row md:items-start">
          <div className="flex-1 space-y-10">
            <section className="space-y-6">
              <div className="border-b border-border pb-2">
                <h2 className="text-xl font-semibold tracking-tight text-foreground">
                  公共资料 (Public profile)
                </h2>
              </div>
              <form
                key={`${user?.id ?? "loading"}:${user?.displayName ?? ""}:${user?.email ?? ""}`}
                onSubmit={profile}
                className="space-y-4 max-w-lg"
              >
                <div className="space-y-1.5">
                  <label htmlFor="display_name" className="text-sm font-medium text-foreground">
                    显示名称 (Name)
                  </label>
                  <div className="relative">
                    <User className="absolute left-3 top-2.5 size-4 text-muted-foreground/75" />
                    <Input
                      id="display_name"
                      autoComplete="name"
                      defaultValue={user?.displayName}
                      maxLength={120}
                      name="display_name"
                      required
                      className="pl-9"
                    />
                  </div>
                  <p className="text-[11px] text-muted-foreground">
                    您的名字会显示在项目提交记录、报告作者和协同会话中。
                  </p>
                </div>

                <div className="space-y-1.5">
                  <label htmlFor="email" className="text-sm font-medium text-foreground">
                    电子邮箱 (Email)
                  </label>
                  <div className="relative">
                    <Mail className="absolute left-3 top-2.5 size-4 text-muted-foreground/75" />
                    <Input
                      id="email"
                      autoComplete="email"
                      defaultValue={user?.email}
                      name="email"
                      required
                      type="email"
                      className="pl-9"
                    />
                  </div>
                  <p className="text-[11px] text-muted-foreground">
                    主要邮箱用于接收项目邀请通知、任务队列异常告警。
                  </p>
                </div>

                <div className="space-y-1.5">
                  <label htmlFor="current_password" className="text-sm font-medium text-foreground">
                    确认当前密码 (Current password)
                  </label>
                  <div className="relative">
                    <Lock className="absolute left-3 top-2.5 size-4 text-muted-foreground/75" />
                    <Input
                      id="current_password"
                      autoComplete="current-password"
                      name="current_password"
                      type="password"
                      placeholder="修改邮箱或敏感操作时必填"
                      className="pl-9"
                    />
                  </div>
                </div>

                <div className="pt-2">
                  <Button
                    disabled={saving}
                    type="submit"
                    className="bg-primary text-primary-foreground hover:bg-primary/90 font-medium"
                  >
                    {saving ? "保存中…" : "更新资料 (Update profile)"}
                  </Button>
                </div>
              </form>
            </section>

            <section className="space-y-6 pt-4">
              <div className="border-b border-border pb-2">
                <h2 className="text-xl font-semibold tracking-tight text-foreground">
                  修改密码 (Change password)
                </h2>
              </div>
              <form onSubmit={password} className="space-y-4 max-w-lg">
                <div className="space-y-1.5">
                  <label htmlFor="sec_current_password" className="text-sm font-medium text-foreground">
                    当前密码 (Old password)
                  </label>
                  <div className="relative">
                    <Lock className="absolute left-3 top-2.5 size-4 text-muted-foreground/75" />
                    <Input
                      id="sec_current_password"
                      autoComplete="current-password"
                      name="current_password"
                      required
                      type="password"
                      className="pl-9"
                    />
                  </div>
                </div>

                <div className="space-y-1.5">
                  <label htmlFor="new_password" className="text-sm font-medium text-foreground">
                    新密码 (New password)
                  </label>
                  <div className="relative">
                    <Lock className="absolute left-3 top-2.5 size-4 text-muted-foreground/75" />
                    <Input
                      id="new_password"
                      autoComplete="new-password"
                      minLength={8}
                      name="new_password"
                      required
                      type="password"
                      placeholder="最少 8 位"
                      className="pl-9"
                    />
                  </div>
                  <p className="text-[11px] text-muted-foreground">
                    修改密码成功后，除当前设备外的其他所有活动会话都将被强制登出。
                  </p>
                </div>

                <div className="pt-2">
                  <Button
                    disabled={changingPassword}
                    type="submit"
                    variant="outline"
                    className="font-medium border-border"
                  >
                    {changingPassword ? "更新中…" : "确认修改密码"}
                  </Button>
                </div>
              </form>
            </section>

            {/* Appearance / Theme Settings Section */}
            <section className="space-y-6 pt-4">
              <div className="border-b border-border pb-2">
                <h2 className="text-xl font-semibold tracking-tight text-foreground flex items-center gap-2">
                  <Moon className="size-5 text-slate-500 hidden dark:block" />
                  <Sun className="size-5 text-amber-500 dark:hidden" />
                  外观设置 (Theme settings)
                </h2>
              </div>
              <div className="space-y-4 max-w-lg">
                <div className="space-y-1.5">
                  <label htmlFor="theme-select" className="text-sm font-medium text-foreground">
                    主题模式
                  </label>
                  <select
                    id="theme-select"
                    className="h-9 w-full max-w-xs rounded-md border border-input bg-background px-3 text-sm focus:outline-none focus:ring-1 focus:ring-ring"
                    value={currentTheme}
                    onChange={(e) => handleThemeChange(e.target.value)}
                  >
                    <option value="light">日间模式 (Light)</option>
                    <option value="dark">黑夜模式 (Dark)</option>
                    <option value="system">跟随系统 (System)</option>
                  </select>
                  <p className="text-[11px] text-muted-foreground">
                    定制您的工作空间外观，您可以在日间模式、黑夜模式或系统默认设置之间进行切换。
                  </p>
                </div>
              </div>
            </section>
          </div>

          <div className="w-full md:w-56 shrink-0 flex flex-col items-center md:items-start text-center md:text-left space-y-4">
            <UserAvatar
              className="size-48 rounded-full border border-border shadow-sm text-5xl shrink-0"
              displayName={user?.displayName}
              email={user?.email}
            />
            <div className="min-w-0 w-full">
              <h1 className="text-2xl font-bold tracking-tight text-foreground truncate">
                {user?.displayName ?? "正在加载…"}
              </h1>
              <p className="text-sm text-muted-foreground truncate mt-0.5">
                {user?.email}
              </p>
              <p className="text-xs text-muted-foreground mt-4 leading-relaxed border-t border-border/50 pt-4">
                头像由 Gravatar 服务基于邮箱自动托管。您可前往 Gravatar 更改全球通用头像。
              </p>
            </div>
          </div>
        </div>
      </main>
    </div>
  );
}
