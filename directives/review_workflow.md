# Directive: Review Workflow

## Goal

Present enriched posts to the user for review in a keyboard-driven TUI. The user sees all images in order, the generated caption (with hashtags), location, and music suggestion. They can approve, reject, edit the caption, toggle story publishing, and choose between immediate publish or schedule-to-queue. No post is ever published without explicit user approval.

## Context / Constraints

- Primary interface: TUI (terminal UI), LazyGit-inspired aesthetics.
  - Panel-based navigation.
  - Minimal keystrokes to approve/reject/edit.
  - Keyboard-driven, not mouse-dependent.
- Alternative interface (deferred): local web page.
- This stage is interactive — it blocks until the user takes an action.
- The TUI operates in two modes:
  1. **Single-manifest mode**: `ppw review --manifest <path>` — review one post.
  2. **Directory mode**: `ppw review --dir <watch-dir>` — review all posts in `pending_review` state.
- When integrated with the watcher (Phase 6), new posts appear in the TUI automatically as they reach `pending_review`.

## Inputs

- Required (one of):
  - `--manifest <path>`: Path to a single manifest in `pending_review` state.
  - `--dir <path>`: Path to the watch directory. The TUI scans for all manifests in `pending_review` state.
- Optional:
  - None. The TUI is fully interactive.

## Outputs

- Files updated: `manifest.json` for each reviewed post.
- Manifest state transitions:
  - Approve → `approved` (with `review.publish_mode: "immediate"` or `"queued"`).
  - Reject → `rejected`.
  - Edit → stays `pending_review` until approved or rejected.
  - Re-enrich → returns to `validated` (triggers re-enrichment outside the TUI).

### Manifest Section Written

```json
{
  "review": {
    "decision": "approved",
    "final_caption": "That quiet moment when the bridge lights first catch the #mist...",
    "caption_edited": true,
    "publish_mode": "immediate",
    "story_enabled": true,
    "reviewed_at": "2026-02-10T18:35:00Z"
  }
}
```

## Steps (high-level)

### TUI Layout

```
+---------------------------+---------------------------+
|  Post List                |  Image Preview            |
|  (pending_review posts)   |  (filename + dimensions)  |
|                           |                           |
|  > erasmusbrug-sunset [4] |  [1/4] erasmusbrug_1.jpg  |
|    canal-houses [6]       |  4000x5000 (4:5)          |
|    vondelpark-morning [2] |                           |
+---------------------------+---------------------------+
|  Caption                  |  Details & Actions        |
|                           |                           |
|  "That quiet moment..."   |  Location: Erasmusbrug    |
|                           |  Music: Olafur Arnalds    |
|                           |  Story: ON                |
|                           |                           |
|                           |  [a] Approve + Publish    |
|                           |  [q] Approve + Queue      |
|                           |  [e] Edit Caption         |
|                           |  [r] Reject               |
|                           |  [s] Toggle Story         |
|                           |  [R] Re-enrich            |
|                           |  [←→] Browse images       |
|                           |  [↑↓] Browse posts        |
+---------------------------+---------------------------+
```

### Navigation

1. **Post list panel** (left): Up/Down arrows or `j`/`k` to select a post. Shows post name and image count.
2. **Image preview panel** (top-right): Left/Right arrows or `h`/`l` to browse images within the selected post. Shows filename, dimensions, and aspect ratio. Opens image in external viewer on Enter (macOS: `open <file>`).
3. **Caption panel** (bottom-left): Shows the generated caption with hashtags highlighted.
4. **Details panel** (bottom-right): Shows location, music suggestion, story toggle status, and keybindings.

### Actions

- **`a` — Approve + Publish Immediately**: Sets `review.decision: "approved"`, `review.publish_mode: "immediate"`. Removes the post from the pending list.
- **`q` — Approve + Queue**: Sets `review.decision: "approved"`, `review.publish_mode: "queued"`. Removes the post from the pending list.
- **`e` — Edit Caption**: Opens an inline text editor for the caption. On save, sets `review.caption_edited: true` and updates `review.final_caption`. Post remains in `pending_review` until explicitly approved.
- **`r` — Reject**: Sets `review.decision: "rejected"`. Removes the post from the pending list.
- **`s` — Toggle Story**: Toggles `review.story_enabled` between true/false for this post.
- **`R` — Re-enrich**: Returns the manifest to `validated` state, which signals the pipeline to re-run AI enrichment. Useful if the caption or location is poor.
- **`?` — Help**: Shows keybinding reference overlay.
- **`Ctrl+C` / `Esc`**: Exit the TUI. Unreviewed posts remain in `pending_review`.

### Caption Editor

- Inline editor within the TUI (not an external editor).
- Pre-populated with the AI-generated caption.
- Shows a character count (Instagram limit: 2200 chars).
- Shows hashtag count (limit: 5).
- On save: validates constraints. If over limits, shows a warning but allows saving.

## Edge Cases / Failure Modes

- **No posts in pending_review**: TUI launches with an empty list and a message: `"No posts pending review. Waiting for new posts..."` (in directory mode) or exits with a message (in single-manifest mode).
- **Manifest file locked or corrupted**: Show error in the TUI status bar. Do not crash. Skip the affected post.
- **User exits without reviewing**: Posts remain in `pending_review`. No data is lost.
- **Caption edit exceeds Instagram limits**: Show a warning in the editor but allow saving. The publish stage will truncate if necessary.
- **Story toggle when story is globally disabled**: Show the toggle as grayed out with a note: `"Story globally disabled in config"`.
- **Image preview**: Phase 1 of the TUI uses external viewer (`open` on macOS). Inline terminal image preview (Kitty/Sixel protocol) is a Phase 7 enhancement.

## Acceptance Criteria

- [ ] Given a manifest in `pending_review` state, the TUI displays all images, the caption, location, and music suggestion.
- [ ] The user can approve a post for immediate publish with a single keystroke (`a`).
- [ ] The user can approve a post for queued scheduling with a single keystroke (`q`).
- [ ] The user can reject a post with a single keystroke (`r`).
- [ ] The user can edit the caption inline and the edited version is saved to `review.final_caption`.
- [ ] The user can toggle story on/off for a post.
- [ ] The user can browse images within a post using arrow keys.
- [ ] The user can browse between posts using arrow keys.
- [ ] Exiting the TUI without reviewing a post leaves it in `pending_review` (no data loss).
- [ ] In directory mode, the TUI shows all posts in `pending_review` state.
- [ ] Re-enrich (`R`) sets the manifest back to `validated` state.

## Safety Notes

- No publishing occurs from the review stage. It only sets the `review` section of the manifest.
- Atomic manifest writes on every action (approve, reject, edit, toggle).
- The TUI must be responsive — AI enrichment and publishing happen outside the TUI process (or in background goroutines that don't block the UI thread).

## Learnings (append-only)

- (None yet)
