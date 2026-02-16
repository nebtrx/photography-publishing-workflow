# Directive: Publish `code=9004` Child Container Failure Diagnosis and Fix

## Goal
Eliminate recurring Instagram publish failures where child container creation fails with Meta `OAuthException code=9004` (`Only photo or video can be accepted as media type`) and provide deterministic diagnostics + recovery.

## Context / Constraints
- Failure occurs at first child container creation during carousel publish.
- Representative runtime log:
  - `2026-02-16T00:56:55Z module=publisher ... action=publish_instagram ... error="create child container 1: create container: Meta API 400: Only photo or video can be accepted as media type. (type=OAuthException, code=9004, fbtrace_id=...)" ...`
- Existing image-count guard was introduced, but failure still needs full root-cause handling.
- Must improve operator observability:
  - identify exact image/file/URL/content-type that failed.

## Inputs
- Required:
  - `internal/publisher/publisher.go`
  - `internal/instagram/client.go`
  - `internal/hosting/hosting.go`
  - `internal/validator/*`
  - logs in `~/.ppw/ppw.log` and per-job JSONL
- Optional:
  - failing manifest snapshots
  - R2 object metadata inspection outputs
- Environment variables:
  - config values in `config/ppw.toml` for `r2`, `meta`

## Outputs
- Files created/updated:
  - publish preflight + diagnostics in publisher/hosting/validator modules
  - tests for failure classification and media preflight
- Report format:
  - root-cause summary + exact changed safeguards
- Where results should be saved:
  - code + `TECHNICAL.md` execution entry

## Steps (high-level)
1. Reproduce against failing manifest/job log and classify failure point.
2. Add media preflight before container creation:
   - validate each publish candidate is decodable media
   - verify URL accessibility and content-type expectations.
3. Improve upload/publish diagnostics:
   - log failing index, filename, local path, public URL, and detected media type.
4. Tighten validator/publisher guardrails for unsupported media edge cases.
5. Ensure failure metadata is persisted cleanly for retry workflows.
6. Add regression tests for `code=9004`-class failures.

## Edge Cases / Failure Modes
- File extension says `.jpg` but content is not valid image.
- R2 object exists but serves unexpected `Content-Type`.
- Signed/public URL fetch returns non-200 while upload succeeded.
- Mixed media types in a single post without matching API params.
- First-image-only failures vs later-image failures.

## Acceptance Criteria
- [x] On failure, logs identify exact failing child index + filename + URL + media diagnostics.
- [x] Invalid media payloads are rejected pre-API with actionable error.
- [x] Valid media set publishes without `code=9004`.
- [x] Regression tests cover `code=9004`-class path.
- [x] `/opt/homebrew/bin/go test ./...` passes.

## Safety Notes
- Do not log secrets/tokens while adding verbose diagnostics.
- Keep retries idempotent for already-uploaded media.

## Learnings (append-only)
- Add confirmed Meta API quirks (media-type/content-type expectations, container constraints) discovered during execution.
- Added media preflight in publisher: local decode/content-type validation for each candidate before container creation.
- Child/single container errors now include `index`, `filename`, `path`, `url`, `local_type`, `remote_type`, and `remote_status` to make `code=9004` root cause visible.
- Remote URL probing is enforced in normal runtime but skipped for deterministic test/dry-run URLs (`test.r2.dev`, `dry-run.r2.dev`) to avoid false failures in unit tests.
