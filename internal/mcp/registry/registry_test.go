package registry

import (
	"encoding/json"
	"testing"

	"github.com/looplj/axonhub/internal/mcp/protocol"
)

func makeTool(name, desc string) protocol.Tool {
	return protocol.Tool{
		Name:        name,
		Description: desc,
		InputSchema: json.RawMessage(`{"type": "object"}`),
	}
}

func makeResource(uri, name, desc string) protocol.Resource {
	return protocol.Resource{
		URI:         uri,
		Name:        name,
		Description: desc,
		MimeType:   "application/json",
	}
}

func makePrompt(name, desc string) protocol.Prompt {
	return protocol.Prompt{
		Name:        name,
		Description: desc,
		Arguments:   []protocol.PromptArgument{},
	}
}

func TestBuildFromChannelsDistinct(t *testing.T) {
	channels := []ChannelConfig{
		{
			ChannelID: "channel-a",
			Namespace: "chA",
			Tools: []protocol.Tool{
				makeTool("tool-a", "Tool A from channel A"),
				makeTool("tool-b", "Tool B from channel A"),
			},
			Resources: []protocol.Resource{
				makeResource("file:///a/resource1", "Resource 1", "Resource from A"),
			},
			Prompts: []protocol.Prompt{
				makePrompt("prompt-a", "Prompt A from channel A"),
			},
		},
		{
			ChannelID: "channel-b",
			Namespace: "chB",
			Tools: []protocol.Tool{
				makeTool("tool-c", "Tool C from channel B"),
			},
			Resources: []protocol.Resource{
				makeResource("file:///b/resource2", "Resource 2", "Resource from B"),
			},
			Prompts: []protocol.Prompt{
				makePrompt("prompt-b", "Prompt B from channel B"),
			},
		},
	}

	reg := NewCapabilityRegistry()
	err := reg.BuildFromChannels(channels)
	if err != nil {
		t.Fatalf("BuildFromChannels failed: %v", err)
	}

	tools := reg.SortedTools()
	if len(tools) != 3 {
		t.Errorf("expected 3 tools, got %d", len(tools))
	}

	expectedTools := map[string]string{
		"chA/tool-a": "channel-a",
		"chA/tool-b": "channel-a",
		"chB/tool-c": "channel-b",
	}
	for _, tool := range tools {
		if expectedChannel, ok := expectedTools[tool.Name]; !ok {
			t.Errorf("unexpected tool: %s", tool.Name)
		} else if tool.ChannelID != expectedChannel {
			t.Errorf("tool %s expected channel %s, got %s", tool.Name, expectedChannel, tool.ChannelID)
		}
		if tool.OriginalName == "" {
			t.Error("OriginalName should be set")
		}
	}

	resources := reg.SortedResources()
	if len(resources) != 2 {
		t.Errorf("expected 2 resources, got %d", len(resources))
	}

	prompts := reg.SortedPrompts()
	if len(prompts) != 2 {
		t.Errorf("expected 2 prompts, got %d", len(prompts))
	}
}

func TestBuildFromChannelsCollisionReject(t *testing.T) {
	channels := []ChannelConfig{
		{
			ChannelID: "channel-a",
			Namespace: "chA",
			Tools: []protocol.Tool{
				makeTool("duplicate-tool", "Tool from channel A"),
			},
		},
		{
			ChannelID: "channel-b",
			Namespace: "chB",
			Tools: []protocol.Tool{
				makeTool("duplicate-tool", "Tool from channel B"),
			},
		},
	}

	reg := NewCapabilityRegistry()
	err := reg.BuildFromChannels(channels)
	if err == nil {
		t.Fatal("expected collision error, got nil")
	}

	collisionErr, ok := err.(*ErrNamespaceCollision)
	if !ok {
		t.Fatalf("expected ErrNamespaceCollision, got %T", err)
	}

	if collisionErr.Type != "tool" {
		t.Errorf("expected type 'tool', got %q", collisionErr.Type)
	}
	if collisionErr.ChannelA != "channel-a" {
		t.Errorf("expected ChannelA 'channel-a', got %q", collisionErr.ChannelA)
	}
	if collisionErr.ChannelB != "channel-b" {
		t.Errorf("expected ChannelB 'channel-b', got %q", collisionErr.ChannelB)
	}
}

func TestBuildFromChannelsCollisionAutoPrefix(t *testing.T) {
	channels := []ChannelConfig{
		{
			ChannelID: "channel-a",
			Namespace: "chA",
			Tools: []protocol.Tool{
				makeTool("shared-tool", "Tool from channel A"),
			},
		},
		{
			ChannelID: "channel-b",
			Namespace: "chB",
			Tools: []protocol.Tool{
				makeTool("shared-tool", "Tool from channel B"),
			},
			Mappings: []NamespaceMapping{
				{From: "shared-tool", To: "shared-tool", Type: "tool"},
			},
		},
	}

	reg := NewCapabilityRegistry()
	err := reg.BuildFromChannels(channels)
	if err != nil {
		t.Fatalf("BuildFromChannels failed: %v", err)
	}

	tools := reg.SortedTools()
	if len(tools) != 2 {
		t.Errorf("expected 2 tools (with auto-prefix), got %d", len(tools))
	}

	foundChA := false
	foundChB := false
	for _, tool := range tools {
		if tool.Name == "chA/shared-tool" && tool.ChannelID == "channel-a" {
			foundChA = true
		}
		if tool.Name == "shared-tool" && tool.ChannelID == "channel-b" {
			foundChB = true
		}
	}
	if !foundChA {
		t.Error("expected chA/shared-tool from channel-a")
	}
	if !foundChB {
		t.Error("expected shared-tool from channel-b (alias mapping)")
	}
}

func TestBuildFromChannelsAliasWins(t *testing.T) {
	channels := []ChannelConfig{
		{
			ChannelID: "channel-a",
			Namespace: "chA",
			Tools: []protocol.Tool{
				makeTool("original-tool", "Tool from channel A"),
			},
		},
		{
			ChannelID: "channel-b",
			Namespace: "chB",
			Tools: []protocol.Tool{
				makeTool("original-tool", "Tool from channel B"),
			},
			Mappings: []NamespaceMapping{
				{From: "aliased-tool", To: "original-tool", Type: "tool"},
			},
		},
	}

	reg := NewCapabilityRegistry()
	err := reg.BuildFromChannels(channels)
	if err != nil {
		t.Fatalf("BuildFromChannels failed: %v", err)
	}

	tools := reg.ListTools()
	if len(tools) != 2 {
		t.Errorf("expected 2 tools, got %d", len(tools))
	}

	foundAlias := false
	foundAutoPrefix := false
	for _, tool := range tools {
		if tool.Name == "aliased-tool" && tool.ChannelID == "channel-b" {
			foundAlias = true
			if tool.OriginalName != "original-tool" {
				t.Errorf("expected OriginalName 'original-tool', got %q", tool.OriginalName)
			}
		}
		if tool.Name == "chA/original-tool" && tool.ChannelID == "channel-a" {
			foundAutoPrefix = true
		}
	}
	if !foundAlias {
		t.Error("expected aliased-tool from channel-b")
	}
	if !foundAutoPrefix {
		t.Error("expected chA/original-tool from channel-a (auto-prefixed)")
	}
}

func TestResolveTool(t *testing.T) {
	channels := []ChannelConfig{
		{
			ChannelID: "channel-a",
			Namespace: "chA",
			Tools: []protocol.Tool{
				makeTool("tool-x", "Tool X"),
			},
		},
	}

	reg := NewCapabilityRegistry()
	if err := reg.BuildFromChannels(channels); err != nil {
		t.Fatalf("BuildFromChannels failed: %v", err)
	}

	channelID, upstreamName, err := reg.ResolveTool("chA/tool-x")
	if err != nil {
		t.Fatalf("ResolveTool failed: %v", err)
	}
	if channelID != "channel-a" {
		t.Errorf("expected channel-a, got %s", channelID)
	}
	if upstreamName != "tool-x" {
		t.Errorf("expected tool-x, got %s", upstreamName)
	}

	_, _, err = reg.ResolveTool("nonexistent")
	if err == nil {
		t.Error("expected error for nonexistent tool")
	}
	if err != ErrToolNotFound {
		t.Errorf("expected ErrToolNotFound, got %v", err)
	}
}

func TestResolveResource(t *testing.T) {
	channels := []ChannelConfig{
		{
			ChannelID: "channel-a",
			Namespace: "chA",
			Resources: []protocol.Resource{
				makeResource("file:///test/resource", "TestResource", "A test resource"),
			},
		},
	}

	reg := NewCapabilityRegistry()
	if err := reg.BuildFromChannels(channels); err != nil {
		t.Fatalf("BuildFromChannels failed: %v", err)
	}

	channelID, upstreamURI, err := reg.ResolveResource("chA/file:///test/resource")
	if err != nil {
		t.Fatalf("ResolveResource failed: %v", err)
	}
	if channelID != "channel-a" {
		t.Errorf("expected channel-a, got %s", channelID)
	}
	if upstreamURI != "file:///test/resource" {
		t.Errorf("expected file:///test/resource, got %s", upstreamURI)
	}

	_, _, err = reg.ResolveResource("nonexistent")
	if err == nil {
		t.Error("expected error for nonexistent resource")
	}
	if err != ErrResourceNotFound {
		t.Errorf("expected ErrResourceNotFound, got %v", err)
	}
}

func TestResolvePrompt(t *testing.T) {
	channels := []ChannelConfig{
		{
			ChannelID: "channel-a",
			Namespace: "chA",
			Prompts: []protocol.Prompt{
				makePrompt("prompt-y", "Prompt Y"),
			},
		},
	}

	reg := NewCapabilityRegistry()
	if err := reg.BuildFromChannels(channels); err != nil {
		t.Fatalf("BuildFromChannels failed: %v", err)
	}

	channelID, upstreamName, err := reg.ResolvePrompt("chA/prompt-y")
	if err != nil {
		t.Fatalf("ResolvePrompt failed: %v", err)
	}
	if channelID != "channel-a" {
		t.Errorf("expected channel-a, got %s", channelID)
	}
	if upstreamName != "prompt-y" {
		t.Errorf("expected prompt-y, got %s", upstreamName)
	}

	_, _, err = reg.ResolvePrompt("nonexistent")
	if err == nil {
		t.Error("expected error for nonexistent prompt")
	}
	if err != ErrPromptNotFound {
		t.Errorf("expected ErrPromptNotFound, got %v", err)
	}
}

func TestDeterministicOrdering(t *testing.T) {
	channels := []ChannelConfig{
		{
			ChannelID: "channel-a",
			Namespace: "chA",
			Tools: []protocol.Tool{
				makeTool("zulu", "Tool Z"),
				makeTool("alpha", "Tool A"),
				makeTool("mike", "Tool M"),
			},
		},
		{
			ChannelID: "channel-b",
			Namespace: "chB",
			Tools: []protocol.Tool{
				makeTool("bravo", "Tool B"),
				makeTool("charlie", "Tool C"),
			},
		},
	}

	reg := NewCapabilityRegistry()
	if err := reg.BuildFromChannels(channels); err != nil {
		t.Fatalf("BuildFromChannels failed: %v", err)
	}

	tools := reg.SortedTools()
	names := make([]string, len(tools))
	for i, t := range tools {
		names[i] = t.Name
	}

	sortedNames := make([]string, len(names))
	copy(sortedNames, names)
	for i := range sortedNames {
		for j := i + 1; j < len(sortedNames); j++ {
			if sortedNames[i] > sortedNames[j] {
				sortedNames[i], sortedNames[j] = sortedNames[j], sortedNames[i]
			}
		}
	}

	for i := range names {
		if names[i] != sortedNames[i] {
			t.Errorf("tools not sorted: at index %d expected %s, got %s", i, sortedNames[i], names[i])
		}
	}
}