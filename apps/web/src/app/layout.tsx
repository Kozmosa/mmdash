import type { Metadata } from "next";
import type { ReactNode } from "react";

import { AppProviders } from "@/components/providers/app-providers";
import { UserProvider } from "@/components/providers/user-provider";

import "./styles.css";

export const metadata: Metadata = {
  title: "mmdash",
  description: "Mathematical modeling and research workspace",
};

const bootstrapUser = {
  id: "bootstrap-user",
  displayName: "开发者",
  email: "developer@mmdash.local",
};

export default function RootLayout({
  children,
}: Readonly<{ children: ReactNode }>) {
  return (
    <html lang="zh-CN">
      <body>
        <AppProviders>
          <UserProvider user={bootstrapUser}>{children}</UserProvider>
        </AppProviders>
      </body>
    </html>
  );
}
