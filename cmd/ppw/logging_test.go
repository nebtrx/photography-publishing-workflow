package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRuntimeLogPath_Default(t *testing.T) {
	t.Setenv("PPW_LOG_FILE", "")
	path, err := runtimeLogPath()
	if err != nil {
		t.Fatalf("runtimeLogPath: %v", err)
	}
	if !strings.HasSuffix(path, string(filepath.Separator)+".ppw"+string(filepath.Separator)+"ppw.log") {
		t.Fatalf("runtimeLogPath = %q, want suffix ~/.ppw/ppw.log", path)
	}
}

func TestRuntimeLogPath_Override(t *testing.T) {
	t.Setenv("PPW_LOG_FILE", "~/custom.log")
	path, err := runtimeLogPath()
	if err != nil {
		t.Fatalf("runtimeLogPath: %v", err)
	}
	if !strings.HasSuffix(path, string(filepath.Separator)+"custom.log") {
		t.Fatalf("runtimeLogPath override = %q, want suffix custom.log", path)
	}
}

func TestCommandLogOutput_WritesBaseAndFile(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "ppw.log")
	t.Setenv("PPW_LOG_FILE", logPath)

	var base bytes.Buffer
	w, closeFn, resolved, err := commandLogOutput(&base)
	if err != nil {
		t.Fatalf("commandLogOutput: %v", err)
	}

	if resolved != logPath {
		t.Fatalf("resolved path = %q, want %q", resolved, logPath)
	}

	writeLogLine(w, "hello %s", "world")
	if err := closeFn(); err != nil {
		t.Fatalf("close log file: %v", err)
	}

	if !strings.Contains(base.String(), "hello world") {
		t.Fatalf("base output missing line, got %q", base.String())
	}

	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read log file: %v", err)
	}
	if !strings.Contains(string(data), "hello world") {
		t.Fatalf("file output missing line, got %q", string(data))
	}
}
