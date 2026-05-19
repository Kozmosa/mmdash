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
