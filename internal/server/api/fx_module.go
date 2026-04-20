package api

import (
	"go.uber.org/fx"

	"github.com/looplj/axonhub/internal/mcp"
	"github.com/looplj/axonhub/internal/server/biz"
)

var Module = fx.Module("api",
	fx.Provide(NewOpenAIHandlers),
	fx.Provide(NewAnthropicHandlers),
	fx.Provide(NewGeminiHandlers),
	fx.Provide(NewAiSDKHandlers),
	fx.Provide(NewPlaygroundHandlers),
	fx.Provide(NewSystemHandlers),
	fx.Provide(NewAuthHandlers),
	fx.Provide(NewAPIKeyHandlers),
	fx.Provide(NewJinaHandlers),
	fx.Provide(NewDoubaoHandlers),
	fx.Provide(NewCodexHandlers),
	fx.Provide(NewClaudeCodeHandlers),
	fx.Provide(NewAntigravityHandlers),
	fx.Provide(NewCopilotHandlers),
	fx.Provide(NewRequestContentHandlers),
	fx.Provide(NewRequestPreviewHandlers),
	fx.Provide(func(proxy *mcp.Proxy, authService *biz.AuthService, channelService *biz.ChannelService) *MCPHandler {
		return NewMCPHandler(proxy, authService, channelService)
	}),
	fx.Invoke(initLogger),
)
