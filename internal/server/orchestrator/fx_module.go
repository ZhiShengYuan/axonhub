package orchestrator

import "go.uber.org/fx"

func ProvideSessionAffinityService(secret string) *SessionAffinityService {
	return NewSessionAffinityService([]byte(secret))
}

var Module = fx.Module("orchestrator",
	fx.Provide(NewDefaultSelector),
	fx.Provide(NewCandidateSelectorDiagnostics),
	fx.Provide(fx.Annotate(ProvideSessionAffinityService, fx.ParamTags(`name:"session_affinity_secret"`))),
)
