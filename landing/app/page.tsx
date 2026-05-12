"use client";

import { useEffect, useState } from "react";
import {
  ArrowRight,
  BarChart3,
  BookOpenText,
  Braces,
  Check,
  ChevronDown,
  CircleDot,
  FileText,
  GitBranch,
  Moon,
  Play,
  Sparkles,
  Sun,
  Users,
} from "lucide-react";
import { motion } from "framer-motion";

const appUrl = process.env.NEXT_PUBLIC_APP_URL ?? "/auth/login";
const themeStorageKey = "mmdash-landing-theme";

type Locale = "zh" | "en";
type ThemeMode = "light" | "dark";

const copy = {
  zh: {
    nav: {
      features: "功能",
      showcase: "展示",
      scenarios: "场景",
      pricing: "价格",
      docs: "文档",
      signIn: "登录",
      startShort: "开始",
      start: "免费开始",
      languageTarget: "EN",
      languageLabel: "Switch to English",
      themeToDark: "切换到深色模式",
      themeToLight: "切换到浅色模式",
    },
    hero: {
      titleLine1: "从建模混乱",
      titleLine2: "到高质量提交。",
      subtitle: "把队友协作、证据资料、模型文档、版本记录和 AI 分析放进一个安静清晰的数学建模工作台。",
      primary: "免费开始",
      secondary: "观看演示",
      tags: ["数学建模团队", "模型文档", "版本追踪"],
      scroll: "滚动到功能区",
      mockupTitle: "建模工作台",
    },
    video: {
      title: "面向建模团队的冷静中枢",
      subtitle: "不用在聊天记录、零散 Markdown 和过期文档里翻找，也能看清项目当前状态。",
      pulse: "项目脉搏",
      preview: "实时预览",
      cards: ["问题界定", "模型演进", "提交状态"],
    },
    visuals: {
      evidence: ["数据集", "模型假设", "关键公式"],
      outlineTitle: "文档大纲",
      outline: ["摘要", "模型假设", "优化模型", "灵敏度分析"],
      versionTitle: "版本时间线",
      versions: ["v1 基线模型", "v2 修正约束", "v3 最终提交"],
    },
    features: {
      title: "围绕建模流程构建",
      subtitle: "从早期假设到最终提交，mmdash 让团队推理过程始终可检查、可组织。",
      items: [
        {
          type: "evidence",
          title: "在写模型的地方收集证据。",
          subtitle: "把数据集、假设、图表、公式和参考资料放在它们支撑的决策旁边。",
          bullets: ["支持多种文档提供方", "团队级项目空间", "为模型分析快速取数"],
          reverse: false,
        },
        {
          type: "outline",
          title: "把零散推理整理成连贯论文。",
          subtitle: "Markdown 优先的写作流，对公式、表格和竞赛论文结构保持友好。",
          bullets: ["Markdown 编辑与预览", "AI 辅助符号检查", "清晰的问题到方案大纲"],
          reverse: true,
        },
        {
          type: "version",
          title: "追踪每一次关键建模决策。",
          subtitle: "提交版本、比较改动，并在深夜修改出问题时安全回滚。",
          bullets: ["版本历史", "差异与回滚预览", "时间线可视化"],
          reverse: false,
        },
      ],
    },
    showcase: {
      title: "默认就是可展示的工作台",
      subtitle: "界面保持安静、紧凑、实用，让团队可以反复扫描、比较和行动。",
      cards: [
        { title: "团队仪表盘", text: "一眼查看活跃项目、成员、文档提供方和当前建模状态。" },
        { title: "分析面板", text: "不离开文档即可检查符号、公式、结构和潜在错误。" },
        { title: "提交叙事", text: "让模型文档始终贴近支撑它的证据和决策。" },
      ],
    },
    scenarios: {
      title: "为真实建模场景设计",
      subtitle: "从课程小组到竞赛现场，同一个工作台都能承受压力。",
      items: ["国赛/美赛团队", "课程项目", "科研原型", "导师审阅", "实验室模板", "最终提交冲刺"],
    },
    pricing: {
      title: "适合建模团队的简单方案",
      subtitle: "在接入计费前，先以产品级版式展示方案能力。",
      recommended: "推荐",
      getStarted: "开始使用",
      perMonth: "/月",
      tiers: [
        { name: "免费版", price: "¥0", description: "适合体验建模工作流。", features: ["个人项目空间", "Markdown 模型文档", "基础版本历史"], highlighted: false },
        { name: "团队版", price: "¥88", description: "适合活跃竞赛团队。", features: ["共享团队项目", "文档后端集成", "AI 分析面板", "优先协作能力"], highlighted: true },
        { name: "实验室版", price: "定制", description: "适合课程和科研小组。", features: ["团队管理", "模板库", "部署支持"], highlighted: false },
      ],
    },
    cta: {
      title: "用结构化方式开始下一次数学建模。",
      subtitle: "给团队一个统一空间来写作、检查、比较，并交付最终建模论文。",
      primary: "打开 mmdash",
      secondary: "查看功能",
      chips: ["在线工作台", "Markdown 原生", "版本感知"],
    },
    footer: {
      features: "功能",
      pricing: "价格",
    },
  },
  en: {
    nav: {
      features: "Features",
      showcase: "Showcase",
      scenarios: "Scenarios",
      pricing: "Pricing",
      docs: "Docs",
      signIn: "Sign in",
      startShort: "Start",
      start: "Start for free",
      languageTarget: "中",
      languageLabel: "切换到中文",
      themeToDark: "Switch to dark mode",
      themeToLight: "Switch to light mode",
    },
    hero: {
      titleLine1: "From modeling chaos",
      titleLine2: "to winning submissions.",
      subtitle: "Coordinate teammates, evidence, model documents, versions, and AI analysis in one quiet workspace built for mathematical modeling competitions.",
      primary: "Start for free",
      secondary: "Watch demo",
      tags: ["CUMCM teams", "Model docs", "Version control"],
      scroll: "Scroll to features",
      mockupTitle: "Modeling Workspace",
    },
    video: {
      title: "A calm command center for modeling teams",
      subtitle: "See the shape of your project without digging through chat threads, scattered markdown files, and stale documents.",
      pulse: "Project pulse",
      preview: "Live preview",
      cards: ["Problem framing", "Model evolution", "Submission status"],
    },
    visuals: {
      evidence: ["Dataset", "Assumptions", "Key equations"],
      outlineTitle: "Document outline",
      outline: ["Abstract", "Model assumptions", "Optimization model", "Sensitivity analysis"],
      versionTitle: "Version timeline",
      versions: ["v1 baseline", "v2 fixed constraints", "v3 final submission"],
    },
    features: {
      title: "Built around the modeling workflow",
      subtitle: "From early assumptions to final submission, mmdash keeps the team's reasoning inspectable and organized.",
      items: [
        {
          type: "evidence",
          title: "Collect evidence where the model is written.",
          subtitle: "Keep datasets, assumptions, charts, formulas, and references close to the decisions they support.",
          bullets: ["Provider-backed documents", "Team scoped project spaces", "Fast retrieval for model analysis"],
          reverse: false,
        },
        {
          type: "outline",
          title: "Turn scattered reasoning into a coherent paper.",
          subtitle: "Use a markdown-first writing flow that stays friendly to formulas, tables, and competition-style structure.",
          bullets: ["Markdown editor and preview", "AI assisted symbol checks", "Clear problem-to-solution outline"],
          reverse: true,
        },
        {
          type: "version",
          title: "Track every important modeling decision.",
          subtitle: "Commit versions, compare changes, and roll back safely when late-night edits go sideways.",
          bullets: ["Version history", "Diff and rollback preview", "Timeline visibility"],
          reverse: false,
        },
      ],
    },
    showcase: {
      title: "Showcase-ready by default",
      subtitle: "The interface stays quiet, dense, and useful so teams can scan, compare, and act repeatedly.",
      cards: [
        { title: "Team dashboard", text: "See active projects, members, providers, and current modeling status in one scan." },
        { title: "Analysis panels", text: "Inspect symbols, formulas, structure, and likely errors without leaving the document." },
        { title: "Submission narrative", text: "Keep the model document close to the evidence and decisions that shaped it." },
      ],
    },
    scenarios: {
      title: "Designed for real modeling scenarios",
      subtitle: "From classroom teams to competition rooms, the same workspace scales with the pressure.",
      items: ["National competitions", "Course projects", "Research prototypes", "Advisor reviews", "Lab templates", "Final submission sprints"],
    },
    pricing: {
      title: "Simple plans for modeling teams",
      subtitle: "Use the pricing layout as a product-ready placeholder until billing is connected.",
      recommended: "Recommended",
      getStarted: "Get started",
      perMonth: "/mo",
      tiers: [
        { name: "Free", price: "$0", description: "For trying the modeling workflow.", features: ["Personal project workspace", "Markdown model docs", "Basic version history"], highlighted: false },
        { name: "Team", price: "$12", description: "For active competition teams.", features: ["Shared team projects", "Provider-backed documents", "AI analysis panels", "Priority collaboration tools"], highlighted: true },
        { name: "Lab", price: "Custom", description: "For classrooms and research groups.", features: ["Team administration", "Template libraries", "Deployment support"], highlighted: false },
      ],
    },
    cta: {
      title: "Start your next model with structure.",
      subtitle: "Give the team a single place to write, inspect, compare, and ship the final mathematical modeling submission.",
      primary: "Open mmdash",
      secondary: "Explore features",
      chips: ["Online workspace", "Markdown native", "Version aware"],
    },
    footer: {
      features: "Features",
      pricing: "Pricing",
    },
  },
} as const;

type Copy = (typeof copy)[Locale];

function getSystemTheme(): ThemeMode {
  if (typeof window !== "undefined" && window.matchMedia("(prefers-color-scheme: dark)").matches) {
    return "dark";
  }
  return "light";
}

const fadeInUp = {
  hidden: { opacity: 0, y: 20 },
  visible: { opacity: 1, y: 0 },
};

const stagger = {
  hidden: {},
  visible: {
    transition: {
      staggerChildren: 0.1,
    },
  },
};

function Header({
  t,
  onToggleLocale,
  onToggleTheme,
  theme,
}: {
  t: Copy;
  onToggleLocale: () => void;
  onToggleTheme: () => void;
  theme: ThemeMode;
}) {
  const ThemeIcon = theme === "light" ? Moon : Sun;
  const themeLabel = theme === "light" ? t.nav.themeToDark : t.nav.themeToLight;

  return (
    <header className="fixed left-0 right-0 top-4 z-50 px-4">
      <nav
        className="mx-auto flex h-[52px] w-full max-w-5xl items-center justify-between rounded-full px-4 shadow-[0_1px_2px_color-mix(in_oklch,var(--text-primary)_3%,transparent)] backdrop-blur-xl sm:px-5 xl:max-w-6xl"
        style={{
          backgroundColor: "color-mix(in oklch, var(--bg-surface-1) 86%, transparent)",
          border: "1px solid var(--border)",
        }}
      >
        <a className="flex min-w-0 items-center gap-3 font-bold tracking-tight" href="#">
          <LogoMark />
          <span className="text-lg">mmdash</span>
        </a>
        <div className="hidden items-center gap-8 text-sm font-medium lg:flex" style={{ color: "var(--text-secondary)" }}>
          <a href="#features">{t.nav.features}</a>
          <a href="#showcase">{t.nav.showcase}</a>
          <a href="#scenarios">{t.nav.scenarios}</a>
          <a href="#pricing">{t.nav.pricing}</a>
          <a href="#docs">{t.nav.docs}</a>
        </div>
        <div className="flex shrink-0 items-center gap-2">
          <button
            className="hidden cursor-pointer items-center rounded-full px-2 text-sm font-medium transition-opacity hover:opacity-80 sm:inline-flex"
            type="button"
            onClick={onToggleLocale}
            aria-label={t.nav.languageLabel}
            style={{ color: "var(--text-secondary)" }}
          >
            {t.nav.languageTarget}
          </button>
          <button
            className="hidden h-9 w-9 cursor-pointer items-center justify-center rounded-full transition-opacity hover:opacity-80 sm:flex"
            type="button"
            onClick={onToggleTheme}
            aria-label={themeLabel}
            title={themeLabel}
          >
            <ThemeIcon className="h-4 w-4" style={{ color: "var(--text-secondary)" }} />
          </button>
          <a
            className="hidden h-9 items-center rounded-full px-4 text-sm font-medium transition-colors sm:flex"
            href={appUrl}
            style={{ border: "1px solid var(--border)", backgroundColor: "var(--bg-surface-1)" }}
          >
            {t.nav.signIn}
          </a>
          <a
            className="inline-flex h-10 shrink-0 items-center gap-2 rounded-full px-3 text-sm font-semibold text-white shadow-[0_8px_20px_color-mix(in_oklch,oklch(var(--brand-600))_28%,transparent)] transition-transform hover:-translate-y-0.5 max-[430px]:h-9 max-[430px]:w-9 max-[430px]:justify-center max-[430px]:px-0 sm:px-5"
            href={appUrl}
            style={{ backgroundColor: "oklch(var(--brand-600))" }}
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

function LogoMark() {
  return (
    <span
      className="flex h-8 w-8 items-center justify-center rounded-full"
      style={{ border: "1px solid var(--border)", backgroundColor: "var(--bg-surface-2)" }}
    >
      <Braces className="h-4 w-4" style={{ color: "oklch(var(--brand-600))" }} />
    </span>
  );
}

function ButtonLink({
  children,
  href,
  variant = "primary",
  icon,
}: {
  children: React.ReactNode;
  href: string;
  variant?: "primary" | "secondary";
  icon?: React.ReactNode;
}) {
  const primary = variant === "primary";
  return (
    <a
      href={href}
      className="inline-flex h-[52px] w-full max-w-[316px] min-w-0 items-center justify-center gap-2 rounded-full px-7 text-base font-semibold transition-all hover:-translate-y-0.5 sm:w-auto sm:min-w-[156px]"
      style={{
        backgroundColor: primary ? "oklch(var(--brand-600))" : "var(--bg-surface-1)",
        color: primary ? "white" : "oklch(var(--brand-600))",
        border: primary ? "none" : "1px solid var(--border)",
        boxShadow: primary
          ? "0 10px 24px color-mix(in oklch, oklch(var(--brand-600)) 28%, transparent)"
          : "0 6px 16px color-mix(in oklch, var(--text-primary) 6%, transparent)",
      }}
    >
      {children}
      {icon}
    </a>
  );
}

function HeroMockup({ title }: { title: string }) {
  const cells = [
    { bar: "oklch(var(--brand-600))", width: "72%", block: false },
    { bar: "oklch(var(--sea-500))", width: "84%", block: false },
    { bar: "color-mix(in oklch, var(--text-muted) 35%, var(--bg-surface-0))", width: "62%", block: true },
    { bar: "color-mix(in oklch, var(--text-muted) 35%, var(--bg-surface-0))", width: "66%", block: true },
    { bar: "color-mix(in oklch, var(--text-muted) 35%, var(--bg-surface-0))", width: "60%", block: false },
    { bar: "color-mix(in oklch, var(--text-muted) 35%, var(--bg-surface-0))", width: "63%", block: true },
  ];

  return (
    <motion.div
      variants={fadeInUp}
      className="relative hidden items-center justify-center lg:flex"
      initial={false}
      transition={{ duration: 0.7, delay: 0.25, ease: [0.2, 0, 0.2, 1] }}
    >
      <div
        className="relative w-full max-w-[580px] overflow-hidden rounded-[20px]"
        style={{
          backgroundColor: "var(--bg-surface-1)",
          border: "1px solid var(--border)",
          boxShadow:
            "0 4px 12px color-mix(in oklch, oklch(var(--brand-600)) 5%, transparent), 0 16px 48px color-mix(in oklch, oklch(var(--brand-600)) 12%, transparent), 0 32px 80px color-mix(in oklch, oklch(var(--sea-500)) 10%, transparent)",
        }}
      >
        <div className="flex items-center gap-2.5 border-b px-6 py-4" style={{ borderColor: "var(--border)" }}>
          <div className="flex items-center gap-2">
            {["#ff5f57", "#ffbd2e", "#28ca42"].map((color) => (
              <span key={color} className="h-3.5 w-3.5 rounded-full" style={{ backgroundColor: color }} />
            ))}
          </div>
          <div className="flex-1 text-center text-[15px] font-semibold tracking-tight">{title}</div>
        </div>
        <div className="p-7 lg:p-8">
          <div className="grid grid-cols-3 gap-4 lg:gap-5">
            {cells.map((cell, index) => (
              <motion.div
                key={index}
                className="aspect-[4/3] overflow-hidden rounded-xl p-3.5"
                initial={{ opacity: 1, scale: 1, y: 0 }}
                animate={{ opacity: 1, scale: 1, y: 0 }}
                transition={{ duration: 0.45, delay: 0.45 + index * 0.06, ease: [0.2, 0, 0.2, 1] }}
                style={{ backgroundColor: "var(--bg-surface-2)", border: "1px solid var(--border)" }}
              >
                <div className="flex h-full flex-col gap-2.5">
                  <div className="h-2.5 rounded-full" style={{ width: cell.width, backgroundColor: cell.bar }} />
                  <div
                    className="h-1.5 rounded-full"
                    style={{
                      width: index % 2 ? "52%" : "44%",
                      backgroundColor: "color-mix(in oklch, var(--text-muted) 25%, var(--bg-surface-0))",
                    }}
                  />
                  {cell.block ? (
                    <div
                      className="mt-1 flex-1 rounded-md"
                      style={{ backgroundColor: "color-mix(in oklch, oklch(var(--sea-500)) 15%, var(--bg-surface-2))" }}
                    />
                  ) : null}
                </div>
              </motion.div>
            ))}
          </div>
        </div>
        <motion.div
          className="absolute right-0 top-0 h-full w-2 rounded-r-[20px]"
          initial={{ opacity: 0, scaleY: 0 }}
          animate={{ opacity: 1, scaleY: 1 }}
          transition={{ duration: 0.65, delay: 0.85, ease: [0.2, 0, 0.2, 1] }}
          style={{
            transformOrigin: "top",
            background: "linear-gradient(to bottom, oklch(var(--brand-500)), oklch(var(--sea-500)) 50%, oklch(var(--accent)) 100%)",
          }}
        />
      </div>
    </motion.div>
  );
}

function Hero({ t }: { t: Copy }) {
  return (
    <section className="relative flex min-h-screen items-center justify-center overflow-hidden pt-[max(80px,10vh)] pb-[max(120px,12vh)]">
      <div className="absolute inset-0" style={{ backgroundColor: "var(--bg-surface-0)" }} />
      <div className="noise absolute inset-0 opacity-50 mix-blend-overlay" />
      <div
        className="pointer-events-none absolute inset-0 opacity-60"
        style={{
          background:
            "radial-gradient(ellipse 1400px 900px at 75% 40%, color-mix(in oklch, oklch(var(--brand-300)) 5%, transparent), transparent), radial-gradient(ellipse 1000px 550px at 10% 90%, color-mix(in oklch, var(--accent) 4%, transparent), transparent), conic-gradient(from 135deg at 85% 15%, color-mix(in oklch, oklch(var(--sea-300)) 6%, transparent) 0deg, transparent 60deg, transparent 300deg, color-mix(in oklch, oklch(var(--brand-300)) 4%, transparent) 360deg)",
        }}
      />
      <div className="relative z-10 mx-auto w-full max-w-5xl px-4 sm:px-6 lg:px-8 xl:max-w-6xl">
        <div className="grid items-center gap-12 sm:gap-16 lg:grid-cols-[0.95fr_1.05fr] lg:gap-24">
          <motion.div className="space-y-6 sm:space-y-8 lg:pr-4" initial={false} animate="visible" variants={stagger}>
            <motion.h1
              variants={fadeInUp}
              className="display-font text-[1.8rem] font-bold leading-[1.3] tracking-normal sm:text-[2rem] md:text-[2.15rem] lg:text-[2rem] xl:text-4xl"
            >
              {t.hero.titleLine1}
              <br />
              <span
                style={{
                  background: "linear-gradient(to right, oklch(var(--brand-600)), oklch(var(--sea-500)))",
                  WebkitBackgroundClip: "text",
                  backgroundClip: "text",
                  WebkitTextFillColor: "transparent",
                }}
              >
                {t.hero.titleLine2}
              </span>
            </motion.h1>
            <motion.p variants={fadeInUp} className="max-w-[520px] text-[0.95rem] leading-[1.7] sm:text-base md:text-lg" style={{ color: "var(--text-secondary)" }}>
              {t.hero.subtitle}
            </motion.p>
            <motion.div variants={fadeInUp} className="flex flex-col items-center gap-3 pt-1 sm:flex-row sm:items-start sm:pt-2">
              <ButtonLink href={appUrl} icon={<ArrowRight className="h-4 w-4" />}>
                {t.hero.primary}
              </ButtonLink>
              <ButtonLink href="#video" variant="secondary" icon={<Play className="h-4 w-4 fill-current stroke-0" />}>
                {t.hero.secondary}
              </ButtonLink>
            </motion.div>
            <motion.div variants={fadeInUp} className="flex flex-wrap gap-2 pt-2">
              {t.hero.tags.map((tag) => (
                <span
                  key={tag}
                  className="inline-flex rounded-full px-3 py-1.5 text-xs font-medium sm:text-sm"
                  style={{ backgroundColor: "var(--bg-surface-1)", border: "1px solid var(--border)", color: "var(--text-secondary)" }}
                >
                  {tag}
                </span>
              ))}
            </motion.div>
          </motion.div>
          <HeroMockup title={t.hero.mockupTitle} />
        </div>
      </div>
      <a
        href="#features"
        aria-label={t.hero.scroll}
        className="absolute bottom-[clamp(4.5rem,10vh,7rem)] left-1/2 z-20 hidden -translate-x-1/2 rounded-full px-4 py-2 md:flex"
        style={{ backgroundColor: "color-mix(in oklch, var(--bg-surface-1) 60%, transparent)", border: "1px solid color-mix(in oklch, var(--border) 50%, transparent)" }}
      >
        <ChevronDown className="h-5 w-5" style={{ color: "var(--text-secondary)" }} />
      </a>
    </section>
  );
}

function SectionHeading({ title, subtitle }: { title: string; subtitle: React.ReactNode }) {
  return (
    <motion.div
      className="mx-auto mb-10 max-w-3xl text-center md:mb-12"
      initial="hidden"
      whileInView="visible"
      viewport={{ once: true, margin: "-80px" }}
      variants={stagger}
    >
      <motion.h2 variants={fadeInUp} className="display-font mb-4 text-3xl font-bold leading-tight md:text-4xl lg:text-5xl">
        {title}
      </motion.h2>
      <motion.p variants={fadeInUp} className="text-base leading-relaxed md:text-lg" style={{ color: "var(--text-secondary)" }}>
        {subtitle}
      </motion.p>
    </motion.div>
  );
}

function VideoSection({ t }: { t: Copy }) {
  return (
    <section id="video" className="relative px-4 pb-24 pt-20" style={{ backgroundColor: "var(--bg-surface-0)", scrollMarginTop: "80px" }}>
      <div
        className="pointer-events-none absolute inset-0 opacity-40"
        style={{ background: "radial-gradient(ellipse 1200px 800px at 50% 50%, color-mix(in oklch, oklch(var(--brand-300)) 3%, transparent), transparent)" }}
      />
      <div className="relative z-10 mx-auto max-w-5xl xl:max-w-6xl">
        <SectionHeading title={t.video.title} subtitle={t.video.subtitle} />
        <motion.div
          className="relative overflow-hidden rounded-2xl"
          initial={{ opacity: 0, y: 20 }}
          whileInView={{ opacity: 1, y: 0 }}
          viewport={{ once: true, margin: "-100px" }}
          transition={{ duration: 0.5, ease: [0.2, 0, 0.2, 1] }}
          style={{
            backgroundColor: "var(--bg-surface-1)",
            border: "1px solid var(--border)",
            boxShadow:
              "0 4px 16px color-mix(in oklch, oklch(var(--brand-600)) 8%, transparent), 0 12px 40px color-mix(in oklch, oklch(var(--brand-600)) 12%, transparent), 0 24px 64px color-mix(in oklch, oklch(var(--sea-500)) 10%, transparent)",
          }}
        >
          <div className="grid aspect-video place-items-center p-6">
            <div className="w-full max-w-4xl rounded-xl p-6 md:p-8" style={{ border: "1px solid var(--border)", backgroundColor: "var(--bg-surface-0)" }}>
              <div className="mb-5 flex items-center justify-between">
                <div className="flex items-center gap-2">
                  <CircleDot className="h-4 w-4" style={{ color: "oklch(var(--brand-600))" }} />
                  <span className="text-sm font-semibold">{t.video.pulse}</span>
                </div>
                <span className="rounded-full px-3 py-1 text-xs font-medium" style={{ backgroundColor: "var(--bg-surface-2)", color: "var(--text-muted)" }}>
                  {t.video.preview}
                </span>
              </div>
              <div className="grid gap-4 md:grid-cols-3">
                {t.video.cards.map((label, index) => (
                  <div key={label} className="rounded-xl p-4" style={{ border: "1px solid var(--border)", backgroundColor: "var(--bg-surface-1)" }}>
                    <div className="mb-4 text-sm font-semibold">{label}</div>
                    <div className="space-y-2">
                      {[80, 62, 46].map((width, line) => (
                        <div
                          key={line}
                          className="h-2 rounded-full"
                          style={{
                            width: `${width - index * 8}%`,
                            backgroundColor: line === 0 ? "oklch(var(--brand-600))" : "color-mix(in oklch, var(--text-muted) 25%, var(--bg-surface-0))",
                          }}
                        />
                      ))}
                    </div>
                  </div>
                ))}
              </div>
            </div>
          </div>
          <div className="absolute bottom-0 left-0 right-0 h-1" style={{ background: "linear-gradient(to right, oklch(var(--brand-500)), oklch(var(--sea-500)) 50%, oklch(var(--accent)) 100%)" }} />
        </motion.div>
      </div>
    </section>
  );
}

function FeatureVisual({ type, t }: { type: "evidence" | "outline" | "version"; t: Copy }) {
  if (type === "evidence") {
    return (
      <div className="grid h-full gap-3 rounded-xl p-4" style={{ border: "1px solid var(--border)", backgroundColor: "var(--bg-surface-1)" }}>
        {t.visuals.evidence.map((item, index) => (
          <div key={item} className="flex items-center gap-3 rounded-lg p-3" style={{ backgroundColor: "var(--bg-surface-0)", border: "1px solid var(--border)" }}>
            <FileText className="h-5 w-5" style={{ color: index === 0 ? "oklch(var(--brand-600))" : "oklch(var(--sea-500))" }} />
            <div className="flex-1">
              <div className="mb-2 text-sm font-semibold">{item}</div>
              <div className="h-1.5 w-4/5 rounded-full" style={{ backgroundColor: "var(--border)" }} />
            </div>
          </div>
        ))}
      </div>
    );
  }

  if (type === "outline") {
    return (
      <div className="rounded-xl p-4" style={{ border: "1px solid var(--border)", backgroundColor: "var(--bg-surface-1)" }}>
        <div className="mb-4 flex items-center justify-between">
          <span className="text-sm font-semibold">{t.visuals.outlineTitle}</span>
          <Sparkles className="h-4 w-4" style={{ color: "oklch(var(--accent))" }} />
        </div>
        <div className="space-y-3">
          {t.visuals.outline.map((item, index) => (
            <div key={item} className="flex items-center gap-3">
              <span className="grid h-6 w-6 place-items-center rounded-full text-xs font-semibold text-white" style={{ backgroundColor: index === 2 ? "oklch(var(--brand-600))" : "oklch(var(--sea-500))" }}>
                {index + 1}
              </span>
              <span className="text-sm" style={{ color: "var(--text-secondary)" }}>
                {item}
              </span>
            </div>
          ))}
        </div>
      </div>
    );
  }

  return (
    <div className="rounded-xl p-4" style={{ border: "1px solid var(--border)", backgroundColor: "var(--bg-surface-1)" }}>
      <div className="mb-4 flex items-center justify-between">
        <span className="text-sm font-semibold">{t.visuals.versionTitle}</span>
        <GitBranch className="h-4 w-4" style={{ color: "oklch(var(--brand-600))" }} />
      </div>
      <div className="space-y-4">
        {t.visuals.versions.map((item, index) => (
          <div key={item} className="flex gap-3">
            <div className="flex flex-col items-center">
              <span className="h-3 w-3 rounded-full" style={{ backgroundColor: index === 2 ? "oklch(var(--brand-600))" : "var(--border)" }} />
              {index < 2 ? <span className="h-10 w-px" style={{ backgroundColor: "var(--border)" }} /> : null}
            </div>
            <div>
              <div className="text-sm font-semibold">{item}</div>
              <div className="mt-1 h-1.5 w-32 rounded-full" style={{ backgroundColor: "var(--border)" }} />
            </div>
          </div>
        ))}
      </div>
    </div>
  );
}

function FeaturesSection({ t }: { t: Copy }) {
  return (
    <section id="features" className="relative px-4 pb-12 pt-24" style={{ backgroundColor: "var(--bg-surface-0)" }}>
      <div className="mx-auto max-w-5xl xl:max-w-6xl">
        <SectionHeading title={t.features.title} subtitle={t.features.subtitle} />
        <div className="mt-8 space-y-16 md:mt-10 md:space-y-20 lg:space-y-24">
          {t.features.items.map((item) => (
            <motion.div
              key={item.type}
              className={`grid items-center gap-8 lg:grid-cols-2 lg:gap-14 ${item.reverse ? "lg:[&>*:first-child]:order-2" : ""}`}
              initial="hidden"
              whileInView="visible"
              viewport={{ once: true, margin: "-100px" }}
              variants={stagger}
            >
              <motion.div variants={fadeInUp}>
                <h3 className="display-font mb-4 text-2xl font-bold leading-tight md:text-3xl">{item.title}</h3>
                <p className="mb-6 max-w-xl leading-relaxed" style={{ color: "var(--text-secondary)" }}>
                  {item.subtitle}
                </p>
                <div className="space-y-3">
                  {item.bullets.map((bullet) => (
                    <div key={bullet} className="flex items-center gap-3">
                      <span className="grid h-6 w-6 place-items-center rounded-full" style={{ backgroundColor: "color-mix(in oklch, oklch(var(--brand-600)) 12%, transparent)" }}>
                        <Check className="h-3.5 w-3.5" style={{ color: "oklch(var(--brand-600))" }} />
                      </span>
                      <span className="text-sm font-medium" style={{ color: "var(--text-secondary)" }}>
                        {bullet}
                      </span>
                    </div>
                  ))}
                </div>
              </motion.div>
              <motion.div variants={fadeInUp} className="min-h-[260px] rounded-2xl p-5" style={{ border: "1px solid var(--border)", backgroundColor: "var(--bg-surface-2)" }}>
                <FeatureVisual type={item.type} t={t} />
              </motion.div>
            </motion.div>
          ))}
        </div>
      </div>
    </section>
  );
}

function ShowcaseSection({ t }: { t: Copy }) {
  const cards = [
    { icon: Users, ...t.showcase.cards[0] },
    { icon: BarChart3, ...t.showcase.cards[1] },
    { icon: BookOpenText, ...t.showcase.cards[2] },
  ];

  return (
    <section id="showcase" className="px-4 py-24" style={{ backgroundColor: "var(--bg-surface-0)" }}>
      <div className="mx-auto max-w-5xl xl:max-w-6xl">
        <SectionHeading title={t.showcase.title} subtitle={t.showcase.subtitle} />
        <motion.div className="grid gap-6 md:grid-cols-3" initial="hidden" whileInView="visible" viewport={{ once: true, margin: "-80px" }} variants={stagger}>
          {cards.map((card) => (
            <motion.div
              key={card.title}
              variants={fadeInUp}
              className="rounded-2xl p-6 transition-transform hover:-translate-y-1"
              style={{ backgroundColor: "var(--bg-surface-1)", border: "1px solid var(--border)", boxShadow: "0 8px 30px color-mix(in oklch, var(--text-primary) 5%, transparent)" }}
            >
              <card.icon className="mb-8 h-6 w-6" style={{ color: "oklch(var(--brand-600))" }} />
              <h3 className="mb-3 text-xl font-semibold">{card.title}</h3>
              <p className="text-sm leading-relaxed" style={{ color: "var(--text-secondary)" }}>
                {card.text}
              </p>
            </motion.div>
          ))}
        </motion.div>
      </div>
    </section>
  );
}

function ScenariosSection({ t }: { t: Copy }) {
  return (
    <section id="scenarios" className="px-4 py-20" style={{ backgroundColor: "var(--bg-surface-0)" }}>
      <div className="mx-auto max-w-5xl xl:max-w-6xl">
        <SectionHeading title={t.scenarios.title} subtitle={t.scenarios.subtitle} />
        <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
          {t.scenarios.items.map((scenario) => (
            <div key={scenario} className="flex items-center gap-3 rounded-xl p-4" style={{ border: "1px solid var(--border)", backgroundColor: "var(--bg-surface-1)" }}>
              <CircleDot className="h-4 w-4" style={{ color: "oklch(var(--sea-500))" }} />
              <span className="text-sm font-medium">{scenario}</span>
            </div>
          ))}
        </div>
      </div>
    </section>
  );
}

function PricingSection({ t }: { t: Copy }) {
  return (
    <section id="pricing" className="px-4 py-24" style={{ backgroundColor: "var(--bg-surface-0)" }}>
      <div className="mx-auto max-w-5xl xl:max-w-6xl">
        <SectionHeading title={t.pricing.title} subtitle={t.pricing.subtitle} />
        <div className="grid gap-6 lg:grid-cols-3">
          {t.pricing.tiers.map((tier) => (
            <div
              key={tier.name}
              className="relative flex min-h-[340px] flex-col rounded-2xl p-6"
              style={{
                backgroundColor: "var(--bg-surface-1)",
                border: tier.highlighted ? "2px solid oklch(var(--brand-600))" : "1px solid color-mix(in oklch, oklch(var(--brand-400)) 25%, var(--bg-surface-2))",
                boxShadow: tier.highlighted ? "0 12px 32px color-mix(in oklch, oklch(var(--brand-600)) 14%, transparent)" : "none",
              }}
            >
              {tier.highlighted ? (
                <span className="absolute -top-3 left-6 rounded-full px-3 py-1 text-xs font-medium" style={{ backgroundColor: "oklch(var(--accent))" }}>
                  {t.pricing.recommended}
                </span>
              ) : null}
              <h3 className="mb-1 text-xl font-semibold">{tier.name}</h3>
              <p className="min-h-10 text-sm" style={{ color: "var(--text-muted)" }}>
                {tier.description}
              </p>
              <div className="my-6 flex items-baseline gap-2">
                <span className="text-4xl font-semibold">{tier.price}</span>
                {/[$¥]/.test(tier.price) ? <span style={{ color: "var(--text-muted)" }}>{t.pricing.perMonth}</span> : null}
              </div>
              <a
                href={appUrl}
                className="mb-5 inline-flex h-11 items-center justify-center rounded-xl text-sm font-semibold"
                style={{
                  backgroundColor: tier.highlighted ? "oklch(var(--brand-600))" : "var(--bg-surface-2)",
                  color: tier.highlighted ? "white" : "oklch(var(--brand-600))",
                  border: tier.highlighted ? "none" : "1px solid oklch(var(--brand-600))",
                }}
              >
                {t.pricing.getStarted}
              </a>
              <div className="space-y-2.5 border-t pt-4" style={{ borderColor: "var(--border)" }}>
                {tier.features.map((feature) => (
                  <div key={feature} className="flex items-start gap-2">
                    <Check className="mt-0.5 h-4 w-4 shrink-0" style={{ color: tier.highlighted ? "oklch(var(--brand-600))" : "var(--text-secondary)" }} />
                    <span className="text-sm leading-relaxed" style={{ color: "var(--text-secondary)" }}>
                      {feature}
                    </span>
                  </div>
                ))}
              </div>
            </div>
          ))}
        </div>
      </div>
    </section>
  );
}

function CTASection({ t }: { t: Copy }) {
  return (
    <section id="docs" className="px-4 py-20 sm:px-6" style={{ backgroundColor: "var(--bg-surface-0)" }}>
      <div className="mx-auto max-w-5xl">
        <div
          className="relative overflow-hidden rounded-3xl p-10 text-center sm:p-16 md:p-20"
          style={{
            backgroundColor: "var(--bg-surface-2)",
            border: "1px solid color-mix(in oklch, oklch(var(--brand-500)) 20%, var(--border))",
            boxShadow: "0 1px 2px color-mix(in oklch, var(--text-primary) 3%, transparent), 0 4px 12px color-mix(in oklch, var(--text-primary) 5%, transparent)",
          }}
        >
          <div
            className="pointer-events-none absolute inset-x-0 top-0 h-48 rounded-3xl"
            style={{ background: "linear-gradient(to bottom, color-mix(in oklch, oklch(var(--brand-500)) 15%, transparent) 0%, transparent 100%)" }}
          />
          <div className="relative mx-auto max-w-3xl">
            <h2 className="display-font mb-5 text-[2rem] font-semibold leading-tight sm:text-5xl">{t.cta.title}</h2>
            <p className="mx-auto mb-10 max-w-2xl text-[0.95rem] leading-relaxed sm:text-lg" style={{ color: "var(--text-secondary)" }}>
              {t.cta.subtitle}
            </p>
            <div className="flex flex-col justify-center gap-3 sm:flex-row">
              <ButtonLink href={appUrl} icon={<ArrowRight className="h-4 w-4" />}>
                {t.cta.primary}
              </ButtonLink>
              <ButtonLink href="#features" variant="secondary" icon={<Play className="h-4 w-4 fill-current stroke-0" />}>
                {t.cta.secondary}
              </ButtonLink>
            </div>
          </div>
        </div>
        <div className="mx-auto mt-10 flex max-w-5xl flex-wrap items-center justify-center gap-4 text-xs" style={{ color: "var(--text-secondary)" }}>
          {[
            [t.cta.chips[0], "oklch(var(--sea-500))"],
            [t.cta.chips[1], "oklch(var(--brand-500))"],
            [t.cta.chips[2], "oklch(var(--accent))"],
          ].map(([label, color]) => (
            <div key={label} className="flex items-center gap-2 rounded-full px-3 py-1.5" style={{ backgroundColor: "var(--bg-surface-1)", border: "1px solid var(--border)" }}>
              <span className="h-2 w-2 rounded-full" style={{ backgroundColor: color }} />
              {label}
            </div>
          ))}
        </div>
      </div>
    </section>
  );
}

function Footer({ t }: { t: Copy }) {
  return (
    <footer className="px-4 py-10" style={{ backgroundColor: "var(--bg-surface-0)", color: "var(--text-muted)" }}>
      <div className="mx-auto flex max-w-5xl flex-col items-center justify-between gap-4 border-t pt-8 text-sm sm:flex-row xl:max-w-6xl" style={{ borderColor: "var(--border)" }}>
        <div className="flex items-center gap-3">
          <LogoMark />
          <span>mmdash</span>
        </div>
        <div className="flex items-center gap-4">
          <a href="#features">{t.footer.features}</a>
          <a href="#pricing">{t.footer.pricing}</a>
          <Moon className="h-4 w-4" />
        </div>
      </div>
    </footer>
  );
}

export default function Page() {
  const [locale, setLocale] = useState<Locale>("zh");
  const [theme, setTheme] = useState<ThemeMode>("light");
  const t = copy[locale];

  useEffect(() => {
    const storedTheme = window.localStorage.getItem(themeStorageKey);
    const initialTheme = storedTheme === "light" || storedTheme === "dark" ? storedTheme : getSystemTheme();
    setTheme(initialTheme);
    document.documentElement.dataset.theme = initialTheme;

    if (storedTheme === "light" || storedTheme === "dark") {
      return;
    }

    const mediaQuery = window.matchMedia("(prefers-color-scheme: dark)");
    const handleChange = (event: MediaQueryListEvent) => {
      const nextTheme = event.matches ? "dark" : "light";
      setTheme(nextTheme);
      document.documentElement.dataset.theme = nextTheme;
    };

    mediaQuery.addEventListener("change", handleChange);
    return () => mediaQuery.removeEventListener("change", handleChange);
  }, []);

  const toggleTheme = () => {
    const nextTheme = theme === "light" ? "dark" : "light";
    setTheme(nextTheme);
    document.documentElement.dataset.theme = nextTheme;
    window.localStorage.setItem(themeStorageKey, nextTheme);
  };

  return (
    <main className="landing-theme overflow-x-hidden">
      <Header
        t={t}
        theme={theme}
        onToggleLocale={() => setLocale((current) => (current === "zh" ? "en" : "zh"))}
        onToggleTheme={toggleTheme}
      />
      <Hero t={t} />
      <VideoSection t={t} />
      <FeaturesSection t={t} />
      <ShowcaseSection t={t} />
      <ScenariosSection t={t} />
      <PricingSection t={t} />
      <CTASection t={t} />
      <Footer t={t} />
    </main>
  );
}
