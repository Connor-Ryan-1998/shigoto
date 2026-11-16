# Calmer, warmer Shigoto UI — design

Date: 2026-11-16 · Size: small project (theme + shell + shared components)

## Intent

Shigoto works and is accessible, but it reads as a stock admin template:
stock DaisyUI `corporate`/`business` themes, 1px borders around every
region, system font, no icons, text-glyph controls. The owner uses it daily
and will later open it to staff and tenants. This change makes the tool
feel calmer and warmer for daily use. A friendlier *experience* (dashboard
rework, onboarding copy, portal UX) is explicitly later work.

## Goals

- A warm-sage visual identity in light and dark, applied everywhere through
  theme tokens.
- Less fencing: soft surfaces and spacing instead of borders on every region.
- A calmer shell (sidebar, top bar) with icons and clear active state.
- One bundled, readable font with tabular figures for money.

## Non-goals

- Dashboard content, empty-state copy, onboarding, or portal UX changes.
- Replacing the ~30 hand-copied `rounded-box border border-base-300 bg-base-100`
  surfaces in routes/modules (option C; possible later cleanup). They soften
  automatically through the theme tokens.
- New component kit, icon library, CSS framework, or remote assets.

## Anti-goals (failure even if it "works")

- Any behaviour change: links, permissions, module gating, focus handling,
  dialog/drawer dismissal, skip links, `aria-current`, print layout.
- Contrast regressions: body text < 4.5:1, or controls/borders < 3:1, in
  either theme.
- Status conveyed by colour alone (badges keep their text).
- Glassmorphism, blur, or translucency over financial tables.
- Requests to Google Fonts or any CDN.

## Approaches considered

| | Approach | Trade-off |
| --- | --- | --- |
| A | Theme tokens only | ~3 files, every screen shifts, but the bordered shell, `☰` glyph and text controls remain and are on every page. |
| **B** | **Theme + shell + shared components (chosen)** | ~15–20 files. Most of the calm comes from the frame and shared parts; route screens inherit without edits. |
| C | B + replace hand-copied surfaces with a shared surface | ~40 files, more test risk, mostly consistency cleanup. Deferred. |

Chosen: B, by the owner. Visual direction "Warm sage" chosen from a
side-by-side mockup over "Soft clay". Font Source Sans 3 chosen over the
system font.

## Decisions

### Themes (`web/src/app.css`, `web/src/lib/theme.svelte.ts`)

- Define two custom DaisyUI 5 themes, `shigoto-light` (`--default`) and
  `shigoto-dark` (`--prefersdark`), replacing `corporate`/`business`.
  Update `theme.svelte.ts` to set these names.
- Palette, from the approved mockup (builder may tune for contrast):

  | Token | Light | Dark |
  | --- | --- | --- |
  | base-100 (surface) | `#fffefb` | `#2b332d` |
  | base-200 (canvas) | `#f7f6f2` | `#202622` |
  | base-300 (lines, sidebar tint) | `#e4e5dc` | `#414c42` |
  | base-content | `#303930` | `#e7ece5` |
  | primary | `#42634d` | `#b1d0ac` |
  | primary-content | `#fffefb` | `#202622` |
  | warning / content | `#f6ebd4` bg family, `#805714` text | `#4a3c27` / `#f1ce8d` |

  Sidebar tint is `#efeee7` light / `#252d26` dark; derive it from base
  tokens or a theme-scoped custom property — builder's call. Error, success
  and info keep DaisyUI hues, warmed slightly, contrast-checked.
- Shape: `--radius-box` ≈ 0.75rem (panels), `--radius-field` ≈ 0.5rem
  (controls), `--radius-selector` ≈ 0.5rem. Keep `--border` at 1px; borders
  just become low-contrast warm lines.
- Muted text (`text-base-content/70`) must still reach 4.5:1 on base-100 and
  base-200 in both themes; adjust base-content if it doesn't.

### Font

- Add `@fontsource-variable/source-sans-3`, import once from the root
  (`app.css` or root `+layout.svelte`), set as the theme's sans family with a
  system fallback stack. No remote fonts. Money/duration columns keep
  `tabular-nums`.

### Shell (`routes/[org]/+layout.svelte`, `routes/portal/+layout.svelte`)

- Staff: remove `border-b` / `border-r`. Sidebar on the tinted background,
  canvas `base-200`, content surfaces `base-100`. A small "shigoto" wordmark
  at the top of the sidebar.
- Nav groups stay as today (order and contents from `NAV_GROUPS`); render the
  group label as small uppercase muted text instead of a `<details>`
  disclosure. Items get an icon, roomier padding, and an active state of
  soft-primary background + primary text + `aria-current="page"` (retained).
- Top bar: the organisation switcher stays a labelled `<select>` (label may be
  visually hidden but must remain associated). Theme toggle becomes an icon
  button (sun/moon/auto) keeping its existing `aria-label`. User name, Portal
  link and Sign out grouped quietly at the right (ghost buttons). Rename the
  visible "Logout" text to "Sign out".
- Mobile: `☰` becomes the menu icon with the existing `aria-label`. Drawer
  behaviour unchanged.
- Portal: same visual treatment (borderless header, ghost buttons, icon theme
  toggle, "Sign out"). No staff nav, org switcher, or shared layout code —
  isolation rules in `docs/ui-standards.md` still apply.
- Print styles (`print:hidden`, `print:bg-white`) retained.

### Icons

- New `web/src/lib/components/Icon.svelte`: inline SVG, 24×24 viewBox,
  1.5px stroke, `currentColor`, `aria-hidden="true"` by default, about 15
  named icons (dashboard, contacts, invoices, reports, settings, members,
  properties, leases, expenses, projects, time, reconciliation, menu,
  sun/moon/auto, sign-out, plus empty-state glyph). No dependency.
- Add optional `icon?: IconName` to `NavItem` in `lib/modules/registry.ts`
  and set it in each module manifest; the shell renders nothing extra when
  absent.

### Shared components (`web/src/lib/components/`)

- `PageHeader`: h1 `text-3xl font-semibold tracking-tight`; back link muted
  with an arrow icon; more space below.
- `FilterBar`: drop the border; sits on the canvas or a soft base-100 panel
  without an outline. Layout rules unchanged.
- `DataTable`: no outer border; base-100 surface, rounded, faint shadow;
  header row tinted (`bg-base-200`), muted medium-weight header text; row
  dividers in base-300. Semantics unchanged.
- `EmptyState`: optional icon above the title; roomier padding. API stays
  backward-compatible.
- `StatusBadge`: retune to the softer palette (rounded, smaller, soft tone);
  text and dot behaviour unchanged.
- `Button`: secondary may move from `btn-outline` to a soft/ghost style if it
  still meets 3:1 boundary contrast; builder's call. Prop contract unchanged.
- Dashboard cards (`DashboardCore` and module `DashboardCard`s): rely on the
  token change; drop `card-border` in favour of a faint shadow. Overdue
  keeps its text label and warning accent.
- `Dialog`/`Drawer`: token change only, unless a border is visibly harsh.

### Docs

- `docs/ui-standards.md`: replace "Keep corporate/business themes" with the
  `shigoto-light`/`shigoto-dark` rule, mention `Icon.svelte` in the shared
  components table, and note the font is bundled (no remote fonts).

## Verification

- `uv run --locked task web:test`, `uv run --locked task lint`,
  `uv run --locked task web:e2e`, run sequentially. Update any test that
  asserts removed classes or the "Logout" label; do not weaken behaviour
  assertions.
- Add `Icon.svelte` and nav-icon coverage where the owning tests live
  (layout tests for icons rendering decoratively; `aria-current` retained).
- Manual browser check, both themes: dashboard, invoices list, an invoice
  detail, a dialog, the mobile drawer at 375px, portal, login, and the
  rental report print preview. Spot-check contrast of muted text, badges,
  and primary buttons.

## Left to the builder

- Exact token values within the palette above, after contrast checks.
- Sidebar tint mechanism, icon artwork, and secondary-button style.
- Whether `Dialog`/`Drawer` need any non-token tweak.
