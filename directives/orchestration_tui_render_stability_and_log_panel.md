# Orchestration: TUI Render Stability and Log Panel

## Phase
Orchestration artifact completed; execution has been finished in code and logged in `TECHNICAL.md`.

## Objective
Execute `directives/tui_render_stability_and_log_panel.md` with clear workstreams, explicit dependencies, and verification gates so implementation can continue in a later session without ambiguity.

## Scope
- In scope:
  - root-cause isolation for TUI deformation
  - log-routing architecture for TUI-safe rendering
  - right-panel split (detail + log)
  - render-bound hardening and stress validation
- Out of scope:
  - redesigning core workflow semantics
  - replacing BubbleTea/Lipgloss stack

## Current Baseline (confirmed)
- Unified TUI exists under `internal/tui/*`.
- Background jobs run while TUI is active (watcher/pipeline/publish).
- Background activity currently emits lines that can corrupt alternate-screen layout.
- Overlay editor path can visually deform under concurrent updates.

## Workstreams

## WS1: Reproduction Harness
- Owner: TUI/runtime
- Deliverables:
  - deterministic reproduction steps
  - minimal stress scenario (overlay + log spam + resize)
  - baseline screenshots/notes
- Dependencies: none
- Output contract:
  - concise bug matrix with triggers and expected fixed behavior

## WS2: TUI-Safe Logging Pipeline
- Owner: runtime plumbing
- Deliverables:
  - centralized log sink for TUI mode (message-based, no raw terminal writes)
  - bounded ring buffer model for log entries
  - adapters for watcher/pipeline/publish events
- Dependencies: WS1 trigger mapping
- Output contract:
  - all runtime logs appear through TUI messages only

## WS3: Layout Split + Bounds Hardening
- Owner: TUI rendering
- Deliverables:
  - right column split into:
    - detail panel (top)
    - log panel (bottom, ~25%-30%)
  - strict bounds clamping for all panel dimensions
  - safe wrapping/truncation for long lines
- Dependencies: WS2 log data source
- Output contract:
  - no border overlap/bleed at common terminal sizes

## WS4: Overlay Stability
- Owner: TUI interactions
- Deliverables:
  - overlay editor geometry clamp and redraw stability
  - no corruption during active typing under background updates
- Dependencies: WS3
- Output contract:
  - stable overlay under load and resize

## WS5: Verification + Docs
- Owner: quality/docs
- Deliverables:
  - regression tests where feasible
  - manual verification checklist execution
  - memory/doc updates (`TECHNICAL.md`)
- Dependencies: WS2-WS4
- Output contract:
  - passing validation + reproducible checklist

## Execution Order
1. WS1
2. WS2
3. WS3 + WS4
4. WS5

## Handoff Gates
- Gate A (after WS1):
  - Repro steps validated against current main branch.
- Gate B (after WS2):
  - No direct stdout/stderr writes in TUI path for background logs.
- Gate C (after WS3/WS4):
  - Layout and overlay stable during stress scenario.
- Gate D (after WS5):
  - Tests/checklist complete; technical log updated.

## Risks and Mitigations
- Risk: Hidden third-party writes to stderr still break layout.
  - Mitigation: wrap command execution streams in TUI mode and route to model logger.
- Risk: Narrow terminals cause new split to crowd primary detail panel.
  - Mitigation: adaptive clamp and compact fallback thresholds.
- Risk: Log throughput causes frame drops.
  - Mitigation: ring buffer cap + incremental rendering + optional throttle markers.

## Resume Checklist (for next execution session)
- [x] Directive created and approved for execution
- [x] Workstreams defined and ordered
- [x] Implement WS1 reproduction harness
- [x] Implement WS2 log routing
- [x] Implement WS3/WS4 rendering fixes
- [x] Run WS5 validation and update memory

## Learnings (append-only)
- Runtime deformation had two root causes:
- raw stderr log writers active during alt-screen (watcher/pipeline/publisher)
- fragile manual overlay compositing (`placeOverlay`) that rewrote buffer lines unsafely.
