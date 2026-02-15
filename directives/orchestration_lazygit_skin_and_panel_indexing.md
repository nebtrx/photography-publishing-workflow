# Orchestration: LazyGit-Inspired Skin and Indexed Headers

## Phase
Orchestration and execution in one pass (requested).

## Objective
Execute `directives/lazygit_skin_and_panel_indexing.md` end-to-end: define visual skin, add numbered panel headers, validate, and log.

## Workstreams

## WS1: Skin Token Update
- Owner: TUI styles
- Deliverables:
  - LazyGit-inspired color palette in `internal/tui/styles.go`
- Risks:
  - poor contrast on some terminals
- Mitigation:
  - keep contrast-safe text colors for dim/normal/error states

## WS2: Indexed Header Rendering
- Owner: TUI view rendering
- Deliverables:
  - Numeric bracket prefixes on panel headers in `internal/tui/app.go`
  - Consistent format: `[n]-Panel Name`

## WS3: Verification and Memory
- Owner: quality/docs
- Deliverables:
  - test pass report
  - technical log update in `TECHNICAL.md`

## Execution Order
1. WS1
2. WS2
3. WS3

## Handoff Gates
- Gate A:
  - Palette compiles and visibly updates active/inactive borders/titles.
- Gate B:
  - All target headers render with indexed format.
- Gate C:
  - Tests pass and memory entry recorded.

## Resume Checklist
- [x] Directive created
- [x] Orchestration created
- [x] Execute WS1/WS2 code updates
- [x] Execute WS3 validation + logging

## Learnings (append-only)
- Indexed header format works best when applied consistently to both left navigation panels and right detail/log panels.
