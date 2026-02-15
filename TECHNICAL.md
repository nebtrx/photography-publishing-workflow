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

## 2026-02-15 — Automated auth lifecycle execution: WS2/WS3 + runtime wiring
**Context:** Continue auth automation after WS1 with OAuth login UX, refresh validation, and runtime integration for CLI/TUI publishing.

**Implemented:**
- New unified auth commands in `cmd/ppw/auth.go`:
  - `ppw auth login`
  - `ppw auth status`
  - `ppw auth logout --yes`
- Added OAuth callback flow:
  - loopback callback server (default `127.0.0.1:8787`)
  - provider-specific callback paths for Meta and Threads
  - browser-open helper with `--no-browser` fallback
  - manual-code fallback flags (`--meta-code`, `--threads-code`, with corresponding redirect URI flags)
- Added shared auth manager constructor in `cmd/ppw/auth_common.go`.

**Runtime integration changes:**
- `cmd/ppw/default.go`:
  - publisher path now prefers managed store-backed tokens via `internal/authn.Manager`.
  - falls back to legacy static `INSTAGRAM_ACCESS_TOKEN` only when managed auth is unavailable.
- `cmd/ppw/publish.go`:
  - publish path now consumes `*authn.Manager` for syndication wiring.
  - updated command help text for managed-auth-first behavior.
- `cmd/ppw/syndication.go`:
  - Facebook and Threads token sources now support managed auth from token store.
  - kept explicit legacy env-token fallback to preserve compatibility during migration.
- `internal/publisher/syndication_clients.go`:
  - `ThreadsClient` migrated from static token field to dynamic token source (`AccessTokenSource`) for refreshable auth.

**Compatibility/deprecation:**
- `cmd/ppw/meta.go` now marks `ppw meta auth` as deprecated and points users to `ppw auth`.

**Tests added:**
- `internal/authn/manager_test.go`:
  - auth URL generation
  - Meta login exchange + page token derivation persistence
  - Threads refresh update path
  - missing-store status behavior
- `internal/publisher/syndication_clients_test.go`:
  - Threads token-source requirements and publish flow with dynamic token source

**Validation:**
- `/opt/homebrew/bin/go test ./...` passed
- `/opt/homebrew/bin/go run ./cmd/ppw --help` shows new `auth` command tree

## 2026-02-15 — TUI render stability + integrated runtime log panel (execution)
**Context:** Unified TUI deformed during modal editing and while watcher/pipeline/publish/archive logs printed to stderr in alt-screen mode.

**Implemented:**
- Right-column split in `internal/tui/app.go`:
  - top detail panel
  - bottom `Runtime Log` panel (~28% target with small-terminal clamps)
- Stable fixed-height layout in `internal/tui/app.go`:
  - explicit left-panel height allocation (config/pending/queue/published)
  - explicit right-panel height allocation (detail/log)
  - tail-trimming for long panel lists to avoid overflow bleed
- Overlay stability change in `internal/tui/app.go`:
  - removed manual buffer-rewrite compositor (`placeOverlay`)
  - modal overlays now render in dedicated centered frame (`lipgloss.Place`)
- TUI-safe log transport:
  - added `AppLogMsg` in `internal/tui/messages.go`
  - added line-buffered event writer `internal/tui/logsink.go`
  - `AppModel` now stores bounded runtime log buffer (last 500 lines)
- Logger routing to TUI channel:
  - `cmd/ppw/default.go` now creates shared event channel + `NewEventLogWriter`
  - `internal/archiver/archiver.go` `Options.LogOutput`
  - `internal/pipeline/pipeline.go` `Options.LogOutput`
  - `internal/watcher/watcher.go` `Options.LogOutput`
  - `internal/publisher/publisher.go` `Options.LogOutput`
  - `internal/tui/background.go` passes watcher log writer

**Validation:**
- `gofmt -w` on changed Go files
- `/opt/homebrew/bin/go mod tidy`
- `/opt/homebrew/bin/go test ./...` passed

**Notes:**
- Legacy CLI commands (non-TUI) still default to stderr logging.
- TUI path now captures watcher/pipeline/publish/archive runtime logs in-panel instead of writing to terminal.

## 2026-02-15 — OAuth secure-redirect compatibility fix (localhost vs 127.0.0.1)
**Context:** `ppw auth login` hit Meta login blocker: “isn't using a secure connection.” The auth flow used loopback IP redirect URIs.

**Change made:**
- Updated auth login default listen address in `cmd/ppw/auth.go`:
  - from `127.0.0.1:8787`
  - to `localhost:8787`
- Hardened callback URL normalization in `cmd/ppw/auth.go`:
  - converts loopback IP redirect hosts (`127.0.0.1`, `0.0.0.0`, `::1`) to `localhost` for OAuth redirect URI generation
  - preserves local callback behavior

**Rationale:**
- Meta OAuth dev flows commonly accept `localhost` redirect URIs while plain IP-based HTTP redirect URIs may be rejected by secure-transport checks.

**Validation:**
- `/opt/homebrew/bin/go test ./...` passed

## 2026-02-15 — Threads auth app-identity compatibility (error_code 4476002)
**Context:** Unified `ppw auth login` could fail on the Threads leg after successful Meta auth with:
`Authorization Failed: No app ID was sent with the request. (error_code 4476002)`.

**Likely cause addressed:**
- Threads OAuth may be configured under a different Meta app than the Instagram/Facebook app. Previous implementation always reused `META_APP_ID/META_APP_SECRET` for Threads exchanges.

**Changes made:**
- Added optional Threads app credential overrides:
  - `THREADS_APP_ID`
  - `THREADS_APP_SECRET`
- Updated auth manager wiring in `cmd/ppw/auth_common.go` to pass these env vars.
- Updated `internal/authn/manager.go` Threads paths to use override credentials when provided:
  - `ThreadsAuthURL`
  - `exchangeThreadsCode`
  - `exchangeThreadsLongLived`
- Kept backward compatibility:
  - if overrides are unset, Threads continues to use `META_APP_ID/META_APP_SECRET`.
- Updated docs:

## 2026-02-15 — TUI border alignment follow-up (detail top edge + log panel bottom fill)
**Context:** After initial TUI stabilization pass, panel geometry still showed visible border defects:
- detail panel top edge missing in some terminal sizes
- runtime log panel not reaching the status bar

**Root cause:**
- `lipgloss.Style.Height(...)` was treated like final rendered block height.
- With bordered styles, height applies to inner content; borders add extra rows, causing panel over/under-sizing and clipping artifacts.

**Fix implemented:**
- Updated panel rendering in `internal/tui/app.go` to:
  - render bordered panels without style height forcing
  - enforce exact final block size via `lipgloss.Place(width, height, ...)`
  - keep per-panel line budgets via `tailLines(..., h-3)` before render
- Applied to:
  - `renderConfigPanel`
  - `renderPendingPanel`
  - `renderQueuePanel`
  - `renderPublishedPanel`
  - `renderDetailPanel`
  - `renderRuntimeLogPanel`

**Validation:**
- `/opt/homebrew/bin/go test ./...` passed

## 2026-02-15 — TUI full-height panel regression fix (slim panels)
**Context:** Follow-up patch for border alignment caused panels to render as slim boxes with large blank gaps.

**Root cause:**
- Replacing bordered panel blocks with `lipgloss.Place` preserved outer layout height but did not force borders to consume that height.

**Fix implemented:**
- Added `renderSizedPanel(...)` helper in `internal/tui/app.go`:
  - computes inner content height as `h - 2` (top/bottom border rows)
  - renders bordered panel at exact assigned height
- Applied helper to all fixed panels:
  - config/pending/queue/published
  - detail
  - runtime log

**Validation:**
- `/opt/homebrew/bin/go test ./...` passed

## 2026-02-15 — LazyGit-inspired TUI skin + indexed panel headers
**Context:** Requested UI refresh to mirror LazyGit’s visual language (lavender dark theme + mint active accents) and panel numbering format.

**Implemented:**
- Updated TUI color tokens in `internal/tui/styles.go`:
  - inactive borders -> lavender tone
  - active borders/titles/selection -> mint accent
  - dim/warning/error/status-bar colors adjusted for LazyGit-like contrast
- Added indexed panel-title system in `internal/tui/app.go`:
  - format: `[n]-Panel Name`
  - mapping:
    - `[0]-Detail`
    - `[1]-Config`
    - `[2]-Pending Review`
    - `[3]-Publish Queue`
    - `[4]-Published`
    - `[5]-Runtime Log`
- Applied indexed-title rendering to both left navigation panels and right detail/log panels.

**Artifacts added:**
- `directives/lazygit_skin_and_panel_indexing.md`
- `directives/orchestration_lazygit_skin_and_panel_indexing.md`

**Validation:**
- `/opt/homebrew/bin/go test ./...` passed

## 2026-02-15 — Header indexing rule tweak (no index on Runtime Log)
**Context:** Runtime/command log panel is informational and not directly focus-selectable via panel cycling.

**Change made:**
- Updated `internal/tui/app.go`:
  - `renderRuntimeLogPanel` now uses plain `Runtime Log` header (no `[n]-` prefix).
  - Kept indexed headers on actionable/selectable panels.

**Validation:**
- `/opt/homebrew/bin/go test ./...` passed

## 2026-02-15 — LazyGit chrome parity: border titles, counters, numeric jump keys
**Context:** UI parity request expanded beyond color theme:
- titles/numbers should be embedded in panel border line
- list panels should show bottom-right counters (`x of y`)
- numeric keys should jump to panels

**Implemented:**
- Replaced bordered panel rendering path in `internal/tui/app.go`:
  - new `renderPanelChrome(...)` renders explicit top/bottom border strings
  - top border contains title text (including `[n]-...` for selectable panels)
  - bottom border supports right-aligned footer counters
- Added counter helpers in `internal/tui/app.go`:
  - `counterText(...)`
  - `pendingCounter()`
  - `queueCounter()`
  - `publishedCounter()`
- Applied counters to left list panels:
  - pending, queue, published
- Added numeric panel navigation in `internal/tui/app.go`:
  - `1` -> Config
  - `2` -> Pending Review
  - `3` -> Publish Queue
  - `4` -> Published
- Kept runtime log panel unnumbered and non-selectable.
- Added panel chrome styles in `internal/tui/styles.go`:
  - border line styles (active/inactive)
  - footer counter styles (active/inactive)

**Artifacts added:**
- `directives/lazygit_panel_chrome_counters_and_numeric_navigation.md`
- `directives/orchestration_lazygit_panel_chrome_counters_and_numeric_navigation.md`

**Validation:**
- `/opt/homebrew/bin/go test ./...` passed

## 2026-02-15 — Watcher auto-retry for errored posts + clearer startup error logs
**Context:** After moving/changing files inside a post already in `state:error`, watcher did not retry automatically and logs were too terse (`Existing: <post> (state: error)`).

**Implemented:**
- Updated `internal/watcher/watcher.go`:
  - watcher now adds fsnotify watches for existing post subdirectories (not only root watch dir)
  - added forced retry path for errored posts when JPEG files change (`create/write/rename/remove`)
  - added per-directory debounced scheduler for both new-dir and retry triggers
  - startup logs for errored manifests now include reason text:
    - from `manifest.errors` (last entry) when present
    - else from first validation issue with severity `error`
- New helper behavior:
  - `isErroredPost(...)` gates forced retries to `state:error` only
  - `manifestErrorReason(...)` extracts actionable reason text

**Tests:**
- Added `TestWatcher_RetriesErroredPostOnImageChange` in `internal/watcher/watcher_test.go`.

**Artifacts added:**
- `directives/error_state_auto_retry_and_watcher_error_logging.md`
- `directives/orchestration_error_state_auto_retry_and_watcher_error_logging.md`

**Validation:**
- `/opt/homebrew/bin/go test ./internal/watcher ./cmd/ppw ./internal/tui` passed
- `/opt/homebrew/bin/go test ./...` passed

## 2026-02-15 — TUI stability follow-up: route enricher warnings into in-app runtime log
**Context:** Even after watcher/pipeline/publisher log routing, UI could still shift upward when enrichment warnings occurred (`caption/location/music` failures). Those warnings were printed by global `log.Printf` in `internal/enricher`, bypassing TUI log sink.

**Fix implemented:**
- Updated `internal/enricher/enricher.go`:
  - added `Options.LogOutput io.Writer`
  - added per-instance logger in `Enricher`
  - replaced global `log.Printf` calls with `e.logger.Printf(...)`
- Updated `internal/pipeline/pipeline.go`:
  - pass `LogOutput` through to `enricher.New(...)`

**Impact:**
- Enricher warnings now go through the same routed writer used by TUI runtime log panel.
- No raw stderr warning lines should break alt-screen frame layout during file-change reprocessing.

**Validation:**
- `/opt/homebrew/bin/go test ./internal/enricher ./internal/pipeline ./internal/tui ./cmd/ppw` passed
- `/opt/homebrew/bin/go test ./...` passed
  - `.env.sample`
  - `README.md`

**Tests added:**
- `internal/authn/manager_test.go`
  - verifies Threads auth URL uses `THREADS_APP_ID`
  - verifies Threads code exchange uses `THREADS_APP_ID/THREADS_APP_SECRET`

**Validation:**
- `/opt/homebrew/bin/go test ./...` passed

## 2026-02-15 — Meta token expiry fallback fix after OAuth login
**Context:** After successful `ppw auth login --meta-only`, status showed:
`expires=<today> (0 days) EXPIRING`.
Store value indicated `meta.user_expires_at` was written near current time.

**Root cause:**
- Auth lifecycle code relied on `expires_in` from OAuth token exchange responses.
- When `expires_in` is missing or zero in a Meta response, code wrote `now + 0s`, causing false-expiring status.

**Fix implemented:**
- In `internal/authn/manager.go`:
  - Added `expiryFromExpiresIn()` helper (returns zero-time when `expires_in <= 0`).
  - Added `lookupMetaTokenExpiryBestEffort()` fallback using `debug_token.expires_at`.
  - Applied fallback in both:
    - `exchangeMetaCode()`
    - `exchangeMetaLongLived()`
  - Threads exchange/refresh paths now also avoid writing `now` when `expires_in` is missing (store `expires_at` as unknown instead).
- In `cmd/ppw/auth.go`:
  - Updated `auth status` output formatting:
    - unknown expiry now prints `expires=unknown` (no misleading `(0 days)`).

**Tests added:**
- `internal/authn/manager_test.go`:
  - `TestLoginMeta_FallsBackToDebugTokenExpiryWhenExpiresInMissing`

**Validation:**
- `/opt/homebrew/bin/go test ./...` passed

## 2026-02-15 — Auth env normalization hardening (quoted app IDs/secrets)
**Context:** Threads OAuth can fail with app-identity errors when env values are copied with wrapping quotes (e.g. `"896099..."`), producing malformed `client_id` values at runtime.

**Changes made:**
- Updated `cmd/ppw/auth_common.go`:
  - Added `normalizeEnvSecretLike()` helper that trims whitespace and wrapping single/double quotes.
  - Applied normalization to:
    - `META_APP_ID`
    - `META_APP_SECRET`
    - `META_PAGE_ID`
    - `THREADS_APP_ID`
    - `THREADS_APP_SECRET`

**Rationale:**
- Prevents subtle OAuth failures from quoted env values in shell/export workflows.

**Validation:**
- `/opt/homebrew/bin/go test ./...` passed

## 2026-02-15 — Threads OAuth base-domain compatibility update
**Context:** Threads authorization URL showed valid `client_id`, but user still got app-identity error during Threads OAuth.

**Change made:**
- Updated default Threads auth base in `internal/authn/manager.go`:
  - from `https://www.threads.net`
  - to `https://threads.net`

**Rationale:**
- Threads OAuth endpoint behavior can vary across domain aliases; canonical non-`www` base improves compatibility when app recognition fails despite valid `client_id` query parameter.

**Validation:**
- `/opt/homebrew/bin/go test ./...` passed

## 2026-02-15 — Auth login redirect-URI override fix for browser flow
**Context:** `ppw auth login --threads-only` ignored `--threads-redirect-uri` unless `--threads-code` was also provided, preventing HTTPS callback URI workflows required by Threads settings.

**Fix made:**
- Updated `cmd/ppw/auth.go`:
  - For both Meta and Threads login flows, if `--*-redirect-uri` is provided and `--*-code` is not, browser auth now uses that redirect URI instead of forcing generated localhost callback URL.

**Impact:**
- Enables tunnel/domain-based HTTPS callback flows (e.g. ngrok) while still using local callback listener.
- Unblocks Threads setup where `http://localhost` redirect cannot be saved in app settings.

**Validation:**
- `/opt/homebrew/bin/go test ./...` passed

## 2026-02-15 — Managed auth refresh command + env cleanup for token-store mode
**Context:** After successful login, users may need to refresh stored tokens without browser auth and want `.env` reduced to active managed-auth variables.

**Implemented:**
- Added new command in `cmd/ppw/auth.go`:
  - `ppw auth refresh`
  - Supports `--meta-only` and `--threads-only`
  - Triggers lifecycle refresh using stored tokens and then prints `auth status`
- Updated docs:
  - `README.md` now includes `ppw auth refresh` in auth bootstrap section.

**Env cleanup applied:**
- `.env.sample`:
  - `PPW_TOKEN_STORE` changed to commented optional override (default path remains implicit)
  - legacy manual token vars changed to commented optional fallback
- `.env`:
  - removed explicit default `PPW_TOKEN_STORE=~/.ppw/tokens.json`
  - removed live legacy token exports (`INSTAGRAM_ACCESS_TOKEN`, `THREADS_ACCESS_TOKEN`) and replaced with commented fallback notes

**Validation:**
- `/opt/homebrew/bin/go test ./...` passed

## 2026-02-15 — New directive/orchestration for TUI deformation and log-panel redesign
**Context:** User reported recurring TUI deformation during overlay typing and background log output. Screenshots show border corruption and line bleed while watcher/pipeline/publish logs are emitted.

**Artifacts added (DOE order):**
- Directive:
  - `directives/tui_render_stability_and_log_panel.md`
- Orchestration:
  - `directives/orchestration_tui_render_stability_and_log_panel.md`

**Planned UX direction captured:**
- Split right column into:
  - top detail panel
  - bottom log panel (~25%-30% height)
- Route runtime logs through TUI-safe message pipeline (no raw stdout/stderr writes in active TUI mode).
- Harden geometry clamps and line wrapping to prevent layout corruption.

## 2026-02-15 — AI execution recovery + syndication observability + meta command removal
**Context:** Latest batch run exposed four operational gaps:
1) AI enrichment not running in all runtime paths,
2) facebook/threads syndication failures hard to see when non-strict,
3) no durable runtime logs,
4) deprecated `ppw meta auth` still present.

**Directive/DOE artifacts:**
- `directives/ai_syndication_observability_and_meta_cleanup.md`
- `directives/orchestration_ai_syndication_observability_and_meta_cleanup.md`

**Implemented:**
- AI provider wiring unified:
  - `cmd/ppw/pipeline.go` and `cmd/ppw/watch.go` now use shared provider selection (`providerForRun` / `buildAIProvider`) instead of hardcoded Claude.
- Durable runtime logs added:
  - new `cmd/ppw/logging.go` with default log file `~/.ppw/ppw.log` and env override `PPW_LOG_FILE`.
  - TUI runtime logs now fan out to both in-app runtime panel and persistent file.
  - CLI runtime paths (`pipeline`, `watch`, `publish`) now also write to persistent log file.
- TUI render hardening for runtime error text:
  - `internal/tui/app.go` status bar now normalizes status text to single-line and truncates it to available width.
  - status messages now store plain text (no embedded ANSI style rendering), preventing multiline overflow from AI error payloads.
- Syndication visibility improved:
  - `cmd/ppw/default.go` now logs explicit warning when syndication setup is disabled/falls back.
  - `cmd/ppw/publish.go` prints per-target syndication summary after publish (`facebook` / `threads` status + error/permalink).
- Deprecated command removed:
  - removed `meta` command registration from `cmd/ppw/main.go`.
  - deleted `cmd/ppw/meta.go`.

**Docs/env updates:**
- `.env.sample`:
  - added `PPW_LOG_FILE` contract.
  - removed legacy text pointing to `ppw meta auth`.
- `.env`:
  - added commented `PPW_LOG_FILE` placeholder.
- `README.md`:
  - added runtime log override env var docs.
  - removed deprecated `ppw meta auth` note.

**Tests added:**
- `cmd/ppw/logging_test.go` (runtime log path + file writer fan-out)
- `cmd/ppw/provider_wiring_test.go` (provider selection: claude/codex/dry-run)
- `cmd/ppw/publish_test.go` (syndication summary output)
- `internal/tui/app_status_test.go` (single-line status normalization)

**Validation:**
- `/opt/homebrew/bin/go mod tidy` passed
- `/opt/homebrew/bin/go test ./...` passed
- `/opt/homebrew/bin/go build -o bin/ppw ./cmd/ppw` passed
- `./bin/ppw --help` confirms `meta` command no longer exists

**Operational note:**
- `make build` can still fail if shell resolves old Go (`/usr/local/bin/go` 1.13.x). Use `/opt/homebrew/bin/go` path or ensure Homebrew Go is first in `PATH`.

## 2026-02-15 — Structured per-job logging + retention sweeper (60m default)
**Context:** Needed verbose, consistent logs with intent/result semantics and automatic disposal of old logs, while preserving runtime visibility in TUI/CLI.

**Directive/orchestration artifacts:**
- `directives/structured_job_logging_and_retention.md`
- `directives/orchestration_structured_job_logging_and_retention.md`

**Implemented:**
- New structured-event helper package:
  - `internal/obslog/obslog.go`
  - Event schema includes: timestamp, module, job_id, post_id, action, type(intent/result), outcome(success/failure), duration_ms, error, details.
- New per-job log session + retention package:
  - `internal/joblog/joblog.go`
  - Job file lifecycle:
    - active: `<job_id>.active.jsonl`
    - finalized: `<job_id>.success.<unix>.jsonl` or `<job_id>.failed.<unix>.jsonl`
  - Retention sweeping supports separate TTL for success vs failure.
  - Periodic sweep runner added.
- Command logging/session wiring refactor in `cmd/ppw/logging.go`:
  - `openCommandLogSession(module, baseWriter)`
  - fan-out to: base output + runtime stream log + per-job log file
  - one-shot sweep hook + periodic sweep helper
- Entry-point integration:
  - `cmd/ppw/default.go` (TUI): starts periodic log sweep and writes logs to job file + runtime stream.
  - `cmd/ppw/watch.go`: starts periodic log sweep; watcher/pipeline logs write to session writer.
  - `cmd/ppw/pipeline.go`, `cmd/ppw/publish.go`: one-shot sweep on startup and per-run job session finalization.
- Structured instrumentation added for key actions:
  - `internal/pipeline/pipeline.go`:
    - scan / validate / enrich intent+result events
  - `internal/enricher/enricher.go`:
    - extract_caption / extract_location / extract_music intent+result events
    - includes success payload previews and failure details
  - `internal/publisher/publisher.go`:
    - verify_instagram_token / publish_instagram / publish_facebook / publish_threads intent+result events

**Retention defaults added:**
- `.env.sample`
  - `PPW_LOG_DIR=~/.ppw/logs`
  - `PPW_LOG_SUCCESS_TTL=24h`
  - `PPW_LOG_FAILED_TTL=720h`
  - `PPW_LOG_SWEEP_INTERVAL=60m`
- `.env`
  - same defaults exported

**Tests added:**
- `internal/joblog/joblog_test.go`
- `internal/obslog/obslog_test.go`
- `cmd/ppw/logging_test.go` updated for new session/finalization behavior

**Validation:**
- `/opt/homebrew/bin/go test ./...` passed
- `/opt/homebrew/bin/go build -o bin/ppw ./cmd/ppw` passed
