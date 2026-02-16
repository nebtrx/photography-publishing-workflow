package manifest

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestNew(t *testing.T) {
	m := New("test-post", "/tmp/test-post")

	if m.Version != Version {
		t.Errorf("version = %d, want %d", m.Version, Version)
	}
	if m.ID != "test-post" {
		t.Errorf("id = %q, want %q", m.ID, "test-post")
	}
	if m.State != StateScanned {
		t.Errorf("state = %q, want %q", m.State, StateScanned)
	}
	if len(m.Images) != 0 {
		t.Errorf("images = %d, want 0", len(m.Images))
	}
}

func TestTransition_Valid(t *testing.T) {
	tests := []struct {
		from State
		to   State
	}{
		{StateScanned, StateValidated},
		{StateScanned, StateError},
		{StateValidated, StatePendingReview},
		{StatePendingReview, StateApproved},
		{StatePendingReview, StateRejected},
		{StatePendingReview, StateValidated}, // re-enrich
		{StateApproved, StatePublishing},
		{StatePublishing, StatePublished},
		{StatePublished, StateArchived},
		{StateError, StateScanned},
		{StateError, StateValidated},
		{StateError, StateApproved},
		{StateError, StatePublished},
	}

	for _, tt := range tests {
		t.Run(string(tt.from)+"→"+string(tt.to), func(t *testing.T) {
			m := New("test", "/tmp/test")
			m.State = tt.from
			if err := m.Transition(tt.to); err != nil {
				t.Errorf("Transition(%q → %q) unexpected error: %v", tt.from, tt.to, err)
			}
			if m.State != tt.to {
				t.Errorf("state = %q, want %q", m.State, tt.to)
			}
		})
	}
}

func TestTransition_Invalid(t *testing.T) {
	m := New("test", "/tmp/test")
	m.State = StateScanned

	if err := m.Transition(StatePublished); err == nil {
		t.Error("expected error for invalid transition scanned → published")
	}
	// State should not change on invalid transition
	if m.State != StateScanned {
		t.Errorf("state changed to %q after invalid transition", m.State)
	}
}

func TestWriteAndRead(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "manifest.json")

	m := New("write-test", dir)
	m.Images = []Image{
		{Filename: "test_1.jpg", Path: filepath.Join(dir, "test_1.jpg"), Order: 1, IsHero: true, Width: 800, Height: 1000, AspectRatio: "4:5"},
	}

	if err := m.Write(path); err != nil {
		t.Fatalf("Write: %v", err)
	}

	// Verify file exists
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("manifest file not found: %v", err)
	}

	// Read it back
	m2, err := Read(path)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}

	if m2.ID != "write-test" {
		t.Errorf("id = %q, want %q", m2.ID, "write-test")
	}
	if m2.State != StateScanned {
		t.Errorf("state = %q, want %q", m2.State, StateScanned)
	}
	if len(m2.Images) != 1 {
		t.Fatalf("images = %d, want 1", len(m2.Images))
	}
	if m2.Images[0].Filename != "test_1.jpg" {
		t.Errorf("image filename = %q, want %q", m2.Images[0].Filename, "test_1.jpg")
	}
}

func TestAtomicWrite_NoPartialFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "manifest.json")

	m := New("atomic-test", dir)
	if err := m.Write(path); err != nil {
		t.Fatalf("Write: %v", err)
	}

	// Verify no .tmp file remains
	tmpPath := path + ".tmp"
	if _, err := os.Stat(tmpPath); !os.IsNotExist(err) {
		t.Error("temp file should not exist after successful write")
	}
}

func TestManifestPath(t *testing.T) {
	got := ManifestPath("/foo/bar/my-post")
	want := filepath.Join("/foo/bar/my-post", "manifest.json")
	if got != want {
		t.Errorf("ManifestPath = %q, want %q", got, want)
	}
}

func TestRetryStateForFailureStage(t *testing.T) {
	tests := []struct {
		stage FailureStage
		want  State
	}{
		{FailureStageScan, StateScanned},
		{FailureStageValidate, StateScanned},
		{FailureStageEnrich, StateValidated},
		{FailureStagePublish, StateApproved},
		{FailureStageArchive, StatePublished},
		{FailureStageSyndicate, StatePublished},
		{FailureStage("unknown"), StateScanned},
	}

	for _, tt := range tests {
		if got := RetryStateForFailureStage(tt.stage); got != tt.want {
			t.Fatalf("RetryStateForFailureStage(%q) = %q, want %q", tt.stage, got, tt.want)
		}
	}
}

func TestRecordFailureAndPrepareRetry(t *testing.T) {
	m := New("retry-test", "/tmp/retry-test")
	m.State = StatePublishing
	m.RecordFailure(FailureStagePublish, "publish failed", StateApproved)

	if m.State != StateError {
		t.Fatalf("state = %q, want %q", m.State, StateError)
	}
	if m.Failure == nil {
		t.Fatal("failure metadata should be set")
	}
	if got := m.Failure.RetryFromState; got != StateApproved {
		t.Fatalf("retry_from_state = %q, want %q", got, StateApproved)
	}
	if len(m.Errors) == 0 {
		t.Fatal("errors should append failure message")
	}

	stage, err := m.PrepareRetry()
	if err != nil {
		t.Fatalf("PrepareRetry: %v", err)
	}
	if stage != FailureStagePublish {
		t.Fatalf("stage = %q, want %q", stage, FailureStagePublish)
	}
	if m.State != StateApproved {
		t.Fatalf("state = %q, want %q", m.State, StateApproved)
	}
	if m.Failure != nil {
		t.Fatal("failure metadata should be cleared after retry prep")
	}
}

func TestPrepareRetryLegacyInference(t *testing.T) {
	m := New("legacy", "/tmp/legacy")
	m.State = StateError
	m.Review = &Review{Decision: "approved"}

	stage, err := m.PrepareRetry()
	if err != nil {
		t.Fatalf("PrepareRetry: %v", err)
	}
	if stage != FailureStagePublish {
		t.Fatalf("stage = %q, want %q", stage, FailureStagePublish)
	}
	if m.State != StateApproved {
		t.Fatalf("state = %q, want %q", m.State, StateApproved)
	}
}

func TestEffectiveFailureStageArchiveInference(t *testing.T) {
	m := New("legacy-archive", "/tmp/legacy-archive")
	m.State = StateError
	m.Publishing = &Publishing{
		InstagramPostID: "123",
		PublishedAt:     time.Now().UTC(),
	}
	if got := m.EffectiveFailureStage(); got != FailureStageArchive {
		t.Fatalf("EffectiveFailureStage = %q, want %q", got, FailureStageArchive)
	}
}

func TestPrepareRetryRequiresErrorState(t *testing.T) {
	m := New("bad", "/tmp/bad")
	m.State = StateApproved
	if _, err := m.PrepareRetry(); err == nil {
		t.Fatal("expected error when preparing retry outside error state")
	}
}
