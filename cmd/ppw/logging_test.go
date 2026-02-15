package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"photography-publishing-workflow/internal/config"
)

func TestRuntimeLogPath_Default(t *testing.T) {
	path, err := runtimeLogPath(&config.Config{})
	if err != nil {
		t.Fatalf("runtimeLogPath: %v", err)
	}
	if !strings.HasSuffix(path, string(filepath.Separator)+".ppw"+string(filepath.Separator)+"ppw.log") {
		t.Fatalf("runtimeLogPath = %q, want suffix ~/.ppw/ppw.log", path)
	}
}

func TestOpenCommandLogSession_WritesAndFinalizesJobLog(t *testing.T) {
	base := t.TempDir()
	cfg := &config.Config{}
	cfg.Logging.RuntimeLogFile = filepath.Join(base, "runtime.log")
	cfg.Logging.JobLogDir = filepath.Join(base, "logs")

	session, err := openCommandLogSession("pipeline", cfg, nil)
	if err != nil {
		t.Fatalf("openCommandLogSession: %v", err)
	}

	writeLogLine(session.Writer, "hello %s", "world")
	if err := session.Close(true); err != nil {
		t.Fatalf("session close: %v", err)
	}

	runtimeData, err := os.ReadFile(filepath.Join(base, "runtime.log"))
	if err != nil {
		t.Fatalf("read runtime file: %v", err)
	}
	if !strings.Contains(string(runtimeData), "hello world") {
		t.Fatalf("runtime missing log line: %q", string(runtimeData))
	}

	if !strings.Contains(filepath.Base(session.JobPath), ".success.") {
		t.Fatalf("finalized job path missing status: %q", session.JobPath)
	}
	jobData, err := os.ReadFile(session.JobPath)
	if err != nil {
		t.Fatalf("read job file: %v", err)
	}
	if !strings.Contains(string(jobData), "hello world") {
		t.Fatalf("job log missing line: %q", string(jobData))
	}
}
