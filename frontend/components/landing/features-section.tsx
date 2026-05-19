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
  title: string; subtitle: string; bullets: readonly string[]; reverse: boolean;
}

interface Copy {
  features: { title: string; subtitle: string; items: readonly FeatureItem[] };
  visuals: {
    evidence: readonly string[]; outlineTitle: string; outline: readonly string[];
    versionTitle: string; versions: readonly string[];
  };
}

function FeatureVisual({ type, t }: { type: string; t: Copy }) {
  if (type === "evidence") {
    return (
      <div className="overflow-hidden rounded-2xl bg-card border p-6">
        <div className="space-y-3">
          {t.visuals.evidence.map((item) => (
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
  return (
    <div className="overflow-hidden rounded-2xl bg-card border p-6">
      <div className="mb-3 text-sm font-semibold text-muted-foreground">{t.visuals.versionTitle}</div>
      <div className="relative pl-6 before:absolute before:left-[11px] before:top-2 before:h-[calc(100%-16px)] before:w-px before:bg-border">
        {t.visuals.versions.map((v) => (
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
          {t.features.items.map((item) => (
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
