"use client";

import { Sparkles } from "lucide-react";
import { motion } from "framer-motion";
import { ButtonLink } from "./button-link";

const appUrl = process.env.NEXT_PUBLIC_APP_URL ?? "/auth/login";

interface Copy {
  cta: {
    title: string; subtitle: string; primary: string;
    secondary: string; chips: readonly string[];
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
