package agent

// Role represents a message sender role.
type Role string

const (
	// RoleSystem represents a system-level message.
	RoleSystem Role = "system"
	// RoleUser represents a user message.
	RoleUser Role = "user"
	// RoleAssistant represents an assistant message.
	RoleAssistant Role = "assistant"
	// RoleTool represents a tool result message.
	RoleTool Role = "tool"
)

// Message represents a conversation message.
type Message struct {
	Role Role `json:"role"`

	// For normal message.
	Content *Content `json:"content,omitempty"`

	// For tool result
	ToolUseID *string `json:"tool_call_id,omitempty"`
	IsError   *bool   `json:"is_error,omitempty"`

	// For tool use message
	// One tool use per message, it is helpful to handle parallel tool use.
	ToolUse *ToolUse `json:"tool_use,omitempty"`

	// RequestIndex groups messages from the same API response.
	// Messages with the same RequestIndex should be aggregated into a single API request message.
	RequestIndex int `json:"request_index,omitempty"`
}

// ToolUse represents an AI tool invocation.
type ToolUse struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Input string `json:"arguments"` // JSON arguments (valid JSON only when complete)
}
