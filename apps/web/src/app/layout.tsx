import type { Metadata } from "next";
import type { ReactNode } from "react";

import { AppProviders } from "@/components/providers/app-providers";
import { UserProvider } from "@/components/providers/user-provider";

import "@fontsource-variable/noto-serif-sc/wght.css";
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
      <head>
        <script
          dangerouslySetInnerHTML={{
            __html: `
              try {
                var theme = localStorage.getItem('theme');
                if (theme === 'dark' || (theme !== 'light' && window.matchMedia('(prefers-color-scheme: dark)').matches)) {
                  document.documentElement.classList.add('dark');
                } else {
                  document.documentElement.classList.remove('dark');
                }
              } catch (_) {}
            `,
          }}
        />
      </head>
      <body>
        <AppProviders>
          <UserProvider>{children}</UserProvider>
        </AppProviders>
      </body>
    </html>
  );
}
