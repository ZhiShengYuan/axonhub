package tui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

var (
	headerStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("205")).
			Bold(true)

	infoStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("241"))

	statusBarStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("241")).
			PaddingLeft(1)

	processingStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("205"))

	inputBoxStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("241")).
			Padding(0, 0)
)

// View renders the full TUI layout.
func (m Model) View() tea.View {
	var v tea.View
	v.AltScreen = true
	if !m.ready {
		v.SetContent("Initializing...")
		return v
	}

	var b strings.Builder

	b.WriteString(m.renderHeader())
	b.WriteString("\n")
	b.WriteString(m.viewport.View())
	b.WriteString("\n")
	b.WriteString(m.renderStatusBar())
	b.WriteString("\n")
	if m.slashActive {
		b.WriteString(m.renderSlashDropdown())
		b.WriteString("\n")
	}
	// Render textarea inside a bordered box
	b.WriteString(inputBoxStyle.Render(m.textarea.View()))

	v.SetContent(b.String())
	return v
}

func (m Model) renderHeader() string {
	var b strings.Builder
	b.WriteString(headerStyle.Render("╔══════════════════════════════════════╗"))
	b.WriteString("\n")
	b.WriteString(headerStyle.Render("║           Axon Cli                   ║"))
	b.WriteString("\n")
	b.WriteString(headerStyle.Render("╚══════════════════════════════════════╝"))
	b.WriteString("\n")
	b.WriteString(infoStyle.Render(fmt.Sprintf("  Model:     %s", m.model)))
	b.WriteString("\n")
	b.WriteString(infoStyle.Render(fmt.Sprintf("  Workspace: %s", m.workspace)))
	b.WriteString("\n")
	b.WriteString(infoStyle.Render(fmt.Sprintf("  Thread:    thread-%s", m.threadID)))
	return b.String()
}

func (m Model) renderStatusBar() string {
	if m.processing {
		return processingStyle.Render(m.spinner.View() + " Processing...")
	}
	// Show thinking toggle hint when there's thinking content
	if m.thinkingState != nil {
		return statusBarStyle.Render("Enter: send · Shift+Enter/Ctrl+J: newline · ↑↓/PgUp/PgDn: scroll · t: toggle thinking · Esc: cancel · /help: commands")
	}
	return statusBarStyle.Render("Enter: send · Shift+Enter/Ctrl+J: newline · ↑↓/PgUp/PgDn: scroll · Esc: cancel · /help: commands")
}

var (
	slashItemStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("241")).
			PaddingLeft(1)

	slashSelectedStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("0")).
				Background(lipgloss.Color("205")).
				PaddingLeft(1)
)

func (m Model) renderSlashDropdown() string {
	if !m.slashActive || len(m.slashMatches) == 0 {
		return ""
	}

	start := m.slashOffset
	end := start + m.slashVisibleCount()
	if end > len(m.slashMatches) {
		end = len(m.slashMatches)
	}

	var b strings.Builder
	for i := start; i < end; i++ {
		if i > start {
			b.WriteString("\n")
		}
		item := m.slashMatches[i]
		line := fmt.Sprintf("%-14s %s", item.Command, item.Description)
		if i == m.slashIndex {
			b.WriteString(slashSelectedStyle.Render(line))
		} else {
			b.WriteString(slashItemStyle.Render(line))
		}
	}
	return b.String()
}
