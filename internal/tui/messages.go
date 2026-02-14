package tui

// Background operation messages.

// PipelineCompleteMsg signals the pipeline finished for a post.
type PipelineCompleteMsg struct {
	PostID       string
	ManifestPath string
	Err          error
}

// PublishCompleteMsg signals publishing finished.
type PublishCompleteMsg struct {
	PostID       string
	ManifestPath string
	Permalink    string
	Err          error
}

// ArchiveCompleteMsg signals archival finished.
type ArchiveCompleteMsg struct {
	PostID string
	Err    error
}

// RefreshMsg triggers a reload of all panel data.
type RefreshMsg struct{}

// StatusMsg updates the status bar text.
type StatusMsg struct {
	Text string
}
