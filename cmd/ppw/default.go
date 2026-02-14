package main

import (
	"context"
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"

	"photography-publishing-workflow/internal/ai"
	"photography-publishing-workflow/internal/archiver"
	"photography-publishing-workflow/internal/config"
	"photography-publishing-workflow/internal/hosting"
	"photography-publishing-workflow/internal/instagram"
	"photography-publishing-workflow/internal/meta"
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
	opts := buildAppOptions(cfg)

	model := tui.NewApp(cfg, cfgErrStr, opts)
	p := tea.NewProgram(model, tea.WithAltScreen())
	_, err := p.Run()
	return err
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
func buildAppOptions(cfg *config.Config) tui.AppOptions {
	var opts tui.AppOptions

	if cfg == nil {
		return opts
	}

	// Pipeline (needs AI provider)
	cliPath := os.Getenv("CLAUDE_CLI_PATH")
	provider := ai.NewClaudeCLI(cliPath)
	opts.Pipeline = pipeline.New(provider, pipeline.Options{
		CorpusPath: cfg.AI.CorpusPath,
	})

	// Publisher (needs R2 + Instagram credentials)
	pub := buildPublisher(cfg)
	if pub != nil {
		opts.Publisher = pub
	}

	// Archiver
	if cfg.Archive.Dir != "" && cfg.Archive.LogFile != "" {
		arch, err := archiver.New(archiver.Options{
			ArchiveDir: cfg.Archive.Dir,
			LogFile:    cfg.Archive.LogFile,
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
//   - If META_APP_ID + META_APP_SECRET + META_PAGE_ID are set: uses Page token
//     derived via TokenManager (recommended, supports auto-retry on 401).
//   - Otherwise: falls back to static INSTAGRAM_ACCESS_TOKEN (legacy).
func buildPublisher(cfg *config.Config) *publisher.Publisher {
	r2Cfg, err := hosting.R2ConfigFromEnv()
	if err != nil {
		return nil // R2 not configured — publishing disabled
	}

	igClient, tm := buildInstagramClientWithMeta()
	if igClient == nil {
		return nil
	}

	ctx := context.Background()
	r2, err := hosting.NewR2(ctx, r2Cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: R2 connection failed: %v\n", err)
		return nil
	}

	fbEnabled, threadsEnabled, err := parseDestinations(defaultDestinations())
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: invalid syndication destinations: %v\n", err)
		fbEnabled, threadsEnabled = false, false
	}

	opts, err := buildSyndicationOptions(ctx, tm, fbEnabled, threadsEnabled, envBool("STRICT_SYNDICATION"), false)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: syndication setup failed: %v\n", err)
		opts = publisher.Options{}
	}

	return publisher.New(r2, igClient, opts)
}

// buildInstagramClient creates an Instagram client, preferring auth-managed mode.
func buildInstagramClient() *instagram.Client {
	client, _ := buildInstagramClientWithMeta()
	return client
}

// buildInstagramClientWithMeta returns the IG client and optional token manager used for managed auth.
func buildInstagramClientWithMeta() (*instagram.Client, *meta.TokenManager) {
	userID := os.Getenv("INSTAGRAM_USER_ID")
	metaCfg := buildMetaConfig()

	// Try auth-managed mode (Page token via TokenManager)
	if metaCfg.AppID != "" && metaCfg.AppSecret != "" && metaCfg.PageID != "" && metaCfg.UserToken != "" {
		tm := meta.NewTokenManager(metaCfg)
		client, err := instagram.NewClientWithTokenSource(
			userID,
			func(ctx context.Context) (string, error) { return tm.GetPageAccessToken(ctx) },
			tm.InvalidatePageTokenCache,
		)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Warning: Meta auth setup failed: %v\n", err)
		} else {
			fmt.Fprintln(os.Stderr, "[publish] Using Page token auth (META_APP_ID configured)")
			return client, tm
		}
	}

	// Fall back to static token mode
	client, err := instagram.NewClient()
	if err != nil {
		return nil, nil // Instagram not configured
	}
	fmt.Fprintln(os.Stderr, "[publish] Using static token auth (set META_APP_ID/META_APP_SECRET/META_PAGE_ID for managed auth)")
	return client, nil
}
