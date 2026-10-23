# Dashboard direction

Mode is Operate. The user asked for a full redesign with a black background, clear explanations, and more visualizations.

## Direction contract

THESIS: Let the operator understand gateway behavior before changing it. Overview monitors, Requests investigates, and Playground runs experiments.

OWN-WORLD: A black canvas, neutral charcoal work areas, bright readable type, precise charts, and restrained mint actions. Amber and red identify degraded and failed states. Ordinary form controls and descriptive text explain each action.

STORY: Send a request, see its outcome, inspect its provider attempts, then change a fake provider to understand fallback. No traffic exists until a real local request runs.

FIRST VIEWPORT: Desktop navigation beside an overview title and connection status. Gateway totals precede latency and outcome evidence, with provider routing and recent requests following. A visible action opens the playground. Small screens stack content below navigation.

FORM: A focused statistical report, with aligned values, explicit observation scopes, and plotted evidence. The user's black-background brief fixes the palette. Seed reference is seed:b5d185d85a9e632c.

FINISH: unreviewed and undocumented is unfinished; this build ends with the finish review, the verdict, DESIGN.md, and every shipping raster carrying its provenance

## Implementation boundary

Frontend source is `web/`. Compiled assets and the Go embedding handler live in `internal/dashboard/`. The dashboard uses real gateway observations, labels estimated costs, and never invents traffic.
