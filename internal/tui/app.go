package tui

import (
	"fmt"
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
	PanelPublished
	panelCount = 4
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
}

// AppModel is the unified TUI model.
type AppModel struct {
	cfg    *config.Config
	cfgErr string // non-empty if config failed to load

	// Panel state
	activePanel  Panel
	pendingPosts []PostEntry
	queuePosts   []PostEntry
	logGroups    []LogGroup

	// Cursors (per panel)
	pendingCursor int
	queueCursor   int
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

	// Background state
	watcherCh  chan tea.Msg // channel for watcher events; nil if watcher not started
	publishing string      // post ID currently being published, or ""
	pipelining string      // post ID currently in pipeline, or ""
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
	}

	// Set up watcher channel if pipeline and watch dir are available
	if m.pipe != nil && watchDir != "" {
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
		startWatcherGoroutine(m.watcherDir, m.pipe, m.watcherCh)
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
			m.statusMsg = errorTextStyle.Render(fmt.Sprintf("Pipeline error: %v", msg.Err))
		} else {
			m.statusMsg = successTextStyle.Render(fmt.Sprintf("New post ready: %s", msg.PostID))
			m.loadData()
		}
		// Re-subscribe to watcher channel
		return m, waitForWatcher(m.watcherCh)

	case PublishCompleteMsg:
		m.publishing = ""
		if msg.Err != nil {
			m.statusMsg = errorTextStyle.Render(fmt.Sprintf("Publish error: %v", msg.Err))
			m.loadData()
		} else {
			m.statusMsg = successTextStyle.Render(fmt.Sprintf("Published: %s", msg.Permalink))
			m.loadData()
			// Auto-archive if archiver is configured
			if m.arch != nil && msg.ManifestPath != "" {
				return m, runArchive(m.arch, msg.ManifestPath)
			}
		}
		return m, nil

	case ArchiveCompleteMsg:
		if msg.Err != nil {
			m.statusMsg = errorTextStyle.Render(fmt.Sprintf("Archive error: %v", msg.Err))
		} else {
			m.statusMsg = successTextStyle.Render(fmt.Sprintf("Archived: %s", msg.PostID))
		}
		m.loadData()
		return m, nil

	case StatusMsg:
		m.statusMsg = msg.Text
		// Re-subscribe to watcher channel (StatusMsg comes from watcher)
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
	}

	// Panel-specific keys
	switch m.activePanel {
	case PanelConfig:
		return m.updateConfig(msg)
	case PanelPending:
		return m.updatePending(msg)
	case PanelQueue:
		return m.updateQueue(msg)
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
		m.reEnrich()
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
		post := m.selectedQueue()
		if post != nil && m.imgCursor < len(post.Manifest.Images) {
			openFile(post.Manifest.Images[m.imgCursor].Path)
		}
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
	case "ctrl+s":
		if m.editingPost != nil {
			post := m.editingPost.Manifest
			if post.Review == nil {
				post.Review = &manifest.Review{}
			}
			post.Review.FinalCaption = m.editor.Value()
			post.Review.CaptionEdited = true
			post.Write(m.editingPost.Path)
			m.statusMsg = successTextStyle.Render("Caption saved")
		}
		m.overlay = OverlayNone
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
				m.statusMsg = warningTextStyle.Render("Approved (publish manually — publisher not configured)")
			}
			return m, nil
		case "q":
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
		case "y":
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
		m.statusMsg = errorTextStyle.Render(fmt.Sprintf("Error: %v", err))
		return
	}
	post.Manifest.Write(post.Path)

	if publishMode == "queued" {
		m.statusMsg = successTextStyle.Render(fmt.Sprintf("Queued: %s", post.Manifest.ID))
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
		m.statusMsg = errorTextStyle.Render(fmt.Sprintf("Error: %v", err))
		return
	}
	post.Manifest.Write(post.Path)
	m.statusMsg = warningTextStyle.Render(fmt.Sprintf("Rejected: %s", post.Manifest.ID))
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

func (m *AppModel) reEnrich() {
	post := m.selectedPending()
	if post == nil {
		return
	}
	post.Manifest.State = manifest.StateValidated
	post.Manifest.Enrichment = nil
	post.Manifest.Write(post.Path)
	m.statusMsg = warningTextStyle.Render(fmt.Sprintf("Re-enrich queued: %s", post.Manifest.ID))
	m.loadData()
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
	m.statusMsg = warningTextStyle.Render(fmt.Sprintf("Dequeued: %s", post.Manifest.ID))
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
		return nil
	}
	if m.pub == nil {
		m.statusMsg = errorTextStyle.Render("Publisher not configured (need R2 + Instagram env vars)")
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
		m.statusMsg = errorTextStyle.Render("Publisher not configured (need R2 + Instagram env vars)")
		return nil
	}
	m.publishing = m.queuePosts[0].Manifest.ID
	m.statusMsg = fmt.Sprintf("Publishing %d posts...", len(m.queuePosts))
	var cmds []tea.Cmd
	for _, post := range m.queuePosts {
		cmds = append(cmds, runPublish(m.pub, post.Path))
	}
	return tea.Batch(cmds...)
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

	// Render left panels
	configPanel := m.renderConfigPanel(leftW)
	pendingPanel := m.renderPendingPanel(leftW)
	queuePanel := m.renderQueuePanel(leftW)
	publishedPanel := m.renderPublishedPanel(leftW)

	// Render right detail panel
	detailPanel := m.renderDetailPanel(rightW, h-4)

	// Compose left column
	leftCol := lipgloss.JoinVertical(lipgloss.Left,
		configPanel,
		pendingPanel,
		queuePanel,
		publishedPanel,
	)

	// Compose body
	body := lipgloss.JoinHorizontal(lipgloss.Top, leftCol, " ", detailPanel)

	// Status bar
	statusBar := m.renderStatusBar(w)

	result := lipgloss.JoinVertical(lipgloss.Left, body, statusBar)

	// Render overlay on top if active
	if m.overlay != OverlayNone {
		result = m.renderOverlay(result, w, h)
	}

	return result
}

func (m AppModel) renderConfigPanel(w int) string {
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

	content := strings.Join(lines, "\n")
	title := m.panelTitle("Config", PanelConfig)
	border := m.borderForPanel(PanelConfig)
	return border.Width(w).Render(title + "\n" + content)
}

func (m AppModel) renderPendingPanel(w int) string {
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

	content := strings.Join(lines, "\n")
	border := m.borderForPanel(PanelPending)
	return border.Width(w).Render(title + "\n" + content)
}

func (m AppModel) renderQueuePanel(w int) string {
	title := m.panelTitle(fmt.Sprintf("Publish Queue (%d)", len(m.queuePosts)), PanelQueue)

	var lines []string
	if len(m.queuePosts) == 0 {
		lines = append(lines, dimTextStyle.Render("(empty)"))
	}
	for i, post := range m.queuePosts {
		name := truncate(post.Manifest.ID, w-10)
		imgCount := fmt.Sprintf("[%d]", len(post.Manifest.Images))
		line := fmt.Sprintf("  %s %s", name, imgCount)
		if i == m.queueCursor && m.activePanel == PanelQueue {
			line = selectedItemStyle.Render(fmt.Sprintf("▸ %s %s", name, imgCount))
		}
		lines = append(lines, line)
	}

	content := strings.Join(lines, "\n")
	border := m.borderForPanel(PanelQueue)
	return border.Width(w).Render(title + "\n" + content)
}

func (m AppModel) renderPublishedPanel(w int) string {
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

	content := strings.Join(lines, "\n")
	border := m.borderForPanel(PanelPublished)
	return border.Width(w).Render(title + "\n" + content)
}

func (m AppModel) renderDetailPanel(w, h int) string {
	title := labelStyle.Render("Detail")
	var content string

	switch m.activePanel {
	case PanelConfig:
		content = m.renderConfigDetail(w)
	case PanelPending:
		content = m.renderPostDetail(m.selectedPending(), w, true)
	case PanelQueue:
		content = m.renderPostDetail(m.selectedQueue(), w, false)
	case PanelPublished:
		content = m.renderLogDetail(w)
	}

	border := activePanelBorder
	return border.Width(w).Height(h).Render(title + "\n\n" + content)
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
		sections = append(sections, "p  Publish now      P  Publish all")
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
		if m.publishing != "" {
			parts = append(parts, fmt.Sprintf("Publishing: %s...", m.publishing))
		}
		if m.pipelining != "" {
			parts = append(parts, fmt.Sprintf("Processing: %s...", m.pipelining))
		}
		left = strings.Join(parts, " │ ")
	}

	right := "ppw"
	gap := w - lipgloss.Width(left) - lipgloss.Width(right) - 2
	if gap < 1 {
		gap = 1
	}

	return statusBarBg.Width(w).Render(left + strings.Repeat(" ", gap) + right)
}

func (m AppModel) renderOverlay(base string, w, h int) string {
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
		return base
	}

	return placeOverlay(base, overlay, w, h)
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
		dimTextStyle.Render("Ctrl+S: save   Esc: cancel"),
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

	content := fmt.Sprintf("Approve %q?\n\n[p] Publish now   [q] Add to queue\n[Esc] Cancel", name)
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
	content := fmt.Sprintf("Publish all %d queued posts?\n\n[y] Yes   [n] No", len(m.queuePosts))
	return overlayBorder.Render(labelStyle.Render("Publish All") + "\n\n" + content)
}

func (m AppModel) renderHelpOverlay() string {
	help := []string{
		labelStyle.Render("Keybindings"),
		"",
		dimTextStyle.Render("─── Navigation ───"),
		"Tab / Shift-Tab    Cycle panels",
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
		"p   Publish selected",
		"P   Publish all",
		"d   Remove from queue",
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
	if m.activePanel == panel {
		return panelTitleStyle.Render(text)
	}
	return panelTitleDimStyle.Render(text)
}

func (m AppModel) borderForPanel(panel Panel) lipgloss.Style {
	if m.activePanel == panel {
		return activePanelBorder
	}
	return panelBorder
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

// placeOverlay centers an overlay string on top of a base string.
func placeOverlay(base, overlay string, w, h int) string {
	overlayLines := strings.Split(overlay, "\n")
	baseLines := strings.Split(base, "\n")

	// Pad base to h lines
	for len(baseLines) < h {
		baseLines = append(baseLines, "")
	}

	overlayH := len(overlayLines)
	overlayW := 0
	for _, line := range overlayLines {
		if lipgloss.Width(line) > overlayW {
			overlayW = lipgloss.Width(line)
		}
	}

	startY := (h - overlayH) / 2
	startX := (w - overlayW) / 2
	if startY < 0 {
		startY = 0
	}
	if startX < 0 {
		startX = 0
	}

	for i, overlayLine := range overlayLines {
		y := startY + i
		if y >= len(baseLines) {
			break
		}

		baseLine := baseLines[y]
		// Pad base line to width
		for lipgloss.Width(baseLine) < startX {
			baseLine += " "
		}

		// Replace portion with overlay
		before := ""
		if startX > 0 && lipgloss.Width(baseLine) >= startX {
			// Take first startX chars (approximate)
			runes := []rune(baseLine)
			if startX < len(runes) {
				before = string(runes[:startX])
			} else {
				before = baseLine
			}
		}
		baseLines[y] = before + overlayLine
	}

	return strings.Join(baseLines, "\n")
}
