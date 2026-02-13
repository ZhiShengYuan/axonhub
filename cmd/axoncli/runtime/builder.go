package runtime

import (
	"strings"

	"github.com/looplj/axonhub/axon/agent"
	"github.com/looplj/axonhub/axon/provider/anthropic"
	cliconf "github.com/looplj/axonhub/cmd/axoncli/conf"
)

func BuildProvider(cfg cliconf.Config) agent.Provider {
	baseURL := strings.TrimRight(cfg.BaseURL, "/")
	var providerOpts []anthropic.Option
	if cfg.TraceHeader != "" {
		providerOpts = append(providerOpts, anthropic.WithTraceHeader(cfg.TraceHeader))
	}
	if cfg.ThreadHeader != "" {
		providerOpts = append(providerOpts, anthropic.WithThreadHeader(cfg.ThreadHeader))
	}
	if cfg.ReasoningEffort != "" {
		providerOpts = append(providerOpts, anthropic.WithReasoningEffort(cfg.ReasoningEffort))
	}
	return anthropic.New(baseURL+"/anthropic", cfg.APIKey, providerOpts...)
}
