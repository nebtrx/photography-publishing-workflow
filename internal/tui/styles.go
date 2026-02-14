package tui

import "github.com/charmbracelet/lipgloss"

// Shared styles for the unified TUI.
var (
	// Panel borders
	panelBorder = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("#626262")).
			Padding(0, 1)

	activePanelBorder = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(lipgloss.Color("#7D56F4")).
				Padding(0, 1)

	// Panel titles
	panelTitleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#7D56F4"))

	panelTitleDimStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.Color("#626262"))

	// List items
	selectedItemStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#7D56F4")).
				Bold(true)

	normalItemStyle = lipgloss.NewStyle()

	dimTextStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#626262"))

	// Status & feedback
	successTextStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#04B575"))

	warningTextStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#FFB347"))

	errorTextStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#FF6B6B"))

	// Status bar
	statusBarBg = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#FFFDF5")).
			Background(lipgloss.Color("#353533")).
			Padding(0, 1)

	// Title bar
	titleBarStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#FAFAFA")).
			Background(lipgloss.Color("#7D56F4")).
			Padding(0, 1)

	// Overlay
	overlayBorder = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("#7D56F4")).
			Padding(1, 2)

	// Detail section labels
	labelStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#7D56F4")).
			Bold(true)

	// Month group header in published log
	monthHeaderStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#FFB347")).
				Bold(true)
)
