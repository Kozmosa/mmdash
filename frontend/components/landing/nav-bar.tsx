"use client";

import { ArrowRight, Moon, Sun } from "lucide-react";
import { useTheme } from "next-themes";
import { LogoMark } from "./logo-mark";

const appUrl = process.env.NEXT_PUBLIC_APP_URL ?? "/auth/login";

type Locale = "zh" | "en";

interface Copy {
  nav: {
    features: string; showcase: string; scenarios: string;
    pricing: string; docs: string; signIn: string;
    startShort: string; start: string; languageTarget: string;
    languageLabel: string; themeToDark: string; themeToLight: string;
  };
}

export function NavBar({
  t,
  locale,
  onToggleLocale,
}: {
  t: Copy;
  locale: Locale;
  onToggleLocale: () => void;
}) {
  const { theme, setTheme } = useTheme();
  const ThemeIcon = theme === "light" ? Moon : Sun;
  const themeLabel = theme === "light" ? t.nav.themeToDark : t.nav.themeToLight;

  return (
    <header className="fixed left-0 right-0 top-4 z-50 px-4">
      <nav className="mx-auto flex h-[52px] w-full max-w-5xl items-center justify-between rounded-full px-4 shadow-[0_1px_2px_var(--color-foreground)_3%] backdrop-blur-xl sm:px-5 xl:max-w-6xl bg-card/86 border">
        <a className="flex min-w-0 items-center gap-3 font-bold tracking-tight" href="#">
          <LogoMark />
          <span className="text-lg">mmdash</span>
        </a>
        <div className="hidden items-center gap-8 text-sm font-medium lg:flex text-muted-foreground">
          <a href="#features">{t.nav.features}</a>
          <a href="#showcase">{t.nav.showcase}</a>
          <a href="#scenarios">{t.nav.scenarios}</a>
          <a href="#pricing">{t.nav.pricing}</a>
          <a href="#docs">{t.nav.docs}</a>
        </div>
        <div className="flex shrink-0 items-center gap-2">
          <button
            className="hidden cursor-pointer items-center rounded-full px-2 text-sm font-medium transition-opacity hover:opacity-80 sm:inline-flex text-muted-foreground"
            type="button"
            onClick={onToggleLocale}
            aria-label={t.nav.languageLabel}
          >
            {t.nav.languageTarget}
          </button>
          <button
            className="hidden h-9 w-9 cursor-pointer items-center justify-center rounded-full transition-opacity hover:opacity-80 sm:flex"
            type="button"
            onClick={() => setTheme(theme === "light" ? "dark" : "light")}
            aria-label={themeLabel}
            title={themeLabel}
          >
            <ThemeIcon className="h-4 w-4 text-muted-foreground" />
          </button>
          <a
            className="hidden h-9 items-center rounded-full px-4 text-sm font-medium transition-colors sm:flex border bg-card"
            href={appUrl}
          >
            {t.nav.signIn}
          </a>
          <a
            className="inline-flex h-10 shrink-0 items-center gap-2 rounded-full px-3 text-sm font-semibold text-primary-foreground shadow-[0_8px_20px_var(--color-primary)_28%] transition-transform hover:-translate-y-0.5 max-[430px]:h-9 max-[430px]:w-9 max-[430px]:justify-center max-[430px]:px-0 sm:px-5 bg-primary"
            href={appUrl}
          >
            <span className="max-[430px]:hidden sm:hidden">{t.nav.startShort}</span>
            <span className="hidden sm:inline">{t.nav.start}</span>
            <ArrowRight className="h-4 w-4" />
          </a>
        </div>
      </nav>
    </header>
  );
}
