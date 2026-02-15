# Directive: Single Config Entrypoint (TOML-Only Runtime)

## Goal
Eliminate runtime config ambiguity by making `ppw.toml` the single source of truth for application configuration (AI, publishing, auth, logging, syndication, and platform IDs).

## Context / Constraints
- Keep token lifecycle storage in `~/.ppw/tokens.json` (dynamic credential state), but configure its path via TOML.
- Keep `PPW_CONFIG` only as an optional path override to locate the TOML file.
- Remove runtime reliance on `.env` values for behavior decisions.
- Preserve backward compatibility only where explicitly marked as legacy inside TOML (not env).

## Inputs
- `internal/config/config.go`
- `config/ppw.toml`
- `cmd/ppw/*.go` runtime wiring
- `internal/hosting/hosting.go`
- `internal/joblog/joblog.go`
- `README.md`, `TECHNICAL.md`

## Outputs
- Expanded TOML schema covering all runtime-configurable settings.
- Command/runtime wiring that reads config from TOML instead of env.
- `.env` no longer required for normal app execution.
- Updated docs for a single-entrypoint config model.

## Steps
1. Expand config schema with sections for AI, auth, logging, R2, Meta/Threads, and publishing.
2. Replace command/runtime env reads with config-derived values.
3. Route logging + retention configuration through TOML.
4. Route provider selection (Codex/Claude) through TOML only.
5. Route publishing/syndication/auth wiring through TOML only.
6. Update config sample/docs and validate tests/build.

## Acceptance Criteria
- [x] Runtime behavior is controlled by `ppw.toml` (except `PPW_CONFIG` path override).
- [x] AI provider selection no longer depends on `PPW_AI_PROVIDER`.
- [x] Publishing/syndication/logging/auth config no longer depends on env vars.
- [x] `go test ./...` passes.
- [x] Documentation explains the single config entrypoint model.

## Learnings (append-only)
- Single-source config removes shell/session drift issues and prevents stale runtime ambiguity.
- Keep legacy env-backed constructors (`R2ConfigFromEnv`, `instagram.NewClient`) only as non-runtime compatibility helpers while active command wiring remains TOML-only.
