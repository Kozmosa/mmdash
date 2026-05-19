import { LogoMark } from "./logo-mark";

interface Copy {
  footer: { features: string; pricing: string };
}

export function Footer({ t }: { t: Copy }) {
  return (
    <footer className="border-t py-10">
      <div className="mx-auto flex max-w-5xl items-center justify-between px-5 xl:max-w-6xl">
        <div className="flex items-center gap-3 text-sm font-semibold tracking-tight">
          <LogoMark />
          <span>mmdash</span>
        </div>
        <div className="flex items-center gap-6 text-sm text-muted-foreground">
          <a href="#features">{t.footer.features}</a>
          <a href="#pricing">{t.footer.pricing}</a>
        </div>
      </div>
    </footer>
  );
}
