package mcp

import (
	"strconv"

	"go.uber.org/fx"

	"github.com/looplj/axonhub/internal/mcp/metrics"
	"github.com/looplj/axonhub/internal/objects"
	"github.com/looplj/axonhub/internal/server/biz"
)

var Module = fx.Module("mcp",
	fx.Provide(metrics.NewMetrics),
	fx.Provide(NewMCPChannelMappings),
	fx.Provide(NewProxy),
)

func NewMCPChannelMappings(channelService *biz.ChannelService) (map[string]string, map[string]*objects.MCPCredentials) {
	channelURLs := make(map[string]string)
	channelCreds := make(map[string]*objects.MCPCredentials)

	for _, ch := range channelService.GetEnabledChannels() {
		if ch.BaseURL == "" {
			continue
		}
		if ch.Credentials.MCP == nil {
			continue
		}
		channelURLs[strconv.Itoa(ch.ID)] = ch.BaseURL
		channelCreds[strconv.Itoa(ch.ID)] = ch.Credentials.MCP
	}

	return channelURLs, channelCreds
}