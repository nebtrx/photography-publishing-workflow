# Directive: Default Enter Actions and Shortcut Simplification

## Goal
Make the TUI easier/faster to operate by introducing a clear default action model where `Enter` executes the primary action in context (editing, approval, and publishing), while keeping existing explicit shortcuts as secondary paths.

## Context / Constraints
- Current interaction is keyboard-heavy but not uniform:
  - caption edit save currently uses `Ctrl+S`
  - publish actions rely on letter-case shortcuts (`p`/`P`, `q`, etc.)
- User requirement:
  - `Enter` should trigger default action
  - in caption edit overlay, default action = save
  - in approval flow, default action should start with queue (`q`) preference
- Preserve advanced shortcuts for power users.
- Do not break multiline caption editing without a documented alternative.

## Inputs
- Required:
  - `internal/tui/app.go`
  - `internal/tui/tui.go` (if legacy path still active)
  - existing keymaps/action hints in detail/overlay views
- Optional:
  - user preference for newline key in editor (`Shift+Enter` or `Alt+Enter`)
- Environment variables:
  - none

## Outputs
- Files created/updated:
  - TUI key handling + help/action copy in `internal/tui/*`
  - tests for key behavior in `internal/tui/*_test.go`
- Report format:
  - concise behavior matrix (`context -> Enter action`)
- Where results should be saved:
  - source files + `TECHNICAL.md` entry

## Steps (high-level)
1. Define a default-action contract per context:
   - caption editor: `Enter -> save`
   - approve dialog: `Enter -> queue` (default)
   - queue panel selected item: `Enter -> publish selected`
   - publish-all dialog (if present): `Enter -> confirm default`
2. Implement key handling and preserve legacy shortcuts (`Ctrl+S`, `p`, `q`, etc.).
3. Provide explicit multiline caption fallback (e.g., `Shift+Enter` newline) and update hints.
4. Update action text/help so default action is visibly marked.
5. Add regression tests for Enter behavior in each context.

## Edge Cases / Failure Modes
- Enter can conflict with text input semantics:
  - must explicitly support an alternate newline key for captions.
- Context ambiguity:
  - Enter should do nothing destructive when no item is selected.
- Modal overlays:
  - Enter should map to modal primary CTA only.

## Acceptance Criteria
- [ ] `Enter` saves caption in editor overlay.
- [ ] `Enter` in approve dialog performs queue/default approval action.
- [ ] `Enter` in queue panel publishes selected queued post.
- [ ] Existing shortcuts continue to work.
- [ ] Help/action hints clearly indicate default Enter action.
- [ ] Tests cover Enter mapping across contexts.

## Safety Notes
- Destructive operations (reject/remove/dequeue) must not become implicit Enter defaults.

## Learnings (append-only)
- Add discoveries during execution (best newline fallback, ergonomic conflicts, legacy path mismatches).
