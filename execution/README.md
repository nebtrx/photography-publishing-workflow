# Execution Tools (Go-first)

Tools in this folder implement deterministic work.

## Rules
- Prefer Go for execution tools.
- Tools must be runnable locally with minimal setup.
- Provide `--help` and usage examples (README or in-tool help).
- If tool writes files or mutates anything, provide `--dry-run` when feasible.
- Use `.tmp/` for intermediate outputs; never commit `.tmp/`.

## Suggested tool shape
Either:
- small standalone tools compiled to `bin/`, or
- one Go CLI with subcommands (recommended once commands grow)

## Conventions
- Log to stdout/stderr with clear, parseable messages.
- Fail fast on validation errors.
