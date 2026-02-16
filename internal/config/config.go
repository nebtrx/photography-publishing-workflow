// Package config loads and validates the ppw configuration file.
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
)

// Config is the top-level ppw configuration.
type Config struct {
	Watch      WatchConfig      `toml:"watch"`
	AI         AIConfig         `toml:"ai"`
	R2         R2Config         `toml:"r2"`
	Meta       MetaConfig       `toml:"meta"`
	Threads    ThreadsConfig    `toml:"threads"`
	Auth       AuthConfig       `toml:"auth"`
	Publishing PublishingConfig `toml:"publishing"`
	Logging    LoggingConfig    `toml:"logging"`
	Archive    ArchiveConfig    `toml:"archive"`
}

// WatchConfig configures the directory watcher.
type WatchConfig struct {
	Dir string `toml:"dir"`
}

// AIConfig configures the AI provider.
type AIConfig struct {
	Provider      string `toml:"provider"`
	CorpusPath    string `toml:"corpus_path"`
	CodexCLIPath  string `toml:"codex_cli_path"`
	CodexModel    string `toml:"codex_model"`
	ClaudeCLIPath string `toml:"claude_cli_path"`
}

// R2Config configures Cloudflare R2 hosting.
type R2Config struct {
	AccessKeyID     string `toml:"access_key_id"`
	SecretAccessKey string `toml:"secret_access_key"`
	Bucket          string `toml:"bucket"`
	Endpoint        string `toml:"endpoint"`
	PublicURL       string `toml:"public_url"`
}

// MetaConfig configures Meta/Instagram settings.
type MetaConfig struct {
	AppID             string `toml:"app_id"`
	AppSecret         string `toml:"app_secret"`
	PageID            string `toml:"page_id"`
	InstagramUserID   string `toml:"instagram_user_id"`
	LegacyAccessToken string `toml:"legacy_access_token"`
}

// ThreadsConfig configures Threads settings.
type ThreadsConfig struct {
	UserID            string `toml:"user_id"`
	AppID             string `toml:"app_id"`
	AppSecret         string `toml:"app_secret"`
	LegacyAccessToken string `toml:"legacy_access_token"`
}

// AuthConfig configures managed token-store behavior.
type AuthConfig struct {
	TokenStore string `toml:"token_store"`
}

// PublishingConfig configures publishing behavior defaults.
type PublishingConfig struct {
	Destinations      []string `toml:"destinations"`
	StrictSyndication bool     `toml:"strict_syndication"`
	CleanupOnFailure  string   `toml:"cleanup_on_failure"` // never|always
}

// LoggingConfig configures runtime and per-job logging.
type LoggingConfig struct {
	RuntimeLogFile string `toml:"runtime_log_file"`
	JobLogDir      string `toml:"job_log_dir"`
	SuccessTTL     string `toml:"success_ttl"`
	FailedTTL      string `toml:"failed_ttl"`
	SweepInterval  string `toml:"sweep_interval"`
}

// ArchiveConfig configures archival.
type ArchiveConfig struct {
	Dir     string `toml:"dir"`
	LogFile string `toml:"log_file"`
}

// DefaultPath returns the default config file path relative to cwd.
func DefaultPath() string {
	return "config/ppw.toml"
}

// Load reads and parses a config file.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config %s: %w", path, err)
	}

	var cfg Config
	if err := toml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse config %s: %w", path, err)
	}

	// Expand ~ in paths
	cfg.Watch.Dir = expandHome(cfg.Watch.Dir)
	cfg.AI.CorpusPath = expandHome(cfg.AI.CorpusPath)
	cfg.Auth.TokenStore = expandHome(cfg.Auth.TokenStore)
	cfg.Logging.RuntimeLogFile = expandHome(cfg.Logging.RuntimeLogFile)
	cfg.Logging.JobLogDir = expandHome(cfg.Logging.JobLogDir)
	cfg.Archive.Dir = expandHome(cfg.Archive.Dir)
	cfg.Archive.LogFile = expandHome(cfg.Archive.LogFile)

	// Defaults
	if cfg.AI.Provider == "" {
		cfg.AI.Provider = "claude"
	}
	cfg.AI.Provider = normalizeProvider(cfg.AI.Provider)
	if cfg.Auth.TokenStore == "" {
		cfg.Auth.TokenStore = "~/.ppw/tokens.json"
		cfg.Auth.TokenStore = expandHome(cfg.Auth.TokenStore)
	}
	if cfg.Logging.RuntimeLogFile == "" {
		cfg.Logging.RuntimeLogFile = "~/.ppw/ppw.log"
		cfg.Logging.RuntimeLogFile = expandHome(cfg.Logging.RuntimeLogFile)
	}
	if cfg.Logging.JobLogDir == "" {
		cfg.Logging.JobLogDir = "~/.ppw/logs"
		cfg.Logging.JobLogDir = expandHome(cfg.Logging.JobLogDir)
	}
	if cfg.Logging.SuccessTTL == "" {
		cfg.Logging.SuccessTTL = "24h"
	}
	if cfg.Logging.FailedTTL == "" {
		cfg.Logging.FailedTTL = "720h"
	}
	if cfg.Logging.SweepInterval == "" {
		cfg.Logging.SweepInterval = "60m"
	}
	if len(cfg.Publishing.Destinations) == 0 {
		cfg.Publishing.Destinations = []string{"instagram"}
	}
	cfg.Publishing.CleanupOnFailure = normalizeCleanupOnFailure(cfg.Publishing.CleanupOnFailure)
	if cfg.Publishing.CleanupOnFailure == "" {
		cfg.Publishing.CleanupOnFailure = "never"
	}

	return &cfg, nil
}

func expandHome(path string) string {
	if len(path) > 1 && path[0] == '~' && path[1] == '/' {
		home, err := os.UserHomeDir()
		if err != nil {
			return path
		}
		return filepath.Join(home, path[2:])
	}
	return path
}

func normalizeProvider(raw string) string {
	v := strings.TrimSpace(strings.ToLower(raw))
	switch v {
	case "claude-cli", "claude":
		return "claude"
	case "codex-cli", "codex":
		return "codex"
	default:
		return "claude"
	}
}

func normalizeCleanupOnFailure(raw string) string {
	v := strings.TrimSpace(strings.ToLower(raw))
	switch v {
	case "always":
		return "always"
	case "", "never":
		return "never"
	default:
		return "never"
	}
}

// NormalizeSecretLike trims quotes/whitespace from copied token-like values.
func NormalizeSecretLike(v string) string {
	s := strings.TrimSpace(v)
	s = strings.Trim(s, `"'`)
	return strings.TrimSpace(s)
}
