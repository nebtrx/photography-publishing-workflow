package tui

import (
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"photography-publishing-workflow/internal/manifest"
	"photography-publishing-workflow/internal/publisher"
)

func TestUpdateEditor_EnterSavesCaption(t *testing.T) {
	root := t.TempDir()
	postDir := filepath.Join(root, "post")
	path := manifest.ManifestPath(postDir)

	man := manifest.New("post", postDir)
	man.State = manifest.StatePendingReview
	if err := man.Write(path); err != nil {
		t.Fatalf("write manifest: %v", err)
	}

	model := NewApp(nil, "", AppOptions{})
	model.overlay = OverlayEditor
	model.editingPost = &PostEntry{Manifest: man, Path: path}
	model.editor.SetValue("hello world\n")

	next, _ := model.updateEditor(tea.KeyMsg{Type: tea.KeyEnter})
	got := next.(AppModel)
	if got.overlay != OverlayNone {
		t.Fatalf("overlay = %v, want OverlayNone", got.overlay)
	}
	if !strings.Contains(got.statusMsg, "Caption saved") {
		t.Fatalf("status = %q, want save confirmation", got.statusMsg)
	}

	saved, err := manifest.Read(path)
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}
	if saved.Review == nil {
		t.Fatal("review should be initialized")
	}
	if saved.Review.FinalCaption != "hello world" {
		t.Fatalf("final_caption = %q, want %q", saved.Review.FinalCaption, "hello world")
	}
	if !saved.Review.CaptionEdited {
		t.Fatal("caption_edited should be true")
	}
}

func TestUpdateEditor_AltEnterInsertsNewline(t *testing.T) {
	model := NewApp(nil, "", AppOptions{})
	model.overlay = OverlayEditor
	model.editor.SetValue("line one")

	next, _ := model.updateEditor(tea.KeyMsg{Type: tea.KeyEnter, Alt: true})
	got := next.(AppModel)
	if got.overlay != OverlayEditor {
		t.Fatalf("overlay = %v, want OverlayEditor", got.overlay)
	}
	if got.editor.Value() != "line one\n" {
		t.Fatalf("editor value = %q, want newline appended", got.editor.Value())
	}
}

func TestUpdateDialog_EnterQueuesApproval(t *testing.T) {
	root := t.TempDir()
	postDir := filepath.Join(root, "pending")
	path := manifest.ManifestPath(postDir)
	man := manifest.New("pending", postDir)
	man.State = manifest.StatePendingReview
	if err := man.Write(path); err != nil {
		t.Fatalf("write manifest: %v", err)
	}

	model := NewApp(nil, "", AppOptions{})
	model.overlay = OverlayConfirmApprove
	model.pendingPosts = []PostEntry{{Manifest: man, Path: path}}

	next, _ := model.updateDialog(tea.KeyMsg{Type: tea.KeyEnter})
	got := next.(AppModel)
	if got.overlay != OverlayNone {
		t.Fatalf("overlay = %v, want OverlayNone", got.overlay)
	}
	if man.State != manifest.StateApproved {
		t.Fatalf("state = %q, want %q", man.State, manifest.StateApproved)
	}
	if man.Review == nil || man.Review.PublishMode != "queued" {
		t.Fatalf("publish_mode = %#v, want queued", man.Review)
	}
}

func TestUpdateQueue_EnterPublishesSelected(t *testing.T) {
	root := t.TempDir()
	postDir := filepath.Join(root, "approved")
	path := manifest.ManifestPath(postDir)
	man := manifest.New("approved", postDir)
	man.State = manifest.StateApproved
	man.Review = &manifest.Review{Decision: "approved"}
	if err := man.Write(path); err != nil {
		t.Fatalf("write manifest: %v", err)
	}

	model := NewApp(nil, "", AppOptions{})
	model.activePanel = PanelQueue
	model.pub = &publisher.Publisher{}
	model.queuePosts = []PostEntry{{Manifest: man, Path: path}}

	next, cmd := model.updateQueue(tea.KeyMsg{Type: tea.KeyEnter})
	got := next.(AppModel)
	if cmd == nil {
		t.Fatal("expected publish command on Enter")
	}
	if !strings.Contains(got.statusMsg, "Publishing: approved") {
		t.Fatalf("status = %q, expected publish status", got.statusMsg)
	}
}
