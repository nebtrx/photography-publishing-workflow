package tui

import (
	"path/filepath"
	"testing"

	"photography-publishing-workflow/internal/config"
	"photography-publishing-workflow/internal/manifest"
)

func TestLoadPendingAndQueue_IncludesRetryableErrorPosts(t *testing.T) {
	root := t.TempDir()
	writeManifest(t, root, "pending", manifest.StatePendingReview, "")
	writeManifest(t, root, "approved", manifest.StateApproved, "approved")
	writeManifest(t, root, "err-approved", manifest.StateError, "approved")
	writeManifest(t, root, "err-other", manifest.StateError, "")

	m := AppModel{
		cfg: &config.Config{
			Watch: config.WatchConfig{Dir: root},
		},
	}
	m.loadPendingAndQueue()

	if got, want := len(m.pendingPosts), 1; got != want {
		t.Fatalf("pending count = %d, want %d", got, want)
	}
	if got := m.pendingPosts[0].Manifest.ID; got != "pending" {
		t.Fatalf("pending[0] = %q, want pending", got)
	}

	if got, want := len(m.queuePosts), 2; got != want {
		t.Fatalf("queue count = %d, want %d", got, want)
	}
	if got := m.queuePosts[0].Manifest.ID; got != "approved" {
		t.Fatalf("queue[0] = %q, want approved", got)
	}
	if got := m.queuePosts[1].Manifest.ID; got != "err-approved" {
		t.Fatalf("queue[1] = %q, want err-approved", got)
	}
}

func TestPreparePostForPublish_RecoversErrorState(t *testing.T) {
	root := t.TempDir()
	manifestPath := writeManifest(t, root, "retry-me", manifest.StateError, "approved")
	man, err := manifest.Read(manifestPath)
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}
	man.Errors = []string{"old error"}
	if err := man.Write(manifestPath); err != nil {
		t.Fatalf("write manifest: %v", err)
	}

	model := AppModel{}
	post := &PostEntry{Manifest: man, Path: manifestPath}
	if err := model.preparePostForPublish(post); err != nil {
		t.Fatalf("preparePostForPublish: %v", err)
	}

	reloaded, err := manifest.Read(manifestPath)
	if err != nil {
		t.Fatalf("read reloaded manifest: %v", err)
	}
	if reloaded.State != manifest.StateApproved {
		t.Fatalf("state = %q, want %q", reloaded.State, manifest.StateApproved)
	}
	if len(reloaded.Errors) != 0 {
		t.Fatalf("errors should be cleared, got %v", reloaded.Errors)
	}
}

func writeManifest(t *testing.T, root, id string, state manifest.State, reviewDecision string) string {
	t.Helper()
	postDir := filepath.Join(root, id)
	man := manifest.New(id, postDir)
	man.State = state
	if reviewDecision != "" {
		man.Review = &manifest.Review{Decision: reviewDecision}
	}
	path := manifest.ManifestPath(postDir)
	if err := man.Write(path); err != nil {
		t.Fatalf("write manifest %s: %v", id, err)
	}
	return path
}
