# Directive: AI/Syndication Observability and Meta Command Cleanup

## Goal
Resolve four operational issues in one pass:
1. AI enrichment not executing reliably in runtime paths.
2. Syndication failures not visible enough when `STRICT_SYNDICATION=false`.
3. Missing durable runtime logs for post-mortem/debug.
4. Remove deprecated `ppw meta auth ...` command group entirely.

## Context / Constraints
- TUI/CLI should remain functional and deterministic.
- Non-strict syndication behavior stays non-fatal, but must be visible.
- Logging must not regress TUI render stability.
- Backward-compatible env behavior should be preserved where reasonable.

## Inputs
- `cmd/ppw/*.go`
- `internal/*` packages used by runtime flows (pipeline/enricher/publisher/watcher)
- `.env.sample`, `README.md`, `TECHNICAL.md`

## Outputs
- AI provider selection fixed across command paths.
- Better syndication visibility in logs/output.
- File-backed runtime logging added.
- Deprecated meta command removed from CLI.
- Added tests (including integration-style command/runtime wiring coverage).

## Steps
1. Unify AI provider selection in all runtime paths.
2. Route remaining direct stderr logger paths into configurable writers.
3. Add runtime log file writer with env-configurable path.
4. Improve syndication visibility in non-strict mode logs/output.
5. Remove `meta` command registration + dead command code.
6. Add tests and validate full suite.

## Acceptance Criteria
- [x] `watch` and `pipeline` use the same AI provider selection logic as default/enrich paths.
- [x] Enrichment warnings no longer bypass structured runtime logging.
- [x] Runtime logs persist to file (default path + env override).
- [x] Non-strict syndication failures are clearly visible in logs/output.
- [x] `ppw meta auth ...` command no longer exists.
- [x] `/opt/homebrew/bin/go test ./...` passes.

## Learnings (append-only)
- Root cause for TUI upward shift persisted in status-bar rendering: multiline error strings (AI stderr text embedded in status messages) were not normalized to a single line and could wrap/corrupt frame geometry.
- Persistent log files are required even when TUI runtime panel exists; otherwise non-strict syndication and enrichment warnings are too easy to miss after session end.
