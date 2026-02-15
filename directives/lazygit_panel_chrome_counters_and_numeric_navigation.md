# Directive: LazyGit Panel Chrome, Counters, and Numeric Navigation

## Goal
Replicate three specific LazyGit interaction/design elements in the TUI:
1. Panel numbers/title rendered in the border line (not inside panel content).
2. Bottom-right per-panel counters (e.g. `1 of 8`).
3. Direct numeric navigation keys to jump between panels.

## Context / Constraints
- Existing TUI already uses LazyGit-inspired colors and partial indexed titles.
- Runtime log panel is informational and should not be number-addressable.
- Current panel sizing/render stability fixes must remain intact.

## Inputs
- `internal/tui/app.go`
- `internal/tui/styles.go`

## Outputs
- Border-line titles for selectable panels.
- Bottom-right counters for list panels.
- Number key bindings for panel navigation.
- Technical log update in `TECHNICAL.md`.

## Steps
1. Introduce panel chrome renderer with title embedded into top border.
2. Add optional footer counter rendering in bottom border.
3. Apply chrome to main left and right detail panels.
4. Add numeric key handlers for panel switching.
5. Validate with `/opt/homebrew/bin/go test ./...`.

## Acceptance Criteria
- [x] Titles are drawn in panel border line.
- [x] Counters render at bottom-right of list panels.
- [x] Number keys navigate panel focus.
- [x] Runtime log panel remains unnumbered/non-selectable.
- [x] Tests pass.

## Learnings (append-only)
- Border-line panel chrome is easier to keep deterministic with explicit string rendering than with style-height overlays.
