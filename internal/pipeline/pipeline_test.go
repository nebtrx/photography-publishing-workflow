package pipeline

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"

	"photography-publishing-workflow/internal/ai"
	"photography-publishing-workflow/internal/manifest"
	"photography-publishing-workflow/internal/testutil"
)

func TestPipeline_FullRun(t *testing.T) {
	dir := t.TempDir()
	if err := testutil.SetupSamplePost(dir); err != nil {
		t.Fatalf("setup: %v", err)
	}

	mockProvider := ai.NewMock(
		// Caption
		`The quiet geometry of #architecture reveals itself`,
		// Location
		`{"location_name": "Rotterdam", "city": "Rotterdam", "country": "Netherlands", "confidence": "high", "reasoning": "visible"}`,
	)

	p := New(mockProvider, Options{})
	result := p.Run(context.Background(), dir)

	if result.Error != nil {
		t.Fatalf("Pipeline error: %v", result.Error)
	}

	if result.PostID == "" {
		t.Error("PostID should be set")
	}
	if result.ManifestPath == "" {
		t.Error("ManifestPath should be set")
	}
	if result.FinalState != manifest.StatePendingReview {
		t.Errorf("FinalState = %q, want %q", result.FinalState, manifest.StatePendingReview)
	}

	// Read back the manifest
	m, err := manifest.Read(result.ManifestPath)
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}
	if m.State != manifest.StatePendingReview {
		t.Errorf("manifest state = %q, want %q", m.State, manifest.StatePendingReview)
	}
	if m.Enrichment == nil || m.Enrichment.Caption == nil {
		t.Error("enrichment caption should be set")
	}
	if m.Validation == nil || !m.Validation.Passed {
		t.Error("validation should pass")
	}
}

func TestPipeline_DryRun(t *testing.T) {
	dir := t.TempDir()
	if err := testutil.SetupSamplePost(dir); err != nil {
		t.Fatalf("setup: %v", err)
	}

	// Provider not needed for dry run
	p := New(nil, Options{DryRun: true})
	result := p.Run(context.Background(), dir)

	if result.Error != nil {
		t.Fatalf("Pipeline dry run error: %v", result.Error)
	}

	// No manifest should be written
	if _, err := manifest.Read(result.ManifestPath); err == nil {
		t.Error("manifest should not be written in dry run")
	}
}

func TestPipeline_EmptyDir(t *testing.T) {
	dir := t.TempDir()

	p := New(ai.NewMock("resp"), Options{})
	result := p.Run(context.Background(), dir)

	if result.Error == nil {
		t.Error("expected error for empty directory")
	}
}

func TestPipeline_SkipOptions(t *testing.T) {
	dir := t.TempDir()
	if err := testutil.SetupSamplePost(dir); err != nil {
		t.Fatalf("setup: %v", err)
	}

	mockProvider := ai.NewMock(
		// Only caption (location skipped)
		`Minimal #urban geometry`,
	)

	p := New(mockProvider, Options{
		SkipLocation: true,
	})
	result := p.Run(context.Background(), dir)

	if result.Error != nil {
		t.Fatalf("Pipeline error: %v", result.Error)
	}

	m, _ := manifest.Read(result.ManifestPath)
	if m.Enrichment.Location != nil {
		t.Error("location should be nil when skipped")
	}

	// Should have made only 1 AI call
	if len(mockProvider.Calls) != 1 {
		t.Errorf("AI calls = %d, want 1", len(mockProvider.Calls))
	}
}

func TestPipeline_ValidationFailureRecordsFailureMetadata(t *testing.T) {
	dir := t.TempDir()
	if err := testutil.SetupSamplePost(dir); err != nil {
		t.Fatalf("setup: %v", err)
	}

	// Add enough images to violate carousel max.
	for i := 0; i < 10; i++ {
		testutil.CreateJPEG(filepath.Join(dir, fmt.Sprintf("extra_%d.jpg", i)), 1080, 1350)
	}

	p := New(ai.NewMock("unused"), Options{})
	result := p.Run(context.Background(), dir)
	if result.Error == nil {
		t.Fatal("expected validation error")
	}
	m, err := manifest.Read(result.ManifestPath)
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}
	if m.State != manifest.StateError {
		t.Fatalf("state = %q, want %q", m.State, manifest.StateError)
	}
	if m.Failure == nil {
		t.Fatal("failure metadata should be present")
	}
	if m.Failure.Stage != manifest.FailureStageValidate {
		t.Fatalf("failure.stage = %q, want %q", m.Failure.Stage, manifest.FailureStageValidate)
	}
	if m.Failure.RetryFromState != manifest.StateScanned {
		t.Fatalf("failure.retry_from_state = %q, want %q", m.Failure.RetryFromState, manifest.StateScanned)
	}
}
