package tui

import "github.com/charmbracelet/lipgloss"

// Shared styles for the unified TUI.
var (
	panelBorderLineStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#8A84BF"))

	panelBorderActiveLineStyle = lipgloss.NewStyle().
					Foreground(lipgloss.Color("#3FD7A3"))

	panelFooterDimStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#C5BBFF")).
				Bold(true)

	panelFooterActiveStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#3FD7A3")).
				Bold(true)

	// Panel borders
	panelBorder = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("#8A84BF")).
			Padding(0, 1)

	activePanelBorder = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(lipgloss.Color("#3FD7A3")).
				Padding(0, 1)

	// Panel titles
	panelTitleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#3FD7A3"))

	panelTitleDimStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.Color("#C5BBFF"))

	// List items
	selectedItemStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#3FD7A3")).
				Bold(true)

	normalItemStyle = lipgloss.NewStyle()

	dimTextStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#8D86A8"))

	// Status & feedback
	successTextStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#3FD7A3"))

	warningTextStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#F5C27A"))

	errorTextStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#FF7D96"))

	// Status bar
	statusBarBg = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#F7E5B7")).
			Background(lipgloss.Color("#2D293A")).
			Padding(0, 1)

	// Title bar
	titleBarStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#F5F3FF")).
			Background(lipgloss.Color("#3E3852")).
			Padding(0, 1)

	// Overlay
	overlayBorder = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("#3FD7A3")).
			Padding(1, 2)

	// Detail section labels
	labelStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#C5BBFF")).
			Bold(true)

	// Month group header in published log
	monthHeaderStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#F5C27A")).
				Bold(true)
)
