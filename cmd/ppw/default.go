package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"photography-publishing-workflow/internal/ai"
	"photography-publishing-workflow/internal/archiver"
	"photography-publishing-workflow/internal/authn"
	"photography-publishing-workflow/internal/config"
	"photography-publishing-workflow/internal/hosting"
	"photography-publishing-workflow/internal/instagram"
	"photography-publishing-workflow/internal/pipeline"
	"photography-publishing-workflow/internal/publisher"
	"photography-publishing-workflow/internal/tui"
)

// runDefault launches the unified lazygit-style TUI.
func runDefault() error {
	// Load config
	cfg, cfgErr := loadConfig()

	cfgErrStr := ""
	if cfgErr != nil {
		cfgErrStr = cfgErr.Error()
	}

	// Build optional dependencies from config + env
	eventCh := make(chan tea.Msg, 256)
	eventLogWriter := tui.NewEventLogWriter(eventCh)
	session, err := openCommandLogSession("tui", eventLogWriter)
	if err != nil {
		eventCh <- tui.AppLogMsg{Line: fmt.Sprintf("[runtime] WARN: persistent logging disabled: %v", err)}
		session = &CommandLogSession{Writer: eventLogWriter}
	} else {
		eventCh <- tui.AppLogMsg{Line: fmt.Sprintf("[runtime] Log stream: %s", session.RuntimePath)}
		eventCh <- tui.AppLogMsg{Line: fmt.Sprintf("[runtime] Job log: %s", session.JobPath)}
	}
	defer session.Close(false)

	sweepCtx, stopSweep := context.WithCancel(context.Background())
	defer stopSweep()
	go startPeriodicLogSweep(sweepCtx, session.Writer)
	sweepLogsNow(session.Writer)

	opts := buildAppOptions(cfg, eventCh, session.Writer)

	model := tui.NewApp(cfg, cfgErrStr, opts)
	p := tea.NewProgram(model, tea.WithAltScreen())
	_, runErr := p.Run()
	_ = session.Close(runErr == nil)
	return runErr
}

// loadConfig tries to load the ppw config file.
func loadConfig() (*config.Config, error) {
	path := config.DefaultPath()
	if envPath := os.Getenv("PPW_CONFIG"); envPath != "" {
		path = envPath
	}
	return config.Load(path)
}

// buildAppOptions creates optional background dependencies from config and env vars.
// Missing credentials are not fatal — features are simply disabled.
func buildAppOptions(cfg *config.Config, eventCh chan tea.Msg, logOutput io.Writer) tui.AppOptions {
	var opts tui.AppOptions

	if cfg == nil {
		return opts
	}
	opts.EventCh = eventCh
	opts.LogOutput = logOutput

	// Pipeline (needs AI provider)
	provider := buildAIProvider()
	opts.Pipeline = pipeline.New(provider, pipeline.Options{
		CorpusPath: cfg.AI.CorpusPath,
		LogOutput:  logOutput,
	})

	// Publisher (needs R2 + Instagram credentials)
	pub := buildPublisher(cfg, logOutput)
	if pub != nil {
		opts.Publisher = pub
	}

	// Archiver
	if cfg.Archive.Dir != "" && cfg.Archive.LogFile != "" {
		arch, err := archiver.New(archiver.Options{
			ArchiveDir: cfg.Archive.Dir,
			LogFile:    cfg.Archive.LogFile,
			LogOutput:  logOutput,
		})
		if err == nil {
			opts.Archiver = arch
		}
	}

	return opts
}

// buildPublisher attempts to create a publisher from environment variables.
// Returns nil if R2 or Instagram credentials are missing.
//
// Auth modes:
//   - Managed mode: run `ppw auth login` once, then publish uses store-backed tokens.
//   - Fallback mode: static INSTAGRAM_ACCESS_TOKEN from environment (legacy).
func buildPublisher(cfg *config.Config, logOutput io.Writer) *publisher.Publisher {
	r2Cfg, err := hosting.R2ConfigFromEnv()
	if err != nil {
		return nil // R2 not configured — publishing disabled
	}

	igClient, tm := buildInstagramClientWithMeta(logOutput)
	if igClient == nil {
		return nil
	}

	ctx := context.Background()
	r2, err := hosting.NewR2(ctx, r2Cfg)
	if err != nil {
		writeLogLine(logOutput, "[runtime] WARN: R2 connection failed: %v", err)
		return nil
	}

	fbEnabled, threadsEnabled, err := parseDestinations(defaultDestinations())
	if err != nil {
		writeLogLine(logOutput, "[runtime] WARN: invalid syndication destinations: %v", err)
		fbEnabled, threadsEnabled = false, false
	}

	opts, err := buildSyndicationOptions(ctx, tm, fbEnabled, threadsEnabled, envBool("STRICT_SYNDICATION"), false)
	if err != nil {
		writeLogLine(logOutput, "[runtime] WARN: syndication disabled due setup failure: %v", err)
		opts = publisher.Options{}
	}

	opts.LogOutput = logOutput
	return publisher.New(r2, igClient, opts)
}

// buildAIProvider creates an AI provider based on PPW_AI_PROVIDER env var.
// Supported values: "claude" (default), "codex".
func buildAIProvider() ai.Provider {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("PPW_AI_PROVIDER"))) {
	case "codex":
		return ai.NewCodexCLI(os.Getenv("CODEX_CLI_PATH"), os.Getenv("CODEX_MODEL"))
	default:
		return ai.NewClaudeCLI(os.Getenv("CLAUDE_CLI_PATH"))
	}
}

func providerForRun(dryRun bool) ai.Provider {
	if dryRun {
		return nil
	}
	return buildAIProvider()
}

// buildInstagramClient creates an Instagram client, preferring auth-managed mode.
func buildInstagramClient() *instagram.Client {
	client, _ := buildInstagramClientWithMeta(os.Stderr)
	return client
}

// buildInstagramClientWithMeta returns the IG client and optional auth manager used for managed auth.
func buildInstagramClientWithMeta(logOutput io.Writer) (*instagram.Client, *authn.Manager) {
	userID := os.Getenv("INSTAGRAM_USER_ID")
	authMgr, err := buildAuthManager()
	if err != nil {
		writeLogLine(logOutput, "[runtime] WARN: auth manager setup failed: %v", err)
	}

	// Try auth-managed mode (store-backed token lifecycle).
	if authMgr != nil && strings.TrimSpace(os.Getenv("META_PAGE_ID")) != "" {
		client, err := instagram.NewClientWithTokenSource(
			userID,
			func(ctx context.Context) (string, error) { return authMgr.PageAccessToken(ctx) },
			nil,
		)
		if err != nil {
			writeLogLine(logOutput, "[runtime] WARN: store-backed auth setup failed: %v", err)
		} else {
			writeLogLine(logOutput, "[publish] Using managed auth (token store + automatic refresh)")
			return client, authMgr
		}
	}

	// Fall back to static token mode
	client, err := instagram.NewClient()
	if err != nil {
		return nil, nil // Instagram not configured
	}
	writeLogLine(logOutput, "[publish] Using legacy static token auth (run `ppw auth login` to migrate)")
	return client, nil
}
