# Landing Page Integration Design

## Summary

Integrate the standalone landing page (`landing/`) into the main Next.js frontend (`frontend/`).  The root route `/` renders the landing page; clicking login/register navigates to the existing auth pages.  Authenticated routes under `(main)/` are unchanged.

## Routing

```
/                        → (landing)/page.tsx     (public)
/auth/login              → auth/login/page.tsx     (public)
/auth/register           → auth/register/page.tsx  (public)
/auth/notion/callback    → auth/notion/callback/   (public)
/home, /model, ...       → (main)/*                (auth-gated)
```

- New route group `app/(landing)/page.tsx` owns `/`.  It imports `LandingPage` from `components/landing/landing-page.tsx`.
- Existing `app/page.tsx` is deleted (was `redirect("/auth/login")`).
- `app/(main)/layout.tsx` auth guard is unchanged — unauthenticated visitors are redirected to `/auth/login`.
- Landing CTA buttons link to `/auth/login?next=/home`.

## Component Tree

```
app/(landing)/page.tsx
  └─ LandingPage  (components/landing/landing-page.tsx)
       ├─ NavBar           — logo, anchor links, locale/theme toggles
       ├─ Hero             — headline, subtitle, CTA buttons, HeroMockup
       ├─ VideoSection     — product preview card
       ├─ FeaturesSection  — three feature blocks with FeatureVisual variants
       ├─ ShowcaseSection  — three cards (dashboard, analysis, narrative)
       ├─ ScenariosSection — six usage-scenario cards
       ├─ PricingSection   — three pricing tiers
       ├─ CTASection      — bottom gradient CTA
       └─ Footer           — logo, links, copyright
```

Shared helpers (also under `components/landing/`):
- `SectionHeading` — title + subtitle with fade-in-up animation
- `ButtonLink` — primary/secondary link styled as button
- `LogoMark` — `{}` braces icon

### File list

```
components/landing/
├── landing-page.tsx       # top-level assembly, holds copy object + locale state
├── nav-bar.tsx
├── hero.tsx
├── video-section.tsx
├── features-section.tsx
├── showcase-section.tsx
├── scenarios-section.tsx
├── pricing-section.tsx
├── cta-section.tsx
├── footer.tsx
├── section-heading.tsx
├── button-link.tsx
└── logo-mark.tsx

app/
├── (landing)/
│   └── page.tsx            # 1-liner: import + render LandingPage
├── (main)/                 # unchanged
├── auth/                   # unchanged
└── layout.tsx              # unchanged
```

## Styling Strategy

The landing page is restyled with the main frontend's CSS variable system (shadcn/ui New York + Tailwind v4).  The standalone landing's custom variables (`brand-*`, `surface-*`, `text-*`, `ocean-*`) are **not** copied.

| Element | Implementation |
|---|---|
| Brand blue | `--primary` / `bg-primary` / `text-primary` |
| Surface layers | `--background`, `--card`, `--muted` |
| Text hierarchy | `--foreground`, `--muted-foreground` |
| Serif display font | `font-serif tracking-tight` (Tailwind built-in) |
| Noise SVG overlay | Inline SVG kept in Hero component |
| Dark mode | next-themes `dark` class (existing app mechanism) |
| Special colors (ocean, accent) | Hardcoded Tailwind values or one-off CSS custom properties scoped to `.landing-section` |

## Animation

- Add `framer-motion` to `frontend/package.json` (~30 kB gzip).
- Port the existing `fadeInUp` / `stagger` patterns 1:1 from the standalone landing.
- `viewport={{ once: true, margin: "-80px" }}` semantics preserved.

## i18n

- The inline `copy` object (zh/en) is placed in `landing-page.tsx`.
- Locale state lives in `LandingPage` and is passed as `t: Copy` prop to every section.
- No external i18n framework is introduced in this change.

## What Does NOT Change

- All pages under `(main)/` (home, model, experiment, references, timeline, settings).
- All pages under `auth/` (login, register, notion/callback).
- Root `app/layout.tsx` (ThemeProvider, TooltipProvider, Toaster).
- `(main)/layout.tsx` auth guard (cookie check + redirect).
- Backend APIs, proxy config, environment variables.

## Dependencies

| Change | Detail |
|---|---|
| Add | `framer-motion` to `frontend/package.json` |
| Remove | Standalone `landing/` project (optional — can be archived after verification) |
