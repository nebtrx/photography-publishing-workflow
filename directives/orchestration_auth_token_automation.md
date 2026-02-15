# Orchestration: Automated Auth Token Lifecycle

## Phase

Orchestration only. No execution changes in this document.

## Objective

Coordinate implementation of `directives/auth_token_automation.md` into parallel workstreams with clear dependencies, handoff contracts, and verification gates.

## Scope

- In scope:
  - token store design and persistence model
  - OAuth login command and callback handling
  - token refresh strategy for Meta and Threads
  - runtime integration into CLI publish and TUI publish
  - tests and migration safeguards
- Out of scope:
  - production deployment infrastructure
  - non-auth product features

## Current Baseline (confirmed)

- Meta token manager exists: `internal/meta/auth.go`
- Meta auth CLI exists: `cmd/ppw/meta.go`
- Instagram uses dynamic token source in managed mode: `internal/instagram/client.go`
- Threads syndication currently uses static token string:
  - `cmd/ppw/syndication.go`
  - `internal/publisher/syndication_clients.go`
- TUI and CLI share publisher construction path:
  - `cmd/ppw/default.go`
  - `cmd/ppw/publish.go`

## Workstreams

## WS1: Token Store Foundation

- Owner: Core auth/storage
- Deliverables:
  - `internal/authstore` package (or equivalent)
  - schema for `~/.ppw/tokens.json`
  - atomic write + permission enforcement + locking strategy
- Dependencies: none
- Output contract:
  - API for `Load()`, `Save()`, `Update()`, `Path()`
  - typed token structs with expiry metadata

## WS2: OAuth Login Flow

- Owner: CLI/auth UX
- Deliverables:
  - `ppw auth login` command
  - loopback callback handler
  - browser-open + no-browser mode
  - persisted token bootstrap into store
- Dependencies:
  - WS1 store API
- Output contract:
  - successful login yields populated token store
  - failures produce deterministic non-zero exits and remediation text

## WS3: Refresh Engine

- Owner: Meta/Threads auth lifecycle
- Deliverables:
  - refresh policy module
  - Meta user token refresh/exchange handling
  - Threads refresh handling
  - safe update back into token store
- Dependencies:
  - WS1 store API
  - WS2 token bootstrap format
- Output contract:
  - `TokenSource` functions that always return the best currently valid token
  - bounded retry behavior + clear error typing

## WS4: Runtime Integration (Publish + TUI)

- Owner: runtime wiring
- Deliverables:
  - replace env-token reads with auth manager sources in:
    - `cmd/ppw/default.go`
    - `cmd/ppw/publish.go`
    - `cmd/ppw/syndication.go`
  - migrate Threads client to dynamic token source interface
- Dependencies:
  - WS3 token source APIs
- Output contract:
  - CLI and TUI publish both work without manual token env vars after login

## WS5: Compatibility + Migration

- Owner: DX/backward compatibility
- Deliverables:
  - explicit fallback policy for legacy env-token mode
  - warnings/deprecation messaging
  - docs updates (`.env.sample`, directives, help text)
- Dependencies:
  - WS4 integrated runtime behavior
- Output contract:
  - no silent behavior changes for existing users

## WS6: Test and Verification

- Owner: quality
- Deliverables:
  - unit tests for store and refresh logic
  - callback/OAuth flow tests
  - integration tests for CLI and TUI token sourcing
- Dependencies:
  - WS1-WS5
- Output contract:
  - passing `go test ./...`
  - failure matrix documented for common auth edge cases

## Execution Order

1. WS1
2. WS2 + WS3 (parallel once WS1 API stabilizes)
3. WS4
4. WS5
5. WS6

## Handoff Gates

- Gate A (after WS1):
  - token store API reviewed and frozen for downstream work
- Gate B (after WS2/WS3):
  - login + refresh demo succeeds against test app
- Gate C (after WS4):
  - publish works in both CLI and TUI using store-backed tokens
- Gate D (after WS6):
  - full tests pass; migration notes complete

## Risks and Mitigations

- Risk: OAuth callback complexity across environments
  - Mitigation: support `--no-browser`, explicit redirect validation, clear CLI diagnostics
- Risk: token corruption from concurrent writes
  - Mitigation: atomic temp-file rename + lock discipline
- Risk: Threads and Meta token lifecycles diverge
  - Mitigation: separate refresh policies with unified store abstraction
- Risk: accidental secret exposure in logs
  - Mitigation: centralized redaction utilities and test assertions

## Resume Checklist (for next execution session)

- [x] Confirm workstream owners and sequence
- [x] Implement WS1 with tests first
- [x] Implement WS2 and WS3 against WS1 contracts
- [x] Integrate WS4 and validate publish in CLI/TUI
- [ ] Complete WS5 migration behavior and docs
- [ ] Run WS6 full verification and capture outputs in TECHNICAL log

## Execution Progress

- 2026-02-14:
  - WS1 completed.
  - Added `internal/authstore` with:
    - `DefaultPath()` (supports `PPW_TOKEN_STORE` override)
    - `New()`, `Path()`, `Load()`, `Save()`, `Update()`
    - atomic writes (`temp + rename`)
    - permission enforcement (`0700` dir, `0600` file)
    - lock discipline (`.lock` file + `flock` + process mutex)
  - Added tests in `internal/authstore/store_test.go`.
  - Validation:
    - `go test ./internal/authstore` passed
    - `go test ./...` passed

- 2026-02-15:
  - WS2 completed.
    - Added unified auth command set:
      - `ppw auth login`
      - `ppw auth status`
      - `ppw auth logout --yes`
    - Implemented loopback OAuth callback handling (`127.0.0.1:8787` default), browser-open flow, and manual code fallback flags.
    - Added shared auth manager wiring helper in `cmd/ppw/auth_common.go`.
  - WS3 completed.
    - Added automated lifecycle test coverage for `internal/authn`:
      - auth URL generation
      - Meta code exchange and long-lived token persistence
      - Threads refresh path with persisted store update
      - empty-store status behavior
  - WS4 integration completed.
    - CLI/TUI publishing now prefer store-backed managed auth via `internal/authn.Manager`.
    - Threads syndication client migrated from static token string to dynamic token source.
    - Facebook/Threads syndication paths include legacy env-token fallback during migration.
  - Compatibility updates:
    - Added deprecation marker for `ppw meta auth` with migration message to `ppw auth`.
  - Validation:
    - `/opt/homebrew/bin/go test ./...` passed
