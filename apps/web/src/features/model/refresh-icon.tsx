import Image from "next/image";

export function RefreshIcon({ spinning = false }: { spinning?: boolean }) {
  return (
    <Image
      alt=""
      aria-hidden="true"
      className={spinning ? "size-4 animate-spin dark:invert" : "size-4 dark:invert"}
      height={16}
      src="/icons/refresh-cw.svg"
      width={16}
    />
  );
}
