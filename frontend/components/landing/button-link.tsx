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
