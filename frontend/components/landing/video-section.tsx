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
    preview: string; cards: readonly string[];
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
