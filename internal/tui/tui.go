// Package tui implements the LazyGit-style review interface for the
// publishing workflow. Users can browse posts, preview images, edit
// captions, and approve/reject posts for publishing.
package tui

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"photography-publishing-workflow/internal/manifest"
)

// Styles
var (
	titleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#FAFAFA")).
			Background(lipgloss.Color("#7D56F4")).
			Padding(0, 1)

	selectedStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#7D56F4")).
			Bold(true)

	dimStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#626262"))

	statusBarStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#FFFDF5")).
			Background(lipgloss.Color("#353533")).
			Padding(0, 1)

	successStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#04B575"))

	warningStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#FFB347"))

	errorStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#FF6B6B"))

	borderStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("#626262")).
			Padding(0, 1)

	activeBorderStyle = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(lipgloss.Color("#7D56F4")).
				Padding(0, 1)
)

// mode represents the current TUI mode.
type mode int

const (
	modeList mode = iota
	modeEdit
)

// Model is the top-level BubbleTea model for the review TUI.
type Model struct {
	posts         []*manifest.Manifest
	manifestPaths []string // paths for writing back
	cursor        int      // selected post index
	imgCursor     int      // selected image within post
	mode          mode
	editor        textarea.Model
	statusMsg     string
	width, height int
	quitting      bool
}

// New creates a new TUI model from a list of manifests and their file paths.
func New(posts []*manifest.Manifest, paths []string) Model {
	ta := textarea.New()
	ta.Placeholder = "Edit caption..."
	ta.SetWidth(60)
	ta.SetHeight(5)

	return Model{
		posts:         posts,
		manifestPaths: paths,
		editor:        ta,
	}
}

func (m Model) Init() tea.Cmd {
	return nil
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case tea.KeyMsg:
		if m.mode == modeEdit {
			return m.updateEditMode(msg)
		}
		return m.updateListMode(msg)
	}

	return m, nil
}

func (m Model) updateListMode(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "q", "ctrl+c":
		m.quitting = true
		return m, tea.Quit

	case "up", "k":
		if m.cursor > 0 {
			m.cursor--
			m.imgCursor = 0
			m.statusMsg = ""
		}

	case "down", "j":
		if m.cursor < len(m.posts)-1 {
			m.cursor++
			m.imgCursor = 0
			m.statusMsg = ""
		}

	case "left", "h":
		if m.imgCursor > 0 {
			m.imgCursor--
		}

	case "right", "l":
		post := m.currentPost()
		if post != nil && m.imgCursor < len(post.Images)-1 {
			m.imgCursor++
		}

	case "enter":
		// Open current image in external viewer
		post := m.currentPost()
		if post != nil && m.imgCursor < len(post.Images) {
			openFile(post.Images[m.imgCursor].Path)
			m.statusMsg = "Opened image in viewer"
		}

	case "a":
		m.approvePost("immediate")

	case "Q":
		m.approvePost("queued")

	case "r":
		m.rejectPost()

	case "e":
		return m.enterEditMode()

	case "s":
		m.toggleStory()

	case "R":
		m.reEnrich()
	}

	return m, nil
}

func (m Model) updateEditMode(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.mode = modeList
		m.statusMsg = "Edit cancelled"
		return m, nil

	case "enter", "ctrl+s":
		// Save edited caption
		post := m.currentPost()
		if post != nil {
			if post.Review == nil {
				post.Review = &manifest.Review{}
			}
			post.Review.FinalCaption = normalizeCaptionText(m.editor.Value())
			post.Review.CaptionEdited = true
			m.writeManifest(m.cursor)
			m.statusMsg = successStyle.Render("Caption saved")
		}
		m.mode = modeList
		return m, nil
	case "shift+enter", "alt+enter":
		m.editor.InsertString("\n")
		return m, nil
	}

	// Pass to textarea
	var cmd tea.Cmd
	m.editor, cmd = m.editor.Update(msg)
	return m, cmd
}

func (m *Model) enterEditMode() (Model, tea.Cmd) {
	post := m.currentPost()
	if post == nil {
		return *m, nil
	}

	caption := currentCaption(post)
	m.editor.SetValue(caption)
	m.editor.Focus()
	m.mode = modeEdit
	m.statusMsg = "Editing caption (Enter save, Shift+Enter newline, Esc cancel)"
	return *m, textarea.Blink
}

func (m *Model) approvePost(publishMode string) {
	post := m.currentPost()
	if post == nil {
		return
	}

	if post.Review == nil {
		post.Review = &manifest.Review{}
	}
	post.Review.Decision = "approved"
	post.Review.PublishMode = publishMode
	post.Review.ReviewedAt = time.Now().UTC()
	if post.Review.FinalCaption == "" && post.Enrichment != nil && post.Enrichment.Caption != nil {
		post.Review.FinalCaption = post.Enrichment.Caption.Text
	}

	if err := post.Transition(manifest.StateApproved); err != nil {
		m.statusMsg = errorStyle.Render(fmt.Sprintf("Error: %v", err))
		return
	}

	m.writeManifest(m.cursor)
	label := "publish now"
	if publishMode == "queued" {
		label = "queued"
	}
	m.statusMsg = successStyle.Render(fmt.Sprintf("Approved (%s): %s", label, post.ID))
	m.removePost(m.cursor)
}

func (m *Model) rejectPost() {
	post := m.currentPost()
	if post == nil {
		return
	}

	if post.Review == nil {
		post.Review = &manifest.Review{}
	}
	post.Review.Decision = "rejected"
	post.Review.ReviewedAt = time.Now().UTC()

	if err := post.Transition(manifest.StateRejected); err != nil {
		m.statusMsg = errorStyle.Render(fmt.Sprintf("Error: %v", err))
		return
	}

	m.writeManifest(m.cursor)
	m.statusMsg = warningStyle.Render(fmt.Sprintf("Rejected: %s", post.ID))
	m.removePost(m.cursor)
}

func (m *Model) toggleStory() {
	post := m.currentPost()
	if post == nil {
		return
	}

	if post.Review == nil {
		post.Review = &manifest.Review{}
	}
	post.Review.StoryEnabled = !post.Review.StoryEnabled
	m.writeManifest(m.cursor)

	state := "ON"
	if !post.Review.StoryEnabled {
		state = "OFF"
	}
	m.statusMsg = fmt.Sprintf("Story: %s", state)
}

func (m *Model) reEnrich() {
	post := m.currentPost()
	if post == nil {
		return
	}

	// Transition back to validated for re-enrichment
	post.State = manifest.StateValidated
	post.Enrichment = nil
	m.writeManifest(m.cursor)
	m.statusMsg = warningStyle.Render(fmt.Sprintf("Re-enrich queued: %s (run ppw enrich)", post.ID))
	m.removePost(m.cursor)
}

func (m *Model) removePost(idx int) {
	m.posts = append(m.posts[:idx], m.posts[idx+1:]...)
	m.manifestPaths = append(m.manifestPaths[:idx], m.manifestPaths[idx+1:]...)
	if m.cursor >= len(m.posts) && m.cursor > 0 {
		m.cursor--
	}
	m.imgCursor = 0

	if len(m.posts) == 0 {
		m.quitting = true
	}
}

func (m *Model) writeManifest(idx int) {
	if idx < len(m.posts) && idx < len(m.manifestPaths) {
		if err := m.posts[idx].Write(m.manifestPaths[idx]); err != nil {
			m.statusMsg = errorStyle.Render(fmt.Sprintf("Write error: %v", err))
		}
	}
}

func (m Model) currentPost() *manifest.Manifest {
	if len(m.posts) == 0 || m.cursor >= len(m.posts) {
		return nil
	}
	return m.posts[m.cursor]
}

// View renders the TUI.
func (m Model) View() string {
	if m.quitting {
		if len(m.posts) == 0 {
			return "All posts reviewed.\n"
		}
		return "Exiting review. Unreviewed posts remain in pending_review.\n"
	}

	if len(m.posts) == 0 {
		return "No posts pending review.\n\nPress q to exit.\n"
	}

	w := m.width
	if w == 0 {
		w = 80
	}
	h := m.height
	if h == 0 {
		h = 24
	}

	// Layout: header + two columns + status bar
	header := titleStyle.Render(" ppw review ")
	postList := m.renderPostList(w/3-2, h-6)
	detail := m.renderDetail(w*2/3-2, h-6)

	leftPanel := borderStyle.Width(w/3 - 4).Render(postList)
	rightPanel := activeBorderStyle.Width(w*2/3 - 4).Render(detail)
	if m.mode == modeEdit {
		leftPanel = borderStyle.Width(w/3 - 4).Render(postList)
		rightPanel = activeBorderStyle.Width(w*2/3 - 4).Render(m.renderEditor(w*2/3-4, h-6))
	}

	body := lipgloss.JoinHorizontal(lipgloss.Top, leftPanel, rightPanel)
	statusBar := m.renderStatusBar(w)

	return lipgloss.JoinVertical(lipgloss.Left, header, body, statusBar)
}

func (m Model) renderPostList(w, h int) string {
	var lines []string
	lines = append(lines, dimStyle.Render("Posts"))
	lines = append(lines, "")

	for i, post := range m.posts {
		imgCount := fmt.Sprintf("[%d]", len(post.Images))
		name := post.ID
		if len(name) > w-6 {
			name = name[:w-9] + "..."
		}

		if i == m.cursor {
			lines = append(lines, selectedStyle.Render(fmt.Sprintf("▸ %s %s", name, imgCount)))
		} else {
			lines = append(lines, fmt.Sprintf("  %s %s", name, imgCount))
		}
	}

	return strings.Join(lines, "\n")
}

func (m Model) renderDetail(w, h int) string {
	post := m.currentPost()
	if post == nil {
		return "No post selected"
	}

	var sections []string

	// Image info
	if m.imgCursor < len(post.Images) {
		img := post.Images[m.imgCursor]
		imgInfo := fmt.Sprintf("%s [%d/%d]  %dx%d  %s",
			img.Filename, m.imgCursor+1, len(post.Images),
			img.Width, img.Height, img.AspectRatio)
		if img.IsHero {
			imgInfo += "  [hero]"
		}
		sections = append(sections, dimStyle.Render("Image")+"\n"+imgInfo)
	}

	// Caption
	caption := currentCaption(post)
	if caption != "" {
		preview := caption
		if len(preview) > 300 {
			preview = preview[:300] + "..."
		}
		sections = append(sections, dimStyle.Render("Caption")+"\n"+preview)
	} else {
		sections = append(sections, dimStyle.Render("Caption")+"\n(not generated)")
	}

	// Location
	if post.Enrichment != nil && post.Enrichment.Location != nil {
		sections = append(sections, dimStyle.Render("Location")+"\n"+
			post.Enrichment.Location.Name+" ("+post.Enrichment.Location.Source+")")
	}

	// Music
	if post.Enrichment != nil && post.Enrichment.MusicSuggestion != nil {
		ms := post.Enrichment.MusicSuggestion
		sections = append(sections, dimStyle.Render("Music")+"\n"+
			ms.Artist+" — "+ms.Title+" ("+ms.Mood+")")
	}

	// Story toggle
	storyState := "OFF"
	if post.Review != nil && post.Review.StoryEnabled {
		storyState = successStyle.Render("ON")
	}
	sections = append(sections, dimStyle.Render("Story")+"\n"+storyState)

	// Keybindings
	keys := []string{
		"",
		dimStyle.Render("─── Actions ───"),
		"a  Approve + Publish    Q  Approve + Queue",
		"e  Edit Caption         r  Reject",
		"s  Toggle Story         R  Re-enrich",
		"←→ Browse images        ↑↓ Browse posts",
		"⏎  Open image           q  Quit",
	}
	sections = append(sections, strings.Join(keys, "\n"))

	return strings.Join(sections, "\n\n")
}

func (m Model) renderEditor(w, h int) string {
	var sections []string
	sections = append(sections, dimStyle.Render("Edit Caption (Enter save, Shift+Enter newline, Esc cancel)"))
	sections = append(sections, m.editor.View())

	charCount := len(m.editor.Value())
	counter := fmt.Sprintf("%d/2200 chars", charCount)
	if charCount > 2200 {
		counter = errorStyle.Render(counter)
	}
	sections = append(sections, counter)

	return strings.Join(sections, "\n\n")
}

func (m Model) renderStatusBar(w int) string {
	left := m.statusMsg
	if left == "" {
		left = fmt.Sprintf("%d post(s) pending review", len(m.posts))
	}

	right := fmt.Sprintf("ppw review")
	gap := w - lipgloss.Width(left) - lipgloss.Width(right) - 2
	if gap < 1 {
		gap = 1
	}

	return statusBarStyle.Width(w).Render(left + strings.Repeat(" ", gap) + right)
}

func currentCaption(post *manifest.Manifest) string {
	if post.Review != nil && post.Review.FinalCaption != "" {
		return normalizeCaptionText(post.Review.FinalCaption)
	}
	if post.Enrichment != nil && post.Enrichment.Caption != nil {
		return normalizeCaptionText(post.Enrichment.Caption.Text)
	}
	return ""
}

// LoadPendingPosts scans a directory for manifests in pending_review state.
func LoadPendingPosts(dir string) ([]*manifest.Manifest, []string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, nil, fmt.Errorf("read directory: %w", err)
	}

	var posts []*manifest.Manifest
	var paths []string

	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		mPath := filepath.Join(dir, e.Name(), "manifest.json")
		m, err := manifest.Read(mPath)
		if err != nil {
			continue // skip directories without valid manifests
		}
		if m.State == manifest.StatePendingReview {
			posts = append(posts, m)
			paths = append(paths, mPath)
		}
	}

	return posts, paths, nil
}

func openFile(path string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", path)
	case "linux":
		cmd = exec.Command("xdg-open", path)
	default:
		cmd = exec.Command("open", path)
	}
	cmd.Start() //nolint:errcheck // best-effort
}
