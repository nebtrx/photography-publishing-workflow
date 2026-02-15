// Package watcher monitors a directory for new post subdirectories
// and triggers the pipeline on each one.
package watcher

import (
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"

	"photography-publishing-workflow/internal/manifest"
)

// Handler is called when a new post directory is detected.
// The handler receives the directory path.
type Handler func(ctx context.Context, dir string)

// Watcher monitors a directory for new subdirectories.
type Watcher struct {
	dir       string
	handler   Handler
	debounce  time.Duration
	logger    *log.Logger
	seen      map[string]bool
	mu        sync.Mutex
	fsWatcher *fsnotify.Watcher
}

// Options configures the watcher.
type Options struct {
	Debounce  time.Duration // wait before triggering handler (default 2s)
	LogOutput io.Writer
}

// New creates a Watcher.
func New(dir string, handler Handler, opts Options) (*Watcher, error) {
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		return nil, fmt.Errorf("watch directory does not exist: %s", dir)
	}

	debounce := opts.Debounce
	if debounce == 0 {
		debounce = 2 * time.Second
	}

	fsw, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, fmt.Errorf("create fsnotify watcher: %w", err)
	}

	logOutput := opts.LogOutput
	if logOutput == nil {
		logOutput = os.Stderr
	}

	return &Watcher{
		dir:       dir,
		handler:   handler,
		debounce:  debounce,
		logger:    log.New(logOutput, "[watcher] ", log.LstdFlags),
		seen:      make(map[string]bool),
		fsWatcher: fsw,
	}, nil
}

// Watch starts watching the directory. Blocks until ctx is cancelled.
func (w *Watcher) Watch(ctx context.Context) error {
	// Scan existing directories first
	w.scanExisting()

	if err := w.fsWatcher.Add(w.dir); err != nil {
		return fmt.Errorf("watch %s: %w", w.dir, err)
	}
	defer w.fsWatcher.Close()

	w.logger.Printf("Watching %s for new post directories...", w.dir)

	// Debounce timers per directory
	timers := make(map[string]*time.Timer)
	var timersMu sync.Mutex

	for {
		select {
		case <-ctx.Done():
			w.logger.Printf("Watcher stopped")
			return nil

		case event, ok := <-w.fsWatcher.Events:
			if !ok {
				return nil
			}

			// Only care about creates in the watch directory
			if !event.Has(fsnotify.Create) {
				continue
			}

			// Check if it's a directory
			info, err := os.Stat(event.Name)
			if err != nil || !info.IsDir() {
				continue
			}

			dir := event.Name
			w.mu.Lock()
			alreadySeen := w.seen[dir]
			w.mu.Unlock()
			if alreadySeen {
				continue
			}

			// Debounce: wait for files to settle (Lightroom may still be writing)
			timersMu.Lock()
			if t, exists := timers[dir]; exists {
				t.Stop()
			}
			timers[dir] = time.AfterFunc(w.debounce, func() {
				w.handleNew(ctx, dir)
				timersMu.Lock()
				delete(timers, dir)
				timersMu.Unlock()
			})
			timersMu.Unlock()

		case err, ok := <-w.fsWatcher.Errors:
			if !ok {
				return nil
			}
			w.logger.Printf("Watch error: %v", err)
		}
	}
}

// scanExisting scans for existing subdirectories and marks them as seen.
// Directories with manifests already in progress are skipped.
// Directories without manifests are candidates for processing.
func (w *Watcher) scanExisting() {
	entries, err := os.ReadDir(w.dir)
	if err != nil {
		w.logger.Printf("Warning: failed to scan existing: %v", err)
		return
	}

	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		dir := filepath.Join(w.dir, e.Name())
		mPath := manifest.ManifestPath(dir)

		if _, err := os.Stat(mPath); err == nil {
			// Has manifest — check state
			m, err := manifest.Read(mPath)
			if err == nil {
				w.mu.Lock()
				w.seen[dir] = true
				w.mu.Unlock()
				w.logger.Printf("Existing: %s (state: %s)", e.Name(), m.State)
			}
		}
		// No manifest = new directory, don't mark as seen so it gets picked up
	}
}

// handleNew processes a newly detected directory.
func (w *Watcher) handleNew(ctx context.Context, dir string) {
	w.mu.Lock()
	if w.seen[dir] {
		w.mu.Unlock()
		return
	}
	w.seen[dir] = true
	w.mu.Unlock()

	// Check it has JPEG files (not just a random dir)
	hasImages := false
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		name := e.Name()
		ext := filepath.Ext(name)
		if ext == ".jpg" || ext == ".jpeg" || ext == ".JPG" || ext == ".JPEG" {
			hasImages = true
			break
		}
	}

	if !hasImages {
		w.logger.Printf("Ignoring %s (no JPEG files)", filepath.Base(dir))
		return
	}

	w.logger.Printf("New post detected: %s", filepath.Base(dir))
	w.handler(ctx, dir)
}

// Close stops the watcher.
func (w *Watcher) Close() error {
	return w.fsWatcher.Close()
}
