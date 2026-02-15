# Orchestration: LazyGit Panel Chrome, Counters, and Numeric Navigation

## Phase
Orchestration + execution in current session.

## Objective
Execute `directives/lazygit_panel_chrome_counters_and_numeric_navigation.md` without regressing TUI layout stability.

## Workstreams

## WS1: Border Chrome Renderer
- Deliverables:
  - top-border title placement
  - bottom-border optional footer text
- Risk:
  - width/ANSI rendering drift
- Mitigation:
  - width-safe rendering with lipgloss width helpers

## WS2: Counter Integration
- Deliverables:
  - `x of y` counters on list panels
  - safe behavior for empty lists (`0 of 0`)

## WS3: Numeric Navigation
- Deliverables:
  - numeric key mapping to panel focus
  - non-selectable runtime log remains excluded

## WS4: Validation + Memory
- Deliverables:
  - tests pass
  - technical log entry

## Execution Order
1. WS1
2. WS2
3. WS3
4. WS4

## Resume Checklist
- [x] Directive created
- [x] Orchestration created
- [x] WS1 implemented
- [x] WS2 implemented
- [x] WS3 implemented
- [x] WS4 completed

## Learnings (append-only)
- Numeric jump keys are cleanly mapped to selectable panels only (`1..4`) to avoid misleading affordances on informational panels.
