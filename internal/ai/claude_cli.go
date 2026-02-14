package ai

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
)

// ClaudeCLI invokes the `claude` CLI as a subprocess for AI generation.
// Uses the user's Claude Code Pro subscription — no API key needed.
type ClaudeCLI struct {
	// BinaryPath is the path to the claude CLI binary.
	// Defaults to "claude" (found via PATH).
	BinaryPath string
}

// NewClaudeCLI creates a ClaudeCLI provider. If binaryPath is empty,
// it defaults to "claude" on PATH.
func NewClaudeCLI(binaryPath string) *ClaudeCLI {
	if binaryPath == "" {
		binaryPath = "claude"
	}
	return &ClaudeCLI{BinaryPath: binaryPath}
}

func (c *ClaudeCLI) Name() string { return "claude-cli" }

// Generate invokes the claude CLI in print mode with the given prompt and images.
func (c *ClaudeCLI) Generate(ctx context.Context, req Request) (*Response, error) {
	// Verify claude CLI exists
	if _, err := exec.LookPath(c.BinaryPath); err != nil {
		return nil, fmt.Errorf("claude CLI not found at %q: %w (set CLAUDE_CLI_PATH or ensure 'claude' is on PATH)", c.BinaryPath, err)
	}

	// Build the prompt: combine system + user prompt into a single message
	// since claude CLI --print mode takes a single prompt string.
	fullPrompt := buildPrompt(req.SystemPrompt, req.UserPrompt)

	// Build command args
	args := []string{
		"--print",        // non-interactive, print response to stdout
		"--output-format", "text", // plain text output
	}

	// Add image attachments
	for _, imgPath := range req.ImagePaths {
		args = append(args, "--add-file", imgPath)
	}

	// The prompt goes as the positional argument
	args = append(args, fullPrompt)

	cmd := exec.CommandContext(ctx, c.BinaryPath, args...)

	out, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return nil, fmt.Errorf("claude CLI failed (exit %d): %s", exitErr.ExitCode(), string(exitErr.Stderr))
		}
		return nil, fmt.Errorf("claude CLI execution error: %w", err)
	}

	text := strings.TrimSpace(string(out))
	if text == "" {
		return nil, fmt.Errorf("claude CLI returned empty response")
	}

	return &Response{
		Text:  text,
		Model: "claude-cli",
	}, nil
}

// buildPrompt combines system and user prompts into a single string.
func buildPrompt(system, user string) string {
	var b strings.Builder
	if system != "" {
		b.WriteString(system)
		b.WriteString("\n\n---\n\n")
	}
	b.WriteString(user)
	return b.String()
}
