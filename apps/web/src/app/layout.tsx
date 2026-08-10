import type { Metadata } from "next";
import type { ReactNode } from "react";

import { AppProviders } from "@/components/providers/app-providers";
import { UserProvider } from "@/components/providers/user-provider";

import "katex/dist/katex.min.css";
import "./styles.css";

export const metadata: Metadata = {
  title: "mmdash",
  description: "Mathematical modeling and research workspace",
};

export default function RootLayout({
  children,
}: Readonly<{ children: ReactNode }>) {
  return (
    <html lang="zh-CN">
      <body>
        <AppProviders>
          <UserProvider>{children}</UserProvider>
        </AppProviders>
      </body>
    </html>
  );
}
