package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

const defaultRuntimeLogRelPath = ".ppw/ppw.log"

// commandLogOutput returns a writer that mirrors command logs to stderr and a
// persistent runtime log file. The file path can be overridden with PPW_LOG_FILE.
func commandLogOutput(base io.Writer) (io.Writer, func() error, string, error) {
	logPath, err := runtimeLogPath()
	if err != nil {
		return base, func() error { return nil }, "", err
	}

	if err := os.MkdirAll(filepath.Dir(logPath), 0o700); err != nil {
		return base, func() error { return nil }, logPath, fmt.Errorf("create runtime log directory: %w", err)
	}

	f, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return base, func() error { return nil }, logPath, fmt.Errorf("open runtime log file: %w", err)
	}

	if base == nil {
		base = io.Discard
	}

	return io.MultiWriter(base, f), f.Close, logPath, nil
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
