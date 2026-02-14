# Photography Publishing Workflow (`ppw`)

`ppw` is a Go-based workflow engine for photo publishing, built around a directive-first operating model:

1. `directives/` define what should happen (requirements, constraints, acceptance criteria).
2. Orchestration decides how to execute a directive.
3. `cmd/` + `internal/` implement deterministic execution.

This repo currently supports a full lifecycle from scan to publish/archive, with a unified TUI and CLI subcommands.

## Architecture

- `directives/`
  - Product/ops directives (SOP-style specs).
  - Includes orchestration docs for larger initiatives.
- `cmd/ppw/`
  - CLI entrypoints and command wiring.
  - `ppw` with no subcommand launches the unified TUI.
- `internal/`
  - Domain packages: scanner, validator, enricher, publisher, archiver, scheduler, watcher, tui, meta auth, etc.
- `execution/`
  - Convention space for deterministic tools/templates.
- `config/`
  - Runtime config (`config/ppw.toml`).
- `TECHNICAL.md`
  - Append-only decisions and implementation memory log.

## Main Workflow

Pipeline stages in this repository:

1. `scan` -> create manifest from image directory.
2. `validate` -> validate image set and metadata.
3. `enrich` -> AI caption/location/music.
4. `review` -> approve/reject/edit in TUI.
5. `publish` -> Instagram (plus optional syndication).
6. `archive` -> move published work to archive + log.

Single-step stage 1-3:

- `ppw pipeline --dir <post-dir>`

## Quickstart

Build:

```bash
make build
```

Show CLI help:

```bash
./bin/ppw --help
```

Run tests:

```bash
/opt/homebrew/bin/go test ./...
```

Launch unified TUI:

```bash
./bin/ppw
```

## Environment and Config

Use `.env.sample` as the contract for required/optional environment variables.

Current key groups:

- Cloudflare R2 credentials (publishing host)
- Platform IDs/config (`INSTAGRAM_USER_ID`, `META_APP_ID`, `META_APP_SECRET`, `META_PAGE_ID`, `THREADS_USER_ID`)
- Token store path override (`PPW_TOKEN_STORE`, default `~/.ppw/tokens.json`)
- Syndication behavior (`PUBLISH_DESTINATIONS`, `STRICT_SYNDICATION`)
- Legacy/manual token vars (temporary during auth automation migration)

## How to Navigate This Repo

When you need to change behavior:

1. Start in `directives/` and identify the controlling directive.
2. If behavior change is new, add/update directive first.
3. Create/extend orchestration doc for multi-step implementation.
4. Implement in `cmd/` + `internal/`.
5. Update `TECHNICAL.md` with concrete changes and outcomes.

Recommended reading order:

1. `directives/publishing_workflow_requirements.md`
2. `directives/unified_tui.md`
3. `directives/instagram_publishing.md`
4. `directives/cross_platform_syndication.md`
5. `directives/auth_token_automation.md`
6. `directives/orchestration_auth_token_automation.md`

## Using Codex with This Repo

Suggested collaboration pattern:

1. Ask Codex to read directives + `TECHNICAL.md` before coding.
2. Require DOE sequencing: directive -> orchestration -> execution.
3. Ask for:
   - concrete file edits
   - tests
   - `go test ./...` verification
   - memory/log update in `TECHNICAL.md`
4. Keep tasks bounded by workstream when possible (WS1/WS2/etc.).

Example prompt:

```text
Read directives/auth_token_automation.md and directives/orchestration_auth_token_automation.md.
Execute WS2 only. Implement code, tests, run go test ./..., then append TECHNICAL.md.
Do not start WS3.
```

## Using Claude with This Repo

This repo already includes agent guidance files:

- `CLAUDE.md`
- `AGENTS.md`
- `GEMINI.md`

For Claude sessions:

1. Start by asking Claude to read `TECHNICAL.md` and relevant directive(s).
2. Ask Claude to persist plans and implementation checkpoints in repo files (avoid ephemeral-only context).
3. Prefer explicit continuation prompts such as:
   - "Continue from directives/orchestration_auth_token_automation.md, WS3 only."
4. Require test run + file references in final output.

## Security Notes

- Never commit real secrets in `.env`.
- Keep `.env` local; use placeholders in `.env.sample`.
- Token material should live in `~/.ppw/tokens.json` (outside repo) as auth automation is completed.

## License

No license file is currently defined in this repository.
