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

	if got, want := len(m.queuePosts), 1; got != want {
		t.Fatalf("queue count = %d, want %d", got, want)
	}
	if got := m.queuePosts[0].Manifest.ID; got != "approved" {
		t.Fatalf("queue[0] = %q, want approved", got)
	}

	if got, want := len(m.deadLetterPosts), 2; got != want {
		t.Fatalf("dead-letter count = %d, want %d", got, want)
	}
	if got := m.deadLetterPosts[0].Manifest.ID; got != "err-approved" {
		t.Fatalf("deadLetter[0] = %q, want err-approved", got)
	}
	if got := m.deadLetterPosts[1].Manifest.ID; got != "err-other" {
		t.Fatalf("deadLetter[1] = %q, want err-other", got)
	}
}

func TestPreparePostForRetry_RecoversErrorState(t *testing.T) {
	root := t.TempDir()
	manifestPath := writeManifest(t, root, "retry-me", manifest.StateError, "approved")
	man, err := manifest.Read(manifestPath)
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}
	man.RecordFailure(manifest.FailureStagePublish, "old error", manifest.StateApproved)
	man.Publishing = &manifest.Publishing{
		ContainerIDs:     &manifest.ContainerIDs{Single: "old_container"},
		R2Keys:           []string{"posts/retry-me/old.jpg"},
		R2URLs:           []string{"https://test.r2.dev/posts/retry-me/old.jpg"},
		InstagramPostID:  "old_media",
		InstagramStoryID: "old_story",
		Permalink:        "https://instagram.test/p/old",
	}
	if err := man.Write(manifestPath); err != nil {
		t.Fatalf("write manifest: %v", err)
	}

	model := AppModel{}
	post := &PostEntry{Manifest: man, Path: manifestPath}
	stage, err := model.preparePostForRetry(post)
	if err != nil {
		t.Fatalf("preparePostForRetry: %v", err)
	}
	if stage != manifest.FailureStagePublish {
		t.Fatalf("stage = %q, want %q", stage, manifest.FailureStagePublish)
	}

	reloaded, err := manifest.Read(manifestPath)
	if err != nil {
		t.Fatalf("read reloaded manifest: %v", err)
	}
	if reloaded.State != manifest.StateApproved {
		t.Fatalf("state = %q, want %q", reloaded.State, manifest.StateApproved)
	}
	if reloaded.Failure != nil {
		t.Fatalf("failure should be cleared, got %#v", reloaded.Failure)
	}
	if reloaded.Publishing != nil {
		if len(reloaded.Publishing.R2Keys) != 0 || len(reloaded.Publishing.R2URLs) != 0 {
			t.Fatalf("publishing R2 artifacts should be cleared on publish retry, got keys=%d urls=%d", len(reloaded.Publishing.R2Keys), len(reloaded.Publishing.R2URLs))
		}
		if reloaded.Publishing.ContainerIDs != nil {
			t.Fatalf("container ids should be cleared on publish retry, got %#v", reloaded.Publishing.ContainerIDs)
		}
		if reloaded.Publishing.InstagramPostID != "" || reloaded.Publishing.InstagramStoryID != "" || reloaded.Publishing.Permalink != "" {
			t.Fatalf("instagram publish outputs should be cleared on publish retry, got post=%q story=%q permalink=%q", reloaded.Publishing.InstagramPostID, reloaded.Publishing.InstagramStoryID, reloaded.Publishing.Permalink)
		}
	}
}

func TestPreparePostForRetry_PublishRetryRequiresApproval(t *testing.T) {
	root := t.TempDir()
	manifestPath := writeManifest(t, root, "retry-me", manifest.StateError, "")
	man, err := manifest.Read(manifestPath)
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}
	man.RecordFailure(manifest.FailureStagePublish, "publish failed", manifest.StateApproved)
	if err := man.Write(manifestPath); err != nil {
		t.Fatalf("write manifest: %v", err)
	}

	model := AppModel{}
	post := &PostEntry{Manifest: man, Path: manifestPath}
	if _, err := model.preparePostForRetry(post); err == nil {
		t.Fatal("expected retry setup error for missing approval")
	}
}

func TestPublishedCounter_UsesLeafEntriesOnly(t *testing.T) {
	model := AppModel{
		logGroups: []LogGroup{
			{Month: "2026-02", Entries: []LogDisplayEntry{{ID: "a"}, {ID: "b"}}},
			{Month: "2026-01", Entries: []LogDisplayEntry{{ID: "c"}}, Collapsed: true},
		},
		logCursor: 0, // header row
	}
	if got := model.publishedCounter(); got != "1 of 3" {
		t.Fatalf("counter at header = %q, want 1 of 3", got)
	}
	model.logCursor = 2 // second leaf in first group
	if got := model.publishedCounter(); got != "2 of 3" {
		t.Fatalf("counter at leaf = %q, want 2 of 3", got)
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
