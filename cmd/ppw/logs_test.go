package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"photography-publishing-workflow/internal/joblog"
	"photography-publishing-workflow/internal/obslog"
)

func TestCollectLogEntries_FilterByModuleOutcomeAndPost(t *testing.T) {
	base := t.TempDir()
	jobsDir := filepath.Join(base, "jobs")
	if err := os.MkdirAll(jobsDir, 0o700); err != nil {
		t.Fatalf("mkdir jobs dir: %v", err)
	}

	now := time.Now().UTC()
	writeEvents(t, filepath.Join(jobsDir, "pipeline-1.success.1739600000.jsonl"), []obslog.Event{
		{TS: now.Add(-3 * time.Minute), Type: "intent", Module: "pipeline", JobID: "pipeline-1", PostID: "post-a", Action: "scan", Intent: "scan dir"},
		{TS: now.Add(-2 * time.Minute), Type: "result", Module: "pipeline", JobID: "pipeline-1", PostID: "post-a", Action: "scan", Outcome: "success"},
	})
	writeEvents(t, filepath.Join(jobsDir, "publish-2.failed.1739600100.jsonl"), []obslog.Event{
		{TS: now.Add(-90 * time.Second), Type: "result", Module: "publisher", JobID: "publish-2", PostID: "post-a", Action: "publish_facebook", Outcome: "failure", Error: "bad token"},
		{TS: now.Add(-30 * time.Second), Type: "result", Module: "publisher", JobID: "publish-2", PostID: "post-b", Action: "publish_threads", Outcome: "success"},
	})

	entries, err := collectLogEntries(joblog.Config{LogDir: base}, logQuery{
		Module:  "publisher",
		Outcome: "failure",
		PostID:  "post-a",
		Limit:   50,
	})
	if err != nil {
		t.Fatalf("collectLogEntries: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("entries=%d, want 1", len(entries))
	}
	if entries[0].Event.Action != "publish_facebook" {
		t.Fatalf("action=%q, want publish_facebook", entries[0].Event.Action)
	}
}

func TestCollectLogEntries_DerivesJobIDFromFilename(t *testing.T) {
	base := t.TempDir()
	jobsDir := filepath.Join(base, "jobs")
	if err := os.MkdirAll(jobsDir, 0o700); err != nil {
		t.Fatalf("mkdir jobs dir: %v", err)
	}

	now := time.Now().UTC()
	writeEvents(t, filepath.Join(jobsDir, "pipeline-xyz.success.1739600000.jsonl"), []obslog.Event{{
		TS: now, Type: "intent", Module: "pipeline", Action: "scan", Intent: "scan",
	}})

	entries, err := collectLogEntries(joblog.Config{LogDir: base}, logQuery{JobID: "pipeline-xyz", Limit: 10})
	if err != nil {
		t.Fatalf("collectLogEntries: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("entries=%d, want 1", len(entries))
	}
	if entries[0].Event.JobID != "pipeline-xyz" {
		t.Fatalf("derived job id=%q", entries[0].Event.JobID)
	}
}

func TestCollectLogEntries_LimitKeepsMostRecent(t *testing.T) {
	base := t.TempDir()
	jobsDir := filepath.Join(base, "jobs")
	if err := os.MkdirAll(jobsDir, 0o700); err != nil {
		t.Fatalf("mkdir jobs dir: %v", err)
	}
	now := time.Now().UTC()
	writeEvents(t, filepath.Join(jobsDir, "job.success.1739600000.jsonl"), []obslog.Event{
		{TS: now.Add(-3 * time.Second), Type: "result", Module: "pipeline", Action: "a", Outcome: "success"},
		{TS: now.Add(-2 * time.Second), Type: "result", Module: "pipeline", Action: "b", Outcome: "success"},
		{TS: now.Add(-1 * time.Second), Type: "result", Module: "pipeline", Action: "c", Outcome: "success"},
	})

	entries, err := collectLogEntries(joblog.Config{LogDir: base}, logQuery{Limit: 2})
	if err != nil {
		t.Fatalf("collectLogEntries: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("entries=%d, want 2", len(entries))
	}
	if entries[0].Event.Action != "b" || entries[1].Event.Action != "c" {
		t.Fatalf("unexpected actions: %s, %s", entries[0].Event.Action, entries[1].Event.Action)
	}
}

func TestFormatLogEntry(t *testing.T) {
	success := true
	line := formatLogEntry(logEntry{Event: obslog.Event{
		TS:      time.Unix(1739600000, 0).UTC(),
		Type:    "result",
		Module:  "publisher",
		JobID:   "publish-1",
		PostID:  "post-a",
		Action:  "publish_instagram",
		Outcome: "success",
		Success: &success,
		Details: map[string]any{"post_id": "123"},
	}, File: "publish-1.success.1739600000.jsonl", Line: 2})

	if !strings.Contains(line, "module=publisher") {
		t.Fatalf("formatted line missing module: %q", line)
	}
	if !strings.Contains(line, "outcome=success") {
		t.Fatalf("formatted line missing outcome: %q", line)
	}
	if !strings.Contains(line, "src=publish-1.success.1739600000.jsonl:2") {
		t.Fatalf("formatted line missing source: %q", line)
	}
}

func TestCollectLogEntries_ParsesPrefixedJSONLines(t *testing.T) {
	base := t.TempDir()
	jobsDir := filepath.Join(base, "jobs")
	if err := os.MkdirAll(jobsDir, 0o700); err != nil {
		t.Fatalf("mkdir jobs dir: %v", err)
	}

	path := filepath.Join(jobsDir, "publish-1.failed.1739600100.jsonl")
	lines := []string{
		`[publish] 2026/02/16 01:17:19 {"ts":"2026-02-16T00:17:19Z","type":"result","module":"publisher","post_id":"rotterdam-surprise-snow","action":"publish_instagram","outcome":"failure","error":"boom"}`,
		`[publish] Uploaded file -> https://example.com/file.jpg`,
	}
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatalf("write prefixed log file: %v", err)
	}

	entries, err := collectLogEntries(joblog.Config{LogDir: base}, logQuery{
		PostID:  "rotterdam-surprise-snow",
		Outcome: "failure",
		Limit:   10,
	})
	if err != nil {
		t.Fatalf("collectLogEntries: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("entries=%d, want 1", len(entries))
	}
	if entries[0].Event.Module != "publisher" || entries[0].Event.Action != "publish_instagram" {
		t.Fatalf("unexpected event: %+v", entries[0].Event)
	}
}

func writeEvents(t *testing.T, path string, events []obslog.Event) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		t.Fatalf("open %s: %v", path, err)
	}
	defer f.Close()

	enc := json.NewEncoder(f)
	for _, event := range events {
		if err := enc.Encode(event); err != nil {
			t.Fatalf("encode event: %v", err)
		}
	}
}
