"use client";

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { type FormEvent, useState } from "react";
import { toast } from "sonner";

import { useCurrentProject } from "@/components/providers/project-provider";
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
type HumanRole = Exclude<Role, "agent" | "box">;
type InvitableHumanRole = Exclude<HumanRole, "owner">;

const assignableRoles: InvitableHumanRole[] = [
  "maintainer",
  "editor",
  "viewer",
];

export function MemberManagement() {
  const project = useCurrentProject();
  const currentUser = useCurrentUser();
  const client = useQueryClient();
  const [issuedInvitationLink, setIssuedInvitationLink] = useState("");
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
      client.invalidateQueries({ queryKey: ["project", project.id] }),
    ]);
  };
  const invite = useMutation({
    mutationFn: (input: { email: string; role: InvitableHumanRole }) =>
      apiClient.request<{ token: string }>(`${base}/invitations`, {
        method: "POST",
        body: input,
      }),
    onSuccess: async (result) => {
      setIssuedInvitationLink(
        `${location.origin}/invite?token=${encodeURIComponent(result.token)}`,
      );
      await refresh();
      toast.success("邀请已创建");
    },
    onError: (error) => {
      toast.error(errorMessage(error, "发送邀请失败"));
    },
  });
  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const f = new FormData(event.currentTarget);
    const email = String(f.get("email") ?? "").trim();
    if (
      currentUser &&
      email.toLowerCase() === currentUser.email.trim().toLowerCase()
    ) {
      toast.error("不能邀请自己");
      return;
    }
    if (
      members.data?.items.some(
        (member) =>
          member.email.trim().toLowerCase() === email.toLowerCase(),
      )
    ) {
      toast.error("该用户已是项目成员");
      return;
    }
    invite.mutate({
      email,
      role: String(f.get("role") ?? "viewer") as InvitableHumanRole,
    });
  }
  async function copyInvitationLink() {
    if (!issuedInvitationLink || !navigator.clipboard) {
      toast.error("当前浏览器无法复制邀请链接");
      return;
    }
    try {
      await navigator.clipboard.writeText(issuedInvitationLink);
      toast.success("邀请链接已复制");
    } catch (error) {
      toast.error(errorMessage(error, "复制邀请链接失败"));
    }
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
              {assignableRoles.map((r) => (
                <option key={r}>{r}</option>
              ))}
            </select>
          </CardContent>
          <CardFooter>
            <div className="w-full space-y-3">
              <Button disabled={invite.isPending} type="submit">
                {invite.isPending ? "发送中…" : "发送邀请"}
              </Button>
              {issuedInvitationLink ? (
                <div className="flex flex-col gap-2 sm:flex-row">
                  <Input
                    aria-label="邀请链接"
                    className="min-w-0 flex-1"
                    readOnly
                    value={issuedInvitationLink}
                  />
                  <Button
                    onClick={copyInvitationLink}
                    type="button"
                    variant="outline"
                  >
                    复制邀请链接
                  </Button>
                </div>
              ) : null}
            </div>
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
              {member.role === "owner" ||
              member.role === "agent" ||
              member.role === "box" ? (
                <span className="rounded-md border bg-muted px-2 py-1 text-sm">
                  {member.role}
                </span>
              ) : (
                <select
                  aria-label={`修改 ${member.display_name} 的角色`}
                  className="h-8 rounded-md border bg-background px-2 text-sm"
                  value={member.role}
                  onChange={async (event) => {
                    try {
                      await apiClient.request(
                        `${base}/members/${member.user_id}`,
                        {
                          method: "PUT",
                          body: { role: event.target.value },
                        },
                      );
                      await refresh();
                      toast.success("角色已更新");
                    } catch (error) {
                      toast.error(errorMessage(error, "更新角色失败"));
                    }
                  }}
                >
                  {assignableRoles.map((role) => (
                    <option key={role}>{role}</option>
                  ))}
                </select>
              )}
              {project.role === "owner" &&
              isTransferTarget(member.role) &&
              member.user_id !== currentUser?.id ? (
                <Button
                  variant="outline"
                  onClick={async () => {
                    if (
                      !window.confirm(
                        `确认将项目所有权转让给 ${member.display_name}？转让后你的角色将变为 maintainer。`,
                      )
                    ) {
                      return;
                    }
                    try {
                      await apiClient.request(
                        `${base}/members/${member.user_id}`,
                        {
                          method: "PUT",
                          body: { role: "owner" },
                        },
                      );
                      await refresh();
                      toast.success("项目所有权已转让");
                    } catch (error) {
                      toast.error(errorMessage(error, "转让所有权失败"));
                    }
                  }}
                >
                  转让所有权
                </Button>
              ) : null}
              {member.role !== "owner" ? (
                <Button
                  variant="ghost"
                  onClick={async () => {
                    try {
                      await apiClient.request(
                        `${base}/members/${member.user_id}`,
                        {
                          method: "DELETE",
                        },
                      );
                      await refresh();
                      toast.success("成员已移除");
                    } catch (error) {
                      toast.error(errorMessage(error, "移除成员失败"));
                    }
                  }}
                >
                  移除
                </Button>
              ) : (
                <span className="text-xs text-muted-foreground">
                  Owner 需先转让所有权
                </span>
              )}
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

function isTransferTarget(role: Role): role is InvitableHumanRole {
  return role === "maintainer" || role === "editor" || role === "viewer";
}

function errorMessage(error: unknown, fallback: string): string {
  return error instanceof Error && error.message ? error.message : fallback;
}
