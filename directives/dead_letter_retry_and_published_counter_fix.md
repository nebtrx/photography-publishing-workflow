# Directive: Dead-Letter Retry Flow and Published Counter Fix

## Goal
Introduce a dedicated dead-letter workflow in the TUI for failed posts, with stage-aware retry behavior, and fix published-panel counters so they count only selectable entries (leaf posts), not month headers.

## Context / Constraints
- Current failed publishes end in `state=error` and are easy to lose from normal review/queue flow.
- Retry intent must be explicit and deterministic:
  - user fixes files/config outside the app
  - user retries from the TUI
  - retry resumes from the stage that failed (not an unrelated stage).
- Existing manifests already contain `errors[]`; we need richer failure metadata.
- Preserve keyboard-first UX and existing panel navigation style.

## Inputs
- Required code areas:
  - `internal/manifest/*`
  - `internal/pipeline/*`
  - `internal/publisher/*`
  - `internal/archiver/*`
  - `internal/tui/*`
- Required behavior references:
  - current state machine transitions in `internal/manifest/manifest.go`
  - current panel counters in `internal/tui/app.go`

## Outputs
- A new dead-letter section/panel in the TUI.
- Manifest failure metadata sufficient for stage-aware retry.
- Retry command/action from dead-letter items.
- Published-panel counters corrected to leaf-only counts.
- Tests covering:
  - failure stage recording
  - retry state mapping
  - dead-letter panel loading
  - leaf-only counters.

## Proposed Data Model
- Add explicit failure metadata to manifest (example structure; exact naming may vary):
  - `failure.stage` (enum-like string): `scan|validate|enrich|publish|archive|syndicate`
  - `failure.message` (latest failure summary)
  - `failure.occurred_at` (timestamp)
  - `failure.retry_from_state` (state to restore before retry)
- Keep `errors[]` append-only for history.
- Preserve compatibility with existing `state=error` manifests:
  - either migrate to `state=failed`
  - or keep `state=error` and treat as dead-letter equivalent.

## Retry Semantics (Required)
- Retry must map to the original failed stage:
  - failed at `validate` -> retry from `scanned`/`validated` path
  - failed at `enrich` -> retry enrich path (starting from `validated`)
  - failed at `publish` -> retry publish path (starting from `approved`)
  - failed at `archive` -> retry archive path (starting from `published`)
  - failed at `syndicate` -> retry syndication stage from post-publish context
- TUI should show both:
  - failed stage
  - last error summary
- Retry action should refuse execution if required preconditions are missing, with clear status message.

## UI Requirements
- Add left-column panel: `Dead Letter` (or `Failed`) with item list.
- Selecting an item shows in detail pane:
  - current state
  - failed stage
  - last error
  - retry hint
- Provide single retry action from this panel:
  - keybinding shown in panel actions
  - invokes stage-aware retry.

## Published Counter Fix
- Current published counter mixes group headers + entries.
- Change counter logic so:
  - numerator = selected leaf index (post entry only)
  - denominator = total leaf entries (post entries only)
- Month group headers remain navigable for collapse, but not counted as leaf totals.

## Steps
1. Extend manifest schema with failure metadata and update read/write compatibility.
2. Capture failure stage+message at each failure source (pipeline/publish/archive/syndication).
3. Implement retry mapping utility (`failed_stage -> retry_state + runner`).
4. Add dead-letter loader and panel rendering in TUI.
5. Add dead-letter detail view + retry command wiring.
6. Fix published leaf counter logic in TUI.
7. Add/adjust tests and validation.

## Acceptance Criteria
- [x] Failed posts are visible in a dedicated dead-letter panel.
- [x] Detail pane for failed posts includes failed stage and last error.
- [x] Retry action sends the post back to the correct failed stage path.
- [x] Retry behavior is deterministic and test-covered.
- [x] Published panel counter shows `x of y` based only on leaf posts.
- [x] `/opt/homebrew/bin/go test ./...` passes.

## Out of Scope
- Automatic retries without user intent.
- Full redesign of panel layout/theme.
- Bulk retry policy tuning.

## Learnings (append-only)
- Keep this section updated during execution with real migration edge cases and retry semantics discovered in testing.
- Implemented with `manifest.Failure` metadata + `PrepareRetry()` so dead-letter retry is stage-aware and backward-compatible with legacy `state=error` manifests.
- Queue panel now only shows actionable publish states (`approved`, `scheduled`); all failed posts live in the dedicated `[4]-Failed` panel.
- Published panel footer counter now tracks leaf entries only, so month headers no longer skew `x of y`.
