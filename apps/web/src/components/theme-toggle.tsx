"use client";

import { useEffect, useState } from "react";
import { Moon, Sun } from "lucide-react";

export function ThemeToggle() {
  const [isDark, setIsDark] = useState(false);

  useEffect(() => {
    setIsDark(document.documentElement.classList.contains("dark"));
    function handleThemeChange() {
      setIsDark(document.documentElement.classList.contains("dark"));
    }
    window.addEventListener("theme-change", handleThemeChange);
    return () => {
      window.removeEventListener("theme-change", handleThemeChange);
    };
  }, []);

  return (
    <button
      aria-label={isDark ? "切换为日间模式" : "切换为黑夜模式"}
      className="inline-flex size-9 items-center justify-center rounded-md border border-border bg-background text-muted-foreground hover:bg-muted hover:text-foreground transition-colors cursor-pointer"
      onClick={() => {
        const nextDark = !document.documentElement.classList.contains("dark");
        if (nextDark) {
          document.documentElement.classList.add("dark");
          localStorage.setItem("theme", "dark");
        } else {
          document.documentElement.classList.remove("dark");
          localStorage.setItem("theme", "light");
        }
        window.dispatchEvent(new Event("theme-change"));
      }}
      type="button"
    >
      {isDark ? (
        <Sun className="size-4 text-amber-500" />
      ) : (
        <Moon className="size-4 text-slate-500" />
      )}
    </button>
  );
}
