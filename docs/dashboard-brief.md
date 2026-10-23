# Dashboard surface

Mode is Operate. The user selected React, TypeScript, Vite, dark mode, and a minimal appearance. The dashboard helps an interviewer inspect real gateway behavior and run a failure demonstration.

## Direction contract

THESIS: Make the relationship between client requests and provider attempts visible. Provider routing and request evidence lead; the streaming playground is the primary action.

OWN-WORLD: Graphite backgrounds, quiet hairline separators, a compact system sans, tabular measurements, restrained cool-green actions, and amber/red only for meaningful failure states. No gradients, glow, decorative illustrations, or marketing copy.

STORY: Inspect current health, send a request, interrupt the primary provider, and follow the resulting attempts. Empty, stale, rejected, cached, and interrupted states remain explicit.

FIRST VIEWPORT: A narrow desktop navigation rail, page title and live status, one horizontal metrics strip, provider routing beside request-latency evidence, then a compact request table. A clearly labeled playground and failure controls sit within the same app. Narrow screens use a top navigation strip and stacked work areas.

FORM: Engineering field notes, the fifth grounded direction, expressed through aligned evidence and concise annotations. The fixed user brief takes precedence over the concept roll's decorative challengers. Seed reference is seed:5207c7a0150c4866. Other considered systems were journal indexes, transit schedules, scientific plot sheets, library catalogs, audit ledgers, and classroom experiment reports.

FINISH: unreviewed and undocumented is unfinished; this build ends with the finish review, the verdict, DESIGN.md, and every shipping raster carrying its provenance

## Implementation boundary

Frontend source is `web/`. Compiled assets and their Go embedding handler live in `internal/dashboard/`. The shared API is documented in `docs/api-contract.md`. The dashboard never invents traffic, uses no paid provider, and stores no provider secret. Use ordinary accessible buttons, forms, tables, and tabs. All data visualizations derive from observed request records.
