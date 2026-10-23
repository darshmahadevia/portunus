---
name: Portunus
description: Black canvas, charcoal panels, and scoped gateway evidence.
colors:
  canvas: "#000"
  panel: "#101010"
  field: "#090909"
  muted: "#a3a3a3"
  line: "#292929"
  mint: "#a4e6c5"
  amber: "#ebc088"
  red: "#f5a6a6"
  text: "#f2f2f2"
  sidebar: "#080808"
  shell-line: "#232323"
  control: "#1b1b1b"
  control-border: "#3b3b3b"
  control-border-hover: "#707070"
  control-active: "#363636"
  mint-hover: "#c2f4da"
  action-ink: "#0b271a"
  quiet-text: "#c1c1c1"
  field-border: "#414141"
  field-border-hover: "#727272"
  field-label: "#cfcfcf"
  placeholder: "#999"
  nav-text: "#b0b0b0"
  nav-hover: "#171717"
  nav-current: "#1a211d"
  nav-current-caption: "#b5c9be"
  badge: "#282828"
  badge-text: "#d1d1d1"
  good: "#b0e8ca"
  good-bg: "#1b3026"
  warn: "#f0cb98"
  warn-bg: "#34291d"
  bad: "#ffc1c1"
  bad-bg: "#382121"
  chart-label: "#a8a8a8"
  chart-grid: "#303030"
  chart-track: "#252525"
  chart-canceled: "#aaa"
  chart-attempt: "#7f8b84"
  detail: "#191919"
  error-text: "#ffb6b6"
  connection-bg: "#241c13"
  connection-border: "#63503b"
  connection-text: "#efcfaa"
typography:
  headline:
    fontSize: "30px"
    fontWeight: 600
    lineHeight: 1.25
    letterSpacing: "-0.035em"
  title:
    fontSize: "16px"
    fontWeight: 600
    lineHeight: 1.4
    letterSpacing: "-0.015em"
  subheading:
    fontSize: "15px"
    fontWeight: 600
  body:
    fontFamily: 'Inter, -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif'
    fontSize: "13px"
    lineHeight: 1.55
  caption:
    fontSize: "12px"
    lineHeight: 1.65
  label:
    fontSize: "11px"
    fontWeight: 500
  measurement:
    fontFamily: '"SFMono-Regular", Consolas, "Liberation Mono", monospace'
    fontSize: "26px"
    fontWeight: 500
    lineHeight: 1.4
    letterSpacing: "-0.035em"
  response:
    fontFamily: '"SFMono-Regular", Consolas, monospace'
    fontSize: "12px"
    lineHeight: 1.85
rounded:
  scope: "4px"
  badge: "5px"
  field: "7px"
  control: "8px"
  alert: "12px"
  panel: "14px"
spacing:
  compact: "8px"
  small: "12px"
  paired: "16px"
  group: "20px"
  section: "24px"
  workspace: "26px"
  columns: "32px"
components:
  button-secondary:
    backgroundColor: "{colors.control}"
    textColor: "{colors.text}"
    rounded: "{rounded.control}"
    padding: "9px 14px"
  button-primary:
    backgroundColor: "{colors.mint}"
    textColor: "{colors.action-ink}"
    rounded: "{rounded.control}"
    padding: "9px 14px"
  button-primary-hover:
    backgroundColor: "{colors.mint-hover}"
  button-quiet:
    backgroundColor: "transparent"
    textColor: "{colors.quiet-text}"
    rounded: "{rounded.control}"
    padding: "9px 14px"
  field:
    backgroundColor: "{colors.field}"
    textColor: "{colors.text}"
    rounded: "{rounded.field}"
    padding: "10px 12px"
  nav-current:
    backgroundColor: "{colors.nav-current}"
    textColor: "{colors.mint}"
    rounded: "{rounded.control}"
    padding: "11px 13px"
  badge:
    backgroundColor: "{colors.badge}"
    textColor: "{colors.badge-text}"
    rounded: "{rounded.badge}"
    padding: "3px 8px"
  chart-panel:
    backgroundColor: "{colors.panel}"
    rounded: "{rounded.panel}"
    padding: "22px 22px 16px"
---

# Design System: Portunus

## Overview

**Creative North Star: "Gateway evidence"**

Portunus uses a black canvas, charcoal panels, bright text, and mint actions. Measurements, chart labels, and explanatory copy help the operator connect a client request to its provider attempts.

This document records source behavior in `web/src/style.css`, `web/src/main.tsx`, and `web/src/visualizations.tsx`. `web/src/telemetry.ts` establishes chart aggregation. `PRODUCT.md` supplies product constraints; `docs/dashboard-brief.md` owns page strategy. This extraction makes no browser, contrast, or finish-review claim.

**Key Characteristics:**

- Separate pages for monitoring, request investigation, and experiments.
- Visible scopes beside measurements and charts.
- Rounded charcoal panels on a black workspace.
- Status words alongside semantic color.

## Colors

Frontmatter preserves the source's CSS color values. The first eight colors correspond to the root custom properties.

Mint identifies the primary action, brand, current navigation, focus outline, caret, successful chart observations, and final routes. Amber identifies rejected chart observations and budget commitment; red identifies chart errors. Canceled chart observations use gray, while canceled table rows use the bad badge. Attempts and reserved budget share the muted chart-attempt color.

Black fills the workspace. Sidebar and panel tones separate navigation and content; field is the inset form background. Line separates content and outlines panels. Good, warn, and bad are distinct badge foreground/background pairs. Connection failures use a separate amber alert treatment.

**The State Label Rule.** Preserve labels, legends, and counts wherever color communicates an outcome.

## Typography

Body and headings inherit the Inter-first platform UI stack. The stylesheet defines no font import or font face. Measurements, identifiers, code, and budget values use monospace and tabular numerals. Chart labels use the inherited UI font with tabular numerals.

Headline, section title, and measurement roles use the frontmatter scale. Provider/response headings use subheading. Small scopes, legends, table headings, and metadata use 10px or 11px text. Table headings retain their written case. Paragraphs cap at 75ch. Textareas use line height 1.7; response output uses the response role.

At widths up to 1250px, measurements become 23px. Up to 760px, headlines become 27px, section titles 15px, and measurements 24px. Up to 380px, measurements become 21px. Mobile chart SVG text sizes are 20px for the timeline, 17px for the histogram, 13px for outcomes, and 19px for provider volume. These sizes scale with each SVG viewBox.

## Layout

The desktop sidebar is fixed at 224px with matching main margin. Main has maximum width 1920px and padding 0 40px 26px. A 65px topbar precedes the page header. Four metric columns sit between horizontal rules with internal vertical dividers. Primary chart columns use 1.8fr and a minimum-280px 1fr column; secondary charts use 1.15fr / 1fr. Both have 20px gaps. Playground form and response use equal columns with a 32px gap and a response-side divider. Provider controls use two columns. Budget rows use identity / meter / remaining columns with a 26px gap.

Responsive rules apply in source order:

- At least 1800px, main side padding becomes 56px.
- Up to 1250px, sidebar/main offset becomes 194px and main side padding 28px. Primary charts use 1.5fr and a minimum-260px 1fr column. Chart titles/scopes stack; Playground gap and response inset become 22px.
- Up to 1000px, sidebar/main offset becomes 174px and main side padding 24px. Metrics become two columns. Primary charts, Playground, and providers stack; secondary charts stay paired. The response divider moves above the output. Budgets place their meter beneath identity and remaining balance.
- Up to 760px, the sidebar becomes a sticky full-width header with three equal navigation links. Secondary charts stack. Main loses its left margin and uses 20px side padding. Guidance and investigation links stack. Header status stays beside the title; its timestamp hides. Chart titles/scopes return to a row.
- Up to 380px, main/header side padding becomes 14px and paired form fields stack. The inline retained badge hides; the metric's explanatory text remains.

Wide tables scroll inside a labeled, focusable region. Detail contents and long output wrap. Response text scrolls vertically above 560px; its minimum height is 260px, then 180px up to 1000px and 190px up to 760px. Filters/actions wrap. The dashboard uses document scrolling.

## Elevation & Depth

Depth comes from dark fills, thin borders, and inset detail rows. There are no ambient shadows or gradients. The mobile current navigation item has an inset mint underline, recorded in the sidecar.

With reduced motion not requested, controls transition background/border colors over 160ms ease-out. Disclosure rotation uses 160ms cubic-bezier(.16, 1, .3, 1). The connected dot changes color once over 600ms with the same curve. Reduced-motion mode omits these declarations.

## Shapes

Panels use the panel radius; alerts, controls, fields, and badges use their frontmatter radii. Borders and separators are 1px. Status dots are 6px circles. Chart swatches are 7px squares with 2px corners. SVG bars use 2px to 4px corners. Navigation uses 7px top corners on mobile. Inline icons use 17px frames with rounded 1.6px strokes; navigation icons are 18px and the brand mark is 30px, narrowing to 25px at the middle breakpoint.

## Components

Buttons have a 42px minimum height. Secondary hover changes fill and border; active darkens the fill. Primary has mint fill, dark ink, and a lighter hover. Quiet starts transparent. Disabled buttons use opacity .42 and a not-allowed cursor. Presets are 40px tall; icon controls are 40px, reducing to 36px on mobile; table disclosures are 36px. Inline Inspect request buttons use mint text, transparent fill, no border, a 36px minimum height, and wrapping IDs.

Fields have visible labels, a 7px label gap, 44px minimum height, and a border that brightens on hover. Disabled fields use opacity .6. Textareas resize vertically. The client key is masked; the 16px native checkbox uses mint. Focusable controls and scroll regions share a 2px mint outline with 4px offset. A focus-revealed skip link targets main content. Required fields use native validation; no custom invalid-border treatment exists.

Navigation links show page names and desktop task subtitles. The current link has mint text, a green-black fill, and a marker. On mobile, subtitles/marker hide and the inset underline marks the current page. Badges are informational labels with no custom interaction states.

Page composition follows the existing brief:

- Overview `/` shows scoped metrics, Playground guidance, latency/outcome charts, provider/distribution charts, client budgets/cache, and the latest eight requests. It has no request filters or fault-control form.
- Requests `/requests` shows the request explorer with search, outcome filters, clear filters, paging, and expandable provider attempts. Search applies only to loaded records, initially the latest 100. Details expose full IDs, reported/estimated usage, timing, estimated cost, and explicit explanations for cache hits or pre-routing endings.
- Playground `/playground` contains the request form/response, failover demo, provider routing and presets, then a link to investigate results. Completed responses and demo results link to `/requests?request=<id>`, which searches retained history automatically, prefilters, and expands the matching record. If the gateway no longer retains that ID, show an explicit unavailable message. Playground stays mounted while hidden on other pages, preserving output and operation state. Provider counters here are lifetime counters. Fault controls require local demo mode and a fake provider. Requests, failover runs, and provider configuration changes share one operation state so their controls cannot compete.

**The Measurement Scope Rule.** Keep lifetime, retained, and latest-100 evidence visibly distinct. Loading earlier explorer records does not expand overview charts.

Overview client totals, success/received ratio, estimated spend, attempts, and fallbacks are lifetime values. Its p95/p50 come from retained latency samples. All four chart panels use the latest 100 request records: latency plots non-rejected durations by start time; outcomes include success, error, rejected, and canceled; distribution uses eight duration buckets excluding rejections. Provider traffic compares final uncached routes with all attempts, including unsuccessful attempts. Circuit state is current snapshot evidence, independent of that chart window. Cache hits have no provider attempt. Charts include SVG summaries, axes, legends where applicable, and per-observation titles on latency points and histogram buckets.

Budgets show lifetime estimated spend plus active reservations, remaining balance, and configured limits. Meter widths cap at the available track. Zero budgets and committed budgets have explicit text; cache totals remain lifetime counters. Costs retain six decimal places in USD.

Feed states are loading, live, stale, and offline. Polling retries every two seconds while the tab is visible. Initial failure shows an alert with retry; stale state keeps last-known data and labels it. Empty charts/tables explain how to create observations; filtered-empty and never-used states differ. Paging errors remain visible beside retained records.

Response states are Ready, streaming, completed, interrupted, and error. Partial output remains after interruption; completion metadata appears only for completed output. Display is limited to 32,000 characters. Conflicting request/demo actions and fields disable while busy; Stop remains available for the active operation. Provider changes show pending, applied, or error and disable that provider's presets while pending. Demo progress/results persist, and interrupted/error copy warns that applied provider settings may remain in effect. Recovery requires a subsequent request after cooldown.

## Do's and Don'ts

### Do

- Keep a black workspace and reuse the charcoal panel, field, control, and badge treatments.
- Keep observation scopes, units, estimated-cost labels, and usage provenance visible.
- Preserve page separation, local table scrolling, wrapped output, and keyboard focus.
- Distinguish empty, filtered-empty, stale, pending, interrupted, and completed states.

### Don't

- Fill empty charts with invented traffic or represent synthetic providers as paid observations.
- Compare request counts and provider attempts as if they were the same quantity.
- Present a partial stream as completed or a restored provider setting as confirmed circuit recovery.
- Add gradients, ambient shadows, decorative imagery, or marketing sections to the console.
