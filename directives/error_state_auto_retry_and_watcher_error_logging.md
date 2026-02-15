# Directive: Error-State Auto-Retry and Watcher Error Logging

## Goal
When a post directory is in `state:error`, the watcher should automatically retry the pipeline when image files in that directory change. Watcher logs must also include actionable error details so failures are easy to diagnose.

## Context / Constraints
- Current watcher logs `Existing: <post> (state: error)` but does not explain why.
- Current watcher flow primarily reacts to newly created directories.
- The desired workflow is watcher-driven recovery without manually running `ppw pipeline`.

## Inputs
- `internal/watcher/watcher.go`
- `internal/watcher/watcher_test.go`

## Outputs
- Watcher retries errored posts when JPEG files are created/updated/renamed/removed.
- Watcher startup logs include manifest-derived error reason for errored posts.
- Tests covering retry behavior.
- Technical memory entry in `TECHNICAL.md`.

## Steps
1. Expand watcher event handling to monitor existing post directories.
2. Detect file-level image changes in errored post directories.
3. Trigger debounced forced reprocessing for errored directories.
4. Enrich startup logs with manifest error reason.
5. Add tests and validate.

## Acceptance Criteria
- [x] Editing/adding/renaming/removing a JPEG in an errored post directory causes automatic retry.
- [x] Watcher logs include clear error cause for `state:error` posts at startup.
- [x] Behavior for new directories remains intact.
- [x] `/opt/homebrew/bin/go test ./...` passes.

## Learnings (append-only)
- Watching existing post subdirectories is required for reliable recovery-on-change behavior.
