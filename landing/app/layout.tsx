import type { Metadata } from "next";
import "./globals.css";

export const metadata: Metadata = {
  title: "mmdash - 数学建模协作平台",
  description:
    "面向数学建模竞赛的协作平台，统一管理团队、模型文档、证据资料、版本与提交。",
};

export default function RootLayout({
  children,
}: Readonly<{
  children: React.ReactNode;
}>) {
  return (
    <html lang="zh-CN" suppressHydrationWarning>
      <body suppressHydrationWarning>{children}</body>
    </html>
  );
}
