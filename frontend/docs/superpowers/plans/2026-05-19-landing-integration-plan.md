# Landing Page Integration Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Integrate the standalone landing page into the main Next.js frontend so `/` renders the landing page with login/register links pointing to existing auth routes.

**Architecture:** New route group `app/(landing)/page.tsx` owns `/`. Landing components live under `components/landing/`. Styling uses the existing shadcn/ui CSS variable system (no custom variables imported). framer-motion is added for scroll animations. i18n stays inline.

**Tech Stack:** Next.js 16, React 19, Tailwind v4, shadcn/ui (new-york), next-themes, framer-motion, lucide-react

---

## CSS Variable Mapping

Standalone landing variables → shadcn/ui equivalents:

| Old | New |
|-----|-----|
| `var(--bg-surface-0)` | `bg-background` |
| `var(--bg-surface-1)` | `bg-card` |
| `var(--bg-surface-2)` | `bg-muted` |
| `var(--text-primary)` | `text-foreground` |
| `var(--text-secondary)` | `text-muted-foreground` |
| `var(--text-muted)` | `text-muted-foreground/50` |
| `var(--border)` | `border-border` |
| `oklch(var(--brand-600))` (bg) | `bg-primary` |
| `oklch(var(--brand-600))` (text) | `text-primary` |
| `oklch(var(--brand-500))` | `bg-primary/90` |
| `oklch(var(--sea-500))` | `bg-cyan-500` |
| `var(--accent)` | `bg-accent text-accent-foreground` |
| `color-mix(..., brand 28%, transparent)` (shadow) | `shadow-primary/30` |
| `color-mix(..., 3%, transparent)` (bg) | `bg-foreground/5` |
| inline `style={{...}}` colors | Tailwind utility classes when possible |

### Task 1: Add framer-motion dependency

**Files:**
- Modify: `package.json`

- [ ] **Step 1: Install framer-motion**

```bash
cd /home/xuyang/code/mmdash/frontend && npm install framer-motion
```

- [ ] **Step 2: Verify install**

```bash
node -e "require('framer-motion')" && echo "OK"
```
Expected: `OK`

- [ ] **Step 3: Commit**

```bash
git add package.json package-lock.json
git commit -m "chore: add framer-motion dependency for landing animations"
```

---

### Task 2: Create shared primitives (LogoMark, ButtonLink, SectionHeading)

**Files:**
- Create: `components/landing/logo-mark.tsx`
- Create: `components/landing/button-link.tsx`
- Create: `components/landing/section-heading.tsx`

- [ ] **Step 1: Create LogoMark**

`components/landing/logo-mark.tsx`:

```tsx
"use client";

import { Braces } from "lucide-react";

export function LogoMark() {
  return (
    <span className="flex h-8 w-8 items-center justify-center rounded-full border bg-muted">
      <Braces className="h-4 w-4 text-primary" />
    </span>
  );
}
```

- [ ] **Step 2: Create ButtonLink**

`components/landing/button-link.tsx`:

```tsx
import Link from "next/link";
import type { ReactNode } from "react";

export function ButtonLink({
  children,
  href,
  variant = "primary",
  icon,
}: {
  children: ReactNode;
  href: string;
  variant?: "primary" | "secondary";
  icon?: ReactNode;
}) {
  const isPrimary = variant === "primary";

  return (
    <Link
      href={href}
      className={`inline-flex h-[52px] w-full max-w-[316px] min-w-0 items-center justify-center gap-2 rounded-full px-7 text-base font-semibold transition-all hover:-translate-y-0.5 sm:w-auto sm:min-w-[156px] ${
        isPrimary
          ? "bg-primary text-primary-foreground shadow-[0_10px_24px_var(--color-primary)_28%]"
          : "bg-card text-primary border shadow-[0_6px_16px_var(--color-foreground)_6%]"
      }`}
    >
      {children}
      {icon}
    </Link>
  );
}
```

- [ ] **Step 3: Create SectionHeading**

`components/landing/section-heading.tsx`:

```tsx
"use client";

import { motion } from "framer-motion";

const fadeInUp = {
  hidden: { opacity: 0, y: 20 },
  visible: { opacity: 1, y: 0 },
};

export function SectionHeading({
  title,
  subtitle,
}: {
  title: string;
  subtitle: string;
}) {
  return (
    <motion.div
      variants={fadeInUp}
      initial="hidden"
      whileInView="visible"
      viewport={{ once: true, margin: "-80px" }}
      transition={{ duration: 0.55, ease: [0.2, 0, 0.2, 1] }}
      className="mx-auto flex max-w-[640px] flex-col items-center gap-5 text-center"
    >
      <h2 className="text-4xl font-bold tracking-tight text-foreground sm:text-5xl">
        {title}
      </h2>
      <p className="text-lg leading-relaxed text-muted-foreground">
        {subtitle}
      </p>
    </motion.div>
  );
}
```

- [ ] **Step 4: Verify TypeScript compiles**

```bash
npx tsc --noEmit 2>&1 | head -20
```
Expected: no errors from new files

- [ ] **Step 5: Commit**

```bash
git add components/landing/logo-mark.tsx components/landing/button-link.tsx components/landing/section-heading.tsx
git commit -m "feat: add landing shared primitives (LogoMark, ButtonLink, SectionHeading)"
```

---

### Task 3: Create NavBar

**Files:**
- Create: `components/landing/nav-bar.tsx`

- [ ] **Step 1: Create NavBar component**

`components/landing/nav-bar.tsx`:

```tsx
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
```

- [ ] **Step 2: Verify TypeScript compiles**

```bash
npx tsc --noEmit 2>&1 | head -20
```

- [ ] **Step 3: Commit**

```bash
git add components/landing/nav-bar.tsx
git commit -m "feat: add landing NavBar component"
```

---

### Task 4: Create Hero section

**Files:**
- Create: `components/landing/hero.tsx`

- [ ] **Step 1: Create Hero component**

`components/landing/hero.tsx`:

```tsx
"use client";

import { ChevronDown, Play } from "lucide-react";
import { motion } from "framer-motion";
import { ButtonLink } from "./button-link";

const appUrl = process.env.NEXT_PUBLIC_APP_URL ?? "/auth/login";

const fadeInUp = {
  hidden: { opacity: 0, y: 20 },
  visible: { opacity: 1, y: 0 },
};

interface Copy {
  hero: {
    titleLine1: string; titleLine2: string; subtitle: string;
    primary: string; secondary: string; tags: string[];
    scroll: string; mockupTitle: string;
  };
}

function HeroMockup({ title }: { title: string }) {
  const cells = [
    { bar: "bg-primary", width: "w-[72%]", block: false },
    { bar: "bg-cyan-500", width: "w-[84%]", block: false },
    { bar: "bg-muted-foreground/20", width: "w-[62%]", block: true },
    { bar: "bg-muted-foreground/20", width: "w-[66%]", block: true },
    { bar: "bg-muted-foreground/20", width: "w-[60%]", block: false },
    { bar: "bg-muted-foreground/20", width: "w-[63%]", block: true },
  ];

  return (
    <motion.div
      variants={fadeInUp}
      className="relative hidden items-center justify-center lg:flex"
      initial={false}
      transition={{ duration: 0.7, delay: 0.25, ease: [0.2, 0, 0.2, 1] }}
    >
      <div className="relative w-full max-w-[580px] overflow-hidden rounded-[20px] bg-card border shadow-[0_4px_12px_var(--color-primary)_5%,0_16px_48px_var(--color-primary)_12%,0_32px_80px_var(--color-cyan-500)_10%]">
        <div className="flex items-center gap-2.5 border-b px-6 py-4">
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
                className="aspect-[4/3] overflow-hidden rounded-xl p-3.5 bg-muted border"
                initial={{ opacity: 1, scale: 1, y: 0 }}
                animate={{ opacity: 1, scale: 1, y: 0 }}
                transition={{ duration: 0.45, delay: 0.45 + index * 0.06, ease: [0.2, 0, 0.2, 1] }}
              >
                <div className="flex h-full flex-col gap-2.5">
                  <div className={`h-2.5 rounded-full ${cell.bar} ${cell.width}`} />
                  <div
                    className={`h-1.5 rounded-full bg-muted-foreground/15 ${index % 2 ? "w-[52%]" : "w-[44%]"}`}
                  />
                  {cell.block ? (
                    <div className="mt-1 flex-1 rounded-md bg-cyan-500/15" />
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
          style={{ transformOrigin: "top", background: "linear-gradient(to bottom, var(--color-primary), var(--color-cyan-500) 50%, var(--color-accent) 100%)" }}
        />
      </div>
    </motion.div>
  );
}

export function Hero({ t }: { t: Copy }) {
  return (
    <section className="min-h-screen flex flex-col relative overflow-hidden">
      {/* Noise overlay */}
      <div
        className="pointer-events-none absolute inset-0 z-10"
        style={{
          backgroundImage: `url("data:image/svg+xml,%3Csvg viewBox='0 0 256 256' xmlns='http://www.w3.org/2000/svg'%3E%3Cfilter id='n'%3E%3CfeTurbulence type='fractalNoise' baseFrequency='0.9' numOctaves='4' stitchTiles='stitch'/%3E%3C/filter%3E%3Crect width='100%25' height='100%25' filter='url(%23n)' opacity='0.025'/%3E%3C/svg%3E")`,
          maskImage: "radial-gradient(ellipse at 50% 0%, black 60%, transparent 100%)",
          WebkitMaskImage: "radial-gradient(ellipse at 50% 0%, black 60%, transparent 100%)",
        }}
      />
      {/* Radial gradient */}
      <div
        className="pointer-events-none absolute inset-0 z-0"
        style={{
          background: "radial-gradient(ellipse at 50% 0%, var(--color-primary) 0%, transparent 60%)",
          opacity: 0.08,
        }}
      />

      <div className="relative z-20 mx-auto flex w-full max-w-5xl flex-1 flex-col items-center justify-center px-5 pb-20 pt-44 text-center xl:max-w-6xl">
        <motion.div
          variants={fadeInUp}
          initial="hidden"
          animate="visible"
          transition={{ duration: 0.55, ease: [0.2, 0, 0.2, 1] }}
          className="flex flex-wrap items-center justify-center gap-3 mb-8"
        >
          {t.hero.tags.map((tag) => (
            <span key={tag} className="rounded-full border bg-card px-4 py-1.5 text-sm font-medium text-muted-foreground">
              {tag}
            </span>
          ))}
        </motion.div>

        <motion.h1
          variants={fadeInUp}
          initial="hidden"
          animate="visible"
          transition={{ duration: 0.55, delay: 0.1, ease: [0.2, 0, 0.2, 1] }}
          className="max-w-4xl text-5xl font-bold tracking-tight sm:text-6xl md:text-7xl font-serif"
        >
          <span className="text-foreground">{t.hero.titleLine1}</span>{" "}
          <span className="bg-gradient-to-r from-primary to-cyan-500 bg-clip-text text-transparent">
            {t.hero.titleLine2}
          </span>
        </motion.h1>

        <motion.p
          variants={fadeInUp}
          initial="hidden"
          animate="visible"
          transition={{ duration: 0.55, delay: 0.2, ease: [0.2, 0, 0.2, 1] }}
          className="mt-7 max-w-[620px] text-lg leading-relaxed text-muted-foreground sm:text-xl"
        >
          {t.hero.subtitle}
        </motion.p>

        <motion.div
          variants={fadeInUp}
          initial="hidden"
          animate="visible"
          transition={{ duration: 0.55, delay: 0.3, ease: [0.2, 0, 0.2, 1] }}
          className="mt-9 flex w-full flex-col items-center gap-4 sm:flex-row sm:justify-center"
        >
          <ButtonLink href={appUrl} icon={undefined}>
            {t.hero.primary}
          </ButtonLink>
          <ButtonLink href="#features" variant="secondary" icon={<Play className="h-4 w-4" />}>
            {t.hero.secondary}
          </ButtonLink>
        </motion.div>

        <motion.div
          variants={fadeInUp}
          initial="hidden"
          animate="visible"
          transition={{ duration: 0.55, delay: 0.5, ease: [0.2, 0, 0.2, 1] }}
          className="mt-20 w-full"
        >
          <HeroMockup title={t.hero.mockupTitle} />
        </motion.div>
      </div>

      <motion.div
        variants={fadeInUp}
        initial="hidden"
        animate="visible"
        transition={{ duration: 0.55, delay: 0.6, ease: [0.2, 0, 0.2, 1] }}
        className="relative z-20 flex justify-center pb-10"
      >
        <a
          href="#features"
          className="flex flex-col items-center gap-2 text-sm font-medium text-muted-foreground transition-colors hover:text-foreground"
          aria-label={t.hero.scroll}
        >
          <span>{t.hero.scroll}</span>
          <ChevronDown className="h-4 w-4 animate-bounce" />
        </a>
      </motion.div>
    </section>
  );
}
```

- [ ] **Step 2: Verify TypeScript compiles**

```bash
npx tsc --noEmit 2>&1 | head -20
```

- [ ] **Step 3: Commit**

```bash
git add components/landing/hero.tsx
git commit -m "feat: add landing Hero section with mockup"
```

---

### Task 5: Create VideoSection

**Files:**
- Create: `components/landing/video-section.tsx`

- [ ] **Step 1: Create VideoSection**

`components/landing/video-section.tsx`:

```tsx
"use client";

import { motion } from "framer-motion";
import { SectionHeading } from "./section-heading";

const fadeInUp = {
  hidden: { opacity: 0, y: 20 },
  visible: { opacity: 1, y: 0 },
};

interface Copy {
  video: {
    title: string; subtitle: string; pulse: string;
    preview: string; cards: string[];
  };
}

export function VideoSection({ t }: { t: Copy }) {
  return (
    <section id="features" className="relative overflow-hidden py-24 sm:py-32">
      <div className="mx-auto max-w-5xl px-5 xl:max-w-6xl">
        <SectionHeading title={t.video.title} subtitle={t.video.subtitle} />

        <motion.div
          variants={fadeInUp}
          initial="hidden"
          whileInView="visible"
          viewport={{ once: true, margin: "-80px" }}
          transition={{ duration: 0.55, delay: 0.15, ease: [0.2, 0, 0.2, 1] }}
          className="mx-auto mt-14 max-w-[720px]"
        >
          <div className="overflow-hidden rounded-2xl bg-card border shadow-lg">
            <div className="flex items-center gap-4 border-b px-6 py-4">
              <div className="flex items-center gap-2.5">
                <span className="flex h-3 w-3 rounded-full bg-primary" />
                <span className="text-sm font-semibold">{t.video.pulse}</span>
              </div>
              <span className="text-sm text-muted-foreground">{t.video.preview}</span>
            </div>
            <div className="grid grid-cols-3 gap-4 p-6">
              {t.video.cards.map((label, i) => (
                <div key={label} className="rounded-xl bg-muted p-5">
                  <div className="mb-3 h-2 w-1/2 rounded-full bg-muted-foreground/20" />
                  <div className="space-y-2">
                    <div className="h-1.5 rounded-full bg-muted-foreground/15" style={{ width: i === 1 ? "58%" : "72%" }} />
                    <div className="h-1.5 rounded-full bg-muted-foreground/15 w-3/4" />
                  </div>
                  <div className="mt-4 text-xs font-medium text-muted-foreground">{label}</div>
                </div>
              ))}
            </div>
          </div>
        </motion.div>
      </div>
    </section>
  );
}
```

- [ ] **Step 2: Commit**

```bash
git add components/landing/video-section.tsx
git commit -m "feat: add landing VideoSection"
```

---

### Task 6: Create FeaturesSection with FeatureVisual

**Files:**
- Create: `components/landing/features-section.tsx`

- [ ] **Step 1: Create FeaturesSection**

`components/landing/features-section.tsx`:

```tsx
"use client";

import { Check, FileText, GitBranch } from "lucide-react";
import { motion } from "framer-motion";
import { SectionHeading } from "./section-heading";

const fadeInUp = {
  hidden: { opacity: 0, y: 20 },
  visible: { opacity: 1, y: 0 },
};

const stagger = {
  hidden: {},
  visible: { transition: { staggerChildren: 0.1 } },
};

interface FeatureItem {
  type: "evidence" | "outline" | "version";
  title: string; subtitle: string; bullets: string[]; reverse: boolean;
}

interface Copy {
  features: { title: string; subtitle: string; items: FeatureItem[] };
  visuals: {
    evidence: string[]; outlineTitle: string; outline: string[];
    versionTitle: string; versions: string[];
  };
}

function FeatureVisual({ type, t }: { type: string; t: Copy }) {
  if (type === "evidence") {
    return (
      <div className="overflow-hidden rounded-2xl bg-card border p-6">
        <div className="space-y-3">
          {t.visuals.evidence.map((item, i) => (
            <div key={item} className="flex items-center gap-3 rounded-lg bg-muted px-4 py-3">
              <FileText className="h-4 w-4 text-primary shrink-0" />
              <span className="text-sm font-medium">{item}</span>
            </div>
          ))}
        </div>
      </div>
    );
  }
  if (type === "outline") {
    return (
      <div className="overflow-hidden rounded-2xl bg-card border p-6">
        <div className="mb-3 text-sm font-semibold text-muted-foreground">{t.visuals.outlineTitle}</div>
        <div className="space-y-1.5">
          {t.visuals.outline.map((item, i) => (
            <div key={item} className="flex items-center gap-3 rounded-md px-3 py-2 text-sm">
              <span className="flex h-6 w-6 items-center justify-center rounded-full bg-primary/10 text-xs font-bold text-primary">
                {i + 1}
              </span>
              {item}
            </div>
          ))}
        </div>
      </div>
    );
  }
  // version
  return (
    <div className="overflow-hidden rounded-2xl bg-card border p-6">
      <div className="mb-3 text-sm font-semibold text-muted-foreground">{t.visuals.versionTitle}</div>
      <div className="relative pl-6 before:absolute before:left-[11px] before:top-2 before:h-[calc(100%-16px)] before:w-px before:bg-border">
        {t.visuals.versions.map((v, i) => (
          <div key={v} className="relative mb-4 last:mb-0">
            <span className="absolute -left-[22px] top-1 flex h-3 w-3 rounded-full border-2 border-primary bg-background" />
            <div className="flex items-center gap-2">
              <GitBranch className="h-3.5 w-3.5 text-muted-foreground" />
              <span className="text-sm font-medium">{v}</span>
            </div>
          </div>
        ))}
      </div>
    </div>
  );
}

export function FeaturesSection({ t }: { t: Copy }) {
  return (
    <section className="py-24 sm:py-32">
      <div className="mx-auto max-w-5xl px-5 xl:max-w-6xl">
        <SectionHeading title={t.features.title} subtitle={t.features.subtitle} />

        <div className="mt-16 space-y-24">
          {t.features.items.map((item, index) => (
            <motion.div
              key={item.type}
              variants={stagger}
              initial="hidden"
              whileInView="visible"
              viewport={{ once: true, margin: "-80px" }}
              className={`flex flex-col items-center gap-12 lg:flex-row ${
                item.reverse ? "lg:flex-row-reverse" : ""
              }`}
            >
              <motion.div variants={fadeInUp} className="flex-1">
                <h3 className="text-3xl font-bold tracking-tight sm:text-4xl">{item.title}</h3>
                <p className="mt-4 text-lg leading-relaxed text-muted-foreground">{item.subtitle}</p>
                <ul className="mt-6 space-y-3">
                  {item.bullets.map((bullet) => (
                    <li key={bullet} className="flex items-start gap-3 text-sm">
                      <Check className="mt-0.5 h-4 w-4 shrink-0 text-primary" />
                      {bullet}
                    </li>
                  ))}
                </ul>
              </motion.div>
              <motion.div variants={fadeInUp} className="flex-1 w-full max-w-[460px]">
                <FeatureVisual type={item.type} t={t} />
              </motion.div>
            </motion.div>
          ))}
        </div>
      </div>
    </section>
  );
}
```

- [ ] **Step 2: Commit**

```bash
git add components/landing/features-section.tsx
git commit -m "feat: add landing FeaturesSection with FeatureVisual"
```

---

### Task 7: Create ShowcaseSection

**Files:**
- Create: `components/landing/showcase-section.tsx`

- [ ] **Step 1: Create ShowcaseSection**

`components/landing/showcase-section.tsx`:

```tsx
"use client";

import { BarChart3, BookOpenText, CircleDot } from "lucide-react";
import { motion } from "framer-motion";
import { SectionHeading } from "./section-heading";

const fadeInUp = {
  hidden: { opacity: 0, y: 20 },
  visible: { opacity: 1, y: 0 },
};

interface Copy {
  showcase: {
    title: string; subtitle: string;
    cards: { title: string; text: string }[];
  };
}

const icons = [BarChart3, BookOpenText, CircleDot];

export function ShowcaseSection({ t }: { t: Copy }) {
  return (
    <section id="showcase" className="py-24 sm:py-32 bg-muted/50">
      <div className="mx-auto max-w-5xl px-5 xl:max-w-6xl">
        <SectionHeading title={t.showcase.title} subtitle={t.showcase.subtitle} />

        <div className="mt-14 grid gap-6 sm:grid-cols-2 lg:grid-cols-3">
          {t.showcase.cards.map((card, i) => {
            const Icon = icons[i] ?? CircleDot;
            return (
              <motion.div
                key={card.title}
                variants={fadeInUp}
                initial="hidden"
                whileInView="visible"
                viewport={{ once: true, margin: "-80px" }}
                transition={{ duration: 0.45, delay: i * 0.1, ease: [0.2, 0, 0.2, 1] }}
                className="rounded-2xl bg-card border p-6"
              >
                <div className="mb-4 flex h-10 w-10 items-center justify-center rounded-full bg-primary/10">
                  <Icon className="h-5 w-5 text-primary" />
                </div>
                <h3 className="text-lg font-bold">{card.title}</h3>
                <p className="mt-2 text-sm leading-relaxed text-muted-foreground">{card.text}</p>
              </motion.div>
            );
          })}
        </div>
      </div>
    </section>
  );
}
```

- [ ] **Step 2: Commit**

```bash
git add components/landing/showcase-section.tsx
git commit -m "feat: add landing ShowcaseSection"
```

---

### Task 8: Create ScenariosSection

**Files:**
- Create: `components/landing/scenarios-section.tsx`

- [ ] **Step 1: Create ScenariosSection**

`components/landing/scenarios-section.tsx`:

```tsx
"use client";

import { Users } from "lucide-react";
import { motion } from "framer-motion";
import { SectionHeading } from "./section-heading";

const fadeInUp = {
  hidden: { opacity: 0, y: 20 },
  visible: { opacity: 1, y: 0 },
};

interface Copy {
  scenarios: {
    title: string; subtitle: string; items: string[];
  };
}

export function ScenariosSection({ t }: { t: Copy }) {
  return (
    <section id="scenarios" className="py-24 sm:py-32">
      <div className="mx-auto max-w-5xl px-5 xl:max-w-6xl">
        <SectionHeading title={t.scenarios.title} subtitle={t.scenarios.subtitle} />

        <div className="mt-14 grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
          {t.scenarios.items.map((item, i) => (
            <motion.div
              key={item}
              variants={fadeInUp}
              initial="hidden"
              whileInView="visible"
              viewport={{ once: true, margin: "-80px" }}
              transition={{ duration: 0.4, delay: i * 0.06, ease: [0.2, 0, 0.2, 1] }}
              className="flex items-center gap-4 rounded-xl bg-card border p-5"
            >
              <span className="flex h-10 w-10 shrink-0 items-center justify-center rounded-full bg-muted">
                <Users className="h-4 w-4 text-primary" />
              </span>
              <span className="text-sm font-medium">{item}</span>
            </motion.div>
          ))}
        </div>
      </div>
    </section>
  );
}
```

- [ ] **Step 2: Commit**

```bash
git add components/landing/scenarios-section.tsx
git commit -m "feat: add landing ScenariosSection"
```

---

### Task 9: Create PricingSection

**Files:**
- Create: `components/landing/pricing-section.tsx`

- [ ] **Step 1: Create PricingSection**

`components/landing/pricing-section.tsx`:

```tsx
"use client";

import { Check } from "lucide-react";
import { motion } from "framer-motion";
import { SectionHeading } from "./section-heading";
import { ButtonLink } from "./button-link";

const appUrl = process.env.NEXT_PUBLIC_APP_URL ?? "/auth/login";

interface Tier {
  name: string; price: string; description: string;
  features: string[]; highlighted: boolean;
}

interface Copy {
  pricing: {
    title: string; subtitle: string; recommended: string;
    getStarted: string; perMonth: string; tiers: Tier[];
  };
}

export function PricingSection({ t }: { t: Copy }) {
  return (
    <section id="pricing" className="py-24 sm:py-32 bg-muted/50">
      <div className="mx-auto max-w-5xl px-5 xl:max-w-6xl">
        <SectionHeading title={t.pricing.title} subtitle={t.pricing.subtitle} />

        <div className="mt-14 grid gap-6 lg:grid-cols-3">
          {t.pricing.tiers.map((tier, i) => (
            <motion.div
              key={tier.name}
              initial={{ opacity: 0, y: 20 }}
              whileInView={{ opacity: 1, y: 0 }}
              viewport={{ once: true, margin: "-80px" }}
              transition={{ duration: 0.45, delay: i * 0.1, ease: [0.2, 0, 0.2, 1] }}
              className={`relative rounded-2xl p-8 ${
                tier.highlighted
                  ? "bg-primary text-primary-foreground shadow-[0_8px_32px_var(--color-primary)_24%] scale-[1.03]"
                  : "bg-card border"
              }`}
            >
              {tier.highlighted && (
                <span className="absolute -top-3 left-1/2 -translate-x-1/2 rounded-full bg-accent px-4 py-1 text-xs font-bold text-accent-foreground">
                  {t.pricing.recommended}
                </span>
              )}
              <div className="text-lg font-bold">{tier.name}</div>
              <div className="mt-3 flex items-baseline gap-1">
                <span className="text-4xl font-bold">{tier.price}</span>
                {tier.price !== "定制" && tier.price !== "Custom" && (
                  <span className={`text-sm ${tier.highlighted ? "text-primary-foreground/70" : "text-muted-foreground"}`}>
                    {t.pricing.perMonth}
                  </span>
                )}
              </div>
              <p className={`mt-2 text-sm ${tier.highlighted ? "text-primary-foreground/80" : "text-muted-foreground"}`}>
                {tier.description}
              </p>
              <ul className="mt-6 space-y-3">
                {tier.features.map((feat) => (
                  <li key={feat} className="flex items-start gap-3 text-sm">
                    <Check className={`mt-0.5 h-4 w-4 shrink-0 ${tier.highlighted ? "text-primary-foreground" : "text-primary"}`} />
                    {feat}
                  </li>
                ))}
              </ul>
              <div className="mt-8">
                <ButtonLink
                  href={appUrl}
                  variant={tier.highlighted ? "secondary" : "primary"}
                >
                  {t.pricing.getStarted}
                </ButtonLink>
              </div>
            </motion.div>
          ))}
        </div>
      </div>
    </section>
  );
}
```

- [ ] **Step 2: Commit**

```bash
git add components/landing/pricing-section.tsx
git commit -m "feat: add landing PricingSection"
```

---

### Task 10: Create CTASection

**Files:**
- Create: `components/landing/cta-section.tsx`

- [ ] **Step 1: Create CTASection**

`components/landing/cta-section.tsx`:

```tsx
"use client";

import { Sparkles } from "lucide-react";
import { motion } from "framer-motion";
import { ButtonLink } from "./button-link";

const appUrl = process.env.NEXT_PUBLIC_APP_URL ?? "/auth/login";

interface Copy {
  cta: {
    title: string; subtitle: string; primary: string;
    secondary: string; chips: string[];
  };
}

export function CTASection({ t }: { t: Copy }) {
  return (
    <section id="docs" className="py-24 sm:py-32">
      <div className="mx-auto max-w-5xl px-5 xl:max-w-6xl">
        <motion.div
          initial={{ opacity: 0, y: 20 }}
          whileInView={{ opacity: 1, y: 0 }}
          viewport={{ once: true, margin: "-80px" }}
          transition={{ duration: 0.55, ease: [0.2, 0, 0.2, 1] }}
          className="relative overflow-hidden rounded-3xl bg-gradient-to-br from-primary to-cyan-500 p-10 text-center sm:p-16"
        >
          <div className="relative z-10">
            <div className="mb-6 flex flex-wrap items-center justify-center gap-3">
              {t.cta.chips.map((chip) => (
                <span key={chip} className="rounded-full bg-white/15 px-4 py-1.5 text-sm font-medium text-white">
                  {chip}
                </span>
              ))}
            </div>
            <h2 className="text-3xl font-bold text-white sm:text-4xl">{t.cta.title}</h2>
            <p className="mx-auto mt-4 max-w-[520px] text-lg leading-relaxed text-white/80">
              {t.cta.subtitle}
            </p>
            <div className="mt-8 flex flex-col items-center gap-4 sm:flex-row sm:justify-center">
              <ButtonLink href={appUrl} variant="secondary" icon={<Sparkles className="h-4 w-4" />}>
                {t.cta.primary}
              </ButtonLink>
              <a
                href="#features"
                className="text-sm font-medium text-white/80 transition-colors hover:text-white"
              >
                {t.cta.secondary} →
              </a>
            </div>
          </div>
        </motion.div>
      </div>
    </section>
  );
}
```

- [ ] **Step 2: Commit**

```bash
git add components/landing/cta-section.tsx
git commit -m "feat: add landing CTASection"
```

---

### Task 11: Create Footer

**Files:**
- Create: `components/landing/footer.tsx`

- [ ] **Step 1: Create Footer**

`components/landing/footer.tsx`:

```tsx
import { LogoMark } from "./logo-mark";

interface Copy {
  footer: { features: string; pricing: string };
}

export function Footer({ t }: { t: Copy }) {
  return (
    <footer className="border-t py-10">
      <div className="mx-auto flex max-w-5xl items-center justify-between px-5 xl:max-w-6xl">
        <div className="flex items-center gap-3 text-sm font-semibold tracking-tight">
          <LogoMark />
          <span>mmdash</span>
        </div>
        <div className="flex items-center gap-6 text-sm text-muted-foreground">
          <a href="#features">{t.footer.features}</a>
          <a href="#pricing">{t.footer.pricing}</a>
        </div>
      </div>
    </footer>
  );
}
```

- [ ] **Step 2: Commit**

```bash
git add components/landing/footer.tsx
git commit -m "feat: add landing Footer"
```

---

### Task 12: Create LandingPage assembly and copy object

**Files:**
- Create: `components/landing/landing-page.tsx`

- [ ] **Step 1: Create LandingPage with full copy object and locale/theme state**

`components/landing/landing-page.tsx`:

```tsx
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
```

- [ ] **Step 2: Verify TypeScript compiles**

```bash
npx tsc --noEmit 2>&1 | head -50
```

- [ ] **Step 3: Commit**

```bash
git add components/landing/landing-page.tsx
git commit -m "feat: add LandingPage assembly with i18n copy"
```

---

### Task 13: Create route group and delete old redirect

**Files:**
- Create: `app/(landing)/page.tsx`
- Delete: `app/page.tsx`

- [ ] **Step 1: Create new route**

```bash
mkdir -p 'app/(landing)'
```

`app/(landing)/page.tsx`:

```tsx
import { LandingPage } from "@/components/landing/landing-page";

export const metadata = {
  title: "mmdash - 数学建模协作平台",
  description: "把队友协作、证据资料、模型文档、版本记录和 AI 分析放进一个安静清晰的数学建模工作台。",
};

export default function Landing() {
  return <LandingPage />;
}
```

- [ ] **Step 2: Delete old redirect page**

```bash
rm app/page.tsx
```

- [ ] **Step 3: Verify build**

```bash
npm run build 2>&1 | tail -30
```
Expected: successful build, no errors. The landing should be at `/`, auth at `/auth/login`, and `(main)` routes should be protected.

- [ ] **Step 4: Commit**

```bash
git add 'app/(landing)/page.tsx'
git rm app/page.tsx
git commit -m "feat: route / to landing page, delete old redirect"
```

---

### Task 14: Integration verification

**Files:** None (verification only)

- [ ] **Step 1: Start dev server**

```bash
make dev
```

- [ ] **Step 2: Verify routes**
  - Visit `http://localhost:3000/` → Landing page renders
  - Click "登录" → navigates to `/auth/login`
  - Click "免费开始" → navigates to `/auth/login?next=/home` (CTA)
  - Log in → redirected to `/home` with sidebar/navbar
  - All `(main)` routes still work

- [ ] **Step 3: Verify dark/light theme**
  - Toggle theme in NavBar → background/text changes correctly
  - Dark mode persists across page reload

- [ ] **Step 4: Verify locale switch**
  - Click "EN" → switches to English copy
  - Click "中" → switches back to Chinese

- [ ] **Step 5: Verify responsive**
  - Resize to mobile → NavBar shows compact layout
  - Sections stack vertically

- [ ] **Step 6: Commit any cleanup**

```bash
git add -A
git commit -m "chore: final verification and cleanup for landing integration"
```
