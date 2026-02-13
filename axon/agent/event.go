package agent

// AgentEvent represents lifecycle events emitted by the agent.
type AgentEvent struct {
	Type       AgentEventType
	Message    *Message
	ToolName   string
	ToolInput  string
	Result     *ToolResult
	Error      error
	Delta      string
	Thinking   string
	Usage      *Usage
}

// AgentEventType identifies the kind of agent lifecycle event.
type AgentEventType string

const (
	EventAgentStart        AgentEventType = "agent_start"
	EventAgentEnd          AgentEventType = "agent_end"
	EventTraceStart        AgentEventType = "trace_start"
	EventTraceEnd          AgentEventType = "trace_end"
	EventMessageStart      AgentEventType = "message_start"
	EventMessageEnd        AgentEventType = "message_end"
	EventMessageAdded      AgentEventType = "message_added"
	EventToolStart         AgentEventType = "tool_start"
	EventToolEnd           AgentEventType = "tool_end"
	EventToolSkipped       AgentEventType = "tool_skipped"
	EventSteeringApplied   AgentEventType = "steering_applied"
	EventError             AgentEventType = "error"
	EventTextDelta         AgentEventType = "text_delta"
	EventTextComplete      AgentEventType = "text_complete"
	EventThinkingDelta     AgentEventType = "thinking_delta"
	EventThinkingComplete  AgentEventType = "thinking_complete"
	EventToolCallDelta     AgentEventType = "tool_call_delta"
	EventToolCallComplete  AgentEventType = "tool_call_complete"
	EventUsage             AgentEventType = "usage"
)
