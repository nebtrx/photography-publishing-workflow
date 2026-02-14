// Package config loads and validates the ppw configuration file.
package config

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
)

// Config is the top-level ppw configuration.
type Config struct {
	Watch   WatchConfig   `toml:"watch"`
	AI      AIConfig      `toml:"ai"`
	Archive ArchiveConfig `toml:"archive"`
}

// WatchConfig configures the directory watcher.
type WatchConfig struct {
	Dir string `toml:"dir"`
}

// AIConfig configures the AI provider.
type AIConfig struct {
	Provider   string `toml:"provider"`
	CorpusPath string `toml:"corpus_path"`
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
	cfg.Archive.Dir = expandHome(cfg.Archive.Dir)
	cfg.Archive.LogFile = expandHome(cfg.Archive.LogFile)

	// Defaults
	if cfg.AI.Provider == "" {
		cfg.AI.Provider = "claude-cli"
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
