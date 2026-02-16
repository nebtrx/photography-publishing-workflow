# Directive: OpenAI Codex CLI AI Provider

## Goal

Add a second AI provider (`codex-cli`) that uses the OpenAI Codex CLI as a subprocess for AI enrichment (caption generation and location identification). This leverages the user's ChatGPT Plus/Pro subscription — no API key or credits needed. The provider is selected at runtime via `PPW_AI_PROVIDER=codex` and satisfies the existing `ai.Provider` interface identically to the Claude CLI provider.

## Context / Constraints

- The existing `ai.Provider` interface (`Generate(ctx, Request) (*Response, error)` + `Name() string`) is the contract. The Codex CLI provider must satisfy it without changes to the interface.
- Claude CLI remains the default provider. Codex CLI is an alternative for when Claude credits run out.
- Codex CLI is designed for code tasks, not text generation. We use `--sandbox read-only` to prevent file modification and steer the model toward pure text output via prompt design.
- Codex CLI requires Node.js and `npm i -g @openai/codex` to install. The user must be logged in (`codex login`).
- The user has a ChatGPT Plus/Pro subscription.
- Codex CLI uses gpt-4o by default for vision tasks. Model can be overridden.
- No changes to the enricher, pipeline, or any consumer of `ai.Provider` should be needed.

## Inputs

- Environment variables:
  - `PPW_AI_PROVIDER`: Provider selection. Values: `claude` (default), `codex`. Case-insensitive.
  - `CODEX_CLI_PATH`: Path to the `codex` CLI binary. Default: `codex` on PATH.
  - `CODEX_MODEL`: Model override for Codex CLI. Default: none (uses Codex CLI's default, typically `gpt-4o`).

## Outputs

- New file: `internal/ai/codex_cli.go` — `CodexCLI` struct implementing `ai.Provider`.
- New file: `internal/ai/codex_cli_test.go` — unit tests.
- Modified file: `cmd/ppw/default.go` — provider selection logic reads `PPW_AI_PROVIDER`.
- Modified file: `.env.sample` — documents new env vars.
- Modified file: `directives/ai_enrichment.md` — learnings section updated.

## Implementation Details

### Codex CLI Invocation Pattern

```bash
codex exec \
  --sandbox read-only \
  --image /path/to/hero.jpg \
  --output-last-message /tmp/ppw-codex-XXXXX.txt \
  --skip-git-repo-check \
  "SYSTEM INSTRUCTIONS:
   <system prompt here>

   ---

   USER REQUEST:
   <user prompt here>"
```

Key flags:
- `codex exec` — non-interactive mode, runs and exits.
- `--sandbox read-only` — prevents the model from writing files or executing commands that modify state. Critical for our use case since we only want text output.
- `--image path[,path...]` — attaches images for vision analysis. Supports PNG, JPEG, WebP. Multiple images: comma-separated or repeat the flag.
- `--output-last-message /tmp/file.txt` — writes the assistant's final message to a file. This is the cleanest way to get the response text (stdout may contain progress/status noise).
- `--skip-git-repo-check` — allows running outside a git repo context (the enricher may run from the inbox dir which isn't a git repo).
- `--model gpt-4o` — optional model override via `CODEX_MODEL` env var.

### Response Extraction

1. Create a temp file path for `--output-last-message`.
2. Run `codex exec` with all flags.
3. Read the temp file contents after the process exits.
4. Clean up the temp file.
5. If the temp file is empty or doesn't exist, fall back to capturing stdout.
6. Trim whitespace, validate non-empty.

### Prompt Construction

Same pattern as Claude CLI — since Codex CLI has no system prompt flag, we embed both in a single prompt string:

```
SYSTEM INSTRUCTIONS:
<system prompt>

---

USER REQUEST:
<user prompt>

IMPORTANT: Respond with ONLY the requested output. Do not attempt to create files, run commands, or modify anything. Just provide the text response.
```

The trailing instruction steers the model away from code execution behavior.

### Provider Selection (in cmd/ppw/default.go)

```go
func buildAIProvider() ai.Provider {
    switch strings.ToLower(os.Getenv("PPW_AI_PROVIDER")) {
    case "codex":
        return ai.NewCodexCLI(os.Getenv("CODEX_CLI_PATH"), os.Getenv("CODEX_MODEL"))
    default: // "claude" or empty
        return ai.NewClaudeCLI(os.Getenv("CLAUDE_CLI_PATH"))
    }
}
```

### CodexCLI Struct

```go
type CodexCLI struct {
    BinaryPath string // defaults to "codex"
    Model      string // optional model override
}
```

Methods:
- `Name() string` — returns `"codex-cli"`
- `Generate(ctx, Request) (*Response, error)`:
  1. Verify `codex` binary exists via `exec.LookPath`.
  2. Build combined prompt from system + user prompts.
  3. Create temp file for `--output-last-message`.
  4. Build args: `exec`, `--sandbox`, `read-only`, `--skip-git-repo-check`, image flags, `--output-last-message`, prompt.
  5. If `Model` is set, add `--model`.
  6. Execute with context (timeout from ctx).
  7. Read response from temp file (fallback: stdout).
  8. Return `&Response{Text: text, Model: "codex-cli"}`.

## Edge Cases / Failure Modes

- **Codex CLI not installed**: `exec.LookPath` fails → return error with install instructions: `"codex CLI not found. Install: npm i -g @openai/codex && codex login"`.
- **Not logged in**: Codex CLI returns non-zero exit. Parse stderr for auth errors → suggest `codex login`.
- **Sandbox prevents response**: If `--sandbox read-only` causes issues with response generation, fall back to capturing stdout directly.
- **Output file empty**: Codex may stream to stdout instead of writing `--output-last-message` in some edge cases. Check stdout as fallback.
- **Model not available**: If `CODEX_MODEL` specifies a model the subscription doesn't cover, Codex CLI returns an error. Surface it clearly.
- **Timeout**: Context cancellation kills the subprocess. Return timeout error.
- **Large images**: Codex CLI handles image encoding internally. No pre-processing needed.
- **Multiple images**: Use comma-separated paths with single `--image` flag, or repeat `--image` per image.

## Acceptance Criteria

- [ ] `PPW_AI_PROVIDER=codex ppw enrich --manifest path/to/manifest.json` generates a caption using Codex CLI.
- [ ] Images are passed to Codex CLI via `--image` flag and the model analyzes them (vision).
- [ ] `PPW_AI_PROVIDER=claude` (or unset) continues to use Claude CLI as before — no behavioral change.
- [ ] When Codex CLI is not installed, the error message includes install instructions.
- [ ] The enricher, pipeline, and TUI are completely unaware of which provider is active — the `ai.Provider` interface is the only boundary.
- [ ] `--dry-run` on enrichment commands works identically regardless of provider.
- [ ] Unit tests cover: binary lookup, prompt construction, response extraction from file, response extraction from stdout fallback, error handling.
- [ ] `.env.sample` documents `PPW_AI_PROVIDER`, `CODEX_CLI_PATH`, `CODEX_MODEL`.

## Safety Notes

- `--sandbox read-only` is mandatory. Never invoke Codex CLI without it in this context — we do not want the model modifying files during enrichment.
- `--skip-git-repo-check` is needed since the working directory may not be a git repo.
- Temp files for `--output-last-message` must be created in `os.TempDir()` and cleaned up in a `defer`.
- The combined prompt explicitly instructs the model not to create files or run commands.

## Learnings (append-only)

- (None yet)
