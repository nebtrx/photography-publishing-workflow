package watcher

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"photography-publishing-workflow/internal/testutil"
)

func TestWatcher_DetectsNewDir(t *testing.T) {
	watchDir := t.TempDir()

	var mu sync.Mutex
	var detected []string

	handler := func(ctx context.Context, dir string) {
		mu.Lock()
		detected = append(detected, dir)
		mu.Unlock()
	}

	w, err := New(watchDir, handler, Options{Debounce: 100 * time.Millisecond})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start watching in background
	errCh := make(chan error, 1)
	go func() {
		errCh <- w.Watch(ctx)
	}()

	// Give watcher time to start
	time.Sleep(200 * time.Millisecond)

	// Create a new directory with images
	postDir := filepath.Join(watchDir, "new-post")
	os.Mkdir(postDir, 0o755)
	testutil.CreateJPEG(filepath.Join(postDir, "photo_1.jpg"), 1080, 1350)

	// Wait for debounce + processing
	time.Sleep(500 * time.Millisecond)

	cancel()
	<-errCh

	mu.Lock()
	defer mu.Unlock()
	if len(detected) != 1 {
		t.Errorf("detected = %d, want 1", len(detected))
	}
	if len(detected) > 0 && detected[0] != postDir {
		t.Errorf("detected dir = %q, want %q", detected[0], postDir)
	}
}

func TestWatcher_IgnoresNonImageDir(t *testing.T) {
	watchDir := t.TempDir()

	var mu sync.Mutex
	var detected []string

	handler := func(ctx context.Context, dir string) {
		mu.Lock()
		detected = append(detected, dir)
		mu.Unlock()
	}

	w, err := New(watchDir, handler, Options{Debounce: 100 * time.Millisecond})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go w.Watch(ctx)
	time.Sleep(200 * time.Millisecond)

	// Create a directory with no images
	noImgDir := filepath.Join(watchDir, "random-dir")
	os.Mkdir(noImgDir, 0o755)
	os.WriteFile(filepath.Join(noImgDir, "notes.txt"), []byte("not an image"), 0o644)

	time.Sleep(500 * time.Millisecond)
	cancel()

	mu.Lock()
	defer mu.Unlock()
	if len(detected) != 0 {
		t.Errorf("should ignore non-image dirs, detected %d", len(detected))
	}
}

func TestWatcher_NoDuplicates(t *testing.T) {
	watchDir := t.TempDir()

	var mu sync.Mutex
	var count int

	handler := func(ctx context.Context, dir string) {
		mu.Lock()
		count++
		mu.Unlock()
	}

	w, err := New(watchDir, handler, Options{Debounce: 100 * time.Millisecond})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go w.Watch(ctx)
	time.Sleep(200 * time.Millisecond)

	// Create directory
	postDir := filepath.Join(watchDir, "post")
	os.Mkdir(postDir, 0o755)
	testutil.CreateJPEG(filepath.Join(postDir, "photo_1.jpg"), 1080, 1350)

	time.Sleep(500 * time.Millisecond)

	// Add another file to the same dir (should not re-trigger)
	testutil.CreateJPEG(filepath.Join(postDir, "photo_2.jpg"), 1080, 1350)
	time.Sleep(500 * time.Millisecond)

	cancel()

	mu.Lock()
	defer mu.Unlock()
	if count != 1 {
		t.Errorf("handler called %d times, want 1", count)
	}
}

func TestWatcher_InvalidDir(t *testing.T) {
	_, err := New("/nonexistent/path", nil, Options{})
	if err == nil {
		t.Error("expected error for nonexistent directory")
	}
}
