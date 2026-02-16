package tui

import (
	"path/filepath"
	"strings"
	"testing"

	"photography-publishing-workflow/internal/manifest"
	"photography-publishing-workflow/internal/pipeline"
)

func TestReEnrich_NoPipelineTransitionsAndPersists(t *testing.T) {
	base := t.TempDir()
	postDir := filepath.Join(base, "post-a")
	manifestPath := filepath.Join(postDir, "manifest.json")

	mf := manifest.New("post-a", postDir)
	mf.State = manifest.StatePendingReview
	mf.Enrichment = &manifest.Enrichment{Caption: &manifest.Caption{Text: "old"}}
	if err := mf.Write(manifestPath); err != nil {
		t.Fatalf("write manifest: %v", err)
	}

	model := AppModel{
		pendingPosts:  []PostEntry{{Manifest: mf, Path: manifestPath}},
		activePanel:   PanelPending,
		pendingCursor: 0,
	}

	cmd := model.reEnrich()
	if cmd != nil {
		t.Fatal("expected nil cmd when pipeline is unavailable")
	}
	if model.pendingPosts[0].Manifest.State != manifest.StateValidated {
		t.Fatalf("state = %q, want %q", model.pendingPosts[0].Manifest.State, manifest.StateValidated)
	}
	if model.pendingPosts[0].Manifest.Enrichment != nil {
		t.Fatal("enrichment should be cleared")
	}
	if !strings.Contains(model.statusMsg, "pipeline unavailable") {
		t.Fatalf("status = %q, expected pipeline unavailable hint", model.statusMsg)
	}

	loaded, err := manifest.Read(manifestPath)
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}
	if loaded.State != manifest.StateValidated {
		t.Fatalf("persisted state = %q, want %q", loaded.State, manifest.StateValidated)
	}
	if loaded.Enrichment != nil {
		t.Fatal("persisted enrichment should be cleared")
	}
}

func TestReEnrich_WithPipelineReturnsCommand(t *testing.T) {
	base := t.TempDir()
	postDir := filepath.Join(base, "post-b")
	manifestPath := filepath.Join(postDir, "manifest.json")

	mf := manifest.New("post-b", postDir)
	mf.State = manifest.StatePendingReview
	if err := mf.Write(manifestPath); err != nil {
		t.Fatalf("write manifest: %v", err)
	}

	model := AppModel{
		pendingPosts:  []PostEntry{{Manifest: mf, Path: manifestPath}},
		activePanel:   PanelPending,
		pendingCursor: 0,
		pipe:          pipeline.New(nil, pipeline.Options{DryRun: true}),
	}

	cmd := model.reEnrich()
	if cmd == nil {
		t.Fatal("expected non-nil cmd when pipeline is configured")
	}
	if model.pipelining != "post-b" {
		t.Fatalf("pipelining = %q, want post-b", model.pipelining)
	}
	if !strings.Contains(model.statusMsg, "Re-enriching: post-b") {
		t.Fatalf("status = %q, unexpected", model.statusMsg)
	}
}
