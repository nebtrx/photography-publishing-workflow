package tui

import (
	"fmt"
	"io"
	"strings"

	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"photography-publishing-workflow/internal/archiver"
	"photography-publishing-workflow/internal/config"
	"photography-publishing-workflow/internal/manifest"
	"photography-publishing-workflow/internal/pipeline"
	"photography-publishing-workflow/internal/publisher"
)

// Panel identifies which left panel is active.
type Panel int

const (
	PanelConfig Panel = iota
	PanelPending
	PanelQueue
	PanelDeadLetter
	PanelPublished
	panelCount = 5
)

const (
	panelIndexConfig    = 1
	panelIndexPending   = 2
	panelIndexQueue     = 3
	panelIndexDead      = 4
	panelIndexPublished = 5
)

// Overlay identifies the current overlay state.
type Overlay int

const (
	OverlayNone Overlay = iota
	OverlayEditor
	OverlayConfirmApprove
	OverlayConfirmReject
	OverlayConfirmPublishAll
	OverlayHelp
)

// PostEntry wraps a manifest + its path for display.
type PostEntry struct {
	Manifest *manifest.Manifest
	Path     string
}

// LogGroup groups published log entries by month.
type LogGroup struct {
	Month     string // "2026-02"
	Entries   []LogDisplayEntry
	Collapsed bool
}

// LogDisplayEntry is a published post for display.
type LogDisplayEntry struct {
	ID              string
	ImageCount      int
	PublishedAt     string
	InstagramPostID string
	Permalink       string
	Caption         string
	Location        string
	StoryPublished  bool
	ArchivePath     string
}

// AppOptions configures optional background operation dependencies.
type AppOptions struct {
	Pipeline  *pipeline.Pipeline
	Publisher *publisher.Publisher
	Archiver  *archiver.Archiver
	EventCh   chan tea.Msg
	LogOutput io.Writer
}

// AppModel is the unified TUI model.
type AppModel struct {
	cfg    *config.Config
	cfgErr string // non-empty if config failed to load

	// Panel state
	activePanel     Panel
	pendingPosts    []PostEntry
	queuePosts      []PostEntry
	deadLetterPosts []PostEntry
	logGroups       []LogGroup

	// Cursors (per panel)
	pendingCursor int
	queueCursor   int
	deadCursor    int
	logCursor     int // flat index across all groups
	imgCursor     int // image browser within selected post

	// Overlay state
	overlay     Overlay
	editor      textarea.Model
	editingPost *PostEntry // which post is being edited

	// Status
	statusMsg  string
	watcherDir string

	// Terminal size
	width, height int
	quitting      bool

	// Background dependencies (nil = feature disabled)
	pipe *pipeline.Pipeline
	pub  *publisher.Publisher
	arch *archiver.Archiver
	logW io.Writer

	// Background state
	watcherCh  chan tea.Msg // shared channel for watcher + runtime log messages
	publishing string       // post ID currently being published, or ""
	pipelining string       // post ID currently in pipeline, or ""

	// In-app runtime logs
	runtimeLogs []string
}

// NewApp creates the unified TUI model.
func NewApp(cfg *config.Config, cfgErr string, opts AppOptions) AppModel {
	ta := textarea.New()
	ta.Placeholder = "Edit caption..."
	ta.CharLimit = 2200

	watchDir := ""
	if cfg != nil {
		watchDir = cfg.Watch.Dir
	}

	m := AppModel{
		cfg:         cfg,
		cfgErr:      cfgErr,
		activePanel: PanelPending,
		editor:      ta,
		watcherDir:  watchDir,
		pipe:        opts.Pipeline,
		pub:         opts.Publisher,
		arch:        opts.Archiver,
		logW:        opts.LogOutput,
		watcherCh:   opts.EventCh,
	}

	// Set up event channel if watcher can run and caller didn't provide one.
	if m.watcherCh == nil && m.pipe != nil && watchDir != "" {
		m.watcherCh = make(chan tea.Msg, 16)
	}

	return m
}

func (m AppModel) Init() tea.Cmd {
	cmds := []tea.Cmd{
		func() tea.Msg { return RefreshMsg{} },
	}

	// Start background watcher if pipeline + watch dir are configured
	if m.watcherCh != nil && m.pipe != nil && m.watcherDir != "" {
		startWatcherGoroutine(m.watcherDir, m.pipe, m.watcherCh, m.logW)
		cmds = append(cmds, waitForWatcher(m.watcherCh))
	}

	return tea.Batch(cmds...)
}

func (m AppModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case RefreshMsg:
		m.loadData()
		return m, nil

	case PipelineCompleteMsg:
		m.pipelining = ""
		if msg.Err != nil {
			m.statusMsg = fmt.Sprintf("Pipeline error: %v", msg.Err)
		} else {
			m.statusMsg = fmt.Sprintf("New post ready: %s", msg.PostID)
			m.loadData()
		}
		// Re-subscribe to watcher channel
		return m, waitForWatcher(m.watcherCh)

	case PublishCompleteMsg:
		m.publishing = ""
		if msg.Err != nil {
			m.statusMsg = fmt.Sprintf("Publish error: %v", msg.Err)
			m.loadData()
		} else {
			m.statusMsg = fmt.Sprintf("Published: %s", msg.Permalink)
			m.loadData()
			// Auto-archive if archiver is configured
			if m.arch != nil && msg.ManifestPath != "" {
				return m, runArchive(m.arch, msg.ManifestPath)
			}
		}
		return m, nil

	case ArchiveCompleteMsg:
		if msg.Err != nil {
			m.statusMsg = fmt.Sprintf("Archive error: %v", msg.Err)
		} else {
			m.statusMsg = fmt.Sprintf("Archived: %s", msg.PostID)
		}
		m.loadData()
		return m, nil

	case StatusMsg:
		m.statusMsg = msg.Text
		// Re-subscribe to watcher channel (StatusMsg comes from watcher)
		return m, waitForWatcher(m.watcherCh)

	case AppLogMsg:
		m.appendRuntimeLog(msg.Line)
		return m, waitForWatcher(m.watcherCh)

	case tea.KeyMsg:
		// Help overlay
		if m.overlay == OverlayHelp {
			m.overlay = OverlayNone
			return m, nil
		}

		// Editor overlay
		if m.overlay == OverlayEditor {
			return m.updateEditor(msg)
		}

		// Confirmation overlays
		if m.overlay == OverlayConfirmApprove ||
			m.overlay == OverlayConfirmReject ||
			m.overlay == OverlayConfirmPublishAll {
			return m.updateDialog(msg)
		}

		// Normal mode
		return m.updateNormal(msg)
	}

	return m, nil
}

func (m AppModel) updateNormal(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "q", "ctrl+c":
		m.quitting = true
		return m, tea.Quit

	case "tab":
		m.activePanel = (m.activePanel + 1) % panelCount
		m.imgCursor = 0
		m.statusMsg = ""
		return m, nil

	case "shift+tab":
		m.activePanel = (m.activePanel - 1 + panelCount) % panelCount
		m.imgCursor = 0
		m.statusMsg = ""
		return m, nil

	case "?":
		m.overlay = OverlayHelp
		return m, nil

	case "1":
		m.activePanel = PanelConfig
		m.imgCursor = 0
		m.statusMsg = ""
		return m, nil
	case "2":
		m.activePanel = PanelPending
		m.imgCursor = 0
		m.statusMsg = ""
		return m, nil
	case "3":
		m.activePanel = PanelQueue
		m.imgCursor = 0
		m.statusMsg = ""
		return m, nil
	case "4":
		m.activePanel = PanelDeadLetter
		m.imgCursor = 0
		m.statusMsg = ""
		return m, nil
	case "5":
		m.activePanel = PanelPublished
		m.imgCursor = 0
		m.statusMsg = ""
		return m, nil
	}

	// Panel-specific keys
	switch m.activePanel {
	case PanelConfig:
		return m.updateConfig(msg)
	case PanelPending:
		return m.updatePending(msg)
	case PanelQueue:
		return m.updateQueue(msg)
	case PanelDeadLetter:
		return m.updateDeadLetter(msg)
	case PanelPublished:
		return m.updatePublished(msg)
	}

	return m, nil
}

func (m AppModel) updateConfig(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "e":
		return m, m.openEditor()
	}
	return m, nil
}

func (m AppModel) updatePending(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "up", "k":
		if m.pendingCursor > 0 {
			m.pendingCursor--
			m.imgCursor = 0
		}
	case "down", "j":
		if m.pendingCursor < len(m.pendingPosts)-1 {
			m.pendingCursor++
			m.imgCursor = 0
		}
	case "left", "h":
		if m.imgCursor > 0 {
			m.imgCursor--
		}
	case "right", "l":
		post := m.selectedPending()
		if post != nil && m.imgCursor < len(post.Manifest.Images)-1 {
			m.imgCursor++
		}
	case "enter":
		post := m.selectedPending()
		if post != nil && m.imgCursor < len(post.Manifest.Images) {
			openFile(post.Manifest.Images[m.imgCursor].Path)
			m.statusMsg = "Opened image in viewer"
		}
	case "e":
		return m.startEditor()
	case "s":
		m.toggleStory()
	case "a":
		if m.selectedPending() != nil {
			m.overlay = OverlayConfirmApprove
		}
	case "r":
		if m.selectedPending() != nil {
			m.overlay = OverlayConfirmReject
		}
	case "R":
		return m, m.reEnrich()
	}
	return m, nil
}

func (m AppModel) updateQueue(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "up", "k":
		if m.queueCursor > 0 {
			m.queueCursor--
			m.imgCursor = 0
		}
	case "down", "j":
		if m.queueCursor < len(m.queuePosts)-1 {
			m.queueCursor++
			m.imgCursor = 0
		}
	case "enter":
		return m, m.publishSelected()
	case "p":
		cmd := m.publishSelected()
		return m, cmd
	case "P":
		if len(m.queuePosts) > 0 {
			m.overlay = OverlayConfirmPublishAll
		}
	case "d":
		m.dequeuePost()
	}
	return m, nil
}

func (m AppModel) updateDeadLetter(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "up", "k":
		if m.deadCursor > 0 {
			m.deadCursor--
			m.imgCursor = 0
		}
	case "down", "j":
		if m.deadCursor < len(m.deadLetterPosts)-1 {
			m.deadCursor++
			m.imgCursor = 0
		}
	case "left", "h":
		if m.imgCursor > 0 {
			m.imgCursor--
		}
	case "right", "l":
		post := m.selectedDeadLetter()
		if post != nil && m.imgCursor < len(post.Manifest.Images)-1 {
			m.imgCursor++
		}
	case "enter":
		post := m.selectedDeadLetter()
		if post != nil && m.imgCursor < len(post.Manifest.Images) {
			openFile(post.Manifest.Images[m.imgCursor].Path)
			m.statusMsg = "Opened image in viewer"
		}
	case "r":
		return m, m.retryDeadLetterPost()
	}
	return m, nil
}

func (m AppModel) updatePublished(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "up", "k":
		if m.logCursor > 0 {
			m.logCursor--
		}
	case "down", "j":
		max := m.logFlatCount() - 1
		if m.logCursor < max {
			m.logCursor++
		}
	case "enter":
		entry := m.selectedLogEntry()
		if entry != nil && entry.Permalink != "" {
			openFile(entry.Permalink)
			m.statusMsg = "Opened permalink in browser"
		}
	case " ":
		m.toggleLogGroup()
	}
	return m, nil
}

// Editor overlay
func (m AppModel) updateEditor(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.overlay = OverlayNone
		m.statusMsg = "Edit cancelled"
		return m, nil
	case "enter", "ctrl+s":
		if m.editingPost != nil {
			post := m.editingPost.Manifest
			if post.Review == nil {
				post.Review = &manifest.Review{}
			}
			post.Review.FinalCaption = normalizeCaptionText(m.editor.Value())
			post.Review.CaptionEdited = true
			post.Write(m.editingPost.Path)
			m.statusMsg = "Caption saved"
		}
		m.overlay = OverlayNone
		return m, nil
	case "shift+enter", "alt+enter":
		m.editor.InsertString("\n")
		return m, nil
	}
	var cmd tea.Cmd
	m.editor, cmd = m.editor.Update(msg)
	return m, cmd
}

// Dialog overlay
func (m AppModel) updateDialog(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch m.overlay {
	case OverlayConfirmApprove:
		switch msg.String() {
		case "p":
			m.overlay = OverlayNone
			// Capture the post path before approvePost reloads data
			post := m.selectedPending()
			postPath := ""
			if post != nil {
				postPath = post.Path
			}
			m.approvePost("immediate")
			// Publish directly using the captured path
			if m.pub != nil && postPath != "" {
				m.publishing = post.Manifest.ID
				m.statusMsg = fmt.Sprintf("Publishing: %s...", post.Manifest.ID)
				return m, runPublish(m.pub, postPath)
			}
			if m.pub == nil {
				m.statusMsg = "Approved (publish manually - publisher not configured)"
			}
			return m, nil
		case "q", "enter":
			m.overlay = OverlayNone
			m.approvePost("queued")
		case "esc":
			m.overlay = OverlayNone
		}
	case OverlayConfirmReject:
		switch msg.String() {
		case "y":
			m.overlay = OverlayNone
			m.rejectPost()
		case "n", "esc":
			m.overlay = OverlayNone
		}
	case OverlayConfirmPublishAll:
		switch msg.String() {
		case "y", "enter":
			m.overlay = OverlayNone
			return m, m.publishAllQueued()
		case "n", "esc":
			m.overlay = OverlayNone
		}
	}
	return m, nil
}

// --- Actions ---

func (m *AppModel) approvePost(publishMode string) {
	post := m.selectedPending()
	if post == nil {
		return
	}
	if post.Manifest.Review == nil {
		post.Manifest.Review = &manifest.Review{}
	}
	post.Manifest.Review.Decision = "approved"
	post.Manifest.Review.PublishMode = publishMode

	caption := currentCaption(post.Manifest)
	if post.Manifest.Review.FinalCaption == "" && caption != "" {
		post.Manifest.Review.FinalCaption = caption
	}

	if err := post.Manifest.Transition(manifest.StateApproved); err != nil {
		m.statusMsg = fmt.Sprintf("Error: %v", err)
		return
	}
	post.Manifest.Write(post.Path)

	if publishMode == "queued" {
		m.statusMsg = fmt.Sprintf("Queued: %s", post.Manifest.ID)
	}

	m.loadData()
	if m.pendingCursor >= len(m.pendingPosts) && m.pendingCursor > 0 {
		m.pendingCursor--
	}
}

func (m *AppModel) rejectPost() {
	post := m.selectedPending()
	if post == nil {
		return
	}
	if post.Manifest.Review == nil {
		post.Manifest.Review = &manifest.Review{}
	}
	post.Manifest.Review.Decision = "rejected"

	if err := post.Manifest.Transition(manifest.StateRejected); err != nil {
		m.statusMsg = fmt.Sprintf("Error: %v", err)
		return
	}
	post.Manifest.Write(post.Path)
	m.statusMsg = fmt.Sprintf("Rejected: %s", post.Manifest.ID)
	m.loadData()
	if m.pendingCursor >= len(m.pendingPosts) && m.pendingCursor > 0 {
		m.pendingCursor--
	}
}

func (m *AppModel) toggleStory() {
	post := m.selectedPending()
	if post == nil {
		return
	}
	if post.Manifest.Review == nil {
		post.Manifest.Review = &manifest.Review{}
	}
	post.Manifest.Review.StoryEnabled = !post.Manifest.Review.StoryEnabled
	post.Manifest.Write(post.Path)
	state := "ON"
	if !post.Manifest.Review.StoryEnabled {
		state = "OFF"
	}
	m.statusMsg = fmt.Sprintf("Story: %s", state)
}

func (m *AppModel) reEnrich() tea.Cmd {
	post := m.selectedPending()
	if post == nil {
		return nil
	}
	if err := post.Manifest.Transition(manifest.StateValidated); err != nil {
		m.statusMsg = fmt.Sprintf("Re-enrich failed: %v", err)
		return nil
	}
	post.Manifest.Enrichment = nil
	if err := post.Manifest.Write(post.Path); err != nil {
		m.statusMsg = fmt.Sprintf("Re-enrich failed: %v", err)
		return nil
	}

	// In unified TUI mode, run pipeline immediately so the post returns to
	// pending_review without disappearing indefinitely in validated state.
	if m.pipe != nil {
		m.pipelining = post.Manifest.ID
		m.statusMsg = fmt.Sprintf("Re-enriching: %s...", post.Manifest.ID)
		return runPipeline(m.pipe, post.Manifest.SourceDir)
	}

	m.statusMsg = fmt.Sprintf("Re-enrich queued: %s (pipeline unavailable)", post.Manifest.ID)
	m.loadData()
	return nil
}

func (m *AppModel) dequeuePost() {
	post := m.selectedQueue()
	if post == nil {
		return
	}
	// Move back to pending_review
	post.Manifest.State = manifest.StatePendingReview
	post.Manifest.Review.Decision = ""
	post.Manifest.Review.PublishMode = ""
	post.Manifest.Write(post.Path)
	m.statusMsg = fmt.Sprintf("Dequeued: %s", post.Manifest.ID)
	m.loadData()
	if m.queueCursor >= len(m.queuePosts) && m.queueCursor > 0 {
		m.queueCursor--
	}
}

func (m AppModel) startEditor() (AppModel, tea.Cmd) {
	post := m.selectedPending()
	if post == nil {
		return m, nil
	}
	caption := currentCaption(post.Manifest)
	m.editor.SetValue(caption)
	m.editor.SetWidth(50)
	m.editor.SetHeight(8)
	m.editor.Focus()
	m.overlay = OverlayEditor
	m.editingPost = post
	return m, textarea.Blink
}

func (m AppModel) openEditor() tea.Cmd {
	// This would open $EDITOR on the config file
	// For now, open the config file in the system editor
	if m.cfg == nil {
		return nil
	}
	path := config.DefaultPath()
	openFile(path)
	m.statusMsg = "Opened config in editor"
	return nil
}

func (m *AppModel) toggleLogGroup() {
	// Find which group the cursor is in
	idx := 0
	for i := range m.logGroups {
		if idx == m.logCursor {
			m.logGroups[i].Collapsed = !m.logGroups[i].Collapsed
			return
		}
		idx++ // group header
		if !m.logGroups[i].Collapsed {
			idx += len(m.logGroups[i].Entries)
		}
	}
}

func (m *AppModel) publishSelected() tea.Cmd {
	post := m.selectedQueue()
	if post == nil {
		m.statusMsg = "No queued post selected"
		return nil
	}
	if m.pub == nil {
		m.statusMsg = "Publisher not configured (need R2 + Instagram env vars)"
		return nil
	}
	if _, err := m.preparePostForRetry(post); err != nil {
		m.statusMsg = fmt.Sprintf("Retry setup failed: %v", err)
		return nil
	}
	m.publishing = post.Manifest.ID
	m.statusMsg = fmt.Sprintf("Publishing: %s...", post.Manifest.ID)
	return runPublish(m.pub, post.Path)
}

func (m *AppModel) publishAllQueued() tea.Cmd {
	if len(m.queuePosts) == 0 {
		return nil
	}
	if m.pub == nil {
		m.statusMsg = "Publisher not configured (need R2 + Instagram env vars)"
		return nil
	}
	var cmds []tea.Cmd
	firstID := ""
	for i := range m.queuePosts {
		post := &m.queuePosts[i]
		if _, err := m.preparePostForRetry(post); err != nil {
			m.appendRuntimeLog(fmt.Sprintf("[runtime] Skip %s: %v", post.Manifest.ID, err))
			continue
		}
		if firstID == "" {
			firstID = post.Manifest.ID
		}
		cmds = append(cmds, runPublish(m.pub, post.Path))
	}
	if len(cmds) == 0 {
		m.statusMsg = "No publishable posts in queue"
		return nil
	}
	m.publishing = firstID
	m.statusMsg = fmt.Sprintf("Publishing %d posts...", len(cmds))
	return tea.Batch(cmds...)
}

func (m *AppModel) retryDeadLetterPost() tea.Cmd {
	post := m.selectedDeadLetter()
	if post == nil {
		m.statusMsg = "No failed post selected"
		return nil
	}
	stage, err := m.preparePostForRetry(post)
	if err != nil {
		m.statusMsg = fmt.Sprintf("Retry setup failed: %v", err)
		return nil
	}
	switch stage {
	case manifest.FailureStagePublish, manifest.FailureStageSyndicate:
		if m.pub == nil {
			m.statusMsg = "Retry blocked: publisher not configured"
			return nil
		}
		m.publishing = post.Manifest.ID
		m.statusMsg = fmt.Sprintf("Retrying publish: %s...", post.Manifest.ID)
		return runPublish(m.pub, post.Path)
	case manifest.FailureStageArchive:
		if m.arch == nil {
			m.statusMsg = "Retry blocked: archiver not configured"
			return nil
		}
		m.statusMsg = fmt.Sprintf("Retrying archive: %s...", post.Manifest.ID)
		return runArchive(m.arch, post.Path)
	default:
		if m.pipe == nil {
			m.statusMsg = "Retry blocked: pipeline not configured"
			return nil
		}
		m.pipelining = post.Manifest.ID
		m.statusMsg = fmt.Sprintf("Retrying pipeline: %s...", post.Manifest.ID)
		return runPipeline(m.pipe, post.Manifest.SourceDir)
	}
}

func (m *AppModel) preparePostForRetry(post *PostEntry) (manifest.FailureStage, error) {
	if post == nil || post.Manifest == nil {
		return "", fmt.Errorf("no post selected")
	}
	man := post.Manifest
	if man.State != manifest.StateError {
		return manifest.FailureStagePublish, nil
	}
	stage, err := man.PrepareRetry()
	if err != nil {
		return "", err
	}
	// For publish-oriented retries, require review approval.
	if stage == manifest.FailureStagePublish || stage == manifest.FailureStageSyndicate {
		if man.Review == nil || man.Review.Decision != "approved" {
			return "", fmt.Errorf("publish retry requires review.decision=approved")
		}
	}
	if err := man.Write(post.Path); err != nil {
		return "", fmt.Errorf("write manifest before retry: %w", err)
	}
	return stage, nil
}

// --- Selectors ---

func (m *AppModel) selectedPending() *PostEntry {
	if len(m.pendingPosts) == 0 || m.pendingCursor >= len(m.pendingPosts) {
		return nil
	}
	return &m.pendingPosts[m.pendingCursor]
}

func (m *AppModel) selectedQueue() *PostEntry {
	if len(m.queuePosts) == 0 || m.queueCursor >= len(m.queuePosts) {
		return nil
	}
	return &m.queuePosts[m.queueCursor]
}

func (m *AppModel) selectedDeadLetter() *PostEntry {
	if len(m.deadLetterPosts) == 0 || m.deadCursor >= len(m.deadLetterPosts) {
		return nil
	}
	return &m.deadLetterPosts[m.deadCursor]
}

func (m *AppModel) selectedLogEntry() *LogDisplayEntry {
	idx := 0
	for i := range m.logGroups {
		if idx == m.logCursor {
			return nil // cursor is on group header
		}
		idx++
		if !m.logGroups[i].Collapsed {
			for j := range m.logGroups[i].Entries {
				if idx == m.logCursor {
					return &m.logGroups[i].Entries[j]
				}
				idx++
			}
		}
	}
	return nil
}

func (m *AppModel) logFlatCount() int {
	count := 0
	for _, g := range m.logGroups {
		count++ // header
		if !g.Collapsed {
			count += len(g.Entries)
		}
	}
	return count
}

// --- View ---

func (m AppModel) View() string {
	if m.quitting {
		return "Goodbye.\n"
	}

	w := m.width
	if w == 0 {
		w = 100
	}
	h := m.height
	if h == 0 {
		h = 30
	}

	// Check minimum size
	if w < 80 || h < 24 {
		return "Terminal too small. Minimum 80x24.\n"
	}

	// Layout
	leftW := w*35/100 - 2
	rightW := w - leftW - 6 // account for borders and gap
	bodyH := h - 1

	// Keep panel heights fixed so screen geometry does not shift while data changes.
	configH := 6
	remaining := bodyH - configH
	if remaining < 12 {
		remaining = 12
		configH = bodyH - remaining
	}
	pendingH := remaining / 4
	queueH := remaining / 4
	deadH := remaining / 4
	publishedH := remaining - pendingH - queueH - deadH

	// Render left panels
	configPanel := m.renderConfigPanel(leftW, configH)
	pendingPanel := m.renderPendingPanel(leftW, pendingH)
	queuePanel := m.renderQueuePanel(leftW, queueH)
	deadPanel := m.renderDeadLetterPanel(leftW, deadH)
	publishedPanel := m.renderPublishedPanel(leftW, publishedH)

	leftCol := lipgloss.JoinVertical(lipgloss.Left, configPanel, pendingPanel, queuePanel, deadPanel, publishedPanel)

	// Split right column into detail (top) + runtime log (bottom).
	logH := clampInt(bodyH*28/100, 7, 14)
	if logH > bodyH-8 {
		logH = bodyH - 8
	}
	detailH := bodyH - logH
	detailPanel := m.renderDetailPanel(rightW, detailH)
	logPanel := m.renderRuntimeLogPanel(rightW, logH)
	rightCol := lipgloss.JoinVertical(lipgloss.Left, detailPanel, logPanel)

	body := lipgloss.JoinHorizontal(lipgloss.Top, leftCol, " ", rightCol)

	// Modal overlays render as a dedicated centered frame to avoid base-buffer corruption.
	if m.overlay != OverlayNone {
		body = m.renderOverlayFrame(w, bodyH)
	}

	statusBar := m.renderStatusBar(w)
	return lipgloss.JoinVertical(lipgloss.Left, body, statusBar)
}

func (m AppModel) renderConfigPanel(w, h int) string {
	var lines []string
	if m.cfg != nil {
		lines = append(lines, fmt.Sprintf("Watch:   %s", truncate(m.cfg.Watch.Dir, w-12)))
		lines = append(lines, fmt.Sprintf("AI:      %s", m.cfg.AI.Provider))
		if m.cfg.AI.CorpusPath != "" {
			lines = append(lines, fmt.Sprintf("Corpus:  %s", truncate(m.cfg.AI.CorpusPath, w-12)))
		}
		lines = append(lines, fmt.Sprintf("Archive: %s", truncate(m.cfg.Archive.Dir, w-12)))
	} else if m.cfgErr != "" {
		lines = append(lines, errorTextStyle.Render("Config error:"))
		lines = append(lines, errorTextStyle.Render(truncate(m.cfgErr, w-4)))
	} else {
		lines = append(lines, dimTextStyle.Render("No config loaded"))
	}

	lines = tailLines(lines, maxInt(1, h-2))
	content := strings.Join(lines, "\n")
	title := m.panelTitle("Config", PanelConfig)
	return renderPanelChrome(w, h, title, content, m.activePanel == PanelConfig, "")
}

func (m AppModel) renderPendingPanel(w, h int) string {
	title := m.panelTitle(fmt.Sprintf("Pending Review (%d)", len(m.pendingPosts)), PanelPending)

	var lines []string
	if len(m.pendingPosts) == 0 {
		lines = append(lines, dimTextStyle.Render("No posts pending review"))
	}
	for i, post := range m.pendingPosts {
		name := truncate(post.Manifest.ID, w-10)
		imgCount := fmt.Sprintf("[%d]", len(post.Manifest.Images))
		line := fmt.Sprintf("  %s %s", name, imgCount)
		if i == m.pendingCursor && m.activePanel == PanelPending {
			line = selectedItemStyle.Render(fmt.Sprintf("▸ %s %s", name, imgCount))
		}
		lines = append(lines, line)
	}

	lines = tailLines(lines, maxInt(1, h-2))
	content := strings.Join(lines, "\n")
	return renderPanelChrome(w, h, title, content, m.activePanel == PanelPending, m.pendingCounter())
}

func (m AppModel) renderQueuePanel(w, h int) string {
	title := m.panelTitle(fmt.Sprintf("Publish Queue (%d)", len(m.queuePosts)), PanelQueue)

	var lines []string
	if len(m.queuePosts) == 0 {
		lines = append(lines, dimTextStyle.Render("(empty)"))
	}
	for i, post := range m.queuePosts {
		name := truncate(post.Manifest.ID, w-10)
		imgCount := fmt.Sprintf("[%d]", len(post.Manifest.Images))
		prefix := "  "
		if post.Manifest.State == manifest.StateError {
			prefix = "! "
		}
		line := fmt.Sprintf("%s%s %s", prefix, name, imgCount)
		if i == m.queueCursor && m.activePanel == PanelQueue {
			line = selectedItemStyle.Render(fmt.Sprintf("▸ %s%s %s", strings.TrimSpace(prefix), name, imgCount))
		}
		lines = append(lines, line)
	}

	lines = tailLines(lines, maxInt(1, h-2))
	content := strings.Join(lines, "\n")
	return renderPanelChrome(w, h, title, content, m.activePanel == PanelQueue, m.queueCounter())
}

func (m AppModel) renderDeadLetterPanel(w, h int) string {
	title := m.panelTitle(fmt.Sprintf("Failed (%d)", len(m.deadLetterPosts)), PanelDeadLetter)

	var lines []string
	if len(m.deadLetterPosts) == 0 {
		lines = append(lines, dimTextStyle.Render("(empty)"))
	}
	for i, post := range m.deadLetterPosts {
		stage := string(post.Manifest.EffectiveFailureStage())
		name := truncate(post.Manifest.ID, w-16)
		imgCount := fmt.Sprintf("[%d]", len(post.Manifest.Images))
		line := fmt.Sprintf("! %s %s (%s)", name, imgCount, stage)
		if i == m.deadCursor && m.activePanel == PanelDeadLetter {
			line = selectedItemStyle.Render(fmt.Sprintf("▸ %s %s (%s)", name, imgCount, stage))
		}
		lines = append(lines, line)
	}

	lines = tailLines(lines, maxInt(1, h-2))
	content := strings.Join(lines, "\n")
	return renderPanelChrome(w, h, title, content, m.activePanel == PanelDeadLetter, m.deadCounter())
}

func (m AppModel) renderPublishedPanel(w, h int) string {
	total := 0
	for _, g := range m.logGroups {
		total += len(g.Entries)
	}
	title := m.panelTitle(fmt.Sprintf("Published (%d)", total), PanelPublished)

	var lines []string
	if len(m.logGroups) == 0 {
		lines = append(lines, dimTextStyle.Render("No published posts"))
	}

	flatIdx := 0
	for _, g := range m.logGroups {
		arrow := "▾"
		if g.Collapsed {
			arrow = "▸"
		}
		header := fmt.Sprintf("%s %s (%d)", arrow, g.Month, len(g.Entries))
		if flatIdx == m.logCursor && m.activePanel == PanelPublished {
			header = selectedItemStyle.Render(header)
		} else {
			header = monthHeaderStyle.Render(header)
		}
		lines = append(lines, header)
		flatIdx++

		if !g.Collapsed {
			for _, e := range g.Entries {
				name := truncate(e.ID, w-10)
				imgCount := fmt.Sprintf("[%d]", e.ImageCount)
				line := fmt.Sprintf("  %s %s", name, imgCount)
				if flatIdx == m.logCursor && m.activePanel == PanelPublished {
					line = selectedItemStyle.Render(fmt.Sprintf("▸ %s %s", name, imgCount))
				}
				lines = append(lines, line)
				flatIdx++
			}
		}
	}

	lines = tailLines(lines, maxInt(1, h-2))
	content := strings.Join(lines, "\n")
	return renderPanelChrome(w, h, title, content, m.activePanel == PanelPublished, m.publishedCounter())
}

func (m AppModel) renderDetailPanel(w, h int) string {
	title := panelTitleDimStyle.Render("Detail")
	var content string

	switch m.activePanel {
	case PanelConfig:
		content = m.renderConfigDetail(w)
	case PanelPending:
		content = m.renderPostDetail(m.selectedPending(), w, true)
	case PanelQueue:
		content = m.renderPostDetail(m.selectedQueue(), w, false)
	case PanelDeadLetter:
		content = m.renderDeadLetterDetail(w)
	case PanelPublished:
		content = m.renderLogDetail(w)
	}

	return renderPanelChrome(w, h, title, content, false, "")
}

func (m AppModel) renderDeadLetterDetail(w int) string {
	post := m.selectedDeadLetter()
	if post == nil {
		return dimTextStyle.Render("No failed post selected")
	}

	man := post.Manifest
	stage := man.EffectiveFailureStage()
	retryFrom := man.EffectiveRetryState()
	lastErr := ""
	if man.Failure != nil && strings.TrimSpace(man.Failure.Message) != "" {
		lastErr = strings.TrimSpace(man.Failure.Message)
	} else if len(man.Errors) > 0 {
		lastErr = strings.TrimSpace(man.Errors[len(man.Errors)-1])
	}

	var sections []string
	sections = append(sections, labelStyle.Render("Post"))
	sections = append(sections, man.ID)
	sections = append(sections, "")
	sections = append(sections, labelStyle.Render("Failed Stage"))
	sections = append(sections, string(stage))
	sections = append(sections, "")
	sections = append(sections, labelStyle.Render("Retry From State"))
	sections = append(sections, string(retryFrom))
	sections = append(sections, "")
	sections = append(sections, labelStyle.Render("Current State"))
	sections = append(sections, string(man.State))

	if lastErr != "" {
		sections = append(sections, "")
		sections = append(sections, labelStyle.Render("Last Error"))
		sections = append(sections, truncate(lastErr, maxInt(20, w-4)))
	}

	if m.imgCursor < len(man.Images) {
		img := man.Images[m.imgCursor]
		sections = append(sections, "")
		sections = append(sections, labelStyle.Render("Image"))
		sections = append(
			sections,
			fmt.Sprintf("%s [%d/%d]  %dx%d  %s", img.Filename, m.imgCursor+1, len(man.Images), img.Width, img.Height, img.AspectRatio),
		)
	}

	sections = append(sections, "")
	sections = append(sections, dimTextStyle.Render("─── Actions ───"))
	sections = append(sections, "r  Retry from failed stage")
	sections = append(sections, "⏎  Open image    ←→ Browse images    ↑↓ Browse posts")
	return strings.Join(sections, "\n")
}

func (m AppModel) renderConfigDetail(w int) string {
	var sections []string
	sections = append(sections, dimTextStyle.Render("Configuration"))
	sections = append(sections, "")

	if m.cfg != nil {
		sections = append(sections, labelStyle.Render("Watch Directory"))
		sections = append(sections, m.cfg.Watch.Dir)
		sections = append(sections, "")

		sections = append(sections, labelStyle.Render("AI Provider"))
		sections = append(sections, m.cfg.AI.Provider)
		if m.cfg.AI.CorpusPath != "" {
			sections = append(sections, fmt.Sprintf("Corpus: %s", m.cfg.AI.CorpusPath))
		}
		sections = append(sections, "")

		sections = append(sections, labelStyle.Render("Archive"))
		sections = append(sections, fmt.Sprintf("Dir: %s", m.cfg.Archive.Dir))
		sections = append(sections, fmt.Sprintf("Log: %s", m.cfg.Archive.LogFile))
	}

	sections = append(sections, "")
	sections = append(sections, dimTextStyle.Render("─── Keys ───"))
	sections = append(sections, "e  Open config in $EDITOR")

	return strings.Join(sections, "\n")
}

func (m AppModel) renderPostDetail(post *PostEntry, w int, showActions bool) string {
	if post == nil {
		return dimTextStyle.Render("No post selected")
	}

	man := post.Manifest
	var sections []string

	// Image info
	if m.imgCursor < len(man.Images) {
		img := man.Images[m.imgCursor]
		info := fmt.Sprintf("%s [%d/%d]  %dx%d  %s",
			img.Filename, m.imgCursor+1, len(man.Images),
			img.Width, img.Height, img.AspectRatio)
		if img.IsHero {
			info += "  [hero]"
		}
		sections = append(sections, labelStyle.Render("Image"))
		sections = append(sections, info)
	}

	// Caption
	sections = append(sections, "")
	caption := currentCaption(man)
	if caption != "" {
		preview := caption
		if len(preview) > 400 {
			preview = preview[:400] + "..."
		}
		sections = append(sections, labelStyle.Render("Caption"))
		sections = append(sections, preview)
	} else {
		sections = append(sections, labelStyle.Render("Caption"))
		sections = append(sections, dimTextStyle.Render("(not generated)"))
	}

	// Location
	if man.Enrichment != nil && man.Enrichment.Location != nil {
		sections = append(sections, "")
		sections = append(sections, labelStyle.Render("Location"))
		sections = append(sections, man.Enrichment.Location.Name+" ("+man.Enrichment.Location.Source+")")
	}

	// Music
	if man.Enrichment != nil && man.Enrichment.MusicSuggestion != nil {
		ms := man.Enrichment.MusicSuggestion
		sections = append(sections, "")
		sections = append(sections, labelStyle.Render("Music"))
		sections = append(sections, ms.Artist+" — "+ms.Title+" ("+ms.Mood+")")
	}

	// Story toggle
	storyState := "OFF"
	if man.Review != nil && man.Review.StoryEnabled {
		storyState = successTextStyle.Render("ON")
	}
	sections = append(sections, "")
	sections = append(sections, labelStyle.Render("Story")+"  "+storyState)

	sections = append(sections, "")
	sections = append(sections, labelStyle.Render("State"))
	sections = append(sections, string(man.State))

	if len(man.Errors) > 0 {
		sections = append(sections, "")
		sections = append(sections, labelStyle.Render("Last Error"))
		sections = append(sections, truncate(man.Errors[len(man.Errors)-1], maxInt(20, w-4)))
	}

	// Actions
	if showActions {
		sections = append(sections, "")
		sections = append(sections, dimTextStyle.Render("─── Actions ───"))
		sections = append(sections, "a  Approve (publish/queue)    r  Reject")
		sections = append(sections, "e  Edit caption              s  Toggle story")
		sections = append(sections, "R  Re-enrich                 ⏎  Open image")
		sections = append(sections, "←→ Browse images             ↑↓ Browse posts")
	} else {
		sections = append(sections, "")
		sections = append(sections, dimTextStyle.Render("─── Actions ───"))
		sections = append(sections, "⏎  Publish selected (default)")
		sections = append(sections, "p  Publish selected      P  Publish all")
		sections = append(sections, "d  Remove from queue")
	}

	return strings.Join(sections, "\n")
}

func (m AppModel) renderLogDetail(w int) string {
	entry := m.selectedLogEntry()
	if entry == nil {
		// Check if cursor is on a group header
		return dimTextStyle.Render("Select a post to view details\n\nSpace: expand/collapse group")
	}

	var sections []string
	sections = append(sections, labelStyle.Render("Post ID"))
	sections = append(sections, entry.ID)

	sections = append(sections, "")
	sections = append(sections, labelStyle.Render("Published"))
	sections = append(sections, entry.PublishedAt)

	if entry.Permalink != "" {
		sections = append(sections, "")
		sections = append(sections, labelStyle.Render("Permalink"))
		sections = append(sections, entry.Permalink)
	}

	if entry.InstagramPostID != "" {
		sections = append(sections, "")
		sections = append(sections, labelStyle.Render("Instagram ID"))
		sections = append(sections, entry.InstagramPostID)
	}

	if entry.Caption != "" {
		sections = append(sections, "")
		sections = append(sections, labelStyle.Render("Caption"))
		preview := entry.Caption
		if len(preview) > 300 {
			preview = preview[:300] + "..."
		}
		sections = append(sections, preview)
	}

	if entry.Location != "" {
		sections = append(sections, "")
		sections = append(sections, labelStyle.Render("Location"))
		sections = append(sections, entry.Location)
	}

	sections = append(sections, "")
	sections = append(sections, labelStyle.Render("Images")+"  "+fmt.Sprintf("%d", entry.ImageCount))
	story := "No"
	if entry.StoryPublished {
		story = "Yes"
	}
	sections = append(sections, labelStyle.Render("Story")+"   "+story)

	if entry.ArchivePath != "" {
		sections = append(sections, "")
		sections = append(sections, labelStyle.Render("Archive"))
		sections = append(sections, dimTextStyle.Render(entry.ArchivePath))
	}

	sections = append(sections, "")
	sections = append(sections, dimTextStyle.Render("─── Keys ───"))
	sections = append(sections, "⏎  Open permalink    Space  Expand/collapse")

	return strings.Join(sections, "\n")
}

func (m AppModel) renderStatusBar(w int) string {
	left := m.statusMsg
	if left == "" {
		parts := []string{}
		if m.watcherDir != "" {
			parts = append(parts, fmt.Sprintf("Watching %s", truncate(m.watcherDir, 30)))
		}
		parts = append(parts, fmt.Sprintf("%d pending", len(m.pendingPosts)))
		parts = append(parts, fmt.Sprintf("%d queued", len(m.queuePosts)))
		parts = append(parts, fmt.Sprintf("%d failed", len(m.deadLetterPosts)))
		if m.publishing != "" {
			parts = append(parts, fmt.Sprintf("Publishing: %s...", m.publishing))
		}
		if m.pipelining != "" {
			parts = append(parts, fmt.Sprintf("Processing: %s...", m.pipelining))
		}
		left = strings.Join(parts, " │ ")
	}
	left = singleLine(left)

	right := "ppw"
	maxLeft := w - lipgloss.Width(right) - 3
	if maxLeft < 1 {
		maxLeft = 1
	}
	left = truncate(left, maxLeft)

	gap := w - lipgloss.Width(left) - lipgloss.Width(right) - 2
	if gap < 1 {
		gap = 1
	}

	return statusBarBg.Width(w).Render(left + strings.Repeat(" ", gap) + right)
}

func (m AppModel) renderRuntimeLogPanel(w, h int) string {
	title := panelTitleDimStyle.Render("Runtime Log")

	var lines []string
	if len(m.runtimeLogs) == 0 {
		lines = append(lines, dimTextStyle.Render("No runtime logs yet"))
	} else {
		for _, line := range tailLines(m.runtimeLogs, maxInt(1, h-2)) {
			lines = append(lines, truncate(line, maxInt(8, w-5)))
		}
	}

	content := strings.Join(lines, "\n")
	return renderPanelChrome(w, h, title, content, false, "")
}

func (m AppModel) renderOverlayFrame(w, h int) string {
	var overlay string

	switch m.overlay {
	case OverlayEditor:
		overlay = m.renderEditorOverlay(w, h)
	case OverlayConfirmApprove:
		overlay = m.renderApproveDialog()
	case OverlayConfirmReject:
		overlay = m.renderRejectDialog()
	case OverlayConfirmPublishAll:
		overlay = m.renderPublishAllDialog()
	case OverlayHelp:
		overlay = m.renderHelpOverlay()
	default:
		return ""
	}
	return lipgloss.Place(w, h, lipgloss.Center, lipgloss.Center, overlay)
}

func (m AppModel) renderEditorOverlay(w, h int) string {
	charCount := len(m.editor.Value())
	counter := fmt.Sprintf("%d/2200 chars", charCount)
	if charCount > 2200 {
		counter = errorTextStyle.Render(counter)
	}

	content := lipgloss.JoinVertical(lipgloss.Left,
		m.editor.View(),
		"",
		counter,
		"",
		dimTextStyle.Render("Enter: save   Shift+Enter/Alt+Enter: newline   Esc: cancel"),
	)

	return overlayBorder.Width(w * 60 / 100).Render(
		labelStyle.Render("Edit Caption") + "\n\n" + content,
	)
}

func (m AppModel) renderApproveDialog() string {
	post := m.selectedPending()
	name := ""
	if post != nil {
		name = post.Manifest.ID
	}

	content := fmt.Sprintf(
		"Approve %q?\n\n[Enter] Add to queue (default)\n[p] Publish now   [q] Add to queue\n[Esc] Cancel",
		name,
	)
	return overlayBorder.Render(labelStyle.Render("Approve Post") + "\n\n" + content)
}

func (m AppModel) renderRejectDialog() string {
	post := m.selectedPending()
	name := ""
	if post != nil {
		name = post.Manifest.ID
	}

	content := fmt.Sprintf("Reject %q?\n\n[y] Yes   [n] No", name)
	return overlayBorder.Render(labelStyle.Render("Reject Post") + "\n\n" + content)
}

func (m AppModel) renderPublishAllDialog() string {
	content := fmt.Sprintf("Publish all %d queued posts?\n\n[Enter] Yes (default)\n[y] Yes   [n] No", len(m.queuePosts))
	return overlayBorder.Render(labelStyle.Render("Publish All") + "\n\n" + content)
}

func (m AppModel) renderHelpOverlay() string {
	help := []string{
		labelStyle.Render("Keybindings"),
		"",
		dimTextStyle.Render("─── Navigation ───"),
		"Tab / Shift-Tab    Cycle panels",
		"1..5               Jump to panel",
		"↑↓ / j k           Navigate items",
		"←→ / h l           Browse images",
		"q                  Quit",
		"?                  This help",
		"",
		dimTextStyle.Render("─── Pending Review ───"),
		"a   Approve (publish or queue)",
		"r   Reject",
		"e   Edit caption",
		"s   Toggle story",
		"R   Re-enrich",
		"⏎   Open image",
		"",
		dimTextStyle.Render("─── Publish Queue ───"),
		"⏎   Publish selected (default)",
		"p   Publish selected",
		"P   Publish all",
		"d   Remove from queue",
		"",
		dimTextStyle.Render("─── Failed (Dead Letter) ───"),
		"r   Retry from failed stage",
		"⏎   Open image",
		"",
		dimTextStyle.Render("─── Published Log ───"),
		"⏎   Open permalink",
		"Space  Expand/collapse",
		"",
		dimTextStyle.Render("─── Config ───"),
		"e   Open config in $EDITOR",
		"",
		dimTextStyle.Render("Press any key to close"),
	}
	return overlayBorder.Render(strings.Join(help, "\n"))
}

// --- Helpers ---

func (m AppModel) panelTitle(text string, panel Panel) string {
	return m.indexedPanelTitle(m.panelIndex(panel), text, m.activePanel == panel)
}

func (m AppModel) indexedPanelTitle(index int, text string, active bool) string {
	value := fmt.Sprintf("[%d]-%s", index, text)
	if active {
		return panelTitleStyle.Render(value)
	}
	return panelTitleDimStyle.Render(value)
}

func (m AppModel) panelIndex(panel Panel) int {
	switch panel {
	case PanelConfig:
		return panelIndexConfig
	case PanelPending:
		return panelIndexPending
	case PanelQueue:
		return panelIndexQueue
	case PanelDeadLetter:
		return panelIndexDead
	case PanelPublished:
		return panelIndexPublished
	default:
		return panelIndexConfig
	}
}

func truncate(s string, max int) string {
	if max < 4 {
		max = 4
	}
	if len(s) > max {
		return s[:max-3] + "..."
	}
	return s
}

func singleLine(s string) string {
	s = strings.ReplaceAll(s, "\r", " ")
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.ReplaceAll(s, "\t", " ")
	return strings.Join(strings.Fields(s), " ")
}

func (m *AppModel) appendRuntimeLog(line string) {
	line = strings.TrimSpace(line)
	if line == "" {
		return
	}
	m.runtimeLogs = append(m.runtimeLogs, line)
	const maxRuntimeLogs = 500
	if len(m.runtimeLogs) > maxRuntimeLogs {
		m.runtimeLogs = m.runtimeLogs[len(m.runtimeLogs)-maxRuntimeLogs:]
	}
}

func tailLines(lines []string, max int) []string {
	if max <= 0 || len(lines) <= max {
		return lines
	}
	return lines[len(lines)-max:]
}

func clampInt(v, min, max int) int {
	if v < min {
		return min
	}
	if v > max {
		return max
	}
	return v
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func renderPanelChrome(w, h int, title, content string, active bool, footer string) string {
	if w < 6 || h < 4 {
		return ""
	}

	borderStyle := panelBorderLineStyle
	footerStyle := panelFooterDimStyle
	if active {
		borderStyle = panelBorderActiveLineStyle
		footerStyle = panelFooterActiveStyle
	}

	innerW := w - 2
	bodyH := h - 2

	titlePrefix := borderStyle.Render("─")
	titlePart := titlePrefix + title
	if lipgloss.Width(titlePart) > innerW {
		maxTitleW := maxInt(1, innerW-1)
		title = lipgloss.NewStyle().MaxWidth(maxTitleW).Render(title)
		titlePart = titlePrefix + title
	}
	topFill := innerW - lipgloss.Width(titlePart)
	if topFill < 0 {
		topFill = 0
	}
	top := borderStyle.Render("╭") + titlePart + borderStyle.Render(strings.Repeat("─", topFill)) + borderStyle.Render("╮")

	bodyLines := strings.Split(content, "\n")
	textW := maxInt(1, innerW-2)
	rows := make([]string, 0, h)
	rows = append(rows, top)
	for i := 0; i < bodyH; i++ {
		line := ""
		if i < len(bodyLines) {
			line = bodyLines[i]
		}
		text := lipgloss.NewStyle().MaxWidth(textW).Width(textW).Render(line)
		row := borderStyle.Render("│") + " " + text + " " + borderStyle.Render("│")
		rows = append(rows, row)
	}

	bottomInner := borderStyle.Render(strings.Repeat("─", innerW))
	if footer != "" {
		foot := footerStyle.Render(footer)
		if lipgloss.Width(foot) > innerW {
			foot = footerStyle.Render(lipgloss.NewStyle().MaxWidth(innerW).Render(footer))
		}
		leftFill := innerW - lipgloss.Width(foot)
		if leftFill < 0 {
			leftFill = 0
		}
		bottomInner = borderStyle.Render(strings.Repeat("─", leftFill)) + foot
	}
	bottom := borderStyle.Render("╰") + bottomInner + borderStyle.Render("╯")
	rows = append(rows, bottom)

	return strings.Join(rows, "\n")
}

func counterText(cursor, total int) string {
	if total <= 0 {
		return "0 of 0"
	}
	index := cursor + 1
	if index < 1 {
		index = 1
	}
	if index > total {
		index = total
	}
	return fmt.Sprintf("%d of %d", index, total)
}

func (m AppModel) pendingCounter() string {
	return counterText(m.pendingCursor, len(m.pendingPosts))
}

func (m AppModel) queueCounter() string {
	return counterText(m.queueCursor, len(m.queuePosts))
}

func (m AppModel) deadCounter() string {
	return counterText(m.deadCursor, len(m.deadLetterPosts))
}

func (m AppModel) publishedCounter() string {
	return counterText(m.publishedLeafCursor(), m.publishedLeafCount())
}

func (m AppModel) publishedLeafCount() int {
	total := 0
	for _, g := range m.logGroups {
		total += len(g.Entries)
	}
	return total
}

func (m AppModel) publishedLeafCursor() int {
	totalLeaves := m.publishedLeafCount()
	if totalLeaves <= 0 {
		return 0
	}

	flatIdx := 0
	leafIdx := -1
	for _, g := range m.logGroups {
		// Header row selected. Map it to the next visible leaf if possible.
		if flatIdx == m.logCursor {
			nextLeaf := leafIdx + 1
			if nextLeaf < 0 {
				nextLeaf = 0
			}
			if nextLeaf >= totalLeaves {
				nextLeaf = totalLeaves - 1
			}
			return nextLeaf
		}
		flatIdx++

		if g.Collapsed {
			continue
		}
		for range g.Entries {
			leafIdx++
			if flatIdx == m.logCursor {
				return leafIdx
			}
			flatIdx++
		}
	}
	return totalLeaves - 1
}
