# Directive: TUI Render Stability and Integrated Log Panel

## Goal
Eliminate TUI deformation during interaction and background activity by enforcing strict render bounds and routing all runtime logs into an in-app log panel. The UI must remain visually stable while typing in overlays, running watcher/pipeline/publish jobs, and receiving frequent log events.

## Context / Constraints
- Current BubbleTea/Lipgloss layout occasionally deforms when:
  - Editing captions or opening overlays.
  - Background jobs emit log output to stdout/stderr.
- Existing behavior appears to mix alternate-screen rendering with raw terminal writes.
- The user requested a permanent log area by splitting the right column:
  - Top: detail view.
  - Bottom: log panel (~25% to 30% of right column height).
- Preserve current keyboard-first workflow and panel structure.
- Keep existing CLI subcommands functional; changes apply to TUI runtime path.

## Inputs
- Required:
  - Existing TUI implementation in `internal/tui/*`.
  - Existing app wiring in `cmd/ppw/default.go`.
  - Background operation emitters (`pipeline`, `publisher`, watcher integration).
- Optional:
  - Real-world screenshot samples of deformation states.
  - Additional terminal-size constraints from user.
- Environment variables:
  - None required for layout stability itself.

## Outputs
- Files created/updated:
  - `internal/tui/*` (layout + rendering)
  - optional shared logging adapter package if needed
  - docs touching TUI behavior (`README.md` or directive references as needed)
- Report format:
  - short implementation summary + file references + before/after behavior
- Where results should be saved:
  - source changes in repo + technical log entry in `TECHNICAL.md`

## Steps (high-level)
1. Reproduce and isolate deformation paths:
   - Overlay editor rendering under active background logs.
   - Publish/pipeline/watcher logs emitted while TUI runs.
2. Introduce TUI-safe log transport:
   - No direct stdout/stderr writes while TUI alternate screen is active.
   - Convert runtime logs into message events consumed by the TUI model.
   - Maintain a bounded in-memory ring buffer.
3. Implement right-column split layout:
   - Right-top: detail panel.
   - Right-bottom: dedicated log panel with independent border.
   - Default split target: 70/30 (allow minor clamp for small terminals).
4. Harden render bounds:
   - Clamp panel widths/heights to non-negative values.
   - Wrap/truncate long lines safely.
   - Reflow on resize without border overlap or bleed.
5. Validate with stress scenarios:
   - Continuous log spam during overlay text editing.
   - Rapid terminal resizes.
   - Multi-line and long-token log entries.

## Edge Cases / Failure Modes
- Terminal too small:
  - degrade gracefully (reduced log panel height, then compact mode message if needed).
- Extremely long log tokens:
  - hard-wrap or truncate with deterministic behavior.
- High-frequency logs:
  - ring buffer cap + viewport scrolling to prevent memory/render blowups.
- External dependency writing directly to stderr:
  - capture/redirect in TUI runtime path when possible; if not possible, suppress and surface summary in log panel.

## Acceptance Criteria
- [x] TUI no longer deforms when background jobs emit logs.
- [x] TUI no longer deforms while caption edit overlay is open and user types.
- [x] Right column is split into two independently bordered panels:
- top detail + bottom log panel (~25% to 30% log height target).
- [x] All runtime log events visible in log panel, newest entries appended in order.
- [x] No direct stdout/stderr writes from TUI runtime path that break alternate-screen rendering.
- [x] Layout remains stable during terminal resize and high log throughput.

## Safety Notes
- Do not drop critical errors silently; if logs are throttled, emit explicit dropped-count messages in panel.
- Keep destructive actions (publish/archive) behavior unchanged.

## Learnings (append-only)
- Rendering instability was caused by both external logger streams and internal overlay compositing.
- Fixed panel heights and message-driven log routing are enough to keep BubbleTea layout stable under load.
