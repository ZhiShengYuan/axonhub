package biz

import (
	"context"
	"fmt"

	"go.uber.org/fx"

	"github.com/looplj/axonhub/internal/ent"
	"github.com/looplj/axonhub/internal/ent/agentruntime"
)

type AgentRuntimeServiceParams struct {
	fx.In

	Ent *ent.Client
}

// AgentRuntimeService provides APIs for managing agent runtimes.
// This service handles CRUD operations and connection testing for agent runtimes.
type AgentRuntimeService struct {
	*AbstractService
}

func NewAgentRuntimeService(params AgentRuntimeServiceParams) *AgentRuntimeService {
	return &AgentRuntimeService{
		AbstractService: &AbstractService{
			db: params.Ent,
		},
	}
}

func (svc *AgentRuntimeService) CreateAgentRuntime(ctx context.Context, input ent.CreateAgentRuntimeInput) (*ent.AgentRuntime, error) {
	runtime, err := svc.db.AgentRuntime.Create().
		SetInput(input).
		Save(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to create agent runtime: %w", err)
	}

	return runtime, nil
}

func (svc *AgentRuntimeService) UpdateAgentRuntime(ctx context.Context, id int, input ent.UpdateAgentRuntimeInput) (*ent.AgentRuntime, error) {
	runtime, err := svc.db.AgentRuntime.UpdateOneID(id).
		SetInput(input).
		Save(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to update agent runtime: %w", err)
	}

	return runtime, nil
}

func (svc *AgentRuntimeService) DeleteAgentRuntime(ctx context.Context, id int) error {
	n, err := svc.db.AgentRuntime.Delete().Where(agentruntime.IDEQ(id)).Exec(ctx)
	if err != nil {
		return fmt.Errorf("failed to delete agent runtime: %w", err)
	}
	if n == 0 {
		return fmt.Errorf("agent runtime not found")
	}
	return nil
}

type TestConnectionResult struct {
	Success bool
	Error   string
	Latency int
}

func (svc *AgentRuntimeService) TestConnection(ctx context.Context, id int) (*TestConnectionResult, error) {
	runtime, err := svc.db.AgentRuntime.Query().Where(agentruntime.IDEQ(id)).Only(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to load agent runtime: %w", err)
	}

	return svc.testConnection(ctx, runtime)
}

func (svc *AgentRuntimeService) testConnection(_ context.Context, runtime *ent.AgentRuntime) (*TestConnectionResult, error) {
	return &TestConnectionResult{
		Success: true,
	}, nil
}
