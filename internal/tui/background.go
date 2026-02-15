package tui

import (
	"context"
	"io"
	"path/filepath"

	tea "github.com/charmbracelet/bubbletea"

	"photography-publishing-workflow/internal/archiver"
	"photography-publishing-workflow/internal/manifest"
	"photography-publishing-workflow/internal/pipeline"
	"photography-publishing-workflow/internal/publisher"
	"photography-publishing-workflow/internal/watcher"
)

// startWatcherGoroutine starts the fsnotify watcher in a background goroutine.
// New post directories trigger the pipeline, and results are sent to ch.
func startWatcherGoroutine(dir string, p *pipeline.Pipeline, ch chan<- tea.Msg, logOutput io.Writer) {
	handler := func(ctx context.Context, postDir string) {
		postName := filepath.Base(postDir)
		ch <- StatusMsg{Text: "Processing: " + postName + "..."}

		result := p.Run(ctx, postDir)
		ch <- PipelineCompleteMsg{
			PostID:       result.PostID,
			ManifestPath: result.ManifestPath,
			Err:          result.Error,
		}
	}

	go func() {
		w, err := watcher.New(dir, handler, watcher.Options{LogOutput: logOutput})
		if err != nil {
			ch <- StatusMsg{Text: "Watcher error: " + err.Error()}
			return
		}
		ctx := context.Background()
		if err := w.Watch(ctx); err != nil {
			ch <- StatusMsg{Text: "Watcher stopped: " + err.Error()}
		}
	}()
}

// waitForWatcher returns a tea.Cmd that blocks until the next message
// arrives on the watcher channel.
func waitForWatcher(ch <-chan tea.Msg) tea.Cmd {
	if ch == nil {
		return nil
	}
	return func() tea.Msg {
		return <-ch
	}
}

// runPublish runs the publisher in a background goroutine for a single post.
func runPublish(pub *publisher.Publisher, manifestPath string) tea.Cmd {
	return func() tea.Msg {
		m, err := manifest.Read(manifestPath)
		if err != nil {
			return PublishCompleteMsg{Err: err}
		}
		ctx := context.Background()
		err = pub.Publish(ctx, m, manifestPath)

		permalink := ""
		if m.Publishing != nil {
			permalink = m.Publishing.Permalink
		}
		return PublishCompleteMsg{
			PostID:       m.ID,
			ManifestPath: manifestPath,
			Permalink:    permalink,
			Err:          err,
		}
	}
}

// runArchive runs the archiver in a background goroutine for a published post.
func runArchive(arch *archiver.Archiver, manifestPath string) tea.Cmd {
	return func() tea.Msg {
		m, err := manifest.Read(manifestPath)
		if err != nil {
			return ArchiveCompleteMsg{Err: err}
		}
		err = arch.Archive(m, manifestPath)
		return ArchiveCompleteMsg{
			PostID: m.ID,
			Err:    err,
		}
	}
}
