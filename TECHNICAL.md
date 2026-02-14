# Technical Decisions Log

Short, factual entries for Future Omar.

## 2026-02-10 — Execution layer language
**Context:** Deterministic execution tools for workflows should minimize ambiguity and runtime drift.  
**Decision:** Prefer Go for execution tools; allow Python only for rapid prototyping or when a library ecosystem is decisively cheaper.  
**Alternatives considered:** Python-first tooling.  
**Trade-offs:** Slightly higher ceremony/build step, but stronger contracts and fewer runtime surprises.  
**Follow-ups:** Evolve toolbox into a single subcommand CLI when commands >3.

## 2026-02-14 — Instagram auth/publish pre-flight hardening
**Context:** Publish stage failed during pre-flight with multiple auth-related errors while testing real account integration.

**Code changes made:**
- Updated Instagram Graph base URL in `internal/instagram/client.go`:
  - From `https://graph.instagram.com/v21.0`
  - To `https://graph.facebook.com/v21.0`
- Added access-token normalization in `internal/instagram/client.go`:
  - Trim whitespace
  - Strip wrapping quotes
  - Strip optional `Bearer ` prefix
  - Collapse accidental whitespace
- URL-encoded token on GET-based API calls in `internal/instagram/client.go`:
  - `VerifyToken`
  - `getContainerStatus`
  - `GetPermalink`
  - `SearchLocation`
- Changed pre-flight token verification in `internal/instagram/client.go`:
  - Old: `GET /me?fields=id,username` (failed due deprecated `username` field usage)
  - New: `GET /{INSTAGRAM_USER_ID}?fields=id`
- Added `INSTAGRAM_USER_ID` normalization + validation in `internal/instagram/client.go`:
  - Trim whitespace/quotes and optional leading `@`
  - Enforce numeric-only ID, fail early if a username/handle is provided
- Updated `.env.sample` comment to explicitly require numeric IG user ID.

**Observed external state (not code defect):**
- Token is valid (`debug_token.is_valid=true`) with required scopes:
  - `pages_show_list`
  - `pages_read_engagement`
  - `instagram_basic`
  - `instagram_content_publish`
- `me/accounts` returned `{"data":[]}` in testing environment, blocking Page/IG asset discovery via that route.
- Debug output includes Instagram target ID under granular scopes:
  - `17841442845568912`
  - This can be used as `INSTAGRAM_USER_ID` for direct IG account calls.

**Trade-offs / notes:**
- Strict numeric validation prevents ambiguous runtime errors like `GET /nebtrx ... unsupported get request`.
- URL encoding and normalization reduce shell/env copy-paste token defects.
- Remaining blocker appears to be Meta asset/account setup consistency rather than Go client logic.

## 2026-02-14 — Cross-posting diagnosis (Instagram -> Facebook/Threads)
**Context:** Instagram publish succeeded, but content did not auto-appear on Threads or Facebook Page despite consumer cross-post preferences.

**Diagnosis:**
- Current app behavior is Instagram-only by design (no implemented Facebook Page or Threads publish step).
- API-level publish should not assume consumer-app cross-post toggles are applied.
- Existing requirements document still lists cross-posting as out-of-scope; this is now an explicit product extension request.

**Decision:**
- Add a dedicated implementation directive for opt-in multi-destination syndication:
  - `directives/cross_platform_syndication.md`

**Scope in new directive:**
- Keep Instagram as primary destination.
- Add optional destination publishing to:
  - Facebook Page
  - Threads
- Record per-destination status/IDs in manifest.
- Default to non-blocking destination failures unless strict mode is enabled.

## 2026-02-14 — v1 syndication implementation (Facebook link-share + Threads text-link)
**Context:** Implemented directive `directives/cross_platform_syndication.md` as opt-in extension on top of existing Instagram publish flow.

**Implemented:**
- Manifest schema extension in `internal/manifest/manifest.go`:
  - `publishing.syndication.facebook`
  - `publishing.syndication.threads`
  - Per-target fields: `enabled`, `status`, `post_id`, `permalink`, `mode`, `error`, timestamps.
- Publisher flow extension in `internal/publisher/publisher.go`:
  - Optional syndication step after Instagram publish/story, before final transition to `published`.
  - Default behavior: destination failures are warnings.
  - Strict mode: destination failure marks manifest `error` and fails command.
  - Idempotent skip when a destination already has published status + post ID.
- New destination clients in `internal/publisher/syndication_clients.go`:
  - Facebook Page `link_share`: `POST /{page-id}/feed` with `message` + `link`.
  - Threads `text_link`: create/publish text post via container + publish flow.
- CLI/env wiring:
  - `cmd/ppw/publish.go` added:
    - `--destinations instagram,facebook,threads`
    - `--strict-syndication`
  - `cmd/ppw/syndication.go` added destination/env parsing + client setup.
  - `cmd/ppw/default.go` now applies same env-driven syndication options for TUI-triggered publishing.
- Env docs updated in `.env.sample`:
  - `PUBLISH_DESTINATIONS`
  - `STRICT_SYNDICATION`
  - `THREADS_USER_ID`
  - `THREADS_ACCESS_TOKEN`

**Tests added/updated:**
- `internal/publisher/publisher_test.go`:
  - syndication success path
  - non-strict failure path
  - strict failure path
- `cmd/ppw/syndication_test.go`:
  - destination parser coverage

**Current limitation:**
- Could not execute tests in this environment due network-restricted Go module resolution (`proxy.golang.org` DNS blocked).

## 2026-02-14 — Toolchain/path fix + full validation
**Context:** Test execution initially failed for reasons unrelated to business logic.

**Findings:**
- Shell default `go` pointed to an old binary:
  - `/usr/local/bin/go` -> `go1.13.4`
- Project requires modern Go (`go 1.24.2` in `go.mod`), and local machine already had:
  - `/opt/homebrew/bin/go` -> `go1.25.6`
- Early failures (`cannot load io/fs`) were caused by the old toolchain, not code defects.

**Changes made:**
- Updated shell path preference so `go` resolves to Homebrew Go (`go1.25.6`).
- Ran `go mod tidy` to synchronize module metadata.
- Fixed vet/build issue in `internal/publisher/publisher.go`:
  - Replaced `fmt.Errorf(strings.Join(errs, "; "))` with `errors.New(strings.Join(errs, "; "))`.
- Re-ran full test suite with modern Go:
  - `/opt/homebrew/bin/go test ./...` passed.

**Outcome:**
- Prior note about blocked module resolution is no longer the active blocker on host machine.
- Repository currently builds/tests successfully with correct Go toolchain.

## 2026-02-14 — Env template hardening for syndication setup
**Context:** Threads/Facebook syndication setup needed explicit, copy-safe env guidance.

**Changes made:**
- Updated `.env.sample` defaults/placeholders for new syndication vars:
  - `PUBLISH_DESTINATIONS=instagram,facebook,threads`
  - `STRICT_SYNDICATION=false`
  - `THREADS_USER_ID=REPLACE_WITH_THREADS_USER_ID`
  - `THREADS_ACCESS_TOKEN=REPLACE_WITH_THREADS_ACCESS_TOKEN`
- Added matching explicit placeholder entries to local `.env` for operational clarity.

**Notes:**
- Current code auto-derives/caches Meta Page token at runtime when Meta auth is configured.
- Threads token/user-id acquisition remains manual (no in-app OAuth callback server yet).

## 2026-02-14 — Automated auth lifecycle planning (directive + orchestration)
**Context:** Manual Threads/Instagram token handling remains a usability blocker. Work resumed in DOE order: directive first, orchestration second, execution deferred.

**Added artifacts:**
- New directive:
  - `directives/auth_token_automation.md`
- New orchestration-phase plan:
  - `directives/orchestration_auth_token_automation.md`

**Directive intent (summary):**
- One-time OAuth login + persistent token store (`~/.ppw/tokens.json`).
- Automatic token refresh and runtime token sourcing for CLI and TUI.
- Migrate Threads path from static token env usage to dynamic token source.

**Orchestration outcome (summary):**
- Workstreams defined:
  - token store
  - OAuth login flow
  - refresh engine
  - runtime integration
  - migration/compatibility
  - verification
- Execution order and handoff gates defined for later implementation pass.

## 2026-02-14 — Automated auth lifecycle execution: WS1 token store foundation
**Context:** Continued from orchestration plan with execution workstream WS1.

**Implemented:**
- New package: `internal/authstore`
  - `internal/authstore/store.go`
  - `internal/authstore/store_test.go`
- Public API delivered:
  - `DefaultPath()` with `PPW_TOKEN_STORE` override support
  - `New(path string)`
  - `Path()`
  - `Load()`
  - `Save()`
  - `Update()`
- Security and integrity behavior:
  - default path `~/.ppw/tokens.json`
  - atomic writes (temp file + rename)
  - permission enforcement (`0700` directory, `0600` file)
  - lock discipline for concurrent updates (`.lock` file + `flock` + in-process mutex)

**Validation:**
- `go test ./internal/authstore` passed
- `go test ./...` passed

**Notes:**
- WS1 is complete; WS2/WS3 (OAuth login + refresh engine) remain next.

## 2026-02-14 — Env contract sync after WS1
**Context:** Align `.env.sample` and `.env` with WS1 token-store foundation while preserving current runtime compatibility.

**Changes made:**
- Added definitive new env var:
  - `PPW_TOKEN_STORE` (path override for token store; default remains `~/.ppw/tokens.json`)
- Reorganized env sections into:
  - core platform IDs/config
  - token store
  - optional syndication behavior
  - legacy/manual token inputs (temporary)
- Kept `INSTAGRAM_ACCESS_TOKEN` and `THREADS_ACCESS_TOKEN` as legacy/manual vars because WS2/WS3 migration is not yet integrated in runtime paths.

**Files updated:**
- `.env.sample`
- `.env`
