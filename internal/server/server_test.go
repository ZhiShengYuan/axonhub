package server

import (
	"context"
	"go.uber.org/fx"
	"go.uber.org/fx/fxevent"
	"syscall"
	"testing"

	"github.com/looplj/axonhub/internal/log"
	"github.com/stretchr/testify/assert"
)

func TestFxLogger_ErrorEvent(t *testing.T) {
	logger := log.GetGlobalLogger()
	fxLogger := &fxLogger{logger: logger}

	fxLogger.LogEvent(&fxevent.Started{Err: assert.AnError})
}

func TestFxLogger_ProvidedErrorEvent(t *testing.T) {
	logger := log.GetGlobalLogger()
	fxLogger := &fxLogger{logger: logger}

	fxLogger.LogEvent(&fxevent.Provided{
		ConstructorName: "TestConstructor",
		Err:             assert.AnError,
	})
}

func TestFxLogger_InvokedErrorEvent(t *testing.T) {
	logger := log.GetGlobalLogger()
	fxLogger := &fxLogger{logger: logger}

	fxLogger.LogEvent(&fxevent.Invoked{
		FunctionName: "testFunc",
		Err:          assert.AnError,
	})
}

func TestFxLogger_SuppliedErrorEvent(t *testing.T) {
	logger := log.GetGlobalLogger()
	fxLogger := &fxLogger{logger: logger}

	fxLogger.LogEvent(&fxevent.Supplied{
		TypeName: "string",
		Err:      assert.AnError,
	})
}

func TestFxLogger_DebugEvents(t *testing.T) {
	logger := log.GetGlobalLogger()
	fxLogger := &fxLogger{logger: logger}

	fxLogger.LogEvent(&fxevent.Started{})
	fxLogger.LogEvent(&fxevent.Stopped{})
	fxLogger.LogEvent(&fxevent.Invoking{FunctionName: "myFunc"})
	fxLogger.LogEvent(&fxevent.Provided{ConstructorName: "myConstructor"})
}

func TestFxLogger_OnStartExecutedEvent(t *testing.T) {
	logger := log.GetGlobalLogger()
	fxLogger := &fxLogger{logger: logger}

	fxLogger.LogEvent(&fxevent.OnStartExecuted{
		FunctionName: "myStart",
		Runtime:      100,
		Err:          nil,
	})

	fxLogger.LogEvent(&fxevent.OnStartExecuted{
		FunctionName: "myStart",
		Runtime:      100,
		Err:          assert.AnError,
	})
}

func TestFxLogger_OnStopExecutedEvent(t *testing.T) {
	logger := log.GetGlobalLogger()
	fxLogger := &fxLogger{logger: logger}

	fxLogger.LogEvent(&fxevent.OnStopExecuted{
		FunctionName: "myStop",
		Runtime:      50,
		Err:          nil,
	})

	fxLogger.LogEvent(&fxevent.OnStopExecuted{
		FunctionName: "myStop",
		Runtime:      50,
		Err:          assert.AnError,
	})
}

func TestFxLogger_RollingBackEvent(t *testing.T) {
	logger := log.GetGlobalLogger()
	fxLogger := &fxLogger{logger: logger}

	fxLogger.LogEvent(&fxevent.RollingBack{
		StartErr: assert.AnError,
	})
}

func TestFxLogger_RolledBackEvent(t *testing.T) {
	logger := log.GetGlobalLogger()
	fxLogger := &fxLogger{logger: logger}

	fxLogger.LogEvent(&fxevent.RolledBack{
		Err: assert.AnError,
	})
}

func TestFxLogger_LoggerInitializedEvent(t *testing.T) {
	logger := log.GetGlobalLogger()
	fxLogger := &fxLogger{logger: logger}

	fxLogger.LogEvent(&fxevent.LoggerInitialized{
		Err: nil,
	})

	fxLogger.LogEvent(&fxevent.LoggerInitialized{
		Err: assert.AnError,
	})
}

func TestFxLogger_StoppingEvent(t *testing.T) {
	logger := log.GetGlobalLogger()
	fxLogger := &fxLogger{logger: logger}

	fxLogger.LogEvent(&fxevent.Stopping{
		Signal: syscall.SIGTERM,
	})
}

func TestFxLogger_StartFailureDoesNotPanic(t *testing.T) {
	fxLogger := &fxLogger{logger: log.GetGlobalLogger()}

	app := fx.New(
		fx.WithLogger(func() fxevent.Logger {
			return fxLogger
		}),
		fx.Provide(func() string { return "ok" }),
	)

	err := app.Start(context.Background())
	assert.NoError(t, err, "app.Start should succeed with valid constructor")

	_ = app.Stop(context.Background())
}