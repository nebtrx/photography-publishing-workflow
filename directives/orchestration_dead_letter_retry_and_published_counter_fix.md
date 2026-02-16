# Orchestration: Dead-Letter Retry Flow and Published Counter Fix

## Phase
Orchestration only. No implementation changes in this artifact.

## Objective
Execute `directives/dead_letter_retry_and_published_counter_fix.md` with clear workstreams and handoff gates, so execution can continue later without ambiguity.

## Scope
- In scope:
  - dead-letter panel for failed posts
  - failure-stage metadata capture
  - stage-aware retry behavior
  - published leaf-counter correction
  - tests and migration safety
- Out of scope:
  - UI skin/theme redesign
  - non-failure workflow redesign
  - auto-retry automation

## Current Baseline (confirmed)
- Manifest has `state=error` and `errors[]`.
- TUI has primary left panels and a published grouped list by month.
- Published counter currently counts flat cursor positions (includes headers).
- Retry ergonomics for failed posts are partial/inconsistent across stages.

## Workstreams

## WS1: Failure Model and State Contract
- Owner: manifest/core state
- Deliverables:
  - failure metadata contract (`stage`, `message`, `occurred_at`, retry mapping token)
  - transition policy for failed/dead-letter state
  - compatibility plan for existing `state=error` manifests
- Dependencies: none
- Output contract:
  - explicit schema + transition matrix documented in code comments/tests

## WS2: Failure Capture Instrumentation
- Owner: pipeline/publisher/archive/syndication
- Deliverables:
  - consistent failure-stage recording at each failure point
  - latest error summary to detail panel-friendly field
- Dependencies: WS1
- Output contract:
  - failed post always contains stage + message metadata

## WS3: Retry Engine (Stage-Aware)
- Owner: runtime orchestration
- Deliverables:
  - deterministic mapping: `failed_stage -> retry entrypoint`
  - retry precondition checks with actionable errors
  - reusable helper consumed by TUI action and CLI hooks if needed
- Dependencies: WS1, WS2
- Output contract:
  - retry runs from failed stage path and is test-covered

## WS4: Dead-Letter TUI Panel
- Owner: TUI
- Deliverables:
  - new left panel (Failed/Dead Letter)
  - list loader + cursor navigation
  - detail pane rendering for stage + message + retry action hints
  - retry keybinding wiring
- Dependencies: WS2, WS3
- Output contract:
  - failed items never disappear from operator workflow

## WS5: Published Counter Leaf Fix
- Owner: TUI
- Deliverables:
  - counter helpers split for grouped headers vs leaf entries
  - `x of y` in published panel reflects only leaf posts
- Dependencies: none
- Output contract:
  - correct counts even when month groups are collapsed/expanded

## WS6: Verification and Docs
- Owner: quality/docs
- Deliverables:
  - unit tests for WS1-WS5 behavior
  - manual verification checklist (fail -> dead-letter -> retry)
  - execution log entry in `TECHNICAL.md`
- Dependencies: WS1-WS5
- Output contract:
  - clean `go test` and reproducible operator flow

## Execution Order
1. WS1
2. WS2
3. WS3
4. WS4 + WS5
5. WS6

## Handoff Gates
- Gate A (after WS1):
  - failure schema and transition contract finalized.
- Gate B (after WS2):
  - every major failure source emits failure stage metadata.
- Gate C (after WS3):
  - retry mapping works for publish/enrich/archive failure cases.
- Gate D (after WS4/WS5):
  - dead-letter panel live and published counter leaf-correct.
- Gate E (after WS6):
  - tests green; memory/doc log complete.

## Risks and Mitigations
- Risk: legacy manifests without new failure fields.
  - Mitigation: tolerant defaults + lazy backfill on first write.
- Risk: ambiguous retry mapping for mixed-stage failures.
  - Mitigation: store authoritative latest failed stage and use it as retry source.
- Risk: panel/keybinding overload.
  - Mitigation: keep single retry action and clear contextual status messaging.
- Risk: counter regressions in grouped lists.
  - Mitigation: dedicated leaf-count tests for expanded/collapsed states.

## Resume Checklist (for execution session)
- [x] Directive created
- [x] Orchestration plan created
- [x] Implement WS1 schema + transitions
- [x] Implement WS2 failure capture
- [x] Implement WS3 retry engine
- [x] Implement WS4 dead-letter panel
- [x] Implement WS5 published leaf counter fix
- [x] Run WS6 verification + update TECHNICAL log

## Learnings (append-only)
- Add findings during execution (state migration pitfalls, retry edge cases, TUI interaction improvements).
- Failure stage metadata and retry intent now live in manifest-level primitives (`RecordFailure`, `PrepareRetry`) and are reused by pipeline/publisher/archiver/TUI.
