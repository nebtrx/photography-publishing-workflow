# Directive: Unified TUI (LazyGit-style)

## Goal

Replace the multi-subcommand workflow with a single, unified terminal UI launched by `ppw` (no subcommand). The TUI presents all pipeline stages — pending review, publish queue, published log, and configuration — in a lazygit-style multi-panel layout. The user navigates between panels, reviews posts, edits captions, approves/rejects, publishes, and browses history — all without leaving the app.

## Context / Constraints

- Modeled after lazygit's UX: bordered panels on the left, detail panel on the right, keyboard-driven navigation, overlay popups for editing.
- Each panel has its own **fully bordered box** with visible separation between panels (no shared borders).
- The watcher runs as a background goroutine inside the TUI — no separate panel needed. New posts appear automatically in the Pending Review panel.
- The pipeline (scan → validate → enrich) runs automatically when the watcher detects a new directory.
- All existing engine packages (`watcher`, `pipeline`, `publisher`, `archiver`, `scheduler`) are reused as-is. Only the presentation layer changes.
- Existing CLI subcommands remain available for scripting/automation. `ppw` with no args launches the TUI.
- BubbleTea + Lipgloss (same stack as the existing review TUI, which gets replaced).

## Layout

```
┌─ Config ─────────────────────┐ ┌─ Detail ─────────────────────────────────────┐
│ Watch: ~/Lightroom/Export    │ │                                               │
│ AI:    claude-cli            │ │  (changes based on active panel + selection)  │
│ Corpus: config/corpus.json   │ │                                               │
└──────────────────────────────┘ │  Pending Review → post detail + caption       │
┌─ Pending Review (3) ────────┐ │  Publish Queue  → post detail + queue info    │
│ ▸ erasmusbrug-sunset    [4] │ │  Published Log  → post detail + publish info  │
│   canal-houses          [2] │ │  Config         → full config display         │
│   markthal-interior     [6] │ │                                               │
└──────────────────────────────┘ │                                               │
┌─ Publish Queue (1) ─────────┐ │                                               │
│   dom-tower             [3] │ │                                               │
└──────────────────────────────┘ │                                               │
┌─ Published ──────────────────┐ │                                               │
│ ▾ 2026-02 (3)               │ │                                               │
│   centraal-station      [5] │ │                                               │
│   vondelpark-morning    [2] │ │                                               │
│   brouwersgracht        [4] │ │                                               │
│ ▸ 2026-01 (7)               │ │                                               │
└──────────────────────────────┘ └───────────────────────────────────────────────┘
─── Status: Watching ~/Lightroom/Export │ 3 pending │ 1 queued ─────── ppw v1 ──
```

### Panel Sizing

- Left column: ~35% of terminal width.
- Right column (Detail): ~65% of terminal width.
- Left panels share the vertical space. Config is fixed height (small). The other three panels split remaining height, with Pending Review getting the most space.
- All panels are independently scrollable.

## Panels

### 1. Config Panel

**Purpose**: Display current app configuration.

**Content**:
- Watch directory path
- AI provider name
- Style corpus path
- Archive directory
- Log file path

**Actions**:
- `e` → Open config file in `$EDITOR` (or `vim` fallback). TUI pauses, resumes when editor exits.

**Behavior**:
- Read-only display. Not editable inline.
- Config is loaded on TUI startup from `config/ppw.toml` or env vars.
- After returning from `$EDITOR`, config is reloaded.

### 2. Pending Review Panel

**Purpose**: Show posts that have completed the pipeline (scan → validate → enrich) and are waiting for user review. State: `pending_review`.

**Content per item**:
- Post ID (directory name)
- Image count `[N]`

**Detail view (right panel)** when an item is selected:
- Image list with dimensions, aspect ratio, hero marker
- Caption (AI-generated or edited)
- Location (if identified)
- Story toggle status
- Keybinding hints

**Actions**:
- `Enter` → Open hero image in system viewer
- `e` → Edit caption (overlay popup editor)
- `s` → Toggle story on/off
- `a` → Approve post → confirmation dialog: "Publish now or add to queue?"
  - Publish now → moves to publishing (background), then Published Log
  - Add to queue → moves to Publish Queue panel
- `r` → Reject post → confirmation: "Reject this post?" → removes from list (state → `rejected`)
- `R` → Re-enrich → resets to `validated`, re-runs AI enrichment in background
- `←` `→` or `h` `l` → Browse images within the post
- `↑` `↓` or `j` `k` → Navigate between posts

### 3. Publish Queue Panel

**Purpose**: Show posts approved and waiting to be published. Queue is manually ordered. State: `approved` or `scheduled`.

**Content per item**:
- Post ID
- Image count `[N]`

**Detail view (right panel)**:
- Same as Pending Review detail, plus:
  - Queue position
  - Queued timestamp

**Actions**:
- `p` → Publish selected post now (runs publisher in background, shows progress in status bar)
- `P` → Publish all queued posts in order (sequential, with progress)
- `Enter` → Open hero image
- `↑` `↓` → Navigate
- `d` → Remove from queue (move back to Pending Review)

### 4. Published Log Panel

**Purpose**: Browse history of published posts. State: `published` or `archived`. Read-only.

**Content**:
- Grouped by month (`2026-02`, `2026-01`, etc.)
- Collapsible month groups (▾ expanded, ▸ collapsed)
- Post ID + image count per entry

**Detail view (right panel)**:
- Caption (final published version)
- Location
- Instagram post ID
- Permalink
- Published timestamp
- Story published (yes/no)
- Archive path

**Actions**:
- `Enter` → Open permalink in browser
- `↑` `↓` → Navigate
- `Space` → Toggle month group expand/collapse

## Navigation

- **Tab** → Cycle forward through panels: Config → Pending Review → Publish Queue → Published Log → Config...
- **Shift-Tab** → Cycle backward.
- Active panel has a **highlighted border** (accent color). Inactive panels have a dimmed border.
- `↑` `↓` or `j` `k` → Navigate items within the active panel.
- `q` → Quit the TUI.
- `?` → Show help overlay with all keybindings.

## Overlay Popup Editor

When editing a caption (`e` in Pending Review):

```
┌─ Edit Caption ─────────────────────────────────────────┐
│                                                         │
│  The quiet geometry of #architecture reveals itself     │
│  in the morning light, when every line becomes a        │
│  conversation between #shadow and #concrete             │
│                                                         │
│                                                         │
│                                                         │
│                                       142/2200 chars    │
│                                                         │
│  Ctrl+S: save   Esc: cancel                             │
└─────────────────────────────────────────────────────────┘
```

- Centered on screen, floating over the panels.
- Full textarea with line wrapping.
- Character counter (warns at >2200).
- `Ctrl+S` saves and closes. `Esc` cancels.

## Confirmation Dialogs

Lazygit-style centered popup:

```
┌─ Approve Post ────────────────────┐
│                                    │
│  Publish now or add to queue?      │
│                                    │
│  [p] Publish now   [q] Add to queue│
│  [Esc] Cancel                      │
└────────────────────────────────────┘
```

Also used for:
- Reject confirmation: "Reject this post? [y/n]"
- Publish all confirmation: "Publish N posts? [y/n]"

## Background Operations

### Watcher

- Starts automatically on TUI launch.
- Monitors the configured watch directory.
- When a new directory with JPEGs is detected:
  1. Runs pipeline (scan → validate → enrich) in background.
  2. Shows progress in status bar: "Processing: erasmusbrug-sunset..."
  3. On completion, the post appears in Pending Review panel.
  4. Status bar shows: "New post ready: erasmusbrug-sunset"

### Publishing

- When "Publish now" is selected:
  1. Post moves to a transient "publishing" state.
  2. Status bar shows: "Publishing: erasmusbrug-sunset..."
  3. On success: post moves to Published Log, status bar shows permalink.
  4. On failure: error message in status bar, post returns to Publish Queue.

### Archival

- After successful publish, archival runs automatically in background.
- No user interaction needed.

## Status Bar

Bottom of the screen, full width:

```
─── Watching ~/Export │ 3 pending │ 1 queued │ Publishing: dom-tower... ── ppw ──
```

Shows:
- Watcher status (watching/paused/error)
- Count of pending review posts
- Count of queued posts
- Current background operation (if any)
- App name

## Configuration

The TUI needs a config file to know where to watch and where to archive. On first run, if no config exists, the TUI shows a setup wizard or an error with instructions.

### Config File: `config/ppw.toml`

```toml
[watch]
dir = "~/Lightroom/Export"

[ai]
provider = "claude-cli"
corpus_path = "config/style_corpus.json"

[archive]
dir = "~/Photos/Archive"
log_file = "~/Photos/publish.log"

[instagram]
# Credentials from env vars: INSTAGRAM_USER_ID, INSTAGRAM_ACCESS_TOKEN

[r2]
# Credentials from env vars: R2_ACCESS_KEY_ID, R2_SECRET_ACCESS_KEY, etc.
```

## Inputs

- `ppw` (no args) → Launch the unified TUI.
- All existing subcommands remain available for scripting.
- Environment variables for credentials (Instagram, R2) — same as before.

## Outputs

- Same manifest state transitions as before.
- Same archival log, same archive directory structure.
- No new data formats — only the presentation layer changes.

## Edge Cases

- **Terminal too small**: Show a "terminal too small" message if width < 80 or height < 24.
- **No config file**: Show error: "Config not found. Run ppw init or create config/ppw.toml".
- **Watch dir doesn't exist**: Show error in Config panel, watcher doesn't start.
- **Credentials missing**: Show warning in status bar. Publishing will fail with a clear message.
- **Pipeline error for a post**: Show error in status bar. Post is skipped (stays as-is on disk). User can retry via re-enrich.
- **Multiple TUI instances**: Warn if another instance is detected (lockfile in `state/ppw.lock`).

## Acceptance Criteria

- [ ] `ppw` with no args launches the unified TUI with 4 left panels + 1 right detail panel.
- [ ] Each panel has its own fully bordered box with visible gaps between panels (no shared borders).
- [ ] Tab/Shift-Tab cycles through panels. Active panel has highlighted border.
- [ ] Pending Review shows all `pending_review` manifests from the watch directory.
- [ ] Selecting a post shows its full detail (images, caption, location) in the right panel.
- [ ] `e` opens an overlay popup editor for the caption. Ctrl+S saves, Esc cancels.
- [ ] `a` on a pending post shows a confirmation dialog: "Publish now or add to queue?"
- [ ] Publishing runs in background with progress in status bar.
- [ ] Publish Queue shows approved posts. `p` publishes selected, `P` publishes all.
- [ ] Published Log shows posts grouped by month, collapsible, with read-only detail.
- [ ] The watcher runs in background. New directories trigger the pipeline automatically.
- [ ] New posts appear in Pending Review without user action.
- [ ] Config panel shows current settings. `e` opens config in `$EDITOR`.
- [ ] Status bar shows watcher status, counts, and current background operations.
- [ ] `q` quits the TUI cleanly (stops watcher, waits for in-progress operations).
- [ ] All existing CLI subcommands continue to work for scripting.

## Safety Notes

- Publishing is irreversible. Always confirm with a dialog.
- The watcher + pipeline runs AI enrichment automatically — this uses Claude CLI calls. The user accepted this by configuring the watch directory.
- Archival moves files. This happens automatically after publish — user should be aware via the config.
- Lockfile prevents multiple instances from conflicting.

## Implementation Notes

- Replace `internal/tui/tui.go` with a new, larger TUI implementation.
- The TUI model holds references to all engine packages (watcher, pipeline, publisher, archiver).
- Use BubbleTea's `tea.Cmd` for background operations (watcher events, publish progress).
- Use `lipgloss` for panel borders, styling, and layout.
- The overlay popup editor is a separate BubbleTea component rendered on top of the main view.
- Confirmation dialogs are another overlay component.

## Learnings (append-only)

- (None yet)
