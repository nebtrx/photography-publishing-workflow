# Orchestration: Default Enter Actions and Shortcut Simplification

## Phase
Orchestration only. No code execution in this artifact.

## Objective
Execute `directives/default_enter_actions_and_shortcuts.md` with minimal regression risk and consistent key behavior across TUI contexts.

## Scope
- In scope:
  - default Enter action contract
  - editor/approve/publish Enter mappings
  - fallback newline mechanism for caption editing
  - UI hints + tests
- Out of scope:
  - full keybinding redesign
  - visual theme/layout changes

## Current Baseline (confirmed)
- Editor save uses `Ctrl+S`.
- Approval/publish flows rely primarily on letter shortcuts.
- Enter behavior is not consistently mapped to primary actions.

## Workstreams

## WS1: Interaction Contract
- Owner: TUI UX logic
- Deliverables:
  - matrix of contexts and their default Enter actions
  - explicit non-go defaults (no destructive Enter actions)
- Dependencies: none
- Output contract:
  - single source of truth for key behavior

## WS2: Key Handling Implementation
- Owner: TUI runtime
- Deliverables:
  - Enter mapped in editor, approval dialog, queue publish contexts
  - newline fallback in editor
- Dependencies: WS1
- Output contract:
  - deterministic Enter behavior by context

## WS3: Help/Hints Synchronization
- Owner: TUI copy
- Deliverables:
  - overlay/action help text updated to show defaults
  - default action visually indicated
- Dependencies: WS2
- Output contract:
  - no stale key docs in UI

## WS4: Tests and Validation
- Owner: quality
- Deliverables:
  - regression tests for Enter behavior
  - manual walkthrough checklist
  - memory update entry in `TECHNICAL.md`
- Dependencies: WS2, WS3
- Output contract:
  - passing tests and verified keyboard flow

## Execution Order
1. WS1
2. WS2
3. WS3
4. WS4

## Handoff Gates
- Gate A:
  - context/default-action matrix approved.
- Gate B:
  - Enter behavior implemented and no destructive side effects.
- Gate C:
  - help text aligned with actual behavior.
- Gate D:
  - tests and manual checks complete.

## Risks and Mitigations
- Risk: Enter save removes ability to insert newlines in captions.
  - Mitigation: explicit alternate newline shortcut + hint.
- Risk: Enter ambiguity in non-focused states.
  - Mitigation: no-op with status hint when no selection/context.
- Risk: key regressions in legacy TUI path.
  - Mitigation: include legacy path in validation matrix.

## Resume Checklist (for execution session)
- [x] Directive created
- [x] Orchestration created
- [x] Implement WS1 contract
- [x] Implement WS2 key handling
- [x] Implement WS3 hints/docs
- [x] Run WS4 tests and update memory

## Learnings (append-only)
- Add execution findings (newline ergonomics, context conflicts, fallback choices).
- `Enter` defaults are now context-driven and explicit in UI copy; multiline editing preserved via `Alt+Enter` / `Shift+Enter`.
