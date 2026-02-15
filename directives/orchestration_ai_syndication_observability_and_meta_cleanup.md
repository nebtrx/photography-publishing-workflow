# Orchestration: AI + Syndication Observability + Meta Cleanup

## Phase
Orchestration and execution in the current session.

## Objective
Execute `directives/ai_syndication_observability_and_meta_cleanup.md` end-to-end to restore AI enrichment execution, improve syndication visibility, add durable runtime logs, and remove deprecated `ppw meta auth` commands.

## Workstreams

## WS1: AI Execution Wiring
- Deliverables:
  - `watch` and `pipeline` commands use shared AI provider selection (`PPW_AI_PROVIDER`) instead of hardcoded Claude provider
  - no behavior regression for dry-run mode
- Risk:
  - command-level drift between TUI/default and direct CLI commands
- Mitigation:
  - route all command provider selection through shared helper

## WS2: Observability and Durable Logs
- Deliverables:
  - runtime log file writer with default path `~/.ppw/ppw.log` and env override
  - log fan-out for TUI and CLI runtime paths
  - status-bar text hardening to prevent render corruption from multiline/long errors
- Risk:
  - introducing new writes to stdout/stderr in TUI mode
- Mitigation:
  - keep TUI runtime output channel-based and file-backed only

## WS3: Syndication Visibility
- Deliverables:
  - explicit setup/fallback warnings for syndication in runtime logs
  - CLI publish summary for facebook/threads statuses when configured
- Risk:
  - strict-mode semantics accidentally changed
- Mitigation:
  - keep strict/non-strict branching in `publisher.Publish` unchanged

## WS4: Deprecation Cleanup
- Deliverables:
  - remove `meta` command registration and implementation file
  - update docs/env samples to remove references to deprecated flow
- Risk:
  - stale references in docs
- Mitigation:
  - update `.env.sample` and `README.md` in same pass

## WS5: Tests + Memory
- Deliverables:
  - add tests covering provider wiring and runtime log file helper
  - run `/opt/homebrew/bin/go test ./...`
  - append `TECHNICAL.md` with concrete changes

## Execution Order
1. WS1
2. WS2
3. WS3
4. WS4
5. WS5

## Resume Checklist
- [x] Directive exists
- [x] Orchestration exists
- [x] WS1 completed
- [x] WS2 completed
- [x] WS3 completed
- [x] WS4 completed
- [x] WS5 completed

## Learnings (append-only)
- `watch` and `pipeline` had drifted from shared provider wiring by hardcoding Claude; explicit shared helper (`providerForRun`) prevents repeated regressions.
- Status bar must always sanitize/truncate runtime error text to one line; long multiline AI errors can break full-screen TUI rendering even when panel layout is fixed.
