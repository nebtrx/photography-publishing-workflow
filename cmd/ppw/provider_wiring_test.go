package main

import (
	"testing"

	"photography-publishing-workflow/internal/ai"
	"photography-publishing-workflow/internal/config"
)

func TestBuildAIProvider_DefaultClaude(t *testing.T) {
	cfg := &config.Config{}
	cfg.AI.Provider = "claude"
	cfg.AI.ClaudeCLIPath = "/tmp/claude"

	provider := buildAIProvider(cfg)
	claude, ok := provider.(*ai.ClaudeCLI)
	if !ok {
		t.Fatalf("provider type = %T, want *ai.ClaudeCLI", provider)
	}
	if claude.BinaryPath != "/tmp/claude" {
		t.Fatalf("claude path = %q, want /tmp/claude", claude.BinaryPath)
	}
}

func TestBuildAIProvider_Codex(t *testing.T) {
	cfg := &config.Config{}
	cfg.AI.Provider = "codex"
	cfg.AI.CodexCLIPath = "/tmp/codex"
	cfg.AI.CodexModel = "gpt-5.2"

	provider := buildAIProvider(cfg)
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
	cfg := &config.Config{}
	cfg.AI.Provider = "codex"
	if provider := providerForRun(cfg, true); provider != nil {
		t.Fatalf("providerForRun(true) = %T, want nil", provider)
	}
	if provider := providerForRun(cfg, false); provider == nil {
		t.Fatal("providerForRun(false) should return a provider")
	}
}
