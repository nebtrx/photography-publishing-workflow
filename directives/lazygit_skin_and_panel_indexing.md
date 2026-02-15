# Directive: LazyGit-Inspired TUI Skin and Indexed Panel Headers

## Goal
Adopt a LazyGit-inspired visual skin for the unified TUI and add bracketed numeric panel headers (e.g. `[1]-Pending Review`) across main panels, while preserving existing workflow behavior.

## Context / Constraints
- This is a visual/UX refinement on the current BubbleTea/Lipgloss TUI.
- Existing layout, keybindings, and publishing behavior must remain intact.
- Colors should be inspired by LazyGit’s dark lavender + mint accent theme.
- The numbering approach should be visible in panel titles, consistent with LazyGit’s panel labeling style.

## Inputs
- Existing TUI code:
  - `internal/tui/styles.go`
  - `internal/tui/app.go`
- Existing render-stability fixes must not regress.

## Outputs
- Updated style palette and panel title rendering.
- Numbered titles for core panels (left + right).
- Technical log entry in `TECHNICAL.md`.

## Steps (high-level)
1. Define LazyGit-inspired palette in shared TUI styles.
2. Add indexed panel title helpers and apply to all panel headers.
3. Ensure right-side panels (`Detail`, `Runtime Log`) use the same indexed-header approach.
4. Validate render output and run test suite.
5. Log changes in `TECHNICAL.md`.

## Acceptance Criteria
- [x] TUI palette matches LazyGit-inspired look (dark lavender base + mint active accents).
- [x] Main panel titles include bracketed numeric prefixes.
- [x] Right-side panel headers also use indexed style.
- [x] No functional regressions in navigation/publishing flow.
- [x] `/opt/homebrew/bin/go test ./...` passes.

## Learnings (append-only)
- LazyGit-like readability depends more on border/title contrast and selected-item accent than on exact hex parity.
