package joblog

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSessionFinalizeAndSweep(t *testing.T) {
	base := t.TempDir()
	cfg := Config{
		LogDir:        base,
		SuccessTTL:    time.Hour,
		FailedTTL:     24 * time.Hour,
		SweepInterval: time.Hour,
	}

	s, err := NewSession(cfg, "pipeline", "job-1", nil)
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	if _, err := s.Writer().Write([]byte("line\n")); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := s.Close(true); err != nil {
		t.Fatalf("Close: %v", err)
	}

	finalPath := s.FinalPath()
	if !strings.Contains(filepath.Base(finalPath), ".success.") {
		t.Fatalf("final path should include success marker, got %q", finalPath)
	}
	if _, err := os.Stat(finalPath); err != nil {
		t.Fatalf("stat final log: %v", err)
	}

	removed, err := Sweep(cfg, time.Now().UTC().Add(2*time.Hour))
	if err != nil {
		t.Fatalf("Sweep: %v", err)
	}
	if removed != 1 {
		t.Fatalf("removed=%d, want 1", removed)
	}
	if _, err := os.Stat(finalPath); !os.IsNotExist(err) {
		t.Fatalf("expected log removal, stat err=%v", err)
	}
}

func TestSweepKeepsFailedLonger(t *testing.T) {
	base := t.TempDir()
	cfg := Config{LogDir: base, SuccessTTL: time.Hour, FailedTTL: 48 * time.Hour}

	s, err := NewSession(cfg, "publish", "job-2", nil)
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	if err := s.Close(false); err != nil {
		t.Fatalf("Close failed session: %v", err)
	}

	removed, err := Sweep(cfg, time.Now().UTC().Add(2*time.Hour))
	if err != nil {
		t.Fatalf("Sweep: %v", err)
	}
	if removed != 0 {
		t.Fatalf("removed=%d, want 0", removed)
	}
}

func TestParseCompletedName(t *testing.T) {
	status, completedAt, ok := parseCompletedName("pipeline-abc.success.1739600000.jsonl")
	if !ok {
		t.Fatal("expected parse success")
	}
	if status != "success" {
		t.Fatalf("status=%q", status)
	}
	if completedAt.Unix() != 1739600000 {
		t.Fatalf("unix=%d", completedAt.Unix())
	}
}
