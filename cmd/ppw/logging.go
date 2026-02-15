package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"photography-publishing-workflow/internal/joblog"
)

const defaultRuntimeLogRelPath = ".ppw/ppw.log"

// CommandLogSession fans logs into terminal/TUI, runtime stream file, and one job log file.
type CommandLogSession struct {
	Writer      io.Writer
	RuntimePath string
	JobPath     string

	runtimeFile *os.File
	jobSession  *joblog.Session
}

func openCommandLogSession(module string, base io.Writer) (*CommandLogSession, error) {
	runtimePath, err := runtimeLogPath()
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

	cfg := joblog.ConfigFromEnv()
	jobSess, err := joblog.NewSession(cfg, module, "", runtimeWriter)
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

func startPeriodicLogSweep(ctx context.Context, logOutput io.Writer) {
	joblog.StartPeriodicSweep(ctx, joblog.ConfigFromEnv(), logOutput)
}

func sweepLogsNow(logOutput io.Writer) {
	joblog.SweepNow(joblog.ConfigFromEnv(), logOutput)
}

func runtimeLogPath() (string, error) {
	if raw := strings.TrimSpace(os.Getenv("PPW_LOG_FILE")); raw != "" {
		return expandHome(raw)
	}
	return expandHome("~/" + defaultRuntimeLogRelPath)
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
