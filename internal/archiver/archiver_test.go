package archiver

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"photography-publishing-workflow/internal/manifest"
	"photography-publishing-workflow/internal/scanner"
	"photography-publishing-workflow/internal/testutil"
	"photography-publishing-workflow/internal/validator"
)

func setupPublishedManifest(t *testing.T) (*manifest.Manifest, string) {
	t.Helper()
	dir := t.TempDir()
	if err := testutil.SetupSamplePost(dir); err != nil {
		t.Fatalf("setup: %v", err)
	}
	m, err := scanner.Scan(dir)
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	if err := validator.Apply(m); err != nil {
		t.Fatalf("validate: %v", err)
	}

	// Simulate full pipeline through to published
	m.State = manifest.StatePendingReview
	m.Enrichment = &manifest.Enrichment{
		Caption: &manifest.Caption{
			Text:       "Test caption #architecture",
			Confidence: "high",
		},
		Location: &manifest.Location{
			Name:   "Amsterdam",
			Source: "ai_vision",
		},
	}
	m.Review = &manifest.Review{
		Decision:     "approved",
		FinalCaption: "Final caption #architecture",
		PublishMode:  "immediate",
		ReviewedAt:   time.Now().UTC(),
	}
	m.State = manifest.StateApproved
	m.State = manifest.StatePublishing
	m.Publishing = &manifest.Publishing{
		InstagramPostID:  "ig_post_123",
		InstagramStoryID: "ig_story_456",
		Permalink:        "https://www.instagram.com/p/TEST/",
		PublishedAt:      time.Date(2026, 2, 10, 18, 0, 0, 0, time.UTC),
		R2Cleaned:        true,
	}
	m.State = manifest.StatePublished

	mPath := manifest.ManifestPath(dir)
	if err := m.Write(mPath); err != nil {
		t.Fatalf("write: %v", err)
	}
	return m, mPath
}

func TestArchive_FullFlow(t *testing.T) {
	m, mPath := setupPublishedManifest(t)
	archiveDir := t.TempDir()
	logFile := filepath.Join(t.TempDir(), "publish.log")

	a, err := New(Options{
		ArchiveDir: archiveDir,
		LogFile:    logFile,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	sourceDir := m.SourceDir
	imgCount := len(m.Images)

	if err := a.Archive(m, mPath); err != nil {
		t.Fatalf("Archive: %v", err)
	}

	// Verify state
	if m.State != manifest.StateArchived {
		t.Errorf("state = %q, want %q", m.State, manifest.StateArchived)
	}

	// Verify archive directory
	expectedDir := filepath.Join(archiveDir, "2026-02", m.ID)
	if m.Archival.ArchiveDir != expectedDir {
		t.Errorf("archive_dir = %q, want %q", m.Archival.ArchiveDir, expectedDir)
	}

	// Verify images moved to archive
	entries, err := os.ReadDir(expectedDir)
	if err != nil {
		t.Fatalf("read archive dir: %v", err)
	}
	// Should have images + manifest.json
	fileCount := 0
	hasManifest := false
	for _, e := range entries {
		if e.Name() == "manifest.json" {
			hasManifest = true
		}
		fileCount++
	}
	if !hasManifest {
		t.Error("manifest.json not found in archive")
	}
	// images + manifest
	if fileCount < imgCount+1 {
		t.Errorf("archive file count = %d, want at least %d", fileCount, imgCount+1)
	}

	// Verify source directory removed
	if _, err := os.Stat(sourceDir); !os.IsNotExist(err) {
		t.Errorf("source dir should be removed, but still exists")
	}

	// Verify log entry
	logData, err := os.ReadFile(logFile)
	if err != nil {
		t.Fatalf("read log: %v", err)
	}
	lines := strings.TrimSpace(string(logData))
	if lines == "" {
		t.Fatal("log file is empty")
	}

	var entry LogEntry
	if err := json.Unmarshal([]byte(lines), &entry); err != nil {
		t.Fatalf("parse log entry: %v", err)
	}
	if entry.ID != m.ID {
		t.Errorf("log id = %q, want %q", entry.ID, m.ID)
	}
	if entry.InstagramPostID != "ig_post_123" {
		t.Errorf("log post_id = %q", entry.InstagramPostID)
	}
	if entry.Permalink != "https://www.instagram.com/p/TEST/" {
		t.Errorf("log permalink = %q", entry.Permalink)
	}
	if entry.ImageCount != imgCount {
		t.Errorf("log image_count = %d, want %d", entry.ImageCount, imgCount)
	}
	if !entry.StoryPublished {
		t.Error("log story_published should be true")
	}
	if entry.Location != "Amsterdam" {
		t.Errorf("log location = %q, want %q", entry.Location, "Amsterdam")
	}

	// Verify log_entry_written in manifest
	if !m.Archival.LogEntryWritten {
		t.Error("log_entry_written should be true")
	}

	// Verify archived manifest is readable
	archivedManifest, err := manifest.Read(filepath.Join(expectedDir, "manifest.json"))
	if err != nil {
		t.Fatalf("read archived manifest: %v", err)
	}
	if archivedManifest.State != manifest.StateArchived {
		t.Errorf("archived manifest state = %q, want %q", archivedManifest.State, manifest.StateArchived)
	}
}

func TestArchive_DryRun(t *testing.T) {
	m, mPath := setupPublishedManifest(t)
	archiveDir := t.TempDir()
	logFile := filepath.Join(t.TempDir(), "publish.log")

	a, err := New(Options{
		ArchiveDir: archiveDir,
		LogFile:    logFile,
		DryRun:     true,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	sourceDir := m.SourceDir

	if err := a.Archive(m, mPath); err != nil {
		t.Fatalf("Archive dry run: %v", err)
	}

	// Source should still exist
	if _, err := os.Stat(sourceDir); os.IsNotExist(err) {
		t.Error("source dir should still exist in dry run")
	}

	// Archive dir should be empty (nothing created)
	entries, _ := os.ReadDir(archiveDir)
	if len(entries) != 0 {
		t.Errorf("archive dir should be empty in dry run, got %d entries", len(entries))
	}

	// Log file should not exist
	if _, err := os.Stat(logFile); !os.IsNotExist(err) {
		t.Error("log file should not exist in dry run")
	}
}

func TestArchive_WrongState(t *testing.T) {
	m := manifest.New("test", "/tmp/test")
	m.State = manifest.StateApproved

	a, _ := New(Options{ArchiveDir: "/tmp", LogFile: "/tmp/log"})
	if err := a.Archive(m, "/tmp/test/manifest.json"); err == nil {
		t.Error("expected error for wrong state")
	}
}

func TestArchive_LogCreatesFile(t *testing.T) {
	m, mPath := setupPublishedManifest(t)
	logDir := t.TempDir()
	logFile := filepath.Join(logDir, "subdir", "publish.log")

	a, _ := New(Options{
		ArchiveDir: t.TempDir(),
		LogFile:    logFile,
	})

	if err := a.Archive(m, mPath); err != nil {
		t.Fatalf("Archive: %v", err)
	}

	if _, err := os.Stat(logFile); os.IsNotExist(err) {
		t.Error("log file should be created, including parent dirs")
	}
}

func TestArchive_MultipleEntries(t *testing.T) {
	logFile := filepath.Join(t.TempDir(), "publish.log")

	// Archive two posts
	for i := 0; i < 2; i++ {
		m, mPath := setupPublishedManifest(t)
		a, _ := New(Options{
			ArchiveDir: t.TempDir(),
			LogFile:    logFile,
		})
		if err := a.Archive(m, mPath); err != nil {
			t.Fatalf("Archive %d: %v", i, err)
		}
	}

	// Log should have 2 lines
	data, _ := os.ReadFile(logFile)
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 2 {
		t.Errorf("log lines = %d, want 2", len(lines))
	}
}
