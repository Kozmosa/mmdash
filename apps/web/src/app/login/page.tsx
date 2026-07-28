"use client";

import { FlaskConical } from "lucide-react";
import Link from "next/link";
import type { FormEvent } from "react";
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

export default function LoginPage() {
  function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    toast.info("认证能力将在 Auth 基座阶段接入。");
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
          <CardFooter className="flex-col">
            <Button className="w-full" type="submit">
              登录
            </Button>
            <Link
              className="mt-3 text-xs text-muted-foreground underline-offset-4 hover:underline"
              href="/projects"
            >
              查看工程壳
            </Link>
          </CardFooter>
        </form>
      </Card>
    </main>
  );
}
