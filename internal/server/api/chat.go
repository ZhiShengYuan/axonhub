package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/gin-contrib/sse"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"

	"github.com/looplj/axonhub/internal/log"
	"github.com/looplj/axonhub/internal/server/orchestrator"
	"github.com/looplj/axonhub/llm"
	"github.com/looplj/axonhub/llm/httpclient"
	"github.com/looplj/axonhub/llm/streams"
)

// StreamWriter is a function type for writing stream events to the response.
// Returns (error, committed) where committed indicates whether any data was sent
// to the client before the function returned. If committed is false and error is
// non-nil, the orchestrator can attempt failover to the next candidate.
type StreamWriter func(c *gin.Context, stream streams.Stream[*httpclient.StreamEvent]) (error, bool)

type ChatCompletionHandlers struct {
	ChatCompletionOrchestrator *orchestrator.ChatCompletionOrchestrator
	StreamWriter               StreamWriter
}

func NewChatCompletionHandlers(orchestrator *orchestrator.ChatCompletionOrchestrator) *ChatCompletionHandlers {
	return &ChatCompletionHandlers{
		ChatCompletionOrchestrator: orchestrator,
		StreamWriter: func(c *gin.Context, stream streams.Stream[*httpclient.StreamEvent]) (error, bool) {
			err, committed := WriteSSEStream(c, stream)
			return err, committed
		},
	}
}

// WithStreamWriter returns a new ChatCompletionHandlers with the specified stream writer.
func (handlers *ChatCompletionHandlers) WithStreamWriter(writer StreamWriter) *ChatCompletionHandlers {
	return &ChatCompletionHandlers{
		ChatCompletionOrchestrator: handlers.ChatCompletionOrchestrator,
		StreamWriter:              writer,
	}
}

func (handlers *ChatCompletionHandlers) ChatCompletion(c *gin.Context) {
	ctx := c.Request.Context()

	// Use ReadHTTPRequest to parse the request
	genericReq, err := httpclient.ReadHTTPRequest(c.Request)
	if err != nil {
		httpErr := handlers.ChatCompletionOrchestrator.Inbound.TransformError(ctx, err)
		c.JSON(httpErr.StatusCode, json.RawMessage(httpErr.Body))

		return
	}

	if len(genericReq.Body) == 0 {
		JSONError(c, http.StatusBadRequest, errors.New("Request body is empty"))
		return
	}

	// log.Debug(ctx, "Chat completion request", log.Any("request", genericReq))

	result, err := handlers.ChatCompletionOrchestrator.Process(ctx, genericReq)
	if err != nil {
		log.Error(ctx, "Error processing chat completion", log.Cause(err))

		httpErr := handlers.ChatCompletionOrchestrator.Inbound.TransformError(ctx, err)
		c.JSON(httpErr.StatusCode, json.RawMessage(httpErr.Body))

		return
	}

	if result.ChatCompletion != nil {
		resp := result.ChatCompletion

		contentType := "application/json"
		if ct := resp.Headers.Get("Content-Type"); ct != "" {
			contentType = ct
		}

		c.Data(resp.StatusCode, contentType, resp.Body)

		return
	}

	if result.ChatCompletionStream != nil {
		defer func() {
			log.Debug(ctx, "Close chat stream")

			err := result.ChatCompletionStream.Close()
			if err != nil {
				logger.Error(ctx, "Error closing stream", log.Cause(err))
			}
		}()

		c.Header("Access-Control-Allow-Origin", "*")

		streamWriter := handlers.StreamWriter
		if streamWriter == nil {
			streamWriter = WriteSSEStream
		}

		streamErr, committed := streamWriter(c, result.ChatCompletionStream)
		if streamErr != nil && !committed {
			if result.FailoverCallback != nil {
				if nextResult := result.FailoverCallback(streamErr, committed); nextResult != nil {
					_ = result.ChatCompletionStream.Close()

					if nextResult.ChatCompletionStream != nil {
						defer func() {
							_ = nextResult.ChatCompletionStream.Close()
						}()
						nextErr, nextCommitted := streamWriter(c, nextResult.ChatCompletionStream)
						if nextErr != nil && !nextCommitted {
							c.SSEvent("error", FormatStreamError(ctx, nextErr))
							c.Writer.Flush()
						}
						return
					}
				}
			}
			c.SSEvent("error", FormatStreamError(ctx, streamErr))
			c.Writer.Flush()
		}
	}
}

// StreamErrorFormatter formats a stream error into a JSON-serializable object for SSE error events.
type StreamErrorFormatter func(ctx context.Context, err error) any

// WriteSSEStreamDirect writes stream events as SSE without buffering.
// This is used for request preview which requires immediate output.
func WriteSSEStreamDirect(c *gin.Context, stream streams.Stream[*httpclient.StreamEvent]) (error, bool) {
	ctx := c.Request.Context()

	c.Header("Content-Type", sse.ContentType)
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Writer.Flush()

	committed := false
	for {
		select {
		case <-ctx.Done():
			return ctx.Err(), committed
		default:
			if stream.Next() {
				cur := stream.Current()
				c.SSEvent(cur.Type, cur.Data)
				c.Writer.Flush()
				committed = true
			} else {
				if stream.Err() != nil {
					c.SSEvent("error", FormatStreamError(ctx, stream.Err()))
					c.Writer.Flush()
					return stream.Err(), committed
				}
				return nil, committed
			}
		}
	}
}

// WriteSSEStream writes stream events as Server-Sent Events (SSE) with default error formatting.
// Returns (streamErr, committed) where committed indicates whether any data was sent to the client.
func WriteSSEStream(c *gin.Context, stream streams.Stream[*httpclient.StreamEvent]) (error, bool) {
	return WriteSSEStreamWithErrorFormatter(c, stream, FormatStreamError)
}

// streamBufferWrapper wraps a StreamBuffer to control release behavior.
type streamBufferWrapper struct {
	sb *StreamBuffer
}

func (w *streamBufferWrapper) Append(event *httpclient.StreamEvent) bool {
	return w.sb.Append(event)
}

func (w *streamBufferWrapper) Committed() bool {
	return w.sb.Committed()
}

func (w *streamBufferWrapper) SetUpstreamDone() {
	w.sb.SetUpstreamDone()
}

func (w *streamBufferWrapper) SuppressRelease() {
	w.sb.SuppressRelease()
}

// WriteSSEStreamWithErrorFormatter writes stream events as SSE with a custom error formatter.
// Returns (streamErr, committed) where committed indicates whether any data was sent to the client
// before the function returned. If committed is false and streamErr is non-nil, the orchestrator
// can attempt failover to the next candidate.
func WriteSSEStreamWithErrorFormatter(c *gin.Context, stream streams.Stream[*httpclient.StreamEvent], formatErr StreamErrorFormatter) (error, bool) {
	ctx := c.Request.Context()
	clientDisconnected := false

	if formatErr == nil {
		formatErr = FormatStreamError
	}

	defer func() {
		if clientDisconnected {
			log.Warn(ctx, "Client disconnected")
		}
	}()

	c.Header("Content-Type", sse.ContentType)
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Writer.Flush()

	var sbw streamBufferWrapper
	sbw.sb = NewStreamBuffer(StreamBufferOptions{
		Writer: func(event *httpclient.StreamEvent) {
			c.SSEvent(event.Type, event.Data)
			c.Writer.Flush()
		},
		MaxChunks:     DefaultMaxChunks,
		Timeout:       DefaultTimeout,
		OverflowLimit: DefaultOverflowLimit,
	})
	defer sbw.sb.Close()

	var streamErr error
	for {
		select {
		case <-ctx.Done():
			clientDisconnected = true
			log.Warn(ctx, "Context done, stopping stream")
			return ctx.Err(), sbw.Committed()
		default:
			if stream.Next() {
				cur := stream.Current()
				log.Debug(ctx, "write stream event", log.Any("event", cur))
				sbw.Append(cur)
			} else {
				streamErr = stream.Err()
				if streamErr != nil {
					if !sbw.Committed() {
						sbw.SuppressRelease()
					} else {
						c.SSEvent("error", formatErr(ctx, streamErr))
						c.Writer.Flush()
					}
				} else {
					sbw.SetUpstreamDone()
				}

				return streamErr, sbw.Committed()
			}
		}
	}
}

// FormatStreamError formats a stream error into an OpenAI-compatible JSON error object.
func FormatStreamError(_ context.Context, err error) any {
	errType := "server_error"
	errCode := ""
	requestID := ""

	var respErr *llm.ResponseError
	if errors.As(err, &respErr) {
		if respErr.Detail.Type != "" {
			errType = respErr.Detail.Type
		}

		errCode = respErr.Detail.Code
		requestID = respErr.Detail.RequestID

		return gin.H{
			"error": gin.H{
				"message": respErr.Detail.Message,
				"type":    errType,
				"code":    errCode,
			},
			"request_id": requestID,
		}
	}

	var httpErr *httpclient.Error
	if errors.As(err, &httpErr) && len(httpErr.Body) > 0 {
		if t := gjson.GetBytes(httpErr.Body, "error.type"); t.Exists() && t.Type == gjson.String && t.String() != "" {
			errType = t.String()
		}

		if c := gjson.GetBytes(httpErr.Body, "error.code"); c.Exists() && c.Type == gjson.String && c.String() != "" {
			errCode = c.String()
		}

		if rid := gjson.GetBytes(httpErr.Body, "request_id"); rid.Exists() && rid.Type == gjson.String && rid.String() != "" {
			requestID = rid.String()
		}
	}

	return gin.H{
		"error": gin.H{
			"message": orchestrator.ExtractErrorMessage(err),
			"type":    errType,
			"code":    errCode,
		},
		"request_id": requestID,
	}
}
