package registry

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/looplj/axonhub/internal/mcp/protocol"
)

// CollisionPolicy defines how namespace collisions are resolved.
type CollisionPolicy string

const (
	// CollisionPolicyAliasWins uses configured alias mappings to resolve collisions.
	CollisionPolicyAliasWins CollisionPolicy = "alias_wins"
	// CollisionPolicyAutoPrefix auto-prefixes colliding names with channel namespace.
	CollisionPolicyAutoPrefix CollisionPolicy = "auto_prefix"
	// CollisionPolicyReject rejects collisions at build time.
	CollisionPolicyReject CollisionPolicy = "reject"
)

// ErrNamespaceCollision is returned when two channels expose the same name
// and no explicit mapping resolves the collision.
type ErrNamespaceCollision struct {
	Name       string
	Type       string // "tool", "resource", "prompt"
	ChannelA   string
	ChannelB   string
	Downstream string // the conflicting downstream name
}

func (e *ErrNamespaceCollision) Error() string {
	return fmt.Sprintf("namespace collision for %s %q: channel %q vs channel %q", e.Type, e.Downstream, e.ChannelA, e.ChannelB)
}

// NamespaceMapping defines a mapping from downstream name to upstream original name.
type NamespaceMapping struct {
	From string // downstream name (exposed name)
	To   string // upstream original name
	Type string // "tool", "resource", "prompt"
}

// ChannelConfig describes an upstream MCP channel's capabilities and configuration.
type ChannelConfig struct {
	ChannelID  string
	Namespace  string
	Tools      []protocol.Tool
	Resources  []protocol.Resource
	Prompts    []protocol.Prompt
	Mappings   []NamespaceMapping
}

type CapabilityRegistry struct {
	mu       sync.RWMutex
	Tools    map[string]ToolEntry
	Resources map[string]ResourceEntry
	Prompts  map[string]PromptEntry

	// reverse mapping from downstream name to channelID+original name
	toolReverseMap    map[string]struct{ ChannelID, OriginalName string }
	resourceReverseMap map[string]struct{ ChannelID, OriginalURI string }
	promptReverseMap  map[string]struct{ ChannelID, OriginalName string }
}

type ToolEntry struct {
	Name         string
	OriginalName string
	Description  string
	InputSchema  json.RawMessage
	ChannelID    string
}

type ResourceEntry struct {
	URI          string
	OriginalURI  string
	Name         string
	Description  string
	MimeType     string
	ChannelID    string
}

type PromptEntry struct {
	Name         string
	OriginalName string
	Description  string
	Arguments    []PromptArgument
	ChannelID    string
}

type PromptArgument struct {
	Name        string
	Description string
	Required    bool
}

func NewCapabilityRegistry() *CapabilityRegistry {
	return &CapabilityRegistry{
		Tools:    make(map[string]ToolEntry),
		Resources: make(map[string]ResourceEntry),
		Prompts:  make(map[string]PromptEntry),

		toolReverseMap:     make(map[string]struct{ ChannelID, OriginalName string }),
		resourceReverseMap: make(map[string]struct{ ChannelID, OriginalURI string }),
		promptReverseMap:   make(map[string]struct{ ChannelID, OriginalName string }),
	}
}

func (r *CapabilityRegistry) RegisterTool(name, channelID string, desc string, schema json.RawMessage) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.Tools[name] = ToolEntry{
		Name:        name,
		Description: desc,
		InputSchema: schema,
		ChannelID:   channelID,
	}
}

func (r *CapabilityRegistry) RegisterResource(uri, channelID, name, desc, mimeType string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.Resources[uri] = ResourceEntry{
		URI:         uri,
		Name:        name,
		Description: desc,
		MimeType:    mimeType,
		ChannelID:   channelID,
	}
}

func (r *CapabilityRegistry) RegisterPrompt(name, channelID string, desc string, args []PromptArgument) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.Prompts[name] = PromptEntry{
		Name:        name,
		Description: desc,
		Arguments:   args,
		ChannelID:   channelID,
	}
}

func (r *CapabilityRegistry) GetTool(name string) (ToolEntry, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	entry, ok := r.Tools[name]
	return entry, ok
}

func (r *CapabilityRegistry) ListTools() []ToolEntry {
	r.mu.RLock()
	defer r.mu.RUnlock()
	tools := make([]ToolEntry, 0, len(r.Tools))
	for _, t := range r.Tools {
		tools = append(tools, t)
	}
	return tools
}

func (r *CapabilityRegistry) ListResources() []ResourceEntry {
	r.mu.RLock()
	defer r.mu.RUnlock()
	resources := make([]ResourceEntry, 0, len(r.Resources))
	for _, r := range r.Resources {
		resources = append(resources, r)
	}
	return resources
}

func (r *CapabilityRegistry) ListPrompts() []PromptEntry {
	r.mu.RLock()
	defer r.mu.RUnlock()
	prompts := make([]PromptEntry, 0, len(r.Prompts))
	for _, p := range r.Prompts {
		prompts = append(prompts, p)
	}
	return prompts
}

var ErrToolNotFound = errors.New("tool not found")
var ErrResourceNotFound = errors.New("resource not found")
var ErrPromptNotFound = errors.New("prompt not found")

func (r *CapabilityRegistry) BuildFromChannels(channels []ChannelConfig) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	// clear existing state
	r.Tools = make(map[string]ToolEntry)
	r.Resources = make(map[string]ResourceEntry)
	r.Prompts = make(map[string]PromptEntry)
	r.toolReverseMap = make(map[string]struct{ ChannelID, OriginalName string })
	r.resourceReverseMap = make(map[string]struct{ ChannelID, OriginalURI string })
	r.promptReverseMap = make(map[string]struct{ ChannelID, OriginalName string })

	// build alias lookup maps per channel
	channelAliasMaps := make(map[string]map[string]string)
	for _, ch := range channels {
		aliasMap := make(map[string]string)
		for _, m := range ch.Mappings {
			if m.Type == "tool" || m.Type == "resource" || m.Type == "prompt" {
				aliasMap[m.To] = m.From
			}
		}
		channelAliasMaps[ch.ChannelID] = aliasMap
	}

	// collect all tool names per channel for collision detection
	channelToolNames := make(map[string]map[string]bool)
	channelResourceURIs := make(map[string]map[string]bool)
	channelPromptNames := make(map[string]map[string]bool)

	for _, ch := range channels {
		channelToolNames[ch.ChannelID] = make(map[string]bool)
		channelResourceURIs[ch.ChannelID] = make(map[string]bool)
		channelPromptNames[ch.ChannelID] = make(map[string]bool)
		for _, t := range ch.Tools {
			channelToolNames[ch.ChannelID][t.Name] = true
		}
		for _, res := range ch.Resources {
			channelResourceURIs[ch.ChannelID][res.URI] = true
		}
		for _, p := range ch.Prompts {
			channelPromptNames[ch.ChannelID][p.Name] = true
		}
	}

	// detect tool collisions across channels
	for i, chA := range channels {
		for _, chB := range channels[i+1:] {
			for toolName := range channelToolNames[chA.ChannelID] {
				if channelToolNames[chB.ChannelID][toolName] {
					aliasA := channelAliasMaps[chA.ChannelID][toolName]
					aliasB := channelAliasMaps[chB.ChannelID][toolName]
					if aliasA != "" && aliasB == "" {
						continue
					}
					if aliasA == "" && aliasB != "" {
						continue
					}
					if aliasA != "" && aliasB != "" && aliasA == aliasB {
						continue
					}
					return &ErrNamespaceCollision{
						Type:       "tool",
						Name:       toolName,
						ChannelA:   chA.ChannelID,
						ChannelB:   chB.ChannelID,
						Downstream: toolName,
					}
				}
			}
		}
	}

	// detect resource URI collisions across channels
	for i, chA := range channels {
		for _, chB := range channels[i+1:] {
			for uri := range channelResourceURIs[chA.ChannelID] {
				if channelResourceURIs[chB.ChannelID][uri] {
					aliasA := channelAliasMaps[chA.ChannelID][uri]
					aliasB := channelAliasMaps[chB.ChannelID][uri]
					if aliasA != "" && aliasB == "" {
						continue
					}
					if aliasA == "" && aliasB != "" {
						continue
					}
					if aliasA != "" && aliasB != "" && aliasA == aliasB {
						continue
					}
					return &ErrNamespaceCollision{
						Type:       "resource",
						Name:       uri,
						ChannelA:   chA.ChannelID,
						ChannelB:   chB.ChannelID,
						Downstream: uri,
					}
				}
			}
		}
	}

	// detect prompt name collisions across channels
	for i, chA := range channels {
		for _, chB := range channels[i+1:] {
			for promptName := range channelPromptNames[chA.ChannelID] {
				if channelPromptNames[chB.ChannelID][promptName] {
					aliasA := channelAliasMaps[chA.ChannelID][promptName]
					aliasB := channelAliasMaps[chB.ChannelID][promptName]
					if aliasA != "" && aliasB == "" {
						continue
					}
					if aliasA == "" && aliasB != "" {
						continue
					}
					if aliasA != "" && aliasB != "" && aliasA == aliasB {
						continue
					}
					return &ErrNamespaceCollision{
						Type:       "prompt",
						Name:       promptName,
						ChannelA:   chA.ChannelID,
						ChannelB:   chB.ChannelID,
						Downstream: promptName,
					}
				}
			}
		}
	}

	// register tools with alias/mapping support
	for _, ch := range channels {
		aliasMap := channelAliasMaps[ch.ChannelID]
		for _, t := range ch.Tools {
			downstreamName := t.Name
			if alias, ok := aliasMap[t.Name]; ok {
				downstreamName = alias
			} else if ch.Namespace != "" {
				downstreamName = ch.Namespace + "/" + t.Name
			}

			r.Tools[downstreamName] = ToolEntry{
				Name:         downstreamName,
				OriginalName: t.Name,
				Description:  t.Description,
				InputSchema:  t.InputSchema,
				ChannelID:    ch.ChannelID,
			}
			r.toolReverseMap[downstreamName] = struct {
				ChannelID    string
				OriginalName string
			}{ChannelID: ch.ChannelID, OriginalName: t.Name}
		}
	}

	// register resources with alias/mapping support
	for _, ch := range channels {
		aliasMap := channelAliasMaps[ch.ChannelID]
		for _, res := range ch.Resources {
			downstreamName := res.URI
			if alias, ok := aliasMap[res.URI]; ok {
				downstreamName = alias
			} else if ch.Namespace != "" {
				downstreamName = ch.Namespace + "/" + res.URI
			}

			r.Resources[downstreamName] = ResourceEntry{
				URI:          downstreamName,
				OriginalURI:  res.URI,
				Name:         res.Name,
				Description:  res.Description,
				MimeType:     res.MimeType,
				ChannelID:    ch.ChannelID,
			}
			r.resourceReverseMap[downstreamName] = struct {
				ChannelID   string
				OriginalURI string
			}{ChannelID: ch.ChannelID, OriginalURI: res.URI}
		}
	}

	// register prompts with alias/mapping support
	for _, ch := range channels {
		aliasMap := channelAliasMaps[ch.ChannelID]
		for _, p := range ch.Prompts {
			downstreamName := p.Name
			if alias, ok := aliasMap[p.Name]; ok {
				downstreamName = alias
			} else if ch.Namespace != "" {
				downstreamName = ch.Namespace + "/" + p.Name
			}

			args := make([]PromptArgument, len(p.Arguments))
			for i, a := range p.Arguments {
				args[i] = PromptArgument{
					Name:        a.Name,
					Description: a.Description,
					Required:    a.Required,
				}
			}

			r.Prompts[downstreamName] = PromptEntry{
				Name:         downstreamName,
				OriginalName: p.Name,
				Description:  p.Description,
				Arguments:    args,
				ChannelID:    ch.ChannelID,
			}
			r.promptReverseMap[downstreamName] = struct {
				ChannelID    string
				OriginalName string
			}{ChannelID: ch.ChannelID, OriginalName: p.Name}
		}
	}

	return nil
}

func (r *CapabilityRegistry) ResolveTool(downstreamName string) (channelID, upstreamName string, err error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	info, ok := r.toolReverseMap[downstreamName]
	if !ok {
		return "", "", ErrToolNotFound
	}
	return info.ChannelID, info.OriginalName, nil
}

func (r *CapabilityRegistry) ResolveResource(downstreamURI string) (channelID, upstreamURI string, err error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	info, ok := r.resourceReverseMap[downstreamURI]
	if !ok {
		return "", "", ErrResourceNotFound
	}
	return info.ChannelID, info.OriginalURI, nil
}

func (r *CapabilityRegistry) ResolvePrompt(downstreamName string) (channelID, upstreamName string, err error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	info, ok := r.promptReverseMap[downstreamName]
	if !ok {
		return "", "", ErrPromptNotFound
	}
	return info.ChannelID, info.OriginalName, nil
}

func (r *CapabilityRegistry) SortedTools() []ToolEntry {
	r.mu.RLock()
	defer r.mu.RUnlock()
	tools := make([]ToolEntry, 0, len(r.Tools))
	for _, t := range r.Tools {
		tools = append(tools, t)
	}
	sort.Slice(tools, func(i, j int) bool {
		return strings.Compare(tools[i].Name, tools[j].Name) < 0
	})
	return tools
}

func (r *CapabilityRegistry) SortedResources() []ResourceEntry {
	r.mu.RLock()
	defer r.mu.RUnlock()
	resources := make([]ResourceEntry, 0, len(r.Resources))
	for _, res := range r.Resources {
		resources = append(resources, res)
	}
	sort.Slice(resources, func(i, j int) bool {
		return strings.Compare(resources[i].URI, resources[j].URI) < 0
	})
	return resources
}

func (r *CapabilityRegistry) SortedPrompts() []PromptEntry {
	r.mu.RLock()
	defer r.mu.RUnlock()
	prompts := make([]PromptEntry, 0, len(r.Prompts))
	for _, p := range r.Prompts {
		prompts = append(prompts, p)
	}
	sort.Slice(prompts, func(i, j int) bool {
		return strings.Compare(prompts[i].Name, prompts[j].Name) < 0
	})
	return prompts
}
