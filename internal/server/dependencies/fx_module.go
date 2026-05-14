package dependencies

import (
	"context"

	"github.com/zhenzou/executors"
	"go.uber.org/fx"

	"github.com/looplj/axonhub/internal/ent"
	"github.com/looplj/axonhub/internal/log"
	"github.com/looplj/axonhub/internal/server/db"
	"github.com/looplj/axonhub/llm/httpclient"
)

type NewHttpClientParams struct {
	fx.In

	DisableSSLVerify bool `name:"disable_ssl_verify"`
}

func NewHttpClient(params NewHttpClientParams) *httpclient.HttpClient {
	return httpclient.NewHttpClient(httpclient.WithInsecureSkipVerify(params.DisableSSLVerify))
}

func configDBProvider(cfg db.Config) *ent.Client {
	return db.NewEntClient(cfg)
}

func logDBProvider(cfg db.Config, logCfg db.Config, configClient *ent.Client) *ent.Client {
	if logCfg.DSN == "" {
		return configClient
	}
	return db.NewEntClientFor("log", logCfg, true)
}

var Module = fx.Module("dependencies",
	fx.Provide(log.New),
	fx.Provide(db.NewEntClient),
	fx.Provide(fx.Annotate(configDBProvider, fx.ResultTags(`name:"config_ent"`))),
	fx.Provide(fx.Annotate(logDBProvider, fx.ResultTags(`name:"log_ent"`))),
	fx.Provide(NewHttpClient),
	fx.Provide(NewExecutors),
	fx.Invoke(func(lc fx.Lifecycle, executor executors.ScheduledExecutor) {
		lc.Append(fx.Hook{
			OnStop: func(ctx context.Context) error {
				return executor.Shutdown(ctx)
			},
		})
	}),
)
