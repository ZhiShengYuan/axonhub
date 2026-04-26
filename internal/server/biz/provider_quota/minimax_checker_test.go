package provider_quota

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/looplj/axonhub/internal/ent"
	"github.com/looplj/axonhub/internal/ent/channel"
	"github.com/looplj/axonhub/internal/objects"
	"github.com/looplj/axonhub/llm/httpclient"
)

func TestMiniMaxQuotaChecker_Success(t *testing.T) {
	httpClient := httpclient.NewHttpClientWithClient(&http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			require.Equal(t, "Bearer test-api-key", req.Header.Get("Authorization"))

			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     make(http.Header),
				Body: io.NopCloser(strings.NewReader(`{
					"interval": {
						"current_interval_total_count": 100,
						"current_interval_usage_count": 30,
						"start_time": "2024-01-01T00:00:00Z",
						"end_time": "2024-01-02T00:00:00Z"
					},
					"weekly": {
						"weekly_total_count": 500,
						"weekly_usage_count": 150,
						"start_time": "2024-01-01T00:00:00Z",
						"end_time": "2024-01-07T00:00:00Z"
					}
				}`)),
			}, nil
		}),
	})

	checker := NewMiniMaxQuotaChecker(httpClient)

	quota, err := checker.CheckQuota(context.Background(), &ent.Channel{
		Type: channel.TypeMinimax,
		Credentials: objects.ChannelCredentials{
			APIKey: "test-api-key",
		},
	})
	require.NoError(t, err)
	require.Equal(t, "available", quota.Status)
	require.Equal(t, "minimax", quota.ProviderType)
	require.True(t, quota.Ready)
	require.NotNil(t, quota.Summary)
	require.Equal(t, 0.3, *quota.Summary.UsageRatio)
	require.Equal(t, "Interval", quota.Summary.PeriodLabel)
	require.False(t, quota.Summary.Partial)
	require.NotNil(t, quota.NextResetAt)
	expectedReset, _ := time.Parse(time.RFC3339, "2024-01-02T00:00:00Z")
	require.Equal(t, expectedReset, *quota.NextResetAt)
	require.NotNil(t, quota.RawData)
	require.Contains(t, quota.RawData, "interval")
	require.Contains(t, quota.RawData, "weekly")
	require.Contains(t, quota.RawData, "remains")
}

func TestMiniMaxQuotaChecker_Warning(t *testing.T) {
	httpClient := httpclient.NewHttpClientWithClient(&http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     make(http.Header),
				Body: io.NopCloser(strings.NewReader(`{
					"interval": {
						"current_interval_total_count": 100,
						"current_interval_usage_count": 85,
						"end_time": "2024-01-02T00:00:00Z"
					}
				}`)),
			}, nil
		}),
	})

	checker := NewMiniMaxQuotaChecker(httpClient)

	quota, err := checker.CheckQuota(context.Background(), &ent.Channel{
		Type: channel.TypeMinimax,
		Credentials: objects.ChannelCredentials{
			APIKey: "test-api-key",
		},
	})
	require.NoError(t, err)
	require.Equal(t, "warning", quota.Status)
	require.True(t, quota.Ready)
	require.NotNil(t, quota.Summary)
	require.Equal(t, 0.85, *quota.Summary.UsageRatio)
}

func TestMiniMaxQuotaChecker_Exhausted(t *testing.T) {
	httpClient := httpclient.NewHttpClientWithClient(&http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     make(http.Header),
				Body: io.NopCloser(strings.NewReader(`{
					"interval": {
						"current_interval_total_count": 100,
						"current_interval_usage_count": 100,
						"end_time": "2024-01-02T00:00:00Z"
					}
				}`)),
			}, nil
		}),
	})

	checker := NewMiniMaxQuotaChecker(httpClient)

	quota, err := checker.CheckQuota(context.Background(), &ent.Channel{
		Type: channel.TypeMinimax,
		Credentials: objects.ChannelCredentials{
			APIKey: "test-api-key",
		},
	})
	require.NoError(t, err)
	require.Equal(t, "exhausted", quota.Status)
	require.False(t, quota.Ready)
	require.NotNil(t, quota.Summary)
	require.Equal(t, 1.0, *quota.Summary.UsageRatio)
}

func TestMiniMaxQuotaChecker_MissingCredentials(t *testing.T) {
	httpClient := httpclient.NewHttpClientWithClient(&http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			t.Fatal("HTTP request should not be made without credentials")
			return nil, nil
		}),
	})

	checker := NewMiniMaxQuotaChecker(httpClient)

	_, err := checker.CheckQuota(context.Background(), &ent.Channel{
		Type: channel.TypeMinimax,
		Credentials: objects.ChannelCredentials{
			APIKey: "",
		},
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "no credentials")
}

func TestMiniMaxQuotaChecker_MalformedResponse(t *testing.T) {
	httpClient := httpclient.NewHttpClientWithClient(&http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     make(http.Header),
				Body:       io.NopCloser(strings.NewReader(`{invalid json`)),
			}, nil
		}),
	})

	checker := NewMiniMaxQuotaChecker(httpClient)

	_, err := checker.CheckQuota(context.Background(), &ent.Channel{
		Type: channel.TypeMinimax,
		Credentials: objects.ChannelCredentials{
			APIKey: "test-api-key",
		},
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "failed to parse")
}

func TestMiniMaxQuotaChecker_HTTPError(t *testing.T) {
	httpClient := httpclient.NewHttpClientWithClient(&http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusUnauthorized,
				Header:     make(http.Header),
				Body:       io.NopCloser(strings.NewReader(`{"error": "unauthorized"}`)),
			}, nil
		}),
	})

	checker := NewMiniMaxQuotaChecker(httpClient)

	_, err := checker.CheckQuota(context.Background(), &ent.Channel{
		Type: channel.TypeMinimax,
		Credentials: objects.ChannelCredentials{
			APIKey: "test-api-key",
		},
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "quota request failed")
}

func TestMiniMaxQuotaChecker_HTTPError429(t *testing.T) {
	httpClient := httpclient.NewHttpClientWithClient(&http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusTooManyRequests,
				Header:     make(http.Header),
				Body:       io.NopCloser(strings.NewReader(`{"error": "rate limited"}`)),
			}, nil
		}),
	})

	checker := NewMiniMaxQuotaChecker(httpClient)

	_, err := checker.CheckQuota(context.Background(), &ent.Channel{
		Type: channel.TypeMinimax,
		Credentials: objects.ChannelCredentials{
			APIKey: "test-api-key",
		},
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "quota request failed")
}

func TestMiniMaxQuotaChecker_UnknownStatus(t *testing.T) {
	httpClient := httpclient.NewHttpClientWithClient(&http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     make(http.Header),
				Body:       io.NopCloser(strings.NewReader(`{}`)),
			}, nil
		}),
	})

	checker := NewMiniMaxQuotaChecker(httpClient)

	quota, err := checker.CheckQuota(context.Background(), &ent.Channel{
		Type: channel.TypeMinimax,
		Credentials: objects.ChannelCredentials{
			APIKey: "test-api-key",
		},
	})
	require.NoError(t, err)
	require.Equal(t, "unknown", quota.Status)
	require.False(t, quota.Ready)
	require.NotNil(t, quota.Summary)
	require.True(t, quota.Summary.Partial)
}

func TestMiniMaxQuotaChecker_SupportsChannel(t *testing.T) {
	httpClient := httpclient.NewHttpClientWithClient(&http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     make(http.Header),
				Body:       io.NopCloser(strings.NewReader(`{}`)),
			}, nil
		}),
	})

	checker := NewMiniMaxQuotaChecker(httpClient)

	require.True(t, checker.SupportsChannel(&ent.Channel{Type: channel.TypeMinimax}))
	require.True(t, checker.SupportsChannel(&ent.Channel{Type: channel.TypeMinimaxAnthropic}))
	require.False(t, checker.SupportsChannel(&ent.Channel{Type: channel.TypeCodex}))
	require.False(t, checker.SupportsChannel(&ent.Channel{Type: channel.TypeClaudecode}))
}

func TestMiniMaxQuotaChecker_WeeklyNotUsedForSummary(t *testing.T) {
	httpClient := httpclient.NewHttpClientWithClient(&http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     make(http.Header),
				Body: io.NopCloser(strings.NewReader(`{
					"interval": {
						"current_interval_total_count": 100,
						"current_interval_usage_count": 20,
						"end_time": "2024-01-02T00:00:00Z"
					},
					"weekly": {
						"weekly_total_count": 500,
						"weekly_usage_count": 400,
						"end_time": "2024-01-07T00:00:00Z"
					}
				}`)),
			}, nil
		}),
	})

	checker := NewMiniMaxQuotaChecker(httpClient)

	quota, err := checker.CheckQuota(context.Background(), &ent.Channel{
		Type: channel.TypeMinimax,
		Credentials: objects.ChannelCredentials{
			APIKey: "test-api-key",
		},
	})
	require.NoError(t, err)
	require.Equal(t, "available", quota.Status)
	require.Equal(t, "Interval", quota.Summary.PeriodLabel)
	require.Equal(t, 0.2, *quota.Summary.UsageRatio)
}