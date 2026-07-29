"use client";

import Link from "next/link";
import { useMutation, useQuery } from "@tanstack/react-query";
import { useRouter, useSearchParams } from "next/navigation";
import { Suspense } from "react";
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
import { apiClient, ApiError } from "@/lib/api-client";

function InviteContent() {
  const search = useSearchParams();
  const router = useRouter();
  const token = search.get("token") ?? "";
  const preview = useQuery({
    queryKey: ["invitation", token],
    enabled: Boolean(token),
    retry: false,
    queryFn: () =>
      apiClient.request<{ project_name: string; email: string; role: string }>(
        "/auth/invitations/preview",
        { method: "POST", body: { token } },
      ),
  });
  const accept = useMutation({
    mutationFn: () =>
      apiClient.request("/auth/invitations/accept", {
        method: "POST",
        body: { token },
      }),
    onSuccess: () => {
      toast.success("已加入项目");
      router.replace("/projects");
    },
  });
  return (
    <main className="grid min-h-screen place-items-center bg-muted/30 p-6">
      <Card className="w-full max-w-md">
        <CardHeader>
          <CardTitle>项目邀请</CardTitle>
          <CardDescription>
            {preview.data
              ? `${preview.data.email} 被邀请以 ${preview.data.role} 身份加入 ${preview.data.project_name}`
              : "正在验证邀请…"}
          </CardDescription>
        </CardHeader>
        <CardContent>
          {preview.error ? (
            <p className="text-sm text-destructive">{preview.error.message}</p>
          ) : null}
        </CardContent>
        <CardFooter className="gap-2">
          <Button
            disabled={!preview.data || accept.isPending}
            onClick={() => accept.mutate()}
          >
            接受邀请
          </Button>
          <Link
            className="inline-flex h-9 items-center rounded-md border border-border px-4 text-sm font-medium"
            href={`/register?token=${encodeURIComponent(token)}`}
          >
            注册并接受
          </Link>
          {accept.error instanceof ApiError && accept.error.status === 401 ? (
            <Link
              className="px-3 text-sm"
              href={`/login?returnTo=${encodeURIComponent(`/invite?token=${token}`)}`}
            >
              先登录
            </Link>
          ) : null}
        </CardFooter>
      </Card>
    </main>
  );
}
export default function InvitePage() {
  return (
    <Suspense>
      <InviteContent />
    </Suspense>
  );
}
