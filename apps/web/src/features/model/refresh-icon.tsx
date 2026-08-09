import Image from "next/image";

export function RefreshIcon({ spinning = false }: { spinning?: boolean }) {
  return (
    <Image
      alt=""
      aria-hidden="true"
      className={spinning ? "size-4 animate-spin invert dark:invert-0" : "size-4 invert dark:invert-0"}
      height={16}
      src="/icons/refresh-cw.svg"
      width={16}
    />
  );
}
