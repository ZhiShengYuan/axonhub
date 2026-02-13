package thread

import (
	"context"
	"fmt"

	"github.com/samber/lo"

	"github.com/looplj/axonhub/axon/agent"
)

// Summarizer generates summaries of conversation messages.
type Summarizer interface {
	Summarize(ctx context.Context, messages []agent.Message) (string, error)
}

// MemoryConfig holds configuration for Memory.
type MemoryConfig struct {
	MaxMessages int        // Maximum number of messages to keep (0 = unlimited)
	Summarizer  Summarizer // Optional summarizer (nil = just truncate without summary)
}

// Memory manages conversation context to fit within LLM context windows.
// It handles message truncation and optional summarization of older messages.
type Memory struct {
	manager     *Manager
	maxMessages int
	summarizer  Summarizer
}

// NewMemory creates a new Memory with the given thread Manager and config.
func NewMemory(manager *Manager, config MemoryConfig) *Memory {
	return &Memory{
		manager:     manager,
		maxMessages: config.MaxMessages,
		summarizer:  config.Summarizer,
	}
}

// Append adds a message to the thread and triggers compaction if needed.
func (m *Memory) Append(ctx context.Context, threadID string, msg agent.Message) error {
	m.manager.AddMessage(threadID, msg)

	if m.maxMessages > 0 {
		return m.compact(ctx, threadID)
	}
	return nil
}

// Messages returns the messages for context, prepending the summary (if any)
// as a system message.
func (m *Memory) Messages(threadID string) []agent.Message {
	messages := m.manager.GetHistory(threadID)
	summary := m.manager.GetSummary(threadID)

	if summary == "" {
		return messages
	}

	summaryMsg := agent.Message{
		Role: agent.RoleSystem,
		Content: &agent.Content{
			Text: lo.ToPtr(fmt.Sprintf("Summary of previous conversation:\n%s", summary)),
		},
	}

	return append([]agent.Message{summaryMsg}, messages...)
}

// compact checks if the thread exceeds maxMessages and if so:
// 1. If summarizer is set: summarize the oldest messages being removed, merge with existing summary
// 2. Truncate to keep only the last maxMessages
func (m *Memory) compact(ctx context.Context, threadID string) error {
	messages := m.manager.GetHistory(threadID)
	if len(messages) <= m.maxMessages {
		return nil
	}

	overflow := messages[:len(messages)-m.maxMessages]

	if m.summarizer != nil {
		existing := m.manager.GetSummary(threadID)

		toSummarize := overflow
		if existing != "" {
			contextMsg := agent.Message{
				Role: agent.RoleSystem,
				Content: &agent.Content{
					Text: lo.ToPtr(fmt.Sprintf("Previous summary:\n%s", existing)),
				},
			}
			toSummarize = append([]agent.Message{contextMsg}, overflow...)
		}

		summary, err := m.summarizer.Summarize(ctx, toSummarize)
		if err != nil {
			return fmt.Errorf("failed to summarize messages: %w", err)
		}

		m.manager.SetSummary(threadID, summary)
	}

	m.manager.TruncateHistory(threadID, m.maxMessages)
	return nil
}
