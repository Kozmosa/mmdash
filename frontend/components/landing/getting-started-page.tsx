"use client";

import { useEffect, useRef, useState } from "react";
import { motion, useScroll, useTransform } from "framer-motion";
import {
  FolderPlus,
  FileSearch,
  PenLine,
  Users,
  GitCompare,
  ArrowRight,
  Moon,
  Sun,
  Braces,
  Sparkles,
  BookOpen,
  BarChart3,
  Target,
  Trophy,
} from "lucide-react";
import { useTheme } from "next-themes";
import { ButtonLink } from "./button-link";

const appUrl = process.env.NEXT_PUBLIC_APP_URL ?? "/auth/login";

type Locale = "zh" | "en";

const copy = {
  zh: {
    nav: {
      backToHome: "返回首页",
      signIn: "登录",
      start: "免费开始",
      languageTarget: "EN",
      languageLabel: "Switch to English",
      themeToDark: "切换到深色模式",
      themeToLight: "切换到浅色模式",
    },
    hero: {
      title: "欢迎使用 mmdash",
      subtitle: "五步开始你的数学建模之旅。",
    },
    steps: [
      {
        number: "01",
        title: "创建工作空间",
        description:
          "注册后即可获得个人项目空间。为每一次建模创建独立项目，所有文档、数据和笔记自动聚合在一起。",
        tip: null,
      },
      {
        number: "02",
        title: "收集证据资料",
        description:
          "上传数据集、论文、参考文档，或连接你的文献管理器。所有资料紧贴模型决策，随时可查。",
        tip: "支持 Zotero 同步导入",
      },
      {
        number: "03",
        title: "用结构化方式写作",
        description:
          "Markdown 编辑器原生支持公式、表格和竞赛论文结构。AI 辅助检查符号一致性，让你专注于推理本身。",
        tip: null,
      },
      {
        number: "04",
        title: "与队友实时协作",
        description:
          "在同一个项目空间里分工协作。每个人都能看到最新进展、评论和改动，无需在聊天记录里翻找。",
        tip: null,
      },
      {
        number: "05",
        title: "版本追踪与提交",
        description:
          "每一次重大改动都有版本记录。比较差异、安全回滚，最终以干净的版本提交竞赛论文。",
        tip: "支持导出为竞赛论文格式",
      },
    ],
    cta: {
      title: "准备好开始了吗？",
      subtitle: "创建你的第一个建模项目，体验从混乱到有序的全过程。",
      primary: "打开 mmdash",
    },
  },
  en: {
    nav: {
      backToHome: "Home",
      signIn: "Sign in",
      start: "Start for free",
      languageTarget: "中",
      languageLabel: "切换到中文",
      themeToDark: "Switch to dark mode",
      themeToLight: "Switch to light mode",
    },
    hero: {
      title: "Welcome to mmdash",
      subtitle: "Five steps to start your mathematical modeling journey.",
    },
    steps: [
      {
        number: "01",
        title: "Create your workspace",
        description:
          "Sign up and get a personal project space. Create a dedicated project for each modeling challenge — documents, data, and notes automatically aggregate in one place.",
        tip: null,
      },
      {
        number: "02",
        title: "Gather your evidence",
        description:
          "Upload datasets, papers, and references — or connect your reference manager. Everything stays close to the model decisions it supports.",
        tip: "Zotero sync supported",
      },
      {
        number: "03",
        title: "Write with structure",
        description:
          "The Markdown editor natively supports formulas, tables, and competition-style structure. AI-assisted symbol checks let you focus on reasoning.",
        tip: null,
      },
      {
        number: "04",
        title: "Collaborate in real time",
        description:
          "Work together in the same project space. Everyone sees the latest progress, comments, and changes — no more digging through chat history.",
        tip: null,
      },
      {
        number: "05",
        title: "Track versions & submit",
        description:
          "Every major change is versioned. Compare diffs, roll back safely, and submit a clean final paper.",
        tip: "Export to competition paper format",
      },
    ],
    cta: {
      title: "Ready to get started?",
      subtitle: "Create your first modeling project and experience the journey from chaos to clarity.",
      primary: "Open mmdash",
    },
  },
} as const;

/* ── small reused pieces ── */

function StepIcon({ index }: { index: number }) {
  const icons = [FolderPlus, FileSearch, PenLine, Users, GitCompare];
  const Icon = icons[index] ?? FolderPlus;
  return (
    <div className="flex h-14 w-14 items-center justify-center rounded-2xl bg-primary/10">
      <Icon className="h-7 w-7 text-primary" />
    </div>
  );
}

const gradients = [
  "from-violet-500/20 via-purple-500/10 to-transparent",
  "from-cyan-500/20 via-sky-500/10 to-transparent",
  "from-amber-500/20 via-orange-500/10 to-transparent",
  "from-emerald-500/20 via-teal-500/10 to-transparent",
  "from-rose-500/20 via-pink-500/10 to-transparent",
];

/* mockup illustrations per step */

function StepMockup({ index }: { index: number }) {
  if (index === 0) {
    return (
      <div className="rounded-2xl bg-card border p-6 space-y-4">
        <div className="flex items-center gap-3">
          <div className="h-10 w-10 rounded-xl bg-primary/10 flex items-center justify-center">
            <FolderPlus className="h-5 w-5 text-primary" />
          </div>
          <div>
            <div className="h-3 w-32 rounded bg-foreground/80" />
            <div className="mt-1.5 h-2 w-20 rounded bg-muted-foreground/30" />
          </div>
        </div>
        <div className="space-y-2">
          {["国赛 2025 · A题", "美赛 2025 · C题", "课程项目 · 优化模型"].map(
            (name) => (
              <div
                key={name}
                className="flex items-center gap-3 rounded-lg bg-muted/60 px-4 py-2.5"
              >
                <div className="h-2 w-2 rounded-full bg-emerald-500" />
                <span className="text-sm text-muted-foreground">{name}</span>
              </div>
            ),
          )}
        </div>
      </div>
    );
  }

  if (index === 1) {
    return (
      <div className="rounded-2xl bg-card border p-6 space-y-3">
        <div className="flex items-center gap-2 text-sm font-medium">
          <BookOpen className="h-4 w-4 text-primary" />
          <span className="text-muted-foreground">参考文献</span>
        </div>
        {[
          { title: "Multi-objective optimization", tag: "PDF" },
          { title: "Sensitivity analysis methods", tag: "DOI" },
          { title: "Statistical modeling survey", tag: "PDF" },
        ].map((ref) => (
          <div
            key={ref.title}
            className="flex items-center justify-between rounded-lg bg-muted/60 px-4 py-2.5"
          >
            <span className="text-sm">{ref.title}</span>
            <span className="text-xs text-muted-foreground">{ref.tag}</span>
          </div>
        ))}
        <div className="flex items-center gap-2 rounded-lg bg-primary/5 px-4 py-2.5">
          <Sparkles className="h-3.5 w-3.5 text-primary" />
          <span className="text-xs text-muted-foreground">Zotero 同步已启用</span>
        </div>
      </div>
    );
  }

  if (index === 2) {
    return (
      <div className="rounded-2xl bg-card border p-6 space-y-3">
        <div className="flex gap-2">
          {["摘要", "假设", "模型", "分析"].map((tab, i) => (
            <span
              key={tab}
              className={`rounded-md px-3 py-1 text-xs font-medium ${
                i === 2
                  ? "bg-primary text-primary-foreground"
                  : "bg-muted text-muted-foreground"
              }`}
            >
              {tab}
            </span>
          ))}
        </div>
        <div className="space-y-2 rounded-lg bg-muted/40 p-4 font-mono text-xs leading-relaxed">
          <div className="text-muted-foreground">{"## 优化模型"}</div>
          <div>
            <span className="text-foreground">{"min "}</span>
            <span className="text-violet-500">{"Z"}</span>
            <span className="text-foreground">{" = "}</span>
            <span className="text-cyan-500">{"w₁"}</span>
            <span className="text-foreground">{"f₁(x) + "}</span>
            <span className="text-cyan-500">{"w₂"}</span>
            <span className="text-foreground">{"f₂(x)"}</span>
          </div>
          <div className="text-muted-foreground">{"s.t.  Ax ≤ b,  x ≥ 0"}</div>
        </div>
      </div>
    );
  }

  if (index === 3) {
    return (
      <div className="rounded-2xl bg-card border p-6 space-y-3">
        <div className="flex items-center gap-2">
          <Users className="h-4 w-4 text-primary" />
          <span className="text-sm font-medium">团队空间</span>
        </div>
        <div className="space-y-2">
          {[
            { name: "张同学", action: "更新了模型假设", time: "2 分钟前" },
            { name: "李同学", action: "上传了数据集 v3", time: "15 分钟前" },
            { name: "王同学", action: "评论了灵敏度分析", time: "1 小时前" },
          ].map((item) => (
            <div
              key={item.name}
              className="flex items-start gap-3 rounded-lg bg-muted/60 px-4 py-2.5"
            >
              <div className="mt-0.5 h-6 w-6 shrink-0 rounded-full bg-primary/20 flex items-center justify-center text-xs font-bold">
                {item.name[0]}
              </div>
              <div className="min-w-0">
                <p className="text-sm">
                  <span className="font-medium">{item.name}</span>{" "}
                  <span className="text-muted-foreground">{item.action}</span>
                </p>
                <p className="text-xs text-muted-foreground">{item.time}</p>
              </div>
            </div>
          ))}
        </div>
      </div>
    );
  }

  return (
    <div className="rounded-2xl bg-card border p-6 space-y-3">
      <div className="flex items-center gap-2">
        <BarChart3 className="h-4 w-4 text-primary" />
        <span className="text-sm font-medium">版本历史</span>
      </div>
      <div className="relative space-y-0 pl-6">
        <div className="absolute left-[11px] top-2 bottom-2 w-px bg-border" />
        {[
          { label: "v3 最终提交", tag: "当前", color: "bg-emerald-500" },
          { label: "v2 修正约束条件", tag: "已对比", color: "bg-cyan-500" },
          { label: "v1 基线模型", tag: "已归档", color: "bg-muted-foreground/30" },
        ].map((v, i) => (
          <div key={v.label} className="relative flex items-center gap-3 py-3">
            <div
              className={`absolute -left-6 h-3 w-3 rounded-full border-2 border-card ${v.color}`}
            />
            <div className="flex flex-1 items-center justify-between">
              <span className="text-sm">{v.label}</span>
              <span className="text-xs text-muted-foreground">{v.tag}</span>
            </div>
          </div>
        ))}
      </div>
      <div className="flex items-center gap-2 rounded-lg bg-primary/5 px-4 py-2.5">
        <Target className="h-3.5 w-3.5 text-primary" />
        <span className="text-xs text-muted-foreground">竞赛论文格式导出就绪</span>
      </div>
    </div>
  );
}

/* ── main page ── */

export function GettingStartedPage() {
  const [locale, setLocale] = useState<Locale>("zh");
  const { theme, setTheme } = useTheme();

  useEffect(() => {
    const stored = localStorage.getItem("mmdash-locale");
    if (stored === "en") setLocale("en");
  }, []);

  const toggleLocale = () => {
    const next = locale === "zh" ? "en" : "zh";
    setLocale(next);
    localStorage.setItem("mmdash-locale", next);
  };

  const t = copy[locale];

  return (
    <div className="min-h-screen bg-background text-foreground">
      {/* ── nav ── */}
      <header className="fixed left-0 right-0 top-4 z-50 px-4">
        <nav className="mx-auto flex h-[52px] w-full max-w-5xl items-center justify-between rounded-full px-4 shadow-[0_1px_2px_var(--color-foreground)_3%] backdrop-blur-xl sm:px-5 xl:max-w-6xl bg-card/86 border">
          <a
            className="flex min-w-0 items-center gap-3 font-bold tracking-tight"
            href="/"
          >
            <span className="flex h-8 w-8 items-center justify-center rounded-full border bg-muted">
              <Braces className="h-4 w-4 text-primary" />
            </span>
            <span className="text-lg">mmdash</span>
          </a>

          <div className="flex shrink-0 items-center gap-2">
            <button
              className="hidden cursor-pointer items-center rounded-full px-2 text-sm font-medium transition-opacity hover:opacity-80 sm:inline-flex text-muted-foreground"
              type="button"
              onClick={toggleLocale}
              aria-label={t.nav.languageLabel}
            >
              {t.nav.languageTarget}
            </button>
            <button
              className="hidden h-9 w-9 cursor-pointer items-center justify-center rounded-full transition-opacity hover:opacity-80 sm:flex"
              type="button"
              onClick={() =>
                setTheme(theme === "light" ? "dark" : "light")
              }
              aria-label={
                theme === "light" ? t.nav.themeToDark : t.nav.themeToLight
              }
              title={
                theme === "light" ? t.nav.themeToDark : t.nav.themeToLight
              }
            >
              {theme === "light" ? (
                <Moon className="h-4 w-4 text-muted-foreground" />
              ) : (
                <Sun className="h-4 w-4 text-muted-foreground" />
              )}
            </button>
            <a
              className="hidden h-9 items-center rounded-full px-4 text-sm font-medium transition-colors sm:flex border bg-card"
              href={appUrl}
            >
              {t.nav.signIn}
            </a>
            <a
              className="inline-flex h-10 shrink-0 items-center gap-2 rounded-full px-5 text-sm font-semibold text-primary-foreground shadow-[0_8px_20px_var(--color-primary)_28%] transition-transform hover:-translate-y-0.5 sm:h-9 bg-primary"
              href={appUrl}
            >
              {t.nav.start}
              <ArrowRight className="h-4 w-4" />
            </a>
          </div>
        </nav>
      </header>

      {/* ── hero ── */}
      <section className="flex min-h-[70vh] flex-col items-center justify-center px-5 pt-32 pb-16 text-center">
        <motion.div
          initial={{ opacity: 0, y: 30 }}
          animate={{ opacity: 1, y: 0 }}
          transition={{ duration: 0.7, ease: [0.2, 0, 0.2, 1] }}
          className="flex flex-col items-center gap-6"
        >
          <div className="flex h-20 w-20 items-center justify-center rounded-3xl bg-primary/10">
            <Braces className="h-10 w-10 text-primary" />
          </div>
          <h1 className="text-4xl font-bold tracking-tight sm:text-5xl lg:text-6xl">
            {t.hero.title}
          </h1>
          <p className="max-w-lg text-lg leading-relaxed text-muted-foreground">
            {t.hero.subtitle}
          </p>
          <div className="mt-4 flex items-center gap-1.5 text-sm text-muted-foreground">
            <ArrowRight className="h-4 w-4 animate-bounce" />
          </div>
        </motion.div>
      </section>

      {/* ── steps ── */}
      <section className="pb-24">
        <div className="mx-auto max-w-5xl px-5 xl:max-w-6xl">
          {t.steps.map((step, i) => {
            const isEven = i % 2 === 0;
            return (
              <StepRow
                key={step.number}
                step={step}
                index={i}
                isEven={isEven}
              />
            );
          })}
        </div>
      </section>

      {/* ── cta ── */}
      <section className="py-24 sm:py-32">
        <div className="mx-auto max-w-5xl px-5 xl:max-w-6xl">
          <motion.div
            initial={{ opacity: 0, y: 24 }}
            whileInView={{ opacity: 1, y: 0 }}
            viewport={{ once: true, margin: "-80px" }}
            transition={{ duration: 0.6, ease: [0.2, 0, 0.2, 1] }}
            className="flex flex-col items-center gap-6 rounded-3xl border bg-card p-12 text-center shadow-[0_4px_24px_var(--color-foreground)_6%]"
          >
            <Trophy className="h-10 w-10 text-primary" />
            <h2 className="text-3xl font-bold tracking-tight sm:text-4xl">
              {t.cta.title}
            </h2>
            <p className="max-w-md text-muted-foreground">
              {t.cta.subtitle}
            </p>
            <ButtonLink href={appUrl} icon={<ArrowRight className="h-4 w-4" />}>
              {t.cta.primary}
            </ButtonLink>
          </motion.div>
        </div>
      </section>

      {/* ── footer ── */}
      <footer className="border-t py-8">
        <div className="mx-auto flex max-w-5xl items-center justify-between px-5 text-sm text-muted-foreground xl:max-w-6xl">
          <a href="/" className="flex items-center gap-2 font-semibold text-foreground">
            <Braces className="h-4 w-4" />
            mmdash
          </a>
          <span>© {new Date().getFullYear()}</span>
        </div>
      </footer>
    </div>
  );
}

/* ── step row with scroll-linked progress ring ── */

function StepRow({
  step,
  index,
  isEven,
}: {
  step: { number: string; title: string; description: string; tip: string | null };
  index: number;
  isEven: boolean;
}) {
  const ref = useRef<HTMLDivElement>(null);
  const { scrollYProgress } = useScroll({
    target: ref,
    offset: ["start end", "end start"],
  });
  const dashOffset = useTransform(scrollYProgress, [0, 0.5], [106, 0]);
  const circumference = 106; // 2 * PI * 17

  return (
    <motion.div
      ref={ref}
      initial={{ opacity: 0, y: 40 }}
      whileInView={{ opacity: 1, y: 0 }}
      viewport={{ once: true, margin: "-60px" }}
      transition={{
        duration: 0.6,
        delay: 0.1,
        ease: [0.2, 0, 0.2, 1],
      }}
      className="relative mb-24 last:mb-0"
    >
      {/* gradient background */}
      <div
        className={`absolute -top-8 -bottom-8 -z-10 w-full rounded-3xl bg-gradient-to-b ${gradients[index]} opacity-60`}
      />

      <div
        className={`flex flex-col gap-10 lg:flex-row lg:items-center ${
          isEven ? "" : "lg:flex-row-reverse"
        }`}
      >
        {/* text side */}
        <div className="flex-1 space-y-4">
          <div className="flex items-center gap-4">
            {/* progress ring + number */}
            <div className="relative flex h-12 w-12 shrink-0 items-center justify-center">
              <svg
                viewBox="0 0 36 36"
                className="absolute h-12 w-12 -rotate-90"
              >
                <circle
                  cx="18"
                  cy="18"
                  r="17"
                  fill="none"
                  stroke="transparent"
                  strokeWidth="2"
                />
                <motion.circle
                  cx="18"
                  cy="18"
                  r="17"
                  fill="none"
                  stroke="currentColor"
                  strokeWidth="2"
                  strokeLinecap="round"
                  strokeDasharray={`${circumference}px`}
                  style={{ strokeDashoffset: dashOffset }}
                  className="text-primary"
                />
              </svg>
              <span className="text-sm font-bold text-primary">
                {step.number}
              </span>
            </div>
            <StepIcon index={index} />
          </div>

          <h2 className="text-2xl font-bold tracking-tight sm:text-3xl">
            {step.title}
          </h2>
          <p className="max-w-md text-base leading-relaxed text-muted-foreground">
            {step.description}
          </p>

          {step.tip && (
            <div className="inline-flex items-center gap-2 rounded-full bg-primary/5 px-4 py-2 text-sm text-muted-foreground">
              <Sparkles className="h-3.5 w-3.5 text-primary" />
              {step.tip}
            </div>
          )}
        </div>

        {/* mockup side */}
        <div className="flex-1">
          <StepMockup index={index} />
        </div>
      </div>
    </motion.div>
  );
}
