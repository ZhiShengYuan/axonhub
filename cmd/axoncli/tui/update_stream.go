package tui

import (
	"context"
	"errors"
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/looplj/axonhub/axon/agent"
	axonconf "github.com/looplj/axonhub/axon/conf"
)

func (m Model) handleConfEvent(ev axonconf.ReloadEvent) (tea.Model, tea.Cmd) {
	switch ev.Type {
	case axonconf.ReloadEventRequested:
		m.appendLine("↻ Reload requested")
	case axonconf.ReloadEventStarted:
		if ev.ConfigFile != "" {
			m.appendLine(fmt.Sprintf("↻ Reloading config (%s)...", ev.ConfigFile))
		} else {
			m.appendLine("↻ Reloading config...")
		}
	case axonconf.ReloadEventNoop:
		m.appendLine("✓ Config unchanged")
	case axonconf.ReloadEventFailed:
		if ev.Error != "" {
			lines := strings.Split(ev.Error, "\n")
			for i, line := range lines {
				if i == 0 {
					m.appendLine(fmt.Sprintf("✗ Reload failed: %s", line))
				} else if line != "" {
					m.appendLine(fmt.Sprintf("  %s", line))
				}
			}
		} else {
			m.appendLine("✗ Reload failed")
		}
	case axonconf.ReloadEventApplied:
		if v, ok := ev.Attributes["model"]; ok && v != "" {
			m.model = v
		}
		if len(ev.ChangedKeys) > 0 {
			m.appendLine(fmt.Sprintf("✓ Config applied (%s)", strings.Join(ev.ChangedKeys, ", ")))
		} else {
			m.appendLine("✓ Config applied")
		}
	}

	m.syncViewport()
	return m, waitForConfEvent(m.confEvents)
}

func (m Model) handleAgentEvent(ev agent.AgentEvent) (tea.Model, tea.Cmd) {
	switch ev.Type {
	case agent.EventAgentStart:
		m.processing = true
	case agent.EventAgentEnd:
		m.processing = false
	case agent.EventToolStart:
		m.appendLine(formatToolStart(ev))
	case agent.EventToolEnd:
		if line := formatToolEnd(ev); line != "" {
			m.appendLine(line)
		}
	case agent.EventMessageAdded:
		if ev.Message != nil {
			if m.threadMgr != nil {
				m.threadMgr.AddMessage(m.threadID, *ev.Message)
			}
			if ev.Message.Role == agent.RoleAssistant && ev.Message.Content != nil {
				if line := formatAssistantMessage(*ev.Message, m.width); line != "" {
					m.appendLine(line)
				}
			}
		}
	case agent.EventToolSkipped:
		m.appendLine(fmt.Sprintf("  ⏭ Skipped tool: %s", ev.ToolName))
	case agent.EventSteeringApplied:
		m.appendLine("  ⤷ Steering applied")
	case agent.EventError:
		if ev.Error != nil && !errors.Is(ev.Error, context.Canceled) {
			errStr := fmt.Sprintf("%v", ev.Error)
			lines := strings.Split(errStr, "\n")
			for i, line := range lines {
				if i == 0 {
					m.appendLine(fmt.Sprintf("✗ Error: %s", line))
				} else if line != "" {
					m.appendLine(fmt.Sprintf("  %s", line))
				}
			}
		}
	}

	m.syncViewport()
	return m, waitForAgentEvent(m.agentEvents)
}

func (m *Model) handleStreamEvent(ev agent.AgentEvent) (tea.Model, tea.Cmd) {
	switch ev.Type {
	case agent.EventTextDelta:
		m.streamText.WriteString(ev.Delta)
		m.updateLastStreamingLine()
	case agent.EventTextComplete:
		m.streamText.Reset()
		m.streamingLineIndex = -1
	case agent.EventThinkingDelta:
		// Initialize thinking state if not exists
		if m.thinkingState == nil {
			m.thinkingState = &thinkingState{expanded: true}
		}
		m.thinkingState.content.WriteString(ev.Delta)
		m.thinkingState.complete = false
	case agent.EventThinkingComplete:
		if m.thinkingState != nil {
			m.thinkingState.complete = true
			// Signature is passed in the Thinking field of the event
			if ev.Thinking != "" {
				m.thinkingState.signature = ev.Thinking
			}
			// Add thinking header to display
			m.appendLine(formatThinkingHeader(m.thinkingState.expanded, true, m.width))
			// If expanded, also add the content
			if m.thinkingState.expanded {
				content := m.thinkingState.content.String()
				if content != "" {
					m.appendLine(formatThinkingContent(content, m.width))
				}
			}
		}
	case agent.EventToolStart:
		m.appendLine(formatToolStart(ev))
	case agent.EventToolEnd:
		if line := formatToolEnd(ev); line != "" {
			m.appendLine(line)
		}
	case agent.EventToolCallDelta:
	case agent.EventToolCallComplete:
	case agent.EventMessageAdded:
		if ev.Message != nil {
			if m.threadMgr != nil {
				m.threadMgr.AddMessage(m.threadID, *ev.Message)
			}
		}
	case agent.EventToolSkipped:
		m.appendLine(fmt.Sprintf("  ⏭ Skipped tool: %s", ev.ToolName))
	case agent.EventSteeringApplied:
		m.appendLine("  ⤷ Steering applied")
	case agent.EventError:
		if ev.Error != nil && !errors.Is(ev.Error, context.Canceled) {
			errStr := fmt.Sprintf("%v", ev.Error)
			lines := strings.Split(errStr, "\n")
			for i, line := range lines {
				if i == 0 {
					m.appendLine(fmt.Sprintf("✗ Error: %s", line))
				} else if line != "" {
					m.appendLine(fmt.Sprintf("  %s", line))
				}
			}
		}
		return m, nil
	case agent.EventAgentEnd:
		m.processing = false
		m.streamEvents = nil
		m.streamText.Reset()
		m.streamingLineIndex = -1
		// Reset thinking state for next message
		m.thinkingState = nil
		m.syncViewport()
		return m, nil
	}

	m.syncViewport()
	if m.streamEvents != nil {
		return m, waitForStreamEvent(m.streamEvents)
	}
	return m, nil
}

func (m *Model) updateLastStreamingLine() {
	text := m.streamText.String()
	if text == "" {
		return
	}
	formatted := formatStreamingText(text, m.width)
	if m.streamingLineIndex >= 0 && m.streamingLineIndex < len(m.lines) {
		m.lines[m.streamingLineIndex] = formatted
	} else {
		m.appendLine(formatted)
		m.streamingLineIndex = len(m.lines) - 1
	}
}
