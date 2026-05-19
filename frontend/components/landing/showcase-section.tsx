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
    cards: readonly { title: string; text: string }[];
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
