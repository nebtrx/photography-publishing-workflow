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
	"strings"
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
	dir        string
	handler    Handler
	debounce   time.Duration
	logger     *log.Logger
	seen       map[string]bool
	watchedDir map[string]bool
	mu         sync.Mutex
	fsWatcher  *fsnotify.Watcher
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
		dir:        dir,
		handler:    handler,
		debounce:   debounce,
		logger:     log.New(logOutput, "[watcher] ", log.LstdFlags),
		seen:       make(map[string]bool),
		watchedDir: make(map[string]bool),
		fsWatcher:  fsw,
	}, nil
}

// Watch starts watching the directory. Blocks until ctx is cancelled.
func (w *Watcher) Watch(ctx context.Context) error {
	if err := w.addWatch(w.dir); err != nil {
		return err
	}
	defer w.fsWatcher.Close()

	// Scan existing directories after root watch is active.
	w.scanExisting()

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

			// New post directory creation in watch root.
			if event.Has(fsnotify.Create) {
				if info, err := os.Stat(event.Name); err == nil && info.IsDir() && filepath.Dir(event.Name) == w.dir {
					if err := w.addWatch(event.Name); err != nil {
						w.logger.Printf("Warning: failed to watch %s: %v", event.Name, err)
					}
					w.scheduleProcess(ctx, timers, &timersMu, event.Name, false, "new directory created")
					continue
				}
			}

			// Existing errored posts auto-retry when image files change.
			postDir := w.postDirForEvent(event.Name)
			if postDir == "" {
				continue
			}
			if !w.isImageChangeEvent(event) {
				continue
			}
			errored, reason := w.isErroredPost(postDir)
			if !errored {
				continue
			}
			msg := fmt.Sprintf("image change detected (%s) after error: %s", event.Op.String(), reason)
			w.scheduleProcess(ctx, timers, &timersMu, postDir, true, msg)

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
		if err := w.addWatch(dir); err != nil {
			w.logger.Printf("Warning: failed to watch existing dir %s: %v", e.Name(), err)
		}
		mPath := manifest.ManifestPath(dir)

		if _, err := os.Stat(mPath); err == nil {
			// Has manifest — check state
			m, err := manifest.Read(mPath)
			if err == nil {
				w.mu.Lock()
				w.seen[dir] = true
				w.mu.Unlock()
				if m.State == manifest.StateError {
					w.logger.Printf("Existing: %s (state: %s, reason: %s)", e.Name(), m.State, manifestErrorReason(m))
				} else {
					w.logger.Printf("Existing: %s (state: %s)", e.Name(), m.State)
				}
			}
		}
		// No manifest = new directory, don't mark as seen so it gets picked up
	}
}

func (w *Watcher) scheduleProcess(
	ctx context.Context,
	timers map[string]*time.Timer,
	timersMu *sync.Mutex,
	dir string,
	force bool,
	reason string,
) {
	timersMu.Lock()
	if t, exists := timers[dir]; exists {
		t.Stop()
	}
	timers[dir] = time.AfterFunc(w.debounce, func() {
		w.handleProcess(ctx, dir, force, reason)
		timersMu.Lock()
		delete(timers, dir)
		timersMu.Unlock()
	})
	timersMu.Unlock()
}

// handleProcess processes a directory, optionally forcing retries for errored posts.
func (w *Watcher) handleProcess(ctx context.Context, dir string, force bool, reason string) {
	w.mu.Lock()
	if w.seen[dir] && !force {
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

	if force {
		w.logger.Printf("Retrying errored post: %s (%s)", filepath.Base(dir), reason)
	} else {
		w.logger.Printf("New post detected: %s", filepath.Base(dir))
	}
	w.handler(ctx, dir)
}

func (w *Watcher) addWatch(dir string) error {
	w.mu.Lock()
	if w.watchedDir[dir] {
		w.mu.Unlock()
		return nil
	}
	w.watchedDir[dir] = true
	w.mu.Unlock()

	if err := w.fsWatcher.Add(dir); err != nil {
		w.mu.Lock()
		delete(w.watchedDir, dir)
		w.mu.Unlock()
		return fmt.Errorf("watch %s: %w", dir, err)
	}
	return nil
}

// postDirForEvent resolves the top-level post directory for a fsnotify event path.
func (w *Watcher) postDirForEvent(path string) string {
	rel, err := filepath.Rel(w.dir, path)
	if err != nil || rel == "." || strings.HasPrefix(rel, "..") {
		return ""
	}
	parts := strings.Split(rel, string(os.PathSeparator))
	if len(parts) == 0 || parts[0] == "" {
		return ""
	}
	return filepath.Join(w.dir, parts[0])
}

func (w *Watcher) isImageChangeEvent(event fsnotify.Event) bool {
	if !(event.Has(fsnotify.Create) || event.Has(fsnotify.Write) || event.Has(fsnotify.Rename) || event.Has(fsnotify.Remove)) {
		return false
	}
	ext := strings.ToLower(filepath.Ext(event.Name))
	return ext == ".jpg" || ext == ".jpeg"
}

func (w *Watcher) isErroredPost(dir string) (bool, string) {
	mPath := manifest.ManifestPath(dir)
	m, err := manifest.Read(mPath)
	if err != nil {
		return false, ""
	}
	if m.State != manifest.StateError {
		return false, ""
	}
	return true, manifestErrorReason(m)
}

func manifestErrorReason(m *manifest.Manifest) string {
	if len(m.Errors) > 0 {
		return strings.TrimSpace(m.Errors[len(m.Errors)-1])
	}
	if m.Validation != nil {
		for _, issue := range m.Validation.Issues {
			if strings.EqualFold(issue.Severity, "error") && strings.TrimSpace(issue.Message) != "" {
				return strings.TrimSpace(issue.Message)
			}
		}
	}
	return "unknown manifest error"
}

// Close stops the watcher.
func (w *Watcher) Close() error {
	return w.fsWatcher.Close()
}
