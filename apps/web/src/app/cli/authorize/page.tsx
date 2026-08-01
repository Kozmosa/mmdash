"use client";

import { Laptop } from "lucide-react";
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

export default function CliAuthorizePage() {
  const search = useSearchParams();
  const [code, setCode] = useState(search.get("user_code") ?? "");
  const [decision, setDecision] = useState<"approved" | "denied" | null>(null);
  const [submitting, setSubmitting] = useState(false);

  async function decide(approve: boolean) {
    setSubmitting(true);
    try {
      await apiClient.request<void>("/auth/device/verify", {
        body: { approve, user_code: code.trim().toUpperCase() },
        method: "POST",
      });
      setDecision(approve ? "approved" : "denied");
      toast.success(approve ? "CLI 登录已授权" : "CLI 登录已拒绝");
    } catch (error) {
      toast.error(error instanceof Error ? error.message : "无法处理授权");
    } finally {
      setSubmitting(false);
    }
  }

  function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    void decide(true);
  }

  return (
    <main className="grid min-h-screen place-items-center bg-muted/30 p-6">
      <Card className="w-full max-w-md">
        <CardHeader>
          <div className="mb-4 flex size-10 items-center justify-center rounded-lg bg-primary text-primary-foreground">
            <Laptop aria-hidden="true" className="size-5" />
          </div>
          <CardTitle>授权 mmdash CLI</CardTitle>
          <CardDescription>
            只批准你刚刚在自己设备上发起的登录。CLI 将以你的项目权限访问
            mmdash。
          </CardDescription>
        </CardHeader>
        {decision ? (
          <CardContent>
            <p className="rounded-md border bg-muted/40 p-4 text-sm">
              {decision === "approved"
                ? "授权成功。你现在可以返回终端。"
                : "已拒绝此次登录请求。"}
            </p>
          </CardContent>
        ) : (
          <form onSubmit={handleSubmit}>
            <CardContent className="space-y-4">
              <label className="grid gap-2 text-sm font-medium">
                设备验证码
                <Input
                  autoCapitalize="characters"
                  autoComplete="one-time-code"
                  maxLength={9}
                  onChange={(event) => setCode(event.target.value)}
                  pattern="[A-Za-z0-9]{4}-[A-Za-z0-9]{4}"
                  placeholder="ABCD-EFGH"
                  required
                  value={code}
                />
              </label>
              <p className="text-xs text-muted-foreground">
                mmdash 不会把你的密码发送给 CLI。授权可通过退出 CLI
                或撤销会话立即失效。
              </p>
            </CardContent>
            <CardFooter className="justify-end gap-3">
              <Button
                disabled={submitting}
                onClick={() => void decide(false)}
                type="button"
                variant="outline"
              >
                拒绝
              </Button>
              <Button disabled={submitting} type="submit">
                {submitting ? "处理中…" : "授权"}
              </Button>
            </CardFooter>
          </form>
        )}
      </Card>
    </main>
  );
}
