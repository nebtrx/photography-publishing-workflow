# Directive: Structured Job Logging and Retention

## Goal
Introduce consistent structured logs across runtime flows (intent/result style), persist logs per job, and enforce automatic retention with periodic sweeping (default every 60 minutes).

## Context / Constraints
- Logging must remain TUI-safe (no screen corruption).
- Retention policy should preserve failed-job evidence longer than successful runs.
- Backward compatibility: existing runtime log visibility should remain available.
- Sensitive data (tokens/secrets) must not be logged.

## Inputs
- `cmd/ppw/*.go`
- `internal/pipeline`, `internal/enricher`, `internal/publisher`, `internal/tui`
- `.env.sample`, `.env`, `TECHNICAL.md`

## Outputs
- Structured logging helper for consistent event format.
- Per-job log file sessions with final status metadata.
- Retention sweeper with default 60-minute interval.
- Env defaults documented and applied.
- Tests for retention/session behavior + log formatting.

## Steps
1. Add structured event logging utility (`intent` + `result`) with consistent schema.
2. Add per-job log session writer and job-file lifecycle finalization.
3. Add retention sweeper with configurable TTL defaults and periodic interval.
4. Wire command/TUI entrypoints to job sessions + retention sweeper.
5. Instrument pipeline/enricher/publisher key actions with structured logs.
6. Update env defaults and docs, then run tests.

## Acceptance Criteria
- [x] Logs include: module, action, timestamp, intent/result, success/failure, and error details when present.
- [x] Per-job logs are written under configurable log directory.
- [x] Periodic sweeper runs every 60 minutes by default.
- [x] Successful-job logs and failed-job logs honor different retention TTLs.
- [x] `.env.sample` and `.env` include retention defaults.
- [x] `/opt/homebrew/bin/go test ./...` passes.

## Learnings (append-only)
- Job-log lifecycle is most robust when files are explicitly finalized from `.active.jsonl` into `.success.<unix>.jsonl` / `.failed.<unix>.jsonl`; retention then becomes deterministic and cheap to evaluate.
- Multiline/verbose module logs can remain human-readable while structured JSON events provide machine-readable intent/result traces in the same stream.
