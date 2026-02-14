# Directive: Automated OAuth and Token Lifecycle (Meta + Threads)

## Goal

Eliminate manual token handling during normal operation by introducing one-time OAuth login plus persistent token storage. After initial consent, `ppw` (CLI and TUI) should acquire, refresh, and use required Meta/Instagram/Facebook/Threads tokens automatically.

## Context / Constraints

- Current implementation still requires manual token values in `.env` for at least one path:
  - `INSTAGRAM_ACCESS_TOKEN`
  - `THREADS_ACCESS_TOKEN`
- Meta Page token derivation already exists (`TokenManager.GetPageAccessToken`) but depends on a user token currently sourced from env.
- Threads publishing currently uses a static access token and does not support dynamic token refresh.
- This repository currently has no runtime HTTP callback server for OAuth redirect handling.
- Existing publishing behavior and manifest schema must remain backward-compatible.
- Token secrets must never be logged.

## Inputs

- Required:
  - App credentials/config in env:
    - `META_APP_ID`
    - `META_APP_SECRET`
    - `META_PAGE_ID`
    - `INSTAGRAM_USER_ID`
  - One-time interactive login (`ppw auth login`) with browser consent.
- Optional:
  - `PPW_TOKEN_STORE` path override (default: `~/.ppw/tokens.json`)
  - `--no-browser` mode to print auth URL
  - `--callback-port` override
  - `--dry-run` for non-mutating diagnostics commands
- Environment variables (legacy compatibility):
  - Existing static token env vars can remain as migration fallback only.

## Outputs

- Files created/updated:
  - `~/.ppw/tokens.json` (mode `0600`)
  - Optional lock file if needed for safe concurrent updates
- Runtime behavior:
  - CLI and TUI publishing paths obtain tokens from token store + refresh logic, not manual token copy/paste.
  - Facebook Page token derivation uses stored Meta user token.
  - Threads client uses a dynamic token source with automatic refresh.
- Commands added/updated:
  - `ppw auth login`
  - `ppw auth status`
  - `ppw auth logout` (or equivalent revoke/clear command)

## Steps (high-level)

1. Add a token store module with atomic read/write, file permissions enforcement, and optional process-safe locking.
2. Add auth manager logic:
   - persist OAuth outputs
   - refresh Meta long-lived user token when near expiry
   - refresh Threads token when near expiry
   - derive/cache Page tokens from refreshed user token
3. Add OAuth login flow command:
   - start local callback server
   - open browser (or print URL)
   - exchange authorization code(s)
   - persist tokens and expiry metadata
4. Refactor runtime token plumbing:
   - Instagram client remains `TokenFn`-driven
   - Facebook syndication uses dynamic token source
   - Threads syndication migrates from static token string to dynamic token source
5. Integrate into both CLI publish and TUI publish paths with the same auth provider.
6. Keep legacy env-token fallback behind explicit compatibility rules during migration.
7. Add tests for store, refresh, OAuth callback handling, and end-to-end publish wiring.

## Edge Cases / Failure Modes

- OAuth callback not received (port blocked or redirect mismatch): fail with actionable guidance and no partial token writes.
- Token store unreadable or wrong permissions: fail with explicit remediation and no silent fallback.
- Concurrent publishes attempt refresh simultaneously: enforce single-writer semantics to avoid token corruption.
- Refresh endpoint transient failure: bounded retries + backoff; preserve prior valid token until exhausted.
- Expired/invalid refresh path during publish:
  - non-strict modes should report clear auth failure per destination
  - strict modes should fail fast
- Missing Threads consent/scopes: Threads destination should fail with actionable message without breaking Instagram core flow unless strict mode requires it.

## Acceptance Criteria

- [ ] After successful `ppw auth login`, publishing works without manually setting `INSTAGRAM_ACCESS_TOKEN`.
- [ ] Threads publishing works without manually setting `THREADS_ACCESS_TOKEN` once consent is completed.
- [ ] Tokens are persisted in `~/.ppw/tokens.json` with secure permissions (`0600`).
- [ ] Token refresh is automatic and transparent for both CLI and TUI publish paths.
- [ ] Facebook Page token derivation continues to work after user-token refresh.
- [ ] On refresh failure, errors are explicit and include recovery instructions.
- [ ] Existing workflows still function for users on legacy env-token mode during migration window.

## Safety Notes

- Never print full access tokens, refresh tokens, app secrets, or callback codes.
- Token writes must be atomic to avoid partial/truncated JSON during crashes.
- OAuth callback endpoint should bind to loopback only.
- Any command that clears tokens should require explicit confirmation unless forced by flag.

## Learnings (append-only)

- `me/accounts` can be empty in valid setups; Page token derivation via `/{PAGE_ID}?fields=access_token` remains a reliable path.
- Threads token lifecycle is separate from Meta long-lived user token lifecycle and must be managed independently.
