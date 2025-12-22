package openrouter

import (
	"strings"

	"github.com/samber/lo"

	"github.com/looplj/axonhub/internal/llm"
	"github.com/looplj/axonhub/internal/llm/transformer/openai"
)

type Response struct {
	openai.Response

	Choices []Choice `json:"choices"`
}

func (r *Response) ToOpenAIResponse() *openai.Response {
	for _, choice := range r.Choices {
		r.Response.Choices = append(r.Response.Choices, choice.ToLLMChoice())
	}

	return &r.Response
}

type Choice struct {
	llm.Choice

	Message *Message `json:"message,omitempty"`
	Delta   *Message `json:"delta,omitempty"`
}

type Image llm.MessageContentPart

func (c *Choice) ToLLMChoice() llm.Choice {
	if c.Message != nil {
		c.Choice.Message = lo.ToPtr(c.Message.ToLLMMessage())
	}

	if c.Delta != nil {
		c.Choice.Delta = lo.ToPtr(c.Delta.ToLLMMessage())
	}

	return c.Choice
}

// Message is the message content from the OpenRouter response.
// The difference from llm.Message is that it has Reasoning field.
type Message struct {
	llm.Message

	Reasoning        *string           `json:"reasoning,omitempty"`
	ReasoningDetails []ReasoningDetail `json:"reasoning_details,omitempty"`
	Images           []Image           `json:"images,omitempty"`
}

type ReasoningDetail struct {
	Type   string `json:"type"`
	Text   string `json:"text"`
	Format string `json:"format"`
	Index  int    `json:"index"`
}

func (m *Message) ToLLMMessage() llm.Message {
	// Handle reasoning content - prefer reasoning_details if available, fallback to reasoning
	if len(m.ReasoningDetails) > 0 {
		var reasoningText strings.Builder
		for _, detail := range m.ReasoningDetails {
			reasoningText.WriteString(detail.Text)
		}

		reasoning := reasoningText.String()
		m.ReasoningContent = &reasoning
	} else {
		m.ReasoningContent = m.Reasoning
	}

	if len(m.Images) > 0 {
		var parts []llm.MessageContentPart
		if m.Content.Content != nil && *m.Content.Content != "" {
			parts = append(parts, llm.MessageContentPart{
				Type: "text",
				Text: m.Content.Content,
			})
		} else {
			parts = m.Content.MultipleContent
		}

		for _, image := range m.Images {
			parts = append(parts, llm.MessageContentPart(image))
		}

		m.Content = llm.MessageContent{MultipleContent: parts}
	}

	return m.Message
}
