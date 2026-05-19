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
