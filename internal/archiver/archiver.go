// Package archiver moves published posts to a date-organized archive
// directory and appends entries to a JSONL publish log.
package archiver

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"time"

	"photography-publishing-workflow/internal/manifest"
)

// Archiver handles post-publish archival.
type Archiver struct {
	archiveDir string
	logFile    string
	logger     *log.Logger
	dryRun     bool
}

// Options configures the archiver.
type Options struct {
	ArchiveDir string // root archive directory
	LogFile    string // path to JSONL publish log
	DryRun     bool
	LogOutput  io.Writer
}

// LogEntry represents one line in the publish log (JSONL).
type LogEntry struct {
	ID              string `json:"id"`
	PublishedAt     string `json:"published_at"`
	InstagramPostID string `json:"instagram_post_id"`
	Permalink       string `json:"permalink"`
	Caption         string `json:"caption"`
	Location        string `json:"location,omitempty"`
	ImageCount      int    `json:"image_count"`
	ArchivePath     string `json:"archive_path"`
	StoryPublished  bool   `json:"story_published"`
}

// New creates a new Archiver.
func New(opts Options) (*Archiver, error) {
	if opts.ArchiveDir == "" {
		return nil, fmt.Errorf("archive directory is required")
	}
	if opts.LogFile == "" {
		return nil, fmt.Errorf("log file path is required")
	}
	logOutput := opts.LogOutput
	if logOutput == nil {
		logOutput = os.Stderr
	}

	return &Archiver{
		archiveDir: opts.ArchiveDir,
		logFile:    opts.LogFile,
		logger:     log.New(logOutput, "[archive] ", log.LstdFlags),
		dryRun:     opts.DryRun,
	}, nil
}

// Archive moves a published post to the archive and writes a log entry.
func (a *Archiver) Archive(m *manifest.Manifest, manifestPath string) error {
	if m.State != manifest.StatePublished {
		return fmt.Errorf("manifest state is %q, expected %q", m.State, manifest.StatePublished)
	}
	fail := func(err error) error {
		a.recordArchiveFailure(m, manifestPath, err)
		return err
	}

	// Determine archive subdirectory: <archive-dir>/<YYYY-MM>/<post-id>/
	publishDate := m.Publishing.PublishedAt
	if publishDate.IsZero() {
		publishDate = time.Now().UTC()
	}
	monthDir := publishDate.Format("2006-01")
	archiveSubdir := filepath.Join(a.archiveDir, monthDir, m.ID)

	if a.dryRun {
		return a.dryRunArchive(m, manifestPath, archiveSubdir)
	}

	// Create archive subdirectory
	if err := os.MkdirAll(archiveSubdir, 0o755); err != nil {
		return fail(fmt.Errorf("create archive dir %s: %w", archiveSubdir, err))
	}

	// Move each image to the archive
	for _, img := range m.Images {
		dst := filepath.Join(archiveSubdir, img.Filename)
		if err := moveFile(img.Path, dst); err != nil {
			return fail(fmt.Errorf("move %s → %s: %w", img.Path, dst, err))
		}
		a.logger.Printf("Moved %s → %s", img.Filename, dst)
	}

	// Move any other files in the source directory (e.g. .DS_Store)
	sourceDir := m.SourceDir
	a.moveRemainingFiles(sourceDir, archiveSubdir, manifestPath)

	// Update manifest archival section
	m.Archival = &manifest.Archival{
		ArchivedAt: time.Now().UTC(),
		ArchiveDir: archiveSubdir,
	}

	// Transition state
	if err := m.Transition(manifest.StateArchived); err != nil {
		return fail(fmt.Errorf("transition to archived: %w", err))
	}

	// Write manifest to archive
	archiveManifestPath := filepath.Join(archiveSubdir, "manifest.json")
	if err := m.Write(archiveManifestPath); err != nil {
		return fail(fmt.Errorf("write archived manifest: %w", err))
	}

	// Append log entry
	if err := a.writeLogEntry(m, archiveSubdir); err != nil {
		a.logger.Printf("Warning: failed to write log entry: %v", err)
		// Non-fatal — archival is still complete
	} else {
		m.Archival.LogEntryWritten = true
		// Update the archive manifest with log confirmation
		if err := m.Write(archiveManifestPath); err != nil {
			a.logger.Printf("Warning: failed to update manifest with log confirmation: %v", err)
		}
	}

	// Remove original post directory
	if err := a.removeSourceDir(sourceDir, manifestPath); err != nil {
		a.logger.Printf("Warning: failed to remove source dir: %v", err)
	}

	a.logger.Printf("Archived %s → %s (%d images)", m.ID, archiveSubdir, len(m.Images))
	return nil
}

func (a *Archiver) recordArchiveFailure(m *manifest.Manifest, manifestPath string, archiveErr error) {
	if m == nil || archiveErr == nil {
		return
	}
	m.RecordFailure(manifest.FailureStageArchive, archiveErr.Error(), manifest.StatePublished)
	if err := m.Write(manifestPath); err != nil {
		a.logger.Printf("Warning: failed to write manifest after archive error: %v", err)
	}
}

// dryRunArchive logs what would happen without doing it.
func (a *Archiver) dryRunArchive(m *manifest.Manifest, manifestPath, archiveSubdir string) error {
	a.logger.Printf("[DRY RUN] Would create archive dir: %s", archiveSubdir)
	for _, img := range m.Images {
		dst := filepath.Join(archiveSubdir, img.Filename)
		a.logger.Printf("[DRY RUN] Would move %s → %s", img.Path, dst)
	}
	a.logger.Printf("[DRY RUN] Would write manifest to %s/manifest.json", archiveSubdir)
	a.logger.Printf("[DRY RUN] Would append log entry to %s", a.logFile)
	a.logger.Printf("[DRY RUN] Would remove source dir: %s", m.SourceDir)
	return nil
}

// writeLogEntry appends a JSONL entry to the publish log.
func (a *Archiver) writeLogEntry(m *manifest.Manifest, archivePath string) error {
	entry := LogEntry{
		ID:              m.ID,
		PublishedAt:     m.Publishing.PublishedAt.Format(time.RFC3339),
		InstagramPostID: m.Publishing.InstagramPostID,
		Permalink:       m.Publishing.Permalink,
		ImageCount:      len(m.Images),
		ArchivePath:     archivePath,
		StoryPublished:  m.Publishing.InstagramStoryID != "",
	}

	// Caption
	if m.Review != nil && m.Review.FinalCaption != "" {
		entry.Caption = m.Review.FinalCaption
	} else if m.Enrichment != nil && m.Enrichment.Caption != nil {
		entry.Caption = m.Enrichment.Caption.Text
	}

	// Location
	if m.Enrichment != nil && m.Enrichment.Location != nil {
		entry.Location = m.Enrichment.Location.Name
	}

	line, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("marshal log entry: %w", err)
	}
	line = append(line, '\n')

	// Ensure log directory exists
	logDir := filepath.Dir(a.logFile)
	if err := os.MkdirAll(logDir, 0o755); err != nil {
		return fmt.Errorf("create log dir: %w", err)
	}

	f, err := os.OpenFile(a.logFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("open log file: %w", err)
	}
	defer f.Close()

	if _, err := f.Write(line); err != nil {
		return fmt.Errorf("write log entry: %w", err)
	}

	return nil
}

// moveRemainingFiles moves non-manifest files from source to archive.
func (a *Archiver) moveRemainingFiles(sourceDir, archiveDir, manifestPath string) {
	entries, err := os.ReadDir(sourceDir)
	if err != nil {
		return
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		srcPath := filepath.Join(sourceDir, e.Name())
		// Skip the manifest itself (we write a fresh copy)
		if srcPath == manifestPath {
			continue
		}
		// Skip already-moved image files
		dstPath := filepath.Join(archiveDir, e.Name())
		if _, err := os.Stat(dstPath); err == nil {
			continue // already exists in archive
		}
		if err := moveFile(srcPath, dstPath); err != nil {
			a.logger.Printf("Warning: failed to move %s: %v", e.Name(), err)
		}
	}
}

// removeSourceDir removes the original post directory after archival.
func (a *Archiver) removeSourceDir(sourceDir, manifestPath string) error {
	// Remove manifest file first
	os.Remove(manifestPath)
	// Remove temp files
	os.Remove(manifestPath + ".tmp")

	// Try removing the directory (only works if empty)
	if err := os.Remove(sourceDir); err != nil {
		// If not empty, force remove
		return os.RemoveAll(sourceDir)
	}
	return nil
}

// moveFile moves a file from src to dst, falling back to copy+delete
// if src and dst are on different filesystems.
func moveFile(src, dst string) error {
	// Try rename first (atomic, same filesystem)
	if err := os.Rename(src, dst); err == nil {
		return nil
	}

	// Fallback: copy + delete (cross-filesystem)
	return copyAndDelete(src, dst)
}

func copyAndDelete(src, dst string) error {
	srcFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer srcFile.Close()

	srcInfo, err := srcFile.Stat()
	if err != nil {
		return err
	}

	dstFile, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, srcInfo.Mode())
	if err != nil {
		return err
	}
	defer dstFile.Close()

	if _, err := io.Copy(dstFile, srcFile); err != nil {
		os.Remove(dst) // cleanup partial copy
		return err
	}

	// Close both before deleting source
	srcFile.Close()
	dstFile.Close()

	// Verify size
	dstInfo, err := os.Stat(dst)
	if err != nil {
		return fmt.Errorf("verify copy: %w", err)
	}
	if srcInfo.Size() != dstInfo.Size() {
		os.Remove(dst)
		return fmt.Errorf("size mismatch: src=%d dst=%d", srcInfo.Size(), dstInfo.Size())
	}

	return os.Remove(src)
}
