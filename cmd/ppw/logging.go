package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"photography-publishing-workflow/internal/config"
	"photography-publishing-workflow/internal/joblog"
)

// CommandLogSession fans logs into terminal/TUI, runtime stream file, and one job log file.
type CommandLogSession struct {
	Writer      io.Writer
	RuntimePath string
	JobPath     string

	runtimeFile *os.File
	jobSession  *joblog.Session
}

func openCommandLogSession(module string, cfg *config.Config, base io.Writer) (*CommandLogSession, error) {
	runtimePath, err := runtimeLogPath(cfg)
	if err != nil {
		return nil, err
	}

	if err := os.MkdirAll(filepath.Dir(runtimePath), 0o700); err != nil {
		return nil, fmt.Errorf("create runtime log directory: %w", err)
	}

	runtimeFile, err := os.OpenFile(runtimePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open runtime log file: %w", err)
	}

	if base == nil {
		base = io.Discard
	}
	runtimeWriter := io.MultiWriter(base, runtimeFile)

	jobCfg := jobLogConfig(cfg)
	jobSess, err := joblog.NewSession(jobCfg, module, "", runtimeWriter)
	if err != nil {
		_ = runtimeFile.Close()
		return nil, err
	}

	return &CommandLogSession{
		Writer:      jobSess.Writer(),
		RuntimePath: runtimePath,
		JobPath:     jobSess.ActivePath(),
		runtimeFile: runtimeFile,
		jobSession:  jobSess,
	}, nil
}

func (s *CommandLogSession) Close(success bool) error {
	if s == nil {
		return nil
	}
	var firstErr error
	if s.jobSession != nil {
		if err := s.jobSession.Close(success); err != nil {
			firstErr = err
		} else {
			s.JobPath = s.jobSession.FinalPath()
		}
	}
	if s.runtimeFile != nil {
		if err := s.runtimeFile.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

func startPeriodicLogSweep(ctx context.Context, cfg *config.Config, logOutput io.Writer) {
	joblog.StartPeriodicSweep(ctx, jobLogConfig(cfg), logOutput)
}

func sweepLogsNow(cfg *config.Config, logOutput io.Writer) {
	joblog.SweepNow(jobLogConfig(cfg), logOutput)
}

func runtimeLogPath(cfg *config.Config) (string, error) {
	if cfg != nil {
		if raw := strings.TrimSpace(cfg.Logging.RuntimeLogFile); raw != "" {
			return expandHome(raw)
		}
	}
	return expandHome("~/.ppw/ppw.log")
}

func expandHome(path string) (string, error) {
	if path == "~" || strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("resolve home directory: %w", err)
		}
		if path == "~" {
			return home, nil
		}
		return filepath.Join(home, strings.TrimPrefix(path, "~/")), nil
	}
	return path, nil
}

func writeLogLine(w io.Writer, format string, args ...any) {
	if w == nil {
		w = os.Stderr
	}
	_, _ = fmt.Fprintf(w, format+"\n", args...)
}

func jobLogConfig(cfg *config.Config) joblog.Config {
	out := joblog.DefaultConfig()
	if cfg == nil {
		return out
	}
	if strings.TrimSpace(cfg.Logging.JobLogDir) != "" {
		out.LogDir = cfg.Logging.JobLogDir
	}
	if d, ok := parseDuration(strings.TrimSpace(cfg.Logging.SuccessTTL)); ok {
		out.SuccessTTL = d
	}
	if d, ok := parseDuration(strings.TrimSpace(cfg.Logging.FailedTTL)); ok {
		out.FailedTTL = d
	}
	if d, ok := parseDuration(strings.TrimSpace(cfg.Logging.SweepInterval)); ok {
		out.SweepInterval = d
	}
	return out
}

func parseDuration(raw string) (time.Duration, bool) {
	if raw == "" {
		return 0, false
	}
	d, err := time.ParseDuration(raw)
	if err != nil {
		return 0, false
	}
	return d, true
}
