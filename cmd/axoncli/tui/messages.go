package tui

import (
	"github.com/looplj/axonhub/axon/agent"
	axonconf "github.com/looplj/axonhub/axon/conf"

	tea "charm.land/bubbletea/v2"
)

// agentEventMsg wraps an agent.AgentEvent received from the event channel.
type agentEventMsg struct {
	event agent.AgentEvent
}

// agentDoneMsg signals that agent.Process has completed.
type agentDoneMsg struct {
	err error
}

// processMsg triggers a new agent.Process call with the given content.
type processMsg struct {
	content agent.Content
}

type confEventMsg struct {
	event axonconf.ReloadEvent
}

type confReloadDoneMsg struct {
	err error
}

type streamEventMsg struct {
	event agent.AgentEvent
}

type streamDoneMsg struct {
	err error
}

func waitForStreamEvent(ch <-chan agent.AgentEvent) tea.Cmd {
	return func() tea.Msg {
		ev, ok := <-ch
		if !ok {
			return streamDoneMsg{}
		}
		return streamEventMsg{event: ev}
	}
}

func waitForAgentEvent(ch <-chan agent.AgentEvent) tea.Cmd {
	return func() tea.Msg {
		ev, ok := <-ch
		if !ok {
			return agentDoneMsg{}
		}
		return agentEventMsg{event: ev}
	}
}

func waitForConfEvent(ch <-chan axonconf.ReloadEvent) tea.Cmd {
	return func() tea.Msg {
		ev, ok := <-ch
		if !ok {
			return confReloadDoneMsg{}
		}
		return confEventMsg{event: ev}
	}
}
