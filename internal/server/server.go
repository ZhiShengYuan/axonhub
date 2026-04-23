package server

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/gin-gonic/gin"
	"go.uber.org/fx"
	"go.uber.org/fx/fxevent"
	"go.uber.org/zap"

	"github.com/looplj/axonhub/internal/log"
	"github.com/looplj/axonhub/internal/mcp"
	"github.com/looplj/axonhub/internal/server/api"
	"github.com/looplj/axonhub/internal/server/backup"
	"github.com/looplj/axonhub/internal/server/biz"
	"github.com/looplj/axonhub/internal/server/dependencies"
	"github.com/looplj/axonhub/internal/server/gc"
	"github.com/looplj/axonhub/internal/server/gql"
	"github.com/looplj/axonhub/internal/server/gql/openapi"
	"github.com/looplj/axonhub/internal/server/middleware"
	"github.com/looplj/axonhub/internal/server/orchestrator"
	"github.com/looplj/axonhub/internal/server/video_storage"
	"github.com/looplj/axonhub/internal/tracing"
)

type fxLogger struct {
	logger *log.Logger
}

func (f *fxLogger) LogEvent(event fxevent.Event) {
	ctx := context.Background()
	switch e := event.(type) {
	case *fxevent.Started:
		if e.Err != nil {
			f.logger.Error(ctx, "fx started error", zap.Error(e.Err))
		} else {
			f.logger.Debug(ctx, "fx started")
		}
	case *fxevent.Stopped:
		if e.Err != nil {
			f.logger.Error(ctx, "fx stopped error", zap.Error(e.Err))
		} else {
			f.logger.Debug(ctx, "fx stopped")
		}
	case *fxevent.OnStartExecuting:
		f.logger.Debug(ctx, "fx on start executing", zap.String("caller", e.CallerName), zap.String("function", e.FunctionName))
	case *fxevent.OnStartExecuted:
		if e.Err != nil {
			f.logger.Error(ctx, "fx on start executed error", zap.String("method", e.Method), zap.Duration("runtime", e.Runtime), zap.Error(e.Err))
		} else {
			f.logger.Debug(ctx, "fx on start executed", zap.String("method", e.Method), zap.Duration("runtime", e.Runtime))
		}
	case *fxevent.OnStopExecuting:
		f.logger.Debug(ctx, "fx on stop executing", zap.String("caller", e.CallerName), zap.String("function", e.FunctionName))
	case *fxevent.OnStopExecuted:
		if e.Err != nil {
			f.logger.Error(ctx, "fx on stop executed error", zap.String("function", e.FunctionName), zap.Duration("runtime", e.Runtime), zap.Error(e.Err))
		} else {
			f.logger.Debug(ctx, "fx on stop executed", zap.String("function", e.FunctionName), zap.Duration("runtime", e.Runtime))
		}
	case *fxevent.Provided:
		if e.Err != nil {
			f.logger.Error(ctx, "fx provided error", zap.String("constructor", e.ConstructorName), zap.Error(e.Err))
		} else {
			f.logger.Debug(ctx, "fx provided", zap.String("constructor", e.ConstructorName))
		}
	case *fxevent.Invoking:
		f.logger.Debug(ctx, "fx invoking", zap.String("function", e.FunctionName))
	case *fxevent.Invoked:
		if e.Err != nil {
			f.logger.Error(ctx, "fx invoked error", zap.String("function", e.FunctionName), zap.Error(e.Err))
		}
	case *fxevent.Supplied:
		if e.Err != nil {
			f.logger.Error(ctx, "fx supplied error", zap.String("type", e.TypeName), zap.Error(e.Err))
		}
	case *fxevent.Replaced:
		if e.Err != nil {
			f.logger.Error(ctx, "fx replaced error", zap.Error(e.Err))
		}
	case *fxevent.Decorated:
		if e.Err != nil {
			f.logger.Error(ctx, "fx decorated error", zap.Error(e.Err))
		}
	case *fxevent.Run:
		f.logger.Debug(ctx, "fx run", zap.String("name", e.Name))
	case *fxevent.RollingBack:
		f.logger.Debug(ctx, "fx rolling back", zap.Error(e.StartErr))
	case *fxevent.RolledBack:
		if e.Err != nil {
			f.logger.Error(ctx, "fx rolled back error", zap.Error(e.Err))
		}
	case *fxevent.Stopping:
		f.logger.Debug(ctx, "fx stopping", zap.String("signal", e.Signal.String()))
	case *fxevent.LoggerInitialized:
		if e.Err != nil {
			f.logger.Error(ctx, "fx logger initialized error", zap.Error(e.Err))
		} else {
			f.logger.Debug(ctx, "fx logger initialized")
		}
	default:
		f.logger.Debug(ctx, "fx event", zap.String("event", fmt.Sprintf("%T", event)))
	}
}

func New(config Config) *Server {
	if !config.Debug {
		gin.SetMode(gin.ReleaseMode)
	}

	engine := gin.New()
	engine.Use(middleware.Recovery())

	// Configure trusted proxies if specified
	if len(config.TrustedProxies) > 0 {
		if err := engine.SetTrustedProxies(config.TrustedProxies); err != nil {
			log.Warn(context.Background(), "failed to set trusted proxies", log.Any("trusted_proxies", config.TrustedProxies), log.String("error", err.Error()))
		} else {
			log.Info(context.Background(), "trusted proxies configured", log.Any("trusted_proxies", config.TrustedProxies))
		}
	}

	return &Server{
		Config: config,
		Engine: engine,
	}
}

type Server struct {
	*gin.Engine

	Config Config
	server *http.Server
	addr   string
}

func (srv *Server) Run() error {
	log.Info(context.Background(), "run server",
		log.String("name", srv.Config.Name),
		log.String("host", srv.Config.Host),
		log.Int("port", srv.Config.Port),
	)
	addr := fmt.Sprintf("%s:%d", srv.Config.Host, srv.Config.Port)
	srv.server = &http.Server{
		Addr:         addr,
		Handler:      srv.Engine,
		ReadTimeout:  srv.Config.ReadTimeout,
		WriteTimeout: max(srv.Config.RequestTimeout, srv.Config.LLMRequestTimeout),
	}
	srv.addr = addr

	err := srv.server.ListenAndServe()
	if err != nil {
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}

		return err
	}

	return nil
}

func (srv *Server) Shutdown(ctx context.Context) error {
	return srv.server.Shutdown(ctx)
}

// StartupFailureContract: Run() must emit a stdio-visible log before exit if fx app
// startup/bootstrap fails. Server-bind failures are logged. Request/runtime panic
// handling is out of scope (handled by middleware.Recovery in the HTTP layer).
func Run(opts ...fx.Option) {
	constructors := []any{
		openapi.NewGraphqlHandlers,
		gql.NewGraphqlHandlers,
		gc.NewWorker,
		New,
	}

	app := fx.New(
		append([]fx.Option{
			fx.WithLogger(func() fxevent.Logger {
				return &fxLogger{logger: log.GetGlobalLogger()}
			}),
			fx.Provide(constructors...),
			dependencies.Module,
		biz.Module,
		orchestrator.Module,
		backup.Module,
		video_storage.Module,
		mcp.Module,
		api.Module,
			fx.Invoke(func(cfg log.Config) {
				log.SetGlobalConfig(cfg)
				tracing.SetupLogger(log.GetGlobalLogger())
				slog.SetDefault(log.GetGlobalLogger().AsSlog())
			}),
			fx.Invoke(func(usageLogSvc *biz.UsageLogService) {
				usageLogSvc.OnUsageLogCreated = gql.InvalidateAllTimeTokenStatsCache
			}),
			fx.Invoke(func(cfg Config) {
				if cfg.Dashboard.AllTimeTokenStatsSoftTTL > 0 && cfg.Dashboard.AllTimeTokenStatsHardTTL > 0 {
					gql.SetTokenStatsCacheTTL(cfg.Dashboard.AllTimeTokenStatsSoftTTL, cfg.Dashboard.AllTimeTokenStatsHardTTL)
				}
			}),
			fx.Invoke(func(lc fx.Lifecycle, worker *gc.Worker) {
				lc.Append(fx.Hook{
					OnStart: func(ctx context.Context) error {
						return worker.Start(ctx)
					},
					OnStop: func(ctx context.Context) error {
						return worker.Stop(ctx)
					},
				})
			}),
			fx.Invoke(SetupRoutes),
		}, opts...)...,
	)

	ctx := context.Background()
	if err := app.Start(ctx); err != nil {
		log.GetGlobalLogger().Error(ctx, "failed to start server", zap.Error(err))
		_ = app.Stop(ctx)
		os.Exit(1)
	}

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh

	if err := app.Stop(ctx); err != nil {
		log.GetGlobalLogger().Error(ctx, "failed to stop server", zap.Error(err))
		os.Exit(1)
	}
}
