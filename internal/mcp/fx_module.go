package mcp

import "go.uber.org/fx"

var Module = fx.Module("mcp",
	fx.Provide(NewProxy),
)