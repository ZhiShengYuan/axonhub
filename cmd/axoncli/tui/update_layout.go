package tui

import (
	"strings"

	"charm.land/bubbles/v2/viewport"
)

func newViewport(width, height int) viewport.Model {
	vp := viewport.New(viewport.WithWidth(width), viewport.WithHeight(height))
	vp.SetContent("")
	return vp
}

func (m *Model) applyWindowSize(width, height int) {
	if width <= 0 || height <= 0 {
		return
	}

	m.updateTextareaHeight()
	vpHeight := height - m.chromeHeight() - m.slashExtraHeight()
	if vpHeight < 1 {
		vpHeight = 1
	}

	if !m.ready {
		m.viewport = newViewport(width, vpHeight)
		m.ready = true
	} else {
		m.viewport.SetWidth(width)
		m.viewport.SetHeight(vpHeight)
	}

	m.textarea.SetWidth(width - inputBoxHorizontalPadding)
	m.syncViewport()
}

func (m *Model) applyLayout() {
	if m.width <= 0 || m.height <= 0 || !m.ready {
		return
	}

	m.updateTextareaHeight()
	vpHeight := max(m.height-m.chromeHeight()-m.slashExtraHeight(), 1)
	m.viewport.SetHeight(vpHeight)
	m.textarea.SetWidth(m.width - inputBoxHorizontalPadding)
}

func (m Model) chromeHeight() int {
	return headerHeight + statusBarHeight + m.textareaHeight + inputBoxPadding + chromePadding
}

func (m *Model) updateTextareaHeight() {
	desired := m.textarea.LineCount()
	if desired < minTextareaHeight {
		desired = minTextareaHeight
	}

	maxAllowed := max(m.height-headerHeight-statusBarHeight-inputBoxPadding-chromePadding-m.slashExtraHeight()-1, minTextareaHeight)
	if desired > maxAllowed {
		desired = maxAllowed
	}

	if m.textareaHeight == desired {
		return
	}

	m.textareaHeight = desired
	m.textarea.SetHeight(desired)

	// When increasing height, we need to ensure the viewport shows all content
	// by moving cursor to the beginning and back to trigger repositioning
	// This must be done AFTER SetHeight
	savedLine := m.textarea.Line()
	m.textarea.MoveToBegin()
	// Restore cursor position
	for i := 0; i < savedLine && i < m.textarea.LineCount(); i++ {
		m.textarea.CursorDown()
	}
}

func (m Model) slashVisibleCount() int {
	if !m.slashActive || len(m.slashMatches) == 0 {
		return 0
	}
	if len(m.slashMatches) < maxSlashVisible {
		return len(m.slashMatches)
	}
	return maxSlashVisible
}

func (m Model) slashExtraHeight() int {
	if m.slashVisibleCount() == 0 {
		return 0
	}
	return m.slashVisibleCount() + 1
}

func (m *Model) appendLine(line string) {
	m.lines = append(m.lines, line)
}

func (m *Model) syncViewport() {
	content := strings.Join(m.lines, "\n")
	m.viewport.SetContent(content)
	m.viewport.GotoBottom()
}
