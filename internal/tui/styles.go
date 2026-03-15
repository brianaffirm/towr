package tui

import "github.com/charmbracelet/lipgloss"

var (
	// Status colors matching the CLI color system.
	statusReady      = lipgloss.NewStyle().Foreground(lipgloss.Color("2")) // green
	statusRunning    = lipgloss.NewStyle().Foreground(lipgloss.Color("3")) // yellow
	statusValidating = lipgloss.NewStyle().Foreground(lipgloss.Color("3")) // yellow
	statusBlocked    = lipgloss.NewStyle().Foreground(lipgloss.Color("1")) // red
	statusOrphaned   = lipgloss.NewStyle().Foreground(lipgloss.Color("1")) // red
	statusLanded     = lipgloss.NewStyle().Foreground(lipgloss.Color("2")) // green
	statusArchived   = lipgloss.NewStyle().Faint(true)
	statusDefault    = lipgloss.NewStyle()
	mergedStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("2")).Italic(true)

	// Layout styles.
	headerStyle    = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("6")) // cyan
	headerColStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("6"))
	footerStyle    = lipgloss.NewStyle().Faint(true)
	selectedRow    = lipgloss.NewStyle().Italic(true)
	normalRow      = lipgloss.NewStyle()
	dimStyle       = lipgloss.NewStyle().Faint(true)
	boldStyle      = lipgloss.NewStyle().Bold(true)

	// Diff colors.
	diffAdded   = lipgloss.NewStyle().Foreground(lipgloss.Color("2"))
	diffRemoved = lipgloss.NewStyle().Foreground(lipgloss.Color("1"))

	// Detail view styles.
	fileListStyle   = lipgloss.NewStyle().Padding(0, 1)
	diffPanelStyle  = lipgloss.NewStyle().Padding(0, 1)
	activeFileStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("6"))
	normalFileStyle = lipgloss.NewStyle()

	// Mission control styles.
	planNameStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("6")).Bold(true) // cyan bold
)

// colorStatus returns styled status text.
func colorStatus(status string) string {
	switch status {
	case "READY":
		return statusReady.Render(status)
	case "RUNNING":
		return statusRunning.Render(status)
	case "VALIDATING":
		return statusValidating.Render(status)
	case "BLOCKED":
		return statusBlocked.Render(status)
	case "ORPHANED":
		return statusOrphaned.Render(status)
	case "LANDED":
		return statusLanded.Render(status)
	case "ARCHIVED":
		return statusArchived.Render(status)
	default:
		return statusDefault.Render(status)
	}
}
