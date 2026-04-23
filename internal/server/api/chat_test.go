package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/looplj/axonhub/llm"
	"github.com/looplj/axonhub/llm/httpclient"
	"github.com/looplj/axonhub/llm/streams"
)

func init() {
	gin.SetMode(gin.TestMode)
}

// errorAfterStream emits items then returns an error.
type errorAfterStream struct {
	items []*httpclient.StreamEvent
	idx   int
	err   error
}

func (s *errorAfterStream) Next() bool {
	if s.idx < len(s.items) {
		return true
	}

	return false
}

func (s *errorAfterStream) Current() *httpclient.StreamEvent {
	item := s.items[s.idx]
	s.idx++

	return item
}

func (s *errorAfterStream) Err() error {
	if s.idx >= len(s.items) {
		return s.err
	}

	return nil
}

func (s *errorAfterStream) Close() error { return nil }

func TestWriteSSEStream_Success(t *testing.T) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/", nil)

	events := []*httpclient.StreamEvent{
		{Type: "", Data: []byte(`{"id":"1","choices":[{"delta":{"content":"Hi"}}]}`)},
		{Type: "", Data: []byte(`[DONE]`)},
	}
	stream := streams.SliceStream(events)

	WriteSSEStream(c, stream)

	body := w.Body.String()
	assert.Contains(t, body, `{"id":"1","choices":[{"delta":{"content":"Hi"}}]}`)
	assert.Contains(t, body, `[DONE]`)
}

func TestWriteSSEStream_ErrorFormatsAsJSON(t *testing.T) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/", nil)

	streamErr := errors.New("upstream connection reset")
	stream := &errorAfterStream{
		items: []*httpclient.StreamEvent{
			{Type: "", Data: []byte(`{"id":"1","choices":[{"delta":{"content":"He"}}]}`)},
			{Type: "", Data: []byte(`{"id":"2"}`)},
			{Type: "", Data: []byte(`{"id":"3"}`)},
			{Type: "", Data: []byte(`{"id":"4"}`)},
			{Type: "", Data: []byte(`{"id":"5"}`)},
			{Type: "", Data: []byte(`{"id":"6"}`)},
			{Type: "", Data: []byte(`{"id":"7"}`)},
			{Type: "", Data: []byte(`{"id":"8"}`)},
		},
		err: streamErr,
	}

	WriteSSEStream(c, stream)

	body := w.Body.String()

	assert.Contains(t, body, `{"id":"1"`)
	assert.Contains(t, body, `{"id":"8"}`)
	assert.Contains(t, body, "event:error")

	lines := strings.Split(body, "\n")

	var errorData string

	foundError := false

	for i, line := range lines {
		if strings.HasPrefix(line, "event:error") {
			foundError = true
			if i+1 < len(lines) {
				errorData = strings.TrimPrefix(lines[i+1], "data:")
			}

			break
		}
	}

	require.True(t, foundError, "should contain an error event")
	require.NotEmpty(t, errorData, "error event should have data")

	var errObj map[string]any

	err := json.Unmarshal([]byte(errorData), &errObj)
	require.NoError(t, err, "error data should be valid JSON: %s", errorData)

	errorField, ok := errObj["error"].(map[string]any)
	require.True(t, ok, "should have 'error' field")
	assert.Equal(t, "upstream connection reset", errorField["message"])
	assert.Equal(t, "server_error", errorField["type"])
	_, hasCode := errorField["code"]
	assert.True(t, hasCode)
}

func TestWriteSSEStream_HttpClientError(t *testing.T) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/", nil)

	httpErr := &httpclient.Error{
		StatusCode: 429,
		Body:       []byte(`{"error":{"message":"Rate limit exceeded","type":"rate_limit_error"}}`),
	}
	stream := &errorAfterStream{
		items: []*httpclient.StreamEvent{
			{Type: "", Data: []byte(`{"id":"1"}`)},
			{Type: "", Data: []byte(`{"id":"2"}`)},
			{Type: "", Data: []byte(`{"id":"3"}`)},
			{Type: "", Data: []byte(`{"id":"4"}`)},
			{Type: "", Data: []byte(`{"id":"5"}`)},
			{Type: "", Data: []byte(`{"id":"6"}`)},
			{Type: "", Data: []byte(`{"id":"7"}`)},
			{Type: "", Data: []byte(`{"id":"8"}`)},
		},
		err: httpErr,
	}

	WriteSSEStream(c, stream)

	body := w.Body.String()

	lines := strings.Split(body, "\n")

	var errorData string

	for i, line := range lines {
		if strings.HasPrefix(line, "event:error") {
			if i+1 < len(lines) {
				errorData = strings.TrimPrefix(lines[i+1], "data:")
			}

			break
		}
	}

	require.NotEmpty(t, errorData)

	var errObj map[string]any

	err := json.Unmarshal([]byte(errorData), &errObj)
	require.NoError(t, err)

	errorField, ok := errObj["error"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "Rate limit exceeded", errorField["message"])
	assert.Equal(t, "rate_limit_error", errorField["type"])
	assert.Empty(t, errorField["code"])
}

func TestWriteSSEStream_CustomErrorFormatter(t *testing.T) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/", nil)

	streamErr := errors.New("custom error")
	stream := &errorAfterStream{
		items: []*httpclient.StreamEvent{
			{Type: "", Data: []byte(`{"id":"1"}`)},
			{Type: "", Data: []byte(`{"id":"2"}`)},
			{Type: "", Data: []byte(`{"id":"3"}`)},
			{Type: "", Data: []byte(`{"id":"4"}`)},
			{Type: "", Data: []byte(`{"id":"5"}`)},
			{Type: "", Data: []byte(`{"id":"6"}`)},
			{Type: "", Data: []byte(`{"id":"7"}`)},
			{Type: "", Data: []byte(`{"id":"8"}`)},
		},
		err: streamErr,
	}

	customFormatter := func(_ context.Context, err error) any {
		return gin.H{"custom_error": err.Error()}
	}

	WriteSSEStreamWithErrorFormatter(c, stream, customFormatter)

	body := w.Body.String()
	lines := strings.Split(body, "\n")

	var errorData string

	for i, line := range lines {
		if strings.HasPrefix(line, "event:error") {
			if i+1 < len(lines) {
				errorData = strings.TrimPrefix(lines[i+1], "data:")
			}

			break
		}
	}

	require.NotEmpty(t, errorData)

	var errObj map[string]any

	err := json.Unmarshal([]byte(errorData), &errObj)
	require.NoError(t, err)
	assert.Equal(t, "custom error", errObj["custom_error"])
}

func TestWriteSSEStream_NoError(t *testing.T) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/", nil)

	stream := streams.SliceStream([]*httpclient.StreamEvent{
		{Type: "", Data: []byte(`[DONE]`)},
	})

	WriteSSEStream(c, stream)

	body := w.Body.String()
	assert.NotContains(t, body, "event:error")
}

func TestFormatStreamError_PlainError(t *testing.T) {
	err := errors.New("something went wrong")
	result := FormatStreamError(context.Background(), err)

	data, marshalErr := json.Marshal(result)
	require.NoError(t, marshalErr)

	var parsed map[string]any
	require.NoError(t, json.Unmarshal(data, &parsed))

	errorField := parsed["error"].(map[string]any)
	assert.Equal(t, "something went wrong", errorField["message"])
	assert.Equal(t, "server_error", errorField["type"])
	assert.Equal(t, "", errorField["code"])
}

func TestFormatStreamError_HttpClientError(t *testing.T) {
	httpErr := &httpclient.Error{
		StatusCode: 500,
		Body:       []byte(`{"error":{"message":"Internal server error","type":"internal_error"}}`),
	}
	result := FormatStreamError(context.Background(), httpErr)

	data, marshalErr := json.Marshal(result)
	require.NoError(t, marshalErr)

	var parsed map[string]any
	require.NoError(t, json.Unmarshal(data, &parsed))

	errorField := parsed["error"].(map[string]any)
	assert.Equal(t, "Internal server error", errorField["message"])
	assert.Equal(t, "internal_error", errorField["type"])
	assert.Equal(t, "", errorField["code"])
}

func TestFormatStreamError_LlmResponseError_PassesCodeAndRequestID(t *testing.T) {
	respErr := &llm.ResponseError{
		Detail: llm.ErrorDetail{
			Code:      "1311",
			Message:   "当前订阅套餐暂未开放GPT-6权限",
			Type:      "permission_error",
			RequestID: "202603112254417d15bd26697445b0",
		},
	}

	result := FormatStreamError(context.Background(), respErr)
	data, marshalErr := json.Marshal(result)
	require.NoError(t, marshalErr)

	var parsed map[string]any
	require.NoError(t, json.Unmarshal(data, &parsed))

	errorField := parsed["error"].(map[string]any)
	assert.Equal(t, "当前订阅套餐暂未开放GPT-6权限", errorField["message"])
	assert.Equal(t, "permission_error", errorField["type"])
	assert.Equal(t, "1311", errorField["code"])
	assert.Equal(t, "202603112254417d15bd26697445b0", parsed["request_id"])
}

func TestWriteSSEStream_PreCommitError_NoPartialOutput(t *testing.T) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/", nil)

	streamErr := errors.New("upstream connection reset")
	stream := &errorAfterStream{
		items: []*httpclient.StreamEvent{
			{Type: "", Data: []byte(`{"id":"1","choices":[{"delta":{"content":"He"}}]}`)},
			{Type: "", Data: []byte(`{"id":"2","choices":[{"delta":{"content":"llo"}}]}`)},
		},
		err: streamErr,
	}

	WriteSSEStream(c, stream)

	body := w.Body.String()

	assert.NotContains(t, body, `{"id":"1"`)
	assert.NotContains(t, body, `{"id":"2"`)
	assert.NotContains(t, body, "event:error")
}

func TestWriteSSEStream_PostCommitError_KeepsSSEErrorBehavior(t *testing.T) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/", nil)

	streamErr := errors.New("upstream connection reset")
	stream := &errorAfterStream{
		items: []*httpclient.StreamEvent{
			{Type: "", Data: []byte(`{"id":"1","choices":[{"delta":{"content":"Hi"}}]}`)},
			{Type: "", Data: []byte(`{"id":"2","choices":[{"delta":{"content":"llo"}}]}`)},
			{Type: "", Data: []byte(`{"id":"3","choices":[{"delta":{"content":"!"}}]}`)},
			{Type: "", Data: []byte(`{"id":"4","choices":[{"delta":{"content":"!"}}]}`)},
			{Type: "", Data: []byte(`{"id":"5","choices":[{"delta":{"content":"!"}}]}`)},
			{Type: "", Data: []byte(`{"id":"6","choices":[{"delta":{"content":"!"}}]}`)},
			{Type: "", Data: []byte(`{"id":"7","choices":[{"delta":{"content":"!"}}]}`)},
			{Type: "", Data: []byte(`{"id":"8","choices":[{"delta":{"content":"!"}}]}`)},
		},
		err: streamErr,
	}

	WriteSSEStream(c, stream)

	body := w.Body.String()

	assert.Contains(t, body, `{"id":"1"`)
	assert.Contains(t, body, `{"id":"8"`)
	assert.Contains(t, body, "event:error")
	assert.Contains(t, body, "upstream connection reset")
}

func TestWriteSSEStream_BufferedRelease_EventsAppearAfterThreshold(t *testing.T) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/", nil)

	events := make([]*httpclient.StreamEvent, 12)
	for i := 0; i < 12; i++ {
		events[i] = &httpclient.StreamEvent{Type: "", Data: []byte(`{"id":"` + fmt.Sprintf("%d", i) + `"}`)}
	}
	stream := streams.SliceStream(events)

	WriteSSEStream(c, stream)

	body := w.Body.String()

	assert.Contains(t, body, `{"id":"0"}`)
	assert.Contains(t, body, `{"id":"7"}`)
	assert.Contains(t, body, `{"id":"8"}`)
	assert.Contains(t, body, `{"id":"9"}`)
	assert.Contains(t, body, `{"id":"10"}`)
	assert.Contains(t, body, `{"id":"11"}`)
}

func TestWriteSSEStream_UpstreamEnd_FlushesAllEvents(t *testing.T) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/", nil)

	events := []*httpclient.StreamEvent{
		{Type: "", Data: []byte(`{"id":"1","choices":[{"delta":{"content":"Hello"}}]}`)},
		{Type: "", Data: []byte(`{"id":"2","choices":[{"delta":{"content":"World"}}]}`)},
		{Type: "", Data: []byte(`[DONE]`)},
	}
	stream := streams.SliceStream(events)

	WriteSSEStream(c, stream)

	body := w.Body.String()
	assert.Contains(t, body, `{"id":"1"`)
	assert.Contains(t, body, `{"id":"2"`)
	assert.Contains(t, body, `[DONE]`)
	assert.NotContains(t, body, "event:error")
}

// TestWriteSSEStream_TerminalCompletionFlushesAll verifies that when upstream
// completes with [DONE], all buffered events are flushed including [DONE].
func TestWriteSSEStream_TerminalCompletionFlushesAll(t *testing.T) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/", nil)

	events := []*httpclient.StreamEvent{
		{Type: "", Data: []byte(`{"id":"1","choices":[{"delta":{"content":"Hi"}}]}`)},
		{Type: "", Data: []byte(`{"id":"2","choices":[{"delta":{"content":"llo"}}]}`)},
		{Type: "", Data: []byte(`[DONE]`)},
	}
	stream := streams.SliceStream(events)

	WriteSSEStream(c, stream)

	body := w.Body.String()
	// Terminal completion should flush all events including [DONE]
	assert.Contains(t, body, `{"id":"1"`)
	assert.Contains(t, body, `{"id":"2"`)
	assert.Contains(t, body, `[DONE]`)
	assert.NotContains(t, body, "event:error")
}

// TestWriteSSEStream_PostCommitErrorPreservesErrorFrame verifies that when
// stream commits (8+ events) then errors, both committed events AND error
// frame are present in the response.
func TestWriteSSEStream_PostCommitErrorPreservesErrorFrame(t *testing.T) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/", nil)

	streamErr := errors.New("upstream connection reset")
	stream := &errorAfterStream{
		items: []*httpclient.StreamEvent{
			{Type: "", Data: []byte(`{"id":"1"}`)},
			{Type: "", Data: []byte(`{"id":"2"}`)},
			{Type: "", Data: []byte(`{"id":"3"}`)},
			{Type: "", Data: []byte(`{"id":"4"}`)},
			{Type: "", Data: []byte(`{"id":"5"}`)},
			{Type: "", Data: []byte(`{"id":"6"}`)},
			{Type: "", Data: []byte(`{"id":"7"}`)},
			{Type: "", Data: []byte(`{"id":"8"}`)},
		},
		err: streamErr,
	}

	WriteSSEStream(c, stream)

	body := w.Body.String()
	// Committed events should be preserved
	assert.Contains(t, body, `{"id":"1"}`)
	assert.Contains(t, body, `{"id":"8"}`)
	// Error frame should also be present
	assert.Contains(t, body, "event:error")
	assert.Contains(t, body, "upstream connection reset")
}

func TestWriteSSEStream_PreCommitErrorSuppressesDataEvents(t *testing.T) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/", nil)

	streamErr := errors.New("upstream connection reset")
	stream := &errorAfterStream{
		items: []*httpclient.StreamEvent{
			{Type: "", Data: []byte(`{"id":"1"}`)},
			{Type: "", Data: []byte(`{"id":"2"}`)},
			{Type: "", Data: []byte(`{"id":"3"}`)},
		},
		err: streamErr,
	}

	WriteSSEStream(c, stream)

	body := w.Body.String()
	assert.NotContains(t, body, `{"id":"1"`)
	assert.NotContains(t, body, `{"id":"2"`)
	assert.NotContains(t, body, `{"id":"3"}`)
	assert.NotContains(t, body, "event:error")
}
