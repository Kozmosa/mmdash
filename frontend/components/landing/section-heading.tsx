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
