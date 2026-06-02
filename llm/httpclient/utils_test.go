package httpclient

import (
	"bytes"
	"compress/flate"
	"compress/gzip"
	"compress/zlib"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/klauspost/compress/zstd"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestReadHTTPRequest_NoContentEncoding(t *testing.T) {
	body := []byte(`{"model":"gpt-4","messages":[{"role":"user","content":"hello"}]}`)
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	got, err := ReadHTTPRequest(req)
	require.NoError(t, err)
	assert.Equal(t, body, got.Body)
	assert.Equal(t, "", got.Headers.Get("Content-Encoding"))
}

func TestReadHTTPRequest_IdentityEncoding(t *testing.T) {
	body := []byte(`{"model":"gpt-4","messages":[{"role":"user","content":"hello"}]}`)
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Content-Encoding", "identity")

	got, err := ReadHTTPRequest(req)
	require.NoError(t, err)
	assert.Equal(t, body, got.Body)
	assert.Equal(t, "identity", got.Headers.Get("Content-Encoding"))
}

func TestReadHTTPRequest_GzipEncoding(t *testing.T) {
	originalBody := []byte(`{"model":"gpt-4","messages":[{"role":"user","content":"hello"}]}`)

	var buf bytes.Buffer
	writer := gzip.NewWriter(&buf)
	_, err := writer.Write(originalBody)
	require.NoError(t, err)
	writer.Close()

	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(buf.Bytes()))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Content-Encoding", "gzip")

	got, err := ReadHTTPRequest(req)
	require.NoError(t, err)
	assert.Equal(t, originalBody, got.Body)
	assert.Equal(t, "", got.Headers.Get("Content-Encoding"))
	assert.Equal(t, "", got.Headers.Get("Content-Length"))
}

func TestReadHTTPRequest_GzipEncodingXGzip(t *testing.T) {
	originalBody := []byte(`{"model":"gpt-4"}`)

	var buf bytes.Buffer
	writer := gzip.NewWriter(&buf)
	_, err := writer.Write(originalBody)
	require.NoError(t, err)
	writer.Close()

	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(buf.Bytes()))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Content-Encoding", "x-gzip")

	got, err := ReadHTTPRequest(req)
	require.NoError(t, err)
	assert.Equal(t, originalBody, got.Body)
	assert.Equal(t, "", got.Headers.Get("Content-Encoding"))
}

func TestReadHTTPRequest_DeflateEncoding(t *testing.T) {
	originalBody := []byte(`{"model":"gpt-4","messages":[{"role":"user","content":"hello"}]}`)

	var buf bytes.Buffer
	writer, err := flate.NewWriter(&buf, flate.DefaultCompression)
	require.NoError(t, err)
	_, err = writer.Write(originalBody)
	require.NoError(t, err)
	writer.Close()

	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(buf.Bytes()))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Content-Encoding", "deflate")

	got, err := ReadHTTPRequest(req)
	require.NoError(t, err)
	assert.Equal(t, originalBody, got.Body)
	assert.Equal(t, "", got.Headers.Get("Content-Encoding"))
	assert.Equal(t, "", got.Headers.Get("Content-Length"))
}

func TestReadHTTPRequest_DeflateZlibEncoding(t *testing.T) {
	originalBody := []byte(`{"model":"gpt-4","messages":[{"role":"user","content":"hello"}]}`)

	var buf bytes.Buffer
	writer := zlib.NewWriter(&buf)
	_, err := writer.Write(originalBody)
	require.NoError(t, err)
	writer.Close()

	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(buf.Bytes()))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Content-Encoding", "deflate")

	got, err := ReadHTTPRequest(req)
	require.NoError(t, err)
	assert.Equal(t, originalBody, got.Body)
	assert.Equal(t, "", got.Headers.Get("Content-Encoding"))
	assert.Equal(t, "", got.Headers.Get("Content-Length"))
}

func TestReadHTTPRequest_ZstdEncoding(t *testing.T) {
	originalBody := []byte(`{"model":"gpt-4","messages":[{"role":"user","content":"hello"}]}`)

	encoder, err := zstd.NewWriter(nil)
	require.NoError(t, err)
	compressedBody := encoder.EncodeAll(originalBody, nil)
	encoder.Close()

	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(compressedBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Content-Encoding", "zstd")

	got, err := ReadHTTPRequest(req)
	require.NoError(t, err)
	assert.Equal(t, originalBody, got.Body)
	assert.Equal(t, "", got.Headers.Get("Content-Encoding"))
	assert.Equal(t, "", got.Headers.Get("Content-Length"))
}

func TestReadHTTPRequest_EncodingCaseInsensitive(t *testing.T) {
	tests := []struct {
		name     string
		encoding string
		compress func(t *testing.T, body []byte) []byte
	}{
		{
			name:     "gzip uppercase",
			encoding: "GZIP",
			compress: func(t *testing.T, body []byte) []byte {
				var buf bytes.Buffer
				writer := gzip.NewWriter(&buf)
				_, err := writer.Write(body)
				require.NoError(t, err)
				writer.Close()
				return buf.Bytes()
			},
		},
		{
			name:     "deflate uppercase",
			encoding: "DEFLATE",
			compress: func(t *testing.T, body []byte) []byte {
				var buf bytes.Buffer
				writer, err := flate.NewWriter(&buf, flate.DefaultCompression)
				require.NoError(t, err)
				_, err = writer.Write(body)
				require.NoError(t, err)
				writer.Close()
				return buf.Bytes()
			},
		},
		{
			name:     "zstd with spaces",
			encoding: "  ZSTD  ",
			compress: func(t *testing.T, body []byte) []byte {
				encoder, err := zstd.NewWriter(nil)
				require.NoError(t, err)
				compressed := encoder.EncodeAll(body, nil)
				encoder.Close()
				return compressed
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			originalBody := []byte(`{"model":"gpt-4"}`)
			compressedBody := tt.compress(t, originalBody)

			req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(compressedBody))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Content-Encoding", tt.encoding)

			got, err := ReadHTTPRequest(req)
			require.NoError(t, err)
			assert.Equal(t, originalBody, got.Body)
		})
	}
}

func TestReadHTTPRequest_UnsupportedContentEncoding(t *testing.T) {
	body := []byte(`{"model":"gpt-4"}`)
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Content-Encoding", "br")

	_, err := ReadHTTPRequest(req)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported content encoding")
}

func TestReadHTTPRequest_InvalidGzipData(t *testing.T) {
	invalidData := []byte("this is not valid gzip compressed data")
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(invalidData))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Content-Encoding", "gzip")

	_, err := ReadHTTPRequest(req)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to create gzip reader")
}

func TestReadHTTPRequest_InvalidDeflateData(t *testing.T) {
	invalidData := []byte("this is not valid deflate compressed data")
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(invalidData))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Content-Encoding", "deflate")

	_, err := ReadHTTPRequest(req)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to decompress deflate body")
}

func TestReadHTTPRequest_InvalidZstdData(t *testing.T) {
	invalidData := []byte("this is not valid zstd compressed data")
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(invalidData))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Content-Encoding", "zstd")

	_, err := ReadHTTPRequest(req)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to decode zstd compressed body")
}

func TestReadHTTPRequest_EmptyBody(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	req.Header.Set("Content-Type", "application/json")

	got, err := ReadHTTPRequest(req)
	require.NoError(t, err)
	assert.Empty(t, got.Body)
}

func TestReadHTTPRequest_EmptyBodyWithContentEncoding(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Content-Encoding", "zstd")

	got, err := ReadHTTPRequest(req)
	require.NoError(t, err)
	assert.Empty(t, got.Body)
}

func TestDecodeRequestBody_NoEncoding(t *testing.T) {
	body := []byte(`{"test":"data"}`)
	headers := http.Header{}

	got, err := decodeRequestBody(body, headers)
	require.NoError(t, err)
	assert.Equal(t, body, got)
}

func TestDecodeRequestBody_IdentityEncoding(t *testing.T) {
	body := []byte(`{"test":"data"}`)
	headers := http.Header{}
	headers.Set("Content-Encoding", "identity")

	got, err := decodeRequestBody(body, headers)
	require.NoError(t, err)
	assert.Equal(t, body, got)
}

func TestDecodeRequestBody_GzipEncoding(t *testing.T) {
	originalBody := []byte(`{"test":"data"}`)

	var buf bytes.Buffer
	writer := gzip.NewWriter(&buf)
	_, err := writer.Write(originalBody)
	require.NoError(t, err)
	writer.Close()

	headers := http.Header{}
	headers.Set("Content-Encoding", "gzip")
	headers.Set("Content-Length", "100")

	got, err := decodeRequestBody(buf.Bytes(), headers)
	require.NoError(t, err)
	assert.Equal(t, originalBody, got)
	assert.Equal(t, "", headers.Get("Content-Encoding"))
	assert.Equal(t, "", headers.Get("Content-Length"))
}

func TestDecodeRequestBody_DeflateEncoding(t *testing.T) {
	originalBody := []byte(`{"test":"data"}`)

	var buf bytes.Buffer
	writer, err := flate.NewWriter(&buf, flate.DefaultCompression)
	require.NoError(t, err)
	_, err = writer.Write(originalBody)
	require.NoError(t, err)
	writer.Close()

	headers := http.Header{}
	headers.Set("Content-Encoding", "deflate")

	got, err := decodeRequestBody(buf.Bytes(), headers)
	require.NoError(t, err)
	assert.Equal(t, originalBody, got)
	assert.Equal(t, "", headers.Get("Content-Encoding"))
}

func TestDecodeRequestBody_ZstdEncoding(t *testing.T) {
	originalBody := []byte(`{"test":"data"}`)

	encoder, err := zstd.NewWriter(nil)
	require.NoError(t, err)
	compressedBody := encoder.EncodeAll(originalBody, nil)
	encoder.Close()

	headers := http.Header{}
	headers.Set("Content-Encoding", "zstd")

	got, err := decodeRequestBody(compressedBody, headers)
	require.NoError(t, err)
	assert.Equal(t, originalBody, got)
	assert.Equal(t, "", headers.Get("Content-Encoding"))
}

func TestMergeHTTPHeaders_BlocksXForwardedFor(t *testing.T) {
	// Test that X-Forwarded-For is NOT merged from source to destination
	// even when explicitly present in source headers.
	// This is because X-Forwarded-For is in the blockedHeaders map.

	dest := http.Header{}
	src := http.Header{}
	src.Set("X-Forwarded-For", "203.0.113.9")
	src.Set("User-Agent", "Test/1.0")

	result := MergeHTTPHeaders(dest, src)

	// X-Forwarded-For should be blocked (not merged)
	assert.Empty(t, result.Get("X-Forwarded-For"), "X-Forwarded-For should be blocked by MergeHTTPHeaders")
	// User-Agent should be merged (not blocked)
	assert.Equal(t, "Test/1.0", result.Get("User-Agent"), "User-Agent should be merged normally")
}

func TestMergeHTTPHeaders_BlocksOtherHopByHopHeaders(t *testing.T) {
	// Verify multiple hop-by-hop headers are blocked from merge
	dest := http.Header{}
	src := http.Header{}

	// Set various headers that should be blocked
	src.Set("X-Forwarded-For", "203.0.113.9")
	src.Set("X-Forwarded-Proto", "https")
	src.Set("X-Forwarded-Host", "example.com")
	src.Set("X-Real-Ip", "192.168.1.1")
	src.Set("Connection", "keep-alive")
	src.Set("Keep-Alive", "timeout=5")
	src.Set("User-Agent", "Test/1.0")

	result := MergeHTTPHeaders(dest, src)

	// All hop-by-hop and proxy headers should be blocked
	assert.Empty(t, result.Get("X-Forwarded-For"))
	assert.Empty(t, result.Get("X-Forwarded-Proto"))
	assert.Empty(t, result.Get("X-Forwarded-Host"))
	assert.Empty(t, result.Get("X-Real-Ip"))
	assert.Empty(t, result.Get("Connection"))
	assert.Empty(t, result.Get("Keep-Alive"))
	// User-Agent should pass through
	assert.Equal(t, "Test/1.0", result.Get("User-Agent"))
}

