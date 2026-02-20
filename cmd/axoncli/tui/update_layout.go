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

func (m Model) viewportTopY() int {
	// renderHeader() currently renders 6 lines, then View() adds a newline before
	// the viewport, making the viewport start at row 6 (zero-based). The
	// headerHeight constant includes this separator newline.
	return max(headerHeight-1, 0)
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
	line = strings.ReplaceAll(line, "\r\n", "\n")
	if strings.Contains(line, "\n") {
		parts := strings.Split(line, "\n")
		m.lines = append(m.lines, parts...)
		return
	}
	m.lines = append(m.lines, line)
}

func (m *Model) syncViewport() {
	atBottom := m.viewport.AtBottom()
	prevYOffset := m.viewport.YOffset()

	content := strings.Join(m.lines, "\n")
	m.viewport.SetContent(content)
	m.rebuildThinkingHeaderViewportLine()

	if atBottom {
		m.viewport.GotoBottom()
		return
	}
	m.viewport.SetYOffset(prevYOffset)
}

func (m *Model) rebuildThinkingHeaderViewportLine() {
	if len(m.thinkingBlocks) == 0 {
		m.thinkingHeaderViewportLine = nil
		return
	}
	if m.thinkingHeaderViewportLine == nil {
		m.thinkingHeaderViewportLine = make(map[int]int, len(m.thinkingBlocks))
	} else {
		clear(m.thinkingHeaderViewportLine)
	}
	for i, block := range m.thinkingBlocks {
		if block == nil {
			continue
		}
		if block.headerLineIndex >= 0 && block.headerLineIndex < len(m.lines) {
			m.thinkingHeaderViewportLine[block.headerLineIndex] = i
		}
	}
}

func (m Model) thinkingBlockIndexAtMouse(x, y int) (int, bool) {
	if len(m.thinkingBlocks) == 0 || m.thinkingHeaderViewportLine == nil {
		return 0, false
	}
	viewportTopY := m.viewportTopY()
	relY := y - viewportTopY
	if relY < 0 || relY >= m.viewport.Height() {
		return 0, false
	}
	absLine := m.viewport.YOffset() + relY
	idx, ok := m.thinkingHeaderViewportLine[absLine]
	if !ok {
		return 0, false
	}
	// Only toggle when clicking the leading indicator area ("▶"/"▼").
	if x > 1 {
		return 0, false
	}
	return idx, true
}

func (m *Model) spliceLines(start, deleteCount int, insert []string) int {
	if deleteCount < 0 {
		deleteCount = 0
	}
	if start < 0 {
		start = 0
	}
	if start > len(m.lines) {
		start = len(m.lines)
	}
	end := start + deleteCount
	if end > len(m.lines) {
		end = len(m.lines)
		deleteCount = end - start
	}

	if len(insert) == 0 && deleteCount == 0 {
		return 0
	}

	before := m.lines[:start]
	after := m.lines[end:]

	// Normalize: never store embedded newlines in a single line entry.
	var normalized []string
	for _, line := range insert {
		line = strings.ReplaceAll(line, "\r\n", "\n")
		if strings.Contains(line, "\n") {
			normalized = append(normalized, strings.Split(line, "\n")...)
		} else {
			normalized = append(normalized, line)
		}
	}
	insert = normalized

	newLines := make([]string, 0, len(before)+len(insert)+len(after))
	newLines = append(newLines, before...)
	newLines = append(newLines, insert...)
	newLines = append(newLines, after...)
	m.lines = newLines

	delta := len(insert) - deleteCount
	m.shiftLineIndices(end, delta)
	return delta
}

func (m *Model) shiftLineIndices(from int, delta int) {
	if delta == 0 {
		return
	}

	if m.streamingStartLineIndex >= from && m.streamingStartLineIndex >= 0 {
		m.streamingStartLineIndex += delta
	}

	for _, block := range m.thinkingBlocks {
		if block == nil {
			continue
		}
		if block.headerLineIndex >= from && block.headerLineIndex >= 0 {
			block.headerLineIndex += delta
		}
		if block.contentStartLineIndex >= from && block.contentStartLineIndex >= 0 {
			block.contentStartLineIndex += delta
		}
	}
}
