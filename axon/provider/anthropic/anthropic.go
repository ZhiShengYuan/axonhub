package anthropic

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync/atomic"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
	"github.com/samber/lo"

	"github.com/looplj/axonhub/axon/agent"
	axoncontext "github.com/looplj/axonhub/axon/context"
)

const defaultMaxTokens = 8192

const (
	defaultThreadHeader = "AH-Thread-Id"
	defaultTraceHeader  = "AH-Trace-Id"
)

var requestIndexCounter int64

// Provider implements agent.Provider using the Anthropic SDK.
type Provider struct {
	client          anthropic.Client
	threadHeader    string
	traceHeader     string
	reasoningEffort string
}

// reasoningEffortToBudget maps reasoning effort to thinking budget tokens.
// off: disabled
// low: 5000 tokens
// medium: 15000 tokens
// high: 30000 tokens
func reasoningEffortToBudget(effort string) int64 {
	switch effort {
	case "off", "none":
		return 0
	case "low":
		return 5000
	case "high":
		return 30000
	case "medium":
		return 15000
	default:
		return 0
	}
}

// Option configures the Anthropic provider.
type Option func(*Provider)

// WithThreadHeader sets the header name used for the thread ID.
func WithThreadHeader(header string) Option {
	return func(p *Provider) {
		p.threadHeader = header
	}
}

// WithTraceHeader sets the header name used for the trace ID.
func WithTraceHeader(header string) Option {
	return func(p *Provider) {
		p.traceHeader = header
	}
}

// WithReasoningEffort sets the reasoning effort for the provider.
// Valid values: "off", "low", "medium", "high".
func WithReasoningEffort(effort string) Option {
	return func(p *Provider) {
		p.reasoningEffort = effort
	}
}

// New creates a new Anthropic provider.
func New(baseURL, apiKey string, opts ...Option) *Provider {
	p := &Provider{
		client: anthropic.NewClient(
			option.WithBaseURL(baseURL),
			option.WithAPIKey(apiKey)),
		threadHeader: defaultThreadHeader,
		traceHeader:  defaultTraceHeader,
	}
	for _, opt := range opts {
		opt(p)
	}
	return p
}

// Chat sends messages to the Anthropic API and returns a response.
func (p *Provider) Chat(ctx context.Context, model string, tools []agent.ToolDefinition, messages []agent.Message) (agent.Response, error) {
	system, msgParams := convertMessages(messages)
	toolParams := convertTools(tools)

	params := anthropic.MessageNewParams{
		Model:     anthropic.Model(model),
		MaxTokens: defaultMaxTokens,
		Messages:  msgParams,
	}
	if len(system) > 0 {
		params.System = system
	}
	if len(toolParams) > 0 {
		params.Tools = toolParams
	}

	// Apply reasoning effort configuration
	if budget := reasoningEffortToBudget(p.reasoningEffort); budget > 0 {
		params.Thinking = anthropic.ThinkingConfigParamUnion{
			OfEnabled: &anthropic.ThinkingConfigEnabledParam{
				BudgetTokens: budget,
			},
		}
	}

	var reqOpts []option.RequestOption
	if threadID := axoncontext.ThreadID(ctx); threadID != "" {
		reqOpts = append(reqOpts, option.WithHeader(p.threadHeader, threadID))
	}
	if traceID := axoncontext.TraceID(ctx); traceID != "" {
		reqOpts = append(reqOpts, option.WithHeader(p.traceHeader, traceID))
	}

	resp, err := p.client.Messages.New(ctx, params, reqOpts...)
	if err != nil {
		return agent.Response{}, wrapAPIError(err)
	}

	return convertResponse(resp), nil
}

// wrapAPIError extracts and simplifies Anthropic API errors for better readability.
func wrapAPIError(err error) error {
	var apiErr *anthropic.Error
	if errors.As(err, &apiErr) {
		// Extract error message from the API response body
		if msg := extractErrorMessage(apiErr); msg != "" {
			return fmt.Errorf("anthropic: %s (status %d)", msg, apiErr.StatusCode)
		}
		return fmt.Errorf("anthropic: request failed (status %d)", apiErr.StatusCode)
	}
	return fmt.Errorf("anthropic: %w", err)
}

// extractErrorMessage parses the API error body to get a human-readable message.
func extractErrorMessage(apiErr *anthropic.Error) string {
	raw := apiErr.RawJSON()
	if raw == "" {
		return ""
	}

	// The error body is typically: {"type": "error", "error": {"type": "...", "message": "..."}}
	var body struct {
		Error struct {
			Type    string `json:"type"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if json.Unmarshal([]byte(raw), &body) == nil && body.Error.Message != "" {
		return body.Error.Message
	}
	return ""
}

// convertMessages splits agent messages into system prompts and Anthropic message params.
func convertMessages(messages []agent.Message) ([]anthropic.TextBlockParam, []anthropic.MessageParam) {
	var system []anthropic.TextBlockParam
	var params []anthropic.MessageParam
	processedRequestIndex := make(map[int]bool)

	for _, msg := range messages {
		switch msg.Role {
		case agent.RoleSystem:
			if msg.Content == nil {
				continue
			}
			if msg.Content.Text != nil {
				system = append(system, anthropic.TextBlockParam{Text: *msg.Content.Text})
			} else {
				for _, part := range msg.Content.Parts {
					if part.Type == agent.ContentPartText {
						system = append(system, anthropic.TextBlockParam{Text: part.Text})
					}
				}
			}
		case agent.RoleUser:
			params = append(params, anthropic.NewUserMessage(contentToBlocks(msg)...))
		case agent.RoleAssistant:
			if msg.RequestIndex > 0 && processedRequestIndex[msg.RequestIndex] {
				continue
			}
			blocks := aggregateAssistantBlocks(messages, msg.RequestIndex)
			params = append(params, anthropic.NewAssistantMessage(blocks...))
			if msg.RequestIndex > 0 {
				processedRequestIndex[msg.RequestIndex] = true
			}
		case agent.RoleTool:
			isError := msg.IsError != nil && *msg.IsError
			content := ""
			if msg.Content != nil {
				content = msg.Content.String()
			}
			toolUseID := ""
			if msg.ToolUseID != nil {
				toolUseID = *msg.ToolUseID
			}
			params = append(params, anthropic.NewUserMessage(
				anthropic.NewToolResultBlock(toolUseID, content, isError),
			))
		}
	}

	return system, params
}

func aggregateAssistantBlocks(messages []agent.Message, requestIndex int) []anthropic.ContentBlockParamUnion {
	var thinkingBlocks []anthropic.ContentBlockParamUnion
	var textBlocks []anthropic.ContentBlockParamUnion
	var toolUseBlocks []anthropic.ContentBlockParamUnion

	for _, msg := range messages {
		if msg.Role != agent.RoleAssistant {
			continue
		}
		if requestIndex > 0 && msg.RequestIndex != requestIndex {
			continue
		}

		if msg.Content != nil {
			if msg.Content.Text != nil {
				textBlocks = append(textBlocks, anthropic.NewTextBlock(*msg.Content.Text))
			} else {
				for _, part := range msg.Content.Parts {
					switch part.Type {
					case agent.ContentPartText:
						textBlocks = append(textBlocks, anthropic.NewTextBlock(part.Text))
					case agent.ContentPartThinking:
						thinkingBlocks = append(thinkingBlocks, anthropic.NewThinkingBlock(part.ThinkingSignature, part.Thinking))
					case agent.ContentPartRedactedThinking:
						thinkingBlocks = append(thinkingBlocks, anthropic.NewRedactedThinkingBlock(part.Data))
					}
				}
			}
		}

		if msg.ToolUse != nil {
			var input any
			if msg.ToolUse.Input != "" {
				_ = json.Unmarshal([]byte(msg.ToolUse.Input), &input)
			}
			toolUseBlocks = append(toolUseBlocks, anthropic.NewToolUseBlock(msg.ToolUse.ID, input, msg.ToolUse.Name))
		}
	}

	var blocks []anthropic.ContentBlockParamUnion
	blocks = append(blocks, thinkingBlocks...)
	blocks = append(blocks, textBlocks...)
	blocks = append(blocks, toolUseBlocks...)
	return blocks
}

// contentToBlocks converts a user message content to Anthropic content blocks.
func contentToBlocks(msg agent.Message) []anthropic.ContentBlockParamUnion {
	if msg.Content == nil {
		return nil
	}

	if msg.Content.Text != nil {
		return []anthropic.ContentBlockParamUnion{anthropic.NewTextBlock(*msg.Content.Text)}
	}

	return lo.Map(msg.Content.Parts, func(part agent.ContentPart, _ int) anthropic.ContentBlockParamUnion {
		switch part.Type {
		case agent.ContentPartText:
			return anthropic.NewTextBlock(part.Text)
		case agent.ContentPartImage:
			if part.URL != "" {
				return anthropic.NewImageBlock(anthropic.URLImageSourceParam{URL: part.URL})
			}
			return anthropic.NewImageBlock(anthropic.Base64ImageSourceParam{
				Data:      part.Data,
				MediaType: anthropic.Base64ImageSourceMediaType(part.MimeType),
			})
		default:
			return anthropic.NewTextBlock(part.Text)
		}
	})
}

// assistantToBlocks converts an assistant message to Anthropic content blocks.
func assistantToBlocks(msg agent.Message) []anthropic.ContentBlockParamUnion {
	var blocks []anthropic.ContentBlockParamUnion

	if msg.ToolUse != nil {
		var input any
		if msg.ToolUse.Input != "" {
			_ = json.Unmarshal([]byte(msg.ToolUse.Input), &input)
		}
		blocks = append(blocks, anthropic.NewToolUseBlock(msg.ToolUse.ID, input, msg.ToolUse.Name))
	}

	if msg.Content != nil {
		if msg.Content.Text != nil {
			blocks = append(blocks, anthropic.NewTextBlock(*msg.Content.Text))
		} else {
			for _, part := range msg.Content.Parts {
				switch part.Type {
				case agent.ContentPartText:
					blocks = append(blocks, anthropic.NewTextBlock(part.Text))
				case agent.ContentPartThinking:
					blocks = append(blocks, anthropic.NewThinkingBlock(part.ThinkingSignature, part.Thinking))
				case agent.ContentPartRedactedThinking:
					blocks = append(blocks, anthropic.NewRedactedThinkingBlock(part.Data))
				}
			}
		}
	}

	return blocks
}

// convertTools converts agent tool definitions to Anthropic tool params.
func convertTools(tools []agent.ToolDefinition) []anthropic.ToolUnionParam {
	return lo.Map(tools, func(t agent.ToolDefinition, _ int) anthropic.ToolUnionParam {
		schemaBytes, _ := json.Marshal(t.Parameters)
		var properties any
		var required []string

		var schema struct {
			Properties any      `json:"properties"`
			Required   []string `json:"required"`
		}
		if json.Unmarshal(schemaBytes, &schema) == nil {
			properties = schema.Properties
			required = schema.Required
		}

		return anthropic.ToolUnionParam{
			OfTool: &anthropic.ToolParam{
				Name:        t.Name,
				Description: anthropic.String(t.Description),
				InputSchema: anthropic.ToolInputSchemaParam{
					Properties: properties,
					Required:   required,
				},
			},
		}
	})
}

// convertResponse converts an Anthropic API response to an agent.Response.
func convertResponse(resp *anthropic.Message) agent.Response {
	var msgs []agent.Message
	var parts []agent.ContentPart

	for _, block := range resp.Content {
		switch variant := block.AsAny().(type) {
		case anthropic.TextBlock:
			parts = append(parts, agent.ContentPart{
				Type: agent.ContentPartText,
				Text: variant.Text,
			})
		case anthropic.ThinkingBlock:
			parts = append(parts, agent.ContentPart{
				Type:              agent.ContentPartThinking,
				Thinking:          variant.Thinking,
				ThinkingSignature: variant.Signature,
			})
		case anthropic.RedactedThinkingBlock:
			parts = append(parts, agent.ContentPart{
				Type: agent.ContentPartRedactedThinking,
				Data: variant.Data,
			})
		case anthropic.ToolUseBlock:
			msgs = append(msgs, agent.Message{
				Role: agent.RoleAssistant,
				ToolUse: &agent.ToolUse{
					ID:    variant.ID,
					Name:  variant.Name,
					Input: string(variant.Input),
				},
			})
		}
	}

	if len(parts) > 0 {
		contentMsg := agent.Message{
			Role:    agent.RoleAssistant,
			Content: &agent.Content{Parts: parts},
		}
		msgs = append([]agent.Message{contentMsg}, msgs...)
	}

	requestIndex := int(atomic.AddInt64(&requestIndexCounter, 1))
	for i := range msgs {
		msgs[i].RequestIndex = requestIndex
	}

	return agent.Response{
		Messages:   msgs,
		StopReason: convertStopReason(resp.StopReason),
		Usage: agent.Usage{
			InputTokens:  int(resp.Usage.InputTokens),
			OutputTokens: int(resp.Usage.OutputTokens),
		},
	}
}

// convertStopReason maps Anthropic stop reasons to agent stop reasons.
func convertStopReason(reason anthropic.StopReason) agent.StopReason {
	switch reason {
	case anthropic.StopReasonEndTurn:
		return agent.StopReasonEndTurn
	case anthropic.StopReasonToolUse:
		return agent.StopReasonToolUse
	case anthropic.StopReasonMaxTokens:
		return agent.StopReasonMaxTokens
	default:
		return agent.StopReasonEndTurn
	}
}
