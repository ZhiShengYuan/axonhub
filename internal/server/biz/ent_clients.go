package biz

import (
	"github.com/looplj/axonhub/internal/ent"
	"go.uber.org/fx"
)

type ConfigEntClient struct {
	fx.In

	Client *ent.Client `name:"config_ent"`
}

type LogEntClient struct {
	fx.In

	Client *ent.Client `name:"log_ent"`
}
