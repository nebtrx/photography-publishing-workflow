package ai

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// CodexCLI invokes the OpenAI `codex` CLI as a subprocess for AI generation.
// Uses the user's ChatGPT Plus/Pro subscription — no API key needed.
type CodexCLI struct {
	// BinaryPath is the path to the codex CLI binary.
	// Defaults to "codex" (found via PATH).
	BinaryPath string

	// Model overrides the default model. Leave empty to use the codex default.
	Model string
}

// NewCodexCLI creates a CodexCLI provider. If binaryPath is empty,
// it defaults to "codex" on PATH.
func NewCodexCLI(binaryPath, model string) *CodexCLI {
	if binaryPath == "" {
		binaryPath = "codex"
	}
	return &CodexCLI{BinaryPath: binaryPath, Model: model}
}

func (c *CodexCLI) Name() string { return "codex-cli" }

// Generate invokes the codex CLI in exec (non-interactive) mode with the given
// prompt and images. Uses --sandbox read-only to prevent file modifications.
//
// Prompt is passed via stdin ("-") instead of positional args to avoid
// ambiguity with variadic --image parsing.
func (c *CodexCLI) Generate(ctx context.Context, req Request) (*Response, error) {
	if _, err := exec.LookPath(c.BinaryPath); err != nil {
		return nil, fmt.Errorf("codex CLI not found at %q: %w (install: npm i -g @openai/codex && codex login)", c.BinaryPath, err)
	}

	fullPrompt := buildCodexPrompt(req.SystemPrompt, req.UserPrompt)

	// Create temp file for --output-last-message
	tmpFile, err := os.CreateTemp("", "ppw-codex-*.txt")
	if err != nil {
		return nil, fmt.Errorf("create temp file for codex output: %w", err)
	}
	outputPath := tmpFile.Name()
	tmpFile.Close()
	defer os.Remove(outputPath)

	args := buildCodexArgs(c.Model, req.ImagePaths, outputPath)

	cmd := exec.CommandContext(ctx, c.BinaryPath, args...)
	// Run from a temp dir so codex doesn't try to read a project
	cmd.Dir = os.TempDir()
	cmd.Stdin = strings.NewReader(fullPrompt)

	stdoutBytes, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			stderr := strings.TrimSpace(string(exitErr.Stderr))
			if strings.Contains(stderr, "auth") || strings.Contains(stderr, "login") || strings.Contains(stderr, "token") {
				return nil, fmt.Errorf("codex CLI auth error (run `codex login`): %s", stderr)
			}
			return nil, fmt.Errorf("codex CLI failed (exit %d): %s", exitErr.ExitCode(), stderr)
		}
		return nil, fmt.Errorf("codex CLI execution error: %w", err)
	}

	// Prefer --output-last-message file; fall back to stdout
	text := readOutputFile(outputPath)
	if text == "" {
		text = strings.TrimSpace(string(stdoutBytes))
	}

	if text == "" {
		return nil, fmt.Errorf("codex CLI returned empty response")
	}

	model := "codex-cli"
	if c.Model != "" {
		model = "codex-cli/" + c.Model
	}

	return &Response{
		Text:  text,
		Model: model,
	}, nil
}

func buildCodexArgs(model string, imagePaths []string, outputPath string) []string {
	args := []string{
		"exec",
		"--sandbox", "read-only",
		"--skip-git-repo-check",
		"--output-last-message", outputPath,
	}

	if model != "" {
		args = append(args, "--model", model)
	}

	// Pass each image independently; variadic --image parsing can otherwise
	// swallow the prompt positional argument.
	for _, p := range imagePaths {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		args = append(args, "--image", p)
	}

	// Stop option parsing and request prompt from stdin explicitly.
	return append(args, "--", "-")
}

// buildCodexPrompt combines system and user prompts into a single string,
// with an explicit instruction to only produce text output.
func buildCodexPrompt(system, user string) string {
	var b strings.Builder
	if system != "" {
		b.WriteString("SYSTEM INSTRUCTIONS:\n")
		b.WriteString(system)
		b.WriteString("\n\n---\n\n")
	}
	b.WriteString(user)
	b.WriteString("\n\nIMPORTANT: Respond with ONLY the requested output text. Do not create files, run commands, or modify anything.")
	return b.String()
}

// readOutputFile reads and trims a file's contents. Returns "" on any error.
func readOutputFile(path string) string {
	abs, err := filepath.Abs(path)
	if err != nil {
		return ""
	}
	data, err := os.ReadFile(abs)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}
