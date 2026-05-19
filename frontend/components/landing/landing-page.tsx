"use client";

import { useEffect, useState } from "react";
import { NavBar } from "./nav-bar";
import { Hero } from "./hero";
import { VideoSection } from "./video-section";
import { FeaturesSection } from "./features-section";
import { ShowcaseSection } from "./showcase-section";
import { ScenariosSection } from "./scenarios-section";
import { PricingSection } from "./pricing-section";
import { CTASection } from "./cta-section";
import { Footer } from "./footer";

type Locale = "zh" | "en";

const copy = {
  zh: {
    nav: {
      features: "功能", showcase: "展示", scenarios: "场景",
      pricing: "价格", docs: "文档", signIn: "登录",
      startShort: "开始", start: "免费开始",
      languageTarget: "EN", languageLabel: "Switch to English",
      themeToDark: "切换到深色模式", themeToLight: "切换到浅色模式",
    },
    hero: {
      titleLine1: "从建模混乱",
      titleLine2: "到高质量提交。",
      subtitle: "把队友协作、证据资料、模型文档、版本记录和 AI 分析放进一个安静清晰的数学建模工作台。",
      primary: "免费开始", secondary: "观看演示",
      tags: ["数学建模团队", "模型文档", "版本追踪"],
      scroll: "滚动到功能区", mockupTitle: "建模工作台",
    },
    video: {
      title: "面向建模团队的冷静中枢",
      subtitle: "不用在聊天记录、零散 Markdown 和过期文档里翻找，也能看清项目当前状态。",
      pulse: "项目脉搏", preview: "实时预览",
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
        { type: "evidence" as const, title: "在写模型的地方收集证据。", subtitle: "把数据集、假设、图表、公式和参考资料放在它们支撑的决策旁边。", bullets: ["支持多种文档提供方", "团队级项目空间", "为模型分析快速取数"], reverse: false },
        { type: "outline" as const, title: "把零散推理整理成连贯论文。", subtitle: "Markdown 优先的写作流，对公式、表格和竞赛论文结构保持友好。", bullets: ["Markdown 编辑与预览", "AI 辅助符号检查", "清晰的问题到方案大纲"], reverse: true },
        { type: "version" as const, title: "追踪每一次关键建模决策。", subtitle: "提交版本、比较改动，并在深夜修改出问题时安全回滚。", bullets: ["版本历史", "差异与回滚预览", "时间线可视化"], reverse: false },
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
      recommended: "推荐", getStarted: "开始使用", perMonth: "/月",
      tiers: [
        { name: "免费版", price: "¥0", description: "适合体验建模工作流。", features: ["个人项目空间", "Markdown 模型文档", "基础版本历史"], highlighted: false },
        { name: "团队版", price: "¥88", description: "适合活跃竞赛团队。", features: ["共享团队项目", "文档后端集成", "AI 分析面板", "优先协作能力"], highlighted: true },
        { name: "实验室版", price: "定制", description: "适合课程和科研小组。", features: ["团队管理", "模板库", "部署支持"], highlighted: false },
      ],
    },
    cta: {
      title: "用结构化方式开始下一次数学建模。",
      subtitle: "给团队一个统一空间来写作、检查、比较，并交付最终建模论文。",
      primary: "打开 mmdash", secondary: "查看功能",
      chips: ["在线工作台", "Markdown 原生", "版本感知"],
    },
    footer: { features: "功能", pricing: "价格" },
  },
  en: {
    nav: {
      features: "Features", showcase: "Showcase", scenarios: "Scenarios",
      pricing: "Pricing", docs: "Docs", signIn: "Sign in",
      startShort: "Start", start: "Start for free",
      languageTarget: "中", languageLabel: "切换到中文",
      themeToDark: "Switch to dark mode", themeToLight: "Switch to light mode",
    },
    hero: {
      titleLine1: "From modeling chaos",
      titleLine2: "to winning submissions.",
      subtitle: "Coordinate teammates, evidence, model documents, versions, and AI analysis in one quiet workspace built for mathematical modeling competitions.",
      primary: "Start for free", secondary: "Watch demo",
      tags: ["CUMCM teams", "Model docs", "Version control"],
      scroll: "Scroll to features", mockupTitle: "Modeling Workspace",
    },
    video: {
      title: "A calm command center for modeling teams",
      subtitle: "See the shape of your project without digging through chat threads, scattered markdown files, and stale documents.",
      pulse: "Project pulse", preview: "Live preview",
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
        { type: "evidence" as const, title: "Collect evidence where the model is written.", subtitle: "Keep datasets, assumptions, charts, formulas, and references close to the decisions they support.", bullets: ["Provider-backed documents", "Team scoped project spaces", "Fast retrieval for model analysis"], reverse: false },
        { type: "outline" as const, title: "Turn scattered reasoning into a coherent paper.", subtitle: "Use a markdown-first writing flow that stays friendly to formulas, tables, and competition-style structure.", bullets: ["Markdown editor and preview", "AI assisted symbol checks", "Clear problem-to-solution outline"], reverse: true },
        { type: "version" as const, title: "Track every important modeling decision.", subtitle: "Commit versions, compare changes, and roll back safely when late-night edits go sideways.", bullets: ["Version history", "Diff and rollback preview", "Timeline visibility"], reverse: false },
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
      recommended: "Recommended", getStarted: "Get started", perMonth: "/mo",
      tiers: [
        { name: "Free", price: "$0", description: "For trying the modeling workflow.", features: ["Personal project workspace", "Markdown model docs", "Basic version history"], highlighted: false },
        { name: "Team", price: "$12", description: "For active competition teams.", features: ["Shared team projects", "Provider-backed documents", "AI analysis panels", "Priority collaboration tools"], highlighted: true },
        { name: "Lab", price: "Custom", description: "For classrooms and research groups.", features: ["Team administration", "Template libraries", "Deployment support"], highlighted: false },
      ],
    },
    cta: {
      title: "Start your next model with structure.",
      subtitle: "Give the team a single place to write, inspect, compare, and ship the final mathematical modeling submission.",
      primary: "Open mmdash", secondary: "Explore features",
      chips: ["Online workspace", "Markdown native", "Version aware"],
    },
    footer: { features: "Features", pricing: "Pricing" },
  },
} as const;

export function LandingPage() {
  const [locale, setLocale] = useState<Locale>("zh");

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
      <NavBar t={t} locale={locale} onToggleLocale={toggleLocale} />
      <Hero t={t} />
      <VideoSection t={t} />
      <FeaturesSection t={t} />
      <ShowcaseSection t={t} />
      <ScenariosSection t={t} />
      <PricingSection t={t} />
      <CTASection t={t} />
      <Footer t={t} />
    </div>
  );
}
