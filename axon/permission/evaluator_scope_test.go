package permission

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/looplj/axonhub/axon/permission/approval"
	"github.com/looplj/axonhub/axon/permission/grant"
	"github.com/looplj/axonhub/axon/permission/policy"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type mockApprover struct {
	response approval.Response
	err      error
}

func (m *mockApprover) Subscribe(ctx context.Context) <-chan approval.Request {
	return nil
}

func (m *mockApprover) Request(ctx context.Context, req approval.Request) (approval.Response, error) {
	return m.response, m.err
}

func (m *mockApprover) Grant(req approval.Request, scope grant.Scope) error {
	return nil
}

func (m *mockApprover) Deny(req approval.Request) error {
	return nil
}

func (m *mockApprover) Active() (approval.Request, bool) {
	return approval.Request{}, false
}

type mockStore struct {
	added         bool
	lastScope     grant.Scope
	lastResources []grant.Resource
	savedGlobal   bool
	savedWs       string
}

func (m *mockStore) Match(req grant.Request, resources []grant.Resource) bool {
	return false
}

func (m *mockStore) Add(req grant.Request, scope grant.Scope, resources []grant.Resource) {
	m.added = true
	m.lastScope = scope
	m.lastResources = resources
}

func (m *mockStore) LoadWorkspace(workspace string) error {
	return nil
}

func (m *mockStore) SaveWorkspace(workspace string) error {
	m.savedWs = workspace
	return nil
}

func (m *mockStore) LoadGlobal() error {
	return nil
}

func (m *mockStore) SaveGlobal() error {
	m.savedGlobal = true
	return nil
}

func mustParseTime(s string) time.Time {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		panic(err)
	}
	return t
}

func TestEvaluator_HandleDecision_GlobalScope_SavesGlobal(t *testing.T) {
	store := &mockStore{}
	approver := &mockApprover{
		response: approval.Response{
			Granted: true,
			Scope:   grant.ScopeGlobal,
		},
	}

	eng, err := policy.New(policy.Document{Version: 1})
	require.NoError(t, err)

	eval := NewEvaluator(EvaluatorOptions{
		Policy:   eng,
		Approver: approver,
		Grants:   store,
	})

	req := ToolRequest{
		ToolCallID: "test-id",
		ThreadID:   "thread-1",
		Workspace:  "/workspace",
		ToolName:   "test_tool",
		StartedAt:  mustParseTime("2024-01-01T00:00:00Z"),
	}

	err = eval.Evaluate(context.Background(), req)
	require.NoError(t, err)

	assert.True(t, store.added, "Add should be called")
	assert.Equal(t, grant.ScopeGlobal, store.lastScope, "scope should be global")
	assert.True(t, store.savedGlobal, "SaveGlobal should be called for global scope")
	assert.Empty(t, store.savedWs, "SaveWorkspace should not be called for global scope")
}

func TestEvaluator_HandleDecision_WorkspaceScope_SavesWorkspace(t *testing.T) {
	store := &mockStore{}
	approver := &mockApprover{
		response: approval.Response{
			Granted: true,
			Scope:   grant.ScopeWorkspace,
		},
	}

	eng, err := policy.New(policy.Document{Version: 1})
	require.NoError(t, err)

	eval := NewEvaluator(EvaluatorOptions{
		Policy:   eng,
		Approver: approver,
		Grants:   store,
	})

	req := ToolRequest{
		ToolCallID: "test-id",
		ThreadID:   "thread-1",
		Workspace:  "/workspace",
		ToolName:   "test_tool",
		StartedAt:  mustParseTime("2024-01-01T00:00:00Z"),
	}

	err = eval.Evaluate(context.Background(), req)
	require.NoError(t, err)

	assert.True(t, store.added, "Add should be called")
	assert.Equal(t, grant.ScopeWorkspace, store.lastScope, "scope should be workspace")
	assert.Equal(t, "/workspace", store.savedWs, "SaveWorkspace should be called with workspace path")
	assert.False(t, store.savedGlobal, "SaveGlobal should not be called for workspace scope")
}

func TestEvaluator_HandleDecision_OnceScope_NoSave(t *testing.T) {
	store := &mockStore{}
	approver := &mockApprover{
		response: approval.Response{
			Granted: true,
			Scope:   grant.ScopeOnce,
		},
	}

	eng, err := policy.New(policy.Document{Version: 1})
	require.NoError(t, err)

	eval := NewEvaluator(EvaluatorOptions{
		Policy:   eng,
		Approver: approver,
		Grants:   store,
	})

	req := ToolRequest{
		ToolCallID: "test-id",
		ThreadID:   "thread-1",
		Workspace:  "/workspace",
		ToolName:   "test_tool",
		StartedAt:  mustParseTime("2024-01-01T00:00:00Z"),
	}

	err = eval.Evaluate(context.Background(), req)
	require.NoError(t, err)

	assert.True(t, store.added, "Add should be called")
	assert.Equal(t, grant.ScopeOnce, store.lastScope, "scope should be once")
	assert.False(t, store.savedGlobal, "SaveGlobal should not be called for once scope")
	assert.Empty(t, store.savedWs, "SaveWorkspace should not be called for once scope")
}

func TestEvaluator_HandleDecision_ThreadScope_NoSave(t *testing.T) {
	store := &mockStore{}
	approver := &mockApprover{
		response: approval.Response{
			Granted: true,
			Scope:   grant.ScopeThread,
		},
	}

	eng, err := policy.New(policy.Document{Version: 1})
	require.NoError(t, err)

	eval := NewEvaluator(EvaluatorOptions{
		Policy:   eng,
		Approver: approver,
		Grants:   store,
	})

	req := ToolRequest{
		ToolCallID: "test-id",
		ThreadID:   "thread-1",
		Workspace:  "/workspace",
		ToolName:   "test_tool",
		StartedAt:  mustParseTime("2024-01-01T00:00:00Z"),
	}

	err = eval.Evaluate(context.Background(), req)
	require.NoError(t, err)

	assert.True(t, store.added, "Add should be called")
	assert.Equal(t, grant.ScopeThread, store.lastScope, "scope should be thread")
	assert.False(t, store.savedGlobal, "SaveGlobal should not be called for thread scope")
	assert.Empty(t, store.savedWs, "SaveWorkspace should not be called for thread scope")
}

func TestEvaluator_HandleDecision_Denied_NoSave(t *testing.T) {
	store := &mockStore{}
	approver := &mockApprover{
		response: approval.Response{
			Granted: false,
		},
	}

	eng, err := policy.New(policy.Document{Version: 1})
	require.NoError(t, err)

	eval := NewEvaluator(EvaluatorOptions{
		Policy:   eng,
		Approver: approver,
		Grants:   store,
	})

	req := ToolRequest{
		ToolCallID: "test-id",
		ThreadID:   "thread-1",
		Workspace:  "/workspace",
		ToolName:   "test_tool",
		StartedAt:  mustParseTime("2024-01-01T00:00:00Z"),
	}

	err = eval.Evaluate(context.Background(), req)
	require.Error(t, err)

	assert.False(t, store.added, "Add should not be called for denied approval")
	assert.False(t, store.savedGlobal, "SaveGlobal should not be called for denied approval")
	assert.Empty(t, store.savedWs, "SaveWorkspace should not be called for denied approval")
}

func TestEvaluator_HandleDecision_GlobalScope_WithSelectedResources(t *testing.T) {
	store := &mockStore{}
	resources := []Resource{
		{Type: ResourcePath, Path: "/workspace/file1.txt", WorkspaceRel: "file1.txt"},
		{Type: ResourcePath, Path: "/workspace/file2.txt", WorkspaceRel: "file2.txt"},
	}
	resourcesJSON, err := json.Marshal(resources[0])
	require.NoError(t, err)

	approver := &mockApprover{
		response: approval.Response{
			Granted:   true,
			Scope:     grant.ScopeGlobal,
			Resources: []json.RawMessage{resourcesJSON},
		},
	}

	eng, err := policy.New(policy.Document{Version: 1})
	require.NoError(t, err)

	eval := NewEvaluator(EvaluatorOptions{
		Policy:   eng,
		Approver: approver,
		Grants:   store,
	})

	req := ToolRequest{
		ToolCallID: "test-id",
		ThreadID:   "thread-1",
		Workspace:  "/workspace",
		ToolName:   "test_tool",
		StartedAt:  mustParseTime("2024-01-01T00:00:00Z"),
	}

	err = eval.Evaluate(context.Background(), req)
	require.NoError(t, err)

	assert.True(t, store.added, "Add should be called")
	assert.True(t, store.savedGlobal, "SaveGlobal should be called for global scope")
	assert.Len(t, store.lastResources, 1, "should save only selected resource")
}
