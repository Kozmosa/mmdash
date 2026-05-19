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
      <div
        className="pointer-events-none absolute inset-0 z-10"
        style={{
          backgroundImage: `url("data:image/svg+xml,%3Csvg viewBox='0 0 256 256' xmlns='http://www.w3.org/2000/svg'%3E%3Cfilter id='n'%3E%3CfeTurbulence type='fractalNoise' baseFrequency='0.9' numOctaves='4' stitchTiles='stitch'/%3E%3C/filter%3E%3Crect width='100%25' height='100%25' filter='url(%23n)' opacity='0.025'/%3E%3C/svg%3E")`,
          maskImage: "radial-gradient(ellipse at 50% 0%, black 60%, transparent 100%)",
          WebkitMaskImage: "radial-gradient(ellipse at 50% 0%, black 60%, transparent 100%)",
        }}
      />
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
