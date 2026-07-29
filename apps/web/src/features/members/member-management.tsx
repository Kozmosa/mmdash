"use client";

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { type FormEvent } from "react";
import { toast } from "sonner";

import { useCurrentProject } from "@/components/providers/project-provider";
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

type Role = "owner" | "maintainer" | "editor" | "viewer" | "agent" | "box";
type Member = {
  user_id: string;
  email: string;
  display_name: string;
  role: Role;
  joined_at: string;
};
type Invitation = {
  id: string;
  email: string;
  role: Role;
  status: string;
  expires_at: string;
};
const roles: Role[] = [
  "owner",
  "maintainer",
  "editor",
  "viewer",
  "agent",
  "box",
];

export function MemberManagement() {
  const project = useCurrentProject();
  const client = useQueryClient();
  const base = `/projects/${encodeURIComponent(project.id)}`;
  const members = useQuery({
    queryKey: ["members", project.id],
    queryFn: () => apiClient.request<{ items: Member[] }>(`${base}/members`),
  });
  const invitations = useQuery({
    queryKey: ["invitations", project.id],
    queryFn: () =>
      apiClient.request<{ items: Invitation[] }>(`${base}/invitations`),
  });
  const refresh = async () => {
    await Promise.all([
      client.invalidateQueries({ queryKey: ["members", project.id] }),
      client.invalidateQueries({ queryKey: ["invitations", project.id] }),
    ]);
  };
  const invite = useMutation({
    mutationFn: (input: { email: string; role: Role }) =>
      apiClient.request<{ token: string }>(`${base}/invitations`, {
        method: "POST",
        body: input,
      }),
    onSuccess: async (result) => {
      await refresh();
      toast.success(
        `邀请已创建：${location.origin}/invite?token=${encodeURIComponent(result.token)}`,
      );
    },
  });
  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const f = new FormData(event.currentTarget);
    invite.mutate({
      email: String(f.get("email") ?? ""),
      role: String(f.get("role") ?? "viewer") as Role,
    });
  }
  return (
    <section className="space-y-4">
      <div>
        <h2 className="text-lg font-semibold">项目成员</h2>
        <p className="text-sm text-muted-foreground">
          通过邮箱邀请成员并管理已有成员角色。
        </p>
      </div>
      <Card>
        <form onSubmit={submit}>
          <CardHeader>
            <CardTitle>邀请成员</CardTitle>
            <CardDescription>
              邀请链接中的 Token 只在首次创建时显示。
            </CardDescription>
          </CardHeader>
          <CardContent className="grid gap-3 md:grid-cols-[1fr_180px]">
            <Input
              name="email"
              type="email"
              placeholder="member@example.com"
              required
            />
            <select
              className="h-9 rounded-md border border-border bg-background px-3 text-sm"
              name="role"
              defaultValue="viewer"
            >
              {roles.map((r) => (
                <option key={r}>{r}</option>
              ))}
            </select>
          </CardContent>
          <CardFooter>
            <Button disabled={invite.isPending}>发送邀请</Button>
          </CardFooter>
        </form>
      </Card>
      <Card>
        <CardHeader>
          <CardTitle>当前成员</CardTitle>
        </CardHeader>
        <CardContent className="space-y-3">
          {members.data?.items.map((member) => (
            <div
              className="flex flex-wrap items-center gap-3 rounded-md border p-3"
              key={member.user_id}
            >
              <div className="min-w-0 flex-1">
                <p className="font-medium">{member.display_name}</p>
                <p className="text-xs text-muted-foreground">{member.email}</p>
              </div>
              <select
                className="h-8 rounded-md border bg-background px-2 text-sm"
                value={member.role}
                onChange={async (e) => {
                  await apiClient.request(`${base}/members/${member.user_id}`, {
                    method: "PUT",
                    body: { role: e.target.value },
                  });
                  await refresh();
                  toast.success("角色已更新");
                }}
              >
                {roles.map((r) => (
                  <option key={r}>{r}</option>
                ))}
              </select>
              <Button
                variant="ghost"
                onClick={async () => {
                  await apiClient.request(`${base}/members/${member.user_id}`, {
                    method: "DELETE",
                  });
                  await refresh();
                  toast.success("成员已移除");
                }}
              >
                移除
              </Button>
            </div>
          ))}
        </CardContent>
      </Card>
      <Card>
        <CardHeader>
          <CardTitle>邀请记录</CardTitle>
        </CardHeader>
        <CardContent className="space-y-2">
          {invitations.data?.items.map((i) => (
            <div className="flex items-center gap-3 text-sm" key={i.id}>
              <span className="flex-1">
                {i.email} · {i.role} · {i.status}
              </span>
              {i.status === "pending" ? (
                <Button
                  size="sm"
                  variant="ghost"
                  onClick={async () => {
                    await apiClient.request(`${base}/invitations/${i.id}`, {
                      method: "DELETE",
                    });
                    await refresh();
                  }}
                >
                  撤销
                </Button>
              ) : null}
            </div>
          ))}
        </CardContent>
      </Card>
    </section>
  );
}
