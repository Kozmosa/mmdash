"use client";

import Link from "next/link";
import { useMutation, useQuery } from "@tanstack/react-query";
import { useRouter, useSearchParams } from "next/navigation";
import { Suspense, useEffect, useRef, useState } from "react";
import { toast } from "sonner";

import {
  useCurrentUser,
  useCurrentUserLoading,
} from "@/components/providers/user-provider";
import { Button } from "@/components/ui/button";
import {
  Card,
  CardContent,
  CardDescription,
  CardFooter,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
import { apiClient } from "@/lib/api-client";

function InviteContent() {
  const search = useSearchParams();
  const router = useRouter();
  const currentUser = useCurrentUser();
  const currentUserLoading = useCurrentUserLoading();
  const [declined, setDeclined] = useState(false);
  const autoAcceptAttempted = useRef(false);
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
    onError: (error) => {
      toast.error(error instanceof Error ? error.message : "接受邀请失败");
    },
  });
  const reject = useMutation({
    mutationFn: () =>
      apiClient.request("/auth/invitations/reject", {
        method: "POST",
        body: { token },
      }),
    onSuccess: () => {
      setDeclined(true);
      toast.success("已拒绝邀请");
    },
    onError: (error) => {
      toast.error(error instanceof Error ? error.message : "拒绝邀请失败");
    },
  });
  const autoAccept = search.get("autoAccept") === "1";
  useEffect(() => {
    if (
      autoAccept &&
      currentUser &&
      preview.data &&
      !autoAcceptAttempted.current
    ) {
      autoAcceptAttempted.current = true;
      accept.mutate();
    }
  }, [accept, autoAccept, currentUser, preview.data]);

  const returnTo = `/invite?token=${encodeURIComponent(token)}&autoAccept=1`;
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
          {declined ? (
            <p className="text-sm">你已拒绝该项目邀请。</p>
          ) : preview.error ? (
            <p className="text-sm text-destructive">{preview.error.message}</p>
          ) : null}
        </CardContent>
        <CardFooter className="gap-2">
          {!currentUserLoading && preview.data && !declined ? (
            <>
              {currentUser ? (
                <Button
                  disabled={accept.isPending || reject.isPending}
                  onClick={() => accept.mutate()}
                >
                  接收邀请
                </Button>
              ) : (
                <>
                  <Link
                    className="inline-flex h-9 items-center rounded-md bg-primary px-4 text-sm font-medium text-primary-foreground"
                    href={`/login?returnTo=${encodeURIComponent(returnTo)}`}
                  >
                    登录并接收
                  </Link>
                  <Link
                    className="inline-flex h-9 items-center rounded-md border border-border px-4 text-sm font-medium"
                    href={`/register?token=${encodeURIComponent(token)}`}
                  >
                    注册并接受
                  </Link>
                </>
              )}
              <Button
                disabled={accept.isPending || reject.isPending}
                onClick={() => reject.mutate()}
                variant="outline"
              >
                拒绝
              </Button>
            </>
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
