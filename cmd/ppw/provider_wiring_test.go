package main

import (
	"testing"

	"photography-publishing-workflow/internal/ai"
)

func TestBuildAIProvider_DefaultClaude(t *testing.T) {
	t.Setenv("PPW_AI_PROVIDER", "")
	t.Setenv("CLAUDE_CLI_PATH", "/tmp/claude")

	provider := buildAIProvider()
	claude, ok := provider.(*ai.ClaudeCLI)
	if !ok {
		t.Fatalf("provider type = %T, want *ai.ClaudeCLI", provider)
	}
	if claude.BinaryPath != "/tmp/claude" {
		t.Fatalf("claude path = %q, want /tmp/claude", claude.BinaryPath)
	}
}

func TestBuildAIProvider_Codex(t *testing.T) {
	t.Setenv("PPW_AI_PROVIDER", "codex")
	t.Setenv("CODEX_CLI_PATH", "/tmp/codex")
	t.Setenv("CODEX_MODEL", "gpt-5.2")

	provider := buildAIProvider()
	codex, ok := provider.(*ai.CodexCLI)
	if !ok {
		t.Fatalf("provider type = %T, want *ai.CodexCLI", provider)
	}
	if codex.BinaryPath != "/tmp/codex" {
		t.Fatalf("codex path = %q, want /tmp/codex", codex.BinaryPath)
	}
	if codex.Model != "gpt-5.2" {
		t.Fatalf("codex model = %q, want gpt-5.2", codex.Model)
	}
}

func TestProviderForRun_DryRunDisablesAI(t *testing.T) {
	t.Setenv("PPW_AI_PROVIDER", "codex")
	if provider := providerForRun(true); provider != nil {
		t.Fatalf("providerForRun(true) = %T, want nil", provider)
	}
	if provider := providerForRun(false); provider == nil {
		t.Fatal("providerForRun(false) should return a provider")
	}
}
