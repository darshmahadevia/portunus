---
name: Portunus
description: Graphite surfaces, compact evidence, and restrained mint actions.
colors:
  mint: "#a4e2c6"
  mint-hover: "#beeeda"
  action-ink: "#10281d"
  canvas: "#0c0f13"
  workspace: "#10141a"
  text: "#e5e9ef"
  muted: "#9da7b4"
  line: "#2a3039"
  control: "#1a2028"
  control-hover: "#272f39"
  control-border: "#39424e"
  control-border-hover: "#657282"
  quiet-text: "#bac3ce"
  field: "#0d1218"
  field-text: "#edf1f5"
  field-label: "#c8d1dc"
  placeholder: "#96a3b3"
  nav-text: "#aab4c1"
  nav-hover: "#171c23"
  nav-hover-text: "#f3f6f9"
  nav-current: "#192922"
  badge: "#252c35"
  badge-text: "#c7d0db"
  good: "#b0e7ca"
  good-bg: "#1d332a"
  warn: "#f1c589"
  warn-bg: "#372b1b"
  bad: "#ffb4b2"
  bad-bg: "#3b2329"
  response: "#0c1117"
  response-border: "#303946"
  response-text: "#c9d5de"
  table-heading: "#131920"
  table-heading-text: "#9da9b7"
  table-line: "#242c35"
  row-hover: "#141b23"
  detail: "#151c24"
  chart-bar: "#90cbb0"
  chart-label: "#aab5c3"
  notice: "#e4c48f"
  connection-bg: "#241f17"
  connection-border: "#67553a"
  connection-text: "#efd1a1"
typography:
  headline:
    fontSize: "25px"
    fontWeight: 600
    lineHeight: 1.25
    letterSpacing: "-0.03em"
  title:
    fontSize: "14px"
    fontWeight: 600
    lineHeight: 1.5
    letterSpacing: "-0.01em"
  body:
    fontFamily: 'Inter, -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif'
    fontSize: "13px"
    fontWeight: 400
    lineHeight: 1.5
  caption:
    fontSize: "12px"
    lineHeight: 1.5
  label:
    fontSize: "11px"
    lineHeight: 1.5
  table-heading:
    fontSize: "10px"
    fontWeight: 500
    letterSpacing: "0.05em"
  measurement:
    fontFamily: '"SFMono-Regular", Consolas, monospace'
    fontSize: "24px"
    fontWeight: 500
    letterSpacing: "-0.04em"
rounded:
  badge: "4px"
  field: "5px"
  control: "6px"
  response: "7px"
spacing:
  compact: "8px"
  field-gap: "14px"
  group: "16px"
  section: "22px"
  workspace-gap: "30px"
  overview-gap: "32px"
components:
  button-primary:
    backgroundColor: "{colors.mint}"
    textColor: "{colors.action-ink}"
    rounded: "{rounded.control}"
    padding: "8px 12px"
  button-primary-hover:
    backgroundColor: "{colors.mint-hover}"
  button-secondary:
    backgroundColor: "{colors.control}"
    textColor: "{colors.text}"
    rounded: "{rounded.control}"
    padding: "8px 12px"
  button-quiet:
    backgroundColor: "transparent"
    textColor: "{colors.quiet-text}"
    rounded: "{rounded.control}"
    padding: "8px 12px"
  field:
    backgroundColor: "{colors.field}"
    textColor: "{colors.field-text}"
    rounded: "{rounded.field}"
    padding: "10px 11px"
  nav-current:
    backgroundColor: "{colors.nav-current}"
    textColor: "{colors.mint}"
    rounded: "{rounded.control}"
    padding: "11px 13px"
  badge:
    backgroundColor: "{colors.badge}"
    textColor: "{colors.badge-text}"
    rounded: "{rounded.badge}"
    padding: "3px 7px"
  response:
    backgroundColor: "{colors.response}"
    textColor: "{colors.response-text}"
    rounded: "{rounded.response}"
    padding: "18px"
---

# Design System: Portunus

## Overview

**Creative North Star: "Engineering field notes"**

Engineering field notes: compact system type, aligned measurements, graphite surfaces, and quiet separators. Mint identifies the main action and current navigation; amber and red carry meaningful state. The interface stays flat and restrained, without gradients, glow, marketing treatments, or decorative imagery.

**Key Characteristics:**

- Compact headings and tabular measurements.
- Flat sections divided by hairlines.
- Explicit status text alongside color.
- Responsive stacking with locally scrollable tables.

This is a record of the shipped implementation, extracted from `web/src/style.css`, `web/src/main.tsx`, and `web/src/api.ts`. Direction comes from `PRODUCT.md`, `docs/dashboard-brief.md`, and `.impeccable/surfaces/web-src-main-tsx.md`. The supplied finish review reports SHIP after independent desktop/mobile and source inspection; it reports checked text contrast pairs above 4.5:1. This document does not extend that claim to disabled controls, separators, or every possible pairing.

Visual evidence inspected: `.impeccable/review/desktop.png` (1440 × 2588) and `mobile.png` (390 × 3363). These are captured app pixels, not generated illustrations. The app itself uses inline SVG marks, icons, and a data histogram; it contains no illustration or raster asset system. There was no approved image composition; code and rendered output establish the implementation.

## Colors

The frontmatter records source values; the role descriptions below explain their use.

### Primary

Mint fills the Send request button, marks the current navigation item, supplies the keyboard focus outline and text caret, and colors the brand mark. The lighter mint is the primary hover state; action ink is its dark text. The histogram uses its own muted green bar color.

### Neutral

Canvas frames the fixed rail; workspace fills the main reading area. Text and muted divide primary evidence from annotations. Line separates sections without enclosing each in a card. Control and field surfaces distinguish interactive elements; response and expanded-detail surfaces group output and attempt evidence.

### Semantic states

Good, warn, and bad are foreground/background pairs for labeled badges. Closed circuits, successful outcomes, cache hits, and live connection use good; open/half-open circuits, rejected requests, and offline/connecting status use warn. Request errors and canceled requests use bad. Streaming interruption uses warn in the response badge and bad-colored explanatory text. Recovery notices use notice; the connection alert has its own amber surface, border, and text.

## Typography

The body stack names Inter first, then platform UI fonts; no font file or external font import is shipped. Monospace is reserved for identifiers, output, definition-list values, chart labels, and major measurements, with tabular numerals.

The page headline uses the headline token, reducing to (24px) on mobile. Section and provider headings are compact (14px); mobile provider headings reduce to (13px). The workspace divider heading is (19px, weight 600, letter spacing -0.025em). Captions and field labels use (12px); badges, small annotations, and definition labels use (11px). Table headings use the uppercase table-heading token. Metric values reduce to (21px) at the middle breakpoint. Paragraphs cap at (75ch). Response text uses (12px) monospace with line height (1.8); textareas use line height (1.65).

## Layout

The desktop rail is fixed (190px), with matching main-content left margin. Main has a maximum width of (1660px), padding (38px 38px 20px), and a full viewport minimum height. The four-column metrics strip uses top/bottom rules and vertical separators. Routing and duration evidence use columns (1.15fr / 1fr) with a (32px) gap; the duration section has a left divider and (28px) inset. Form and response use equal columns with a (30px) gap. Paired form fields remain two columns.

At viewport widths of at least (1500px), main horizontal padding becomes (48px) and the overview gap becomes (42px). At widths up to (1150px), main padding is (28px 24px), routing/duration stack, and the duration divider moves above it. At widths up to (760px), the rail becomes a static top strip, navigation runs horizontally, and main padding becomes (25px 20px 20px). The header stacks, metrics become two columns, and form/response stack with a (22px) gap. Navigation remains visible without a collapsed menu.

Tables preserve their columns and nowrap cells inside labeled, keyboard-focusable horizontal scroll regions. Detail rows wrap their contents. Response output wraps long strings and scrolls vertically, with a maximum height of (360px). Its minimum text height changes from (190px) to (140px) on mobile; the mobile response container has a minimum height of (260px). Provider measurements retain four columns and permit value wrapping. Actions and filters wrap; no global fixed-height dashboard or page-level horizontal scroller is introduced.

## Elevation & Depth

No box shadows, gradients, or glows are defined. Depth comes from neighboring dark tones, thin borders, and the inset response and expanded request-detail surfaces. Most sections have separators rather than a card shell.

Motion is limited to background/border transitions (150ms ease) for buttons and links and a one-time connected-dot color animation (600ms). Both exist only when reduced motion is not requested. The sidecar records the exact values.

## Shapes

Controls have gently rounded corners; fields, badges, and the response container use their own frontmatter radii. Section separators and table rules are (1px). Status dots are circles (7px). Histogram bars use a small SVG corner radius (2px). Mobile navigation rounds only the top corners (5px). Inline icons use a (17px) frame and (1.6px) rounded strokes; the brand mark is a separate (28px) outline, narrowed to (24px) on mobile.

## Components

### Buttons

The default secondary button has a thin control border, minimum height (40px), medium weight (500), and an (8px) content gap. Hover changes its background and border. Primary uses mint and dark action ink; quiet uses a transparent background/border and muted light text, then inherits the default hover treatment. Icon buttons and table disclosure buttons have a minimum width of (40px). Provider presets use smaller labels (11px) and padding (6px 9px), retaining the (40px) minimum height. Disabled buttons use opacity (0.45) and a not-allowed cursor; their hover treatment is suppressed.

### Inputs / Fields

Visible labels sit above full-width native controls with a (7px) gap. Inputs/selects/textareas have minimum height (42px) and a thin control border. Placeholders have a distinct muted color. Textarea resizing is vertical. Disabled fields use opacity (0.6). The checkbox is native (16px), with mint accent. The client-key input masks its value. While a request or demo is pending, conflicting fields and actions are disabled; Stop remains available for the active operation.

All buttons, links, fields, selects, textareas, and focusable scroll regions share a visible mint outline (2px, offset 3px). No extra field hover or custom invalid-border style is defined; required fields retain native validation.

### Navigation

Three ordinary links use outlined SVG icons, muted text, and a (42px) minimum height. Hover brightens the text and darkens its local surface. `aria-current="page"` marks the mint-on-green current item. On mobile the links share the strip width equally. The connection footer moves beside the brand and keeps text alongside its status dot.

### Status badges

Compact text badges use the badge token and good/warn/bad variants. They are informational labels, not filter chips or action pills. State words remain visible; color is supplementary. No custom badge hover or pressed state exists.

### Sections and response container

Routing, metrics, activity, budgets, and demo controls are flat sections. The response container is the recurring bordered card treatment: a title and state badge above wrapping monospace output, with completion metadata separated below. Ready, streaming, completed, interrupted, and error are distinct. Partial text remains visible after interruption; completion metadata requires a validated done event. Output display is capped at (32,000 characters).

### Evidence tables and request disclosure

Tables use uppercase small headings, subdued heading backgrounds, and row separators. Hover lightly tints ordinary rows. A disclosure button rotates its arrow (90deg) when expanded and exposes `aria-expanded`. The inset detail row lists provider attempts, identifiers, timings, usage provenance, and errors. Cached requests with no attempt and requests ending before routing have distinct explanations. Full request activity exposes search/outcome filters and paging; compact recent activity shows the latest eight records without those filters.

### Data, empty, and failure states

Loading shows a centered status message. Feed failure before data arrives shows an amber alert and retry; failure after a successful snapshot preserves last-known evidence and labels it stale. The live badge, updated time, and connection text remain explicit. Empty charts and tables use concise text and an action cue, without placeholder data. The duration histogram is SVG with eight buckets derived from retained non-rejected requests, labeled axes, an accessible summary, and per-bar titles.

Provider controls distinguish idle, pending, applied, and error; pending disables that provider's presets. Circuit recovery copy identifies when a later request can test recovery. Demo progress and results remain visible, with error alerts and an available Stop demo action while pending. Budget and cache summaries use aligned numeric evidence and label costs as estimated.

## Do's and Don'ts

### Do:

- **Do** reuse the documented control, field, badge, and response treatments.
- **Do** pair status colors with labels and preserve visible keyboard focus.
- **Do** wrap long output and identifiers; keep wide tables inside their own scroll regions.
- **Do** preserve empty, stale, pending, interrupted, and error messages as distinct states.

### Don't:

- **Don't** introduce gradients, glow, decorative illustrations, or marketing copy.
- **Don't** replace missing observations with invented traffic or a decorative chart.
- **Don't** present partial streaming output as a completed response.
- **Don't** assume Inter is bundled; the implementation uses a system fallback stack.
