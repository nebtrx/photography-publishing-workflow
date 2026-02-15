# Orchestration: Single Config Entrypoint (TOML-Only Runtime)

## Phase
Execution-ready orchestration for `directives/single_config_entrypoint_toml.md`.

## Objective
Migrate runtime configuration to a single TOML entrypoint, remove env-based behavior ambiguity, and preserve current publishing/auth capabilities.

## Workstreams

## WS1: Config Schema Expansion
- Deliverables:
  - Extend `internal/config/config.go` with runtime sections:
    - `ai`, `r2`, `meta`, `threads`, `auth`, `publishing`, `logging`, `watch`, `archive`
  - Add defaults and path expansion for path-like fields.
- Gate:
  - New schema loads with sane defaults.

## WS2: Runtime Wiring Refactor
- Deliverables:
  - Replace env reads in command wiring with config reads:
    - AI provider selection
    - auth manager creation
    - instagram/publisher setup
    - syndication destinations + strict mode
    - log file + job retention settings
- Gate:
  - Core commands run with TOML only.

## WS3: Service Adapters
- Deliverables:
  - Add config-based constructors/helpers for hosting and joblog.
  - Keep legacy env helpers deprecated but unused in runtime path.
- Gate:
  - No env dependency in active runtime path.

## WS4: Docs + Memory
- Deliverables:
  - Update `config/ppw.toml` with full schema and comments.
  - Update `README.md` and `TECHNICAL.md`.
- Gate:
  - Users can configure/run with TOML-only instructions.

## WS5: Validation
- Deliverables:
  - Update/adjust impacted tests.
  - Run build/tests and capture status.
- Gate:
  - `go test ./...` passes.

## Execution Order
1. WS1
2. WS2
3. WS3
4. WS4
5. WS5

## Risks / Mitigations
- Risk: partial migration leaves hidden env paths.
  - Mitigation: ripgrep audit for `os.Getenv` in runtime packages and refactor all active paths.
- Risk: existing user secrets currently in `.env`.
  - Mitigation: move values into TOML and keep `.env` explicitly deprecated.

## Resume Checklist
- [x] Directive created
- [x] Orchestration created
- [x] Runtime config schema expanded
- [x] Runtime wiring switched to TOML
- [x] Docs + tests updated

## Learnings (append-only)
- Config ambiguity is operationally expensive; a single runtime source is preferable even if migration is broad.
- `make build` can silently pick a stale Go toolchain from PATH; pinning/auto-selecting Homebrew Go in `Makefile` avoids false build failures during migration.
