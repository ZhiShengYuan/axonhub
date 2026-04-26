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
					"model_remains": [
						{
							"start_time": 1777204800000,
							"end_time": 1777219200000,
							"remains_time": 5225062,
							"current_interval_total_count": 4500,
							"current_interval_usage_count": 4393,
							"model_name": "MiniMax-M2.5",
							"current_weekly_total_count": 0,
							"current_weekly_usage_count": 0,
							"weekly_start_time": 1776614400000,
							"weekly_end_time": 1777219200000,
							"weekly_remains_time": 5225062
						}
					],
					"base_resp": {"status_code": 0, "status_msg": "success"}
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

	// UsageRatio should be ~0.0238 (107/4500 = 2.38%)
	// Used = total - remaining = 4500 - 4393 = 107
	// UsageRatio = 107/4500 = 0.02378
	require.InDelta(t, 0.0238, *quota.Summary.UsageRatio, 0.001)
	require.Equal(t, "Interval", quota.Summary.PeriodLabel)
	require.False(t, quota.Summary.Partial)

	// Verify ProviderUsedCount = total - remaining = 107 (NOT the usage_count field directly)
	require.NotNil(t, quota.Summary.ProviderUsedCount)
	require.Equal(t, int64(107), *quota.Summary.ProviderUsedCount)

	// Verify ProviderTotalCount = 4500
	require.NotNil(t, quota.Summary.ProviderTotalCount)
	require.Equal(t, int64(4500), *quota.Summary.ProviderTotalCount)

	// Verify ProviderRemainingCount = usage_count field directly = 4393
	require.NotNil(t, quota.Summary.ProviderRemainingCount)
	require.Equal(t, int64(4393), *quota.Summary.ProviderRemainingCount)

	// Verify ProviderUsedPercentage = 2.38% (107/4500*100)
	require.NotNil(t, quota.Summary.ProviderUsedPercentage)
	require.InDelta(t, 2.38, *quota.Summary.ProviderUsedPercentage, 0.1)

	// Verify timestamps are correctly converted from milliseconds
	require.NotNil(t, quota.NextResetAt)
	expectedReset := time.Unix(0, 1777219200000*int64(time.Millisecond))
	require.Equal(t, expectedReset, *quota.NextResetAt)

	require.NotNil(t, quota.Summary.PeriodStartAt)
	expectedStart := time.Unix(0, 1777204800000*int64(time.Millisecond))
	require.Equal(t, expectedStart, *quota.Summary.PeriodStartAt)

	require.NotNil(t, quota.RawData)
	require.Contains(t, quota.RawData, "matched_model")
	require.Equal(t, "MiniMax-M2.5", quota.RawData["matched_model"])
	require.Contains(t, quota.RawData, "remains")
	require.Contains(t, quota.RawData, "interval")
}

func TestMiniMaxQuotaChecker_Warning(t *testing.T) {
	httpClient := httpclient.NewHttpClientWithClient(&http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     make(http.Header),
				Body: io.NopCloser(strings.NewReader(`{
					"model_remains": [
						{
							"start_time": 1777204800000,
							"end_time": 1777219200000,
							"remains_time": 5225062,
							"current_interval_total_count": 100,
							"current_interval_usage_count": 15,
							"model_name": "MiniMax-M*"
						}
					],
					"base_resp": {"status_code": 0, "status_msg": "success"}
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

	// Used = 100 - 15 = 85, UsageRatio = 85/100 = 0.85
	require.InDelta(t, 0.85, *quota.Summary.UsageRatio, 0.001)

	// Verify ProviderUsedCount = used = 85
	require.Equal(t, int64(85), *quota.Summary.ProviderUsedCount)
	// Verify ProviderRemainingCount = remaining = 15
	require.Equal(t, int64(15), *quota.Summary.ProviderRemainingCount)
}

func TestMiniMaxQuotaChecker_Exhausted(t *testing.T) {
	httpClient := httpclient.NewHttpClientWithClient(&http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     make(http.Header),
				Body: io.NopCloser(strings.NewReader(`{
					"model_remains": [
						{
							"start_time": 1777204800000,
							"end_time": 1777219200000,
							"current_interval_total_count": 100,
							"current_interval_usage_count": 0,
							"model_name": "MiniMax-M*"
						}
					],
					"base_resp": {"status_code": 0, "status_msg": "success"}
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

	// Used = 100 - 0 = 100, UsageRatio = 100/100 = 1.0
	require.InDelta(t, 1.0, *quota.Summary.UsageRatio, 0.001)
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
	require.Contains(t, err.Error(), "no API key")
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

func TestMiniMaxQuotaChecker_EmptyModelRemains(t *testing.T) {
	httpClient := httpclient.NewHttpClientWithClient(&http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     make(http.Header),
				Body:       io.NopCloser(strings.NewReader(`{"model_remains": [], "base_resp": {"status_code": 0, "status_msg": "success"}}`)),
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
				Body:       io.NopCloser(strings.NewReader(`{"model_remains": [], "base_resp": {"status_code": 0, "status_msg": "success"}}`)),
			}, nil
		}),
	})

	checker := NewMiniMaxQuotaChecker(httpClient)

	require.True(t, checker.SupportsChannel(&ent.Channel{Type: channel.TypeMinimax}))
	require.True(t, checker.SupportsChannel(&ent.Channel{Type: channel.TypeMinimaxAnthropic}))
	require.False(t, checker.SupportsChannel(&ent.Channel{Type: channel.TypeCodex}))
	require.False(t, checker.SupportsChannel(&ent.Channel{Type: channel.TypeClaudecode}))
}

func TestMiniMaxQuotaChecker_MultipleModelsMatchesMiniMaxMFirst(t *testing.T) {
	httpClient := httpclient.NewHttpClientWithClient(&http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     make(http.Header),
				Body: io.NopCloser(strings.NewReader(`{
					"model_remains": [
						{
							"start_time": 1777204800000,
							"end_time": 1777219200000,
							"current_interval_total_count": 19000,
							"current_interval_usage_count": 19000,
							"model_name": "speech-hd"
						},
						{
							"start_time": 1777204800000,
							"end_time": 1777219200000,
							"current_interval_total_count": 4500,
							"current_interval_usage_count": 4393,
							"model_name": "MiniMax-M2.5"
						},
						{
							"start_time": 1777204800000,
							"end_time": 1777219200000,
							"current_interval_total_count": 500,
							"current_interval_usage_count": 200,
							"model_name": "coding-plan-vlm"
						}
					],
					"base_resp": {"status_code": 0, "status_msg": "success"}
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

	// Verify MiniMax-M* is matched first (not speech-hd or coding-plan-vlm)
	require.Equal(t, "MiniMax-M2.5", quota.RawData["matched_model"])

	// Verify we got the MiniMax-M2.5 data (107 used out of 4500)
	require.Equal(t, int64(107), *quota.Summary.ProviderUsedCount)
	require.Equal(t, int64(4500), *quota.Summary.ProviderTotalCount)
	require.Equal(t, int64(4393), *quota.Summary.ProviderRemainingCount)
}

func TestMiniMaxQuotaChecker_CodingPlanFallback(t *testing.T) {
	httpClient := httpclient.NewHttpClientWithClient(&http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     make(http.Header),
				Body: io.NopCloser(strings.NewReader(`{
					"model_remains": [
						{
							"start_time": 1777204800000,
							"end_time": 1777219200000,
							"current_interval_total_count": 500,
							"current_interval_usage_count": 200,
							"model_name": "coding-plan-vlm"
						},
						{
							"start_time": 1777204800000,
							"end_time": 1777219200000,
							"current_interval_total_count": 19000,
							"current_interval_usage_count": 19000,
							"model_name": "speech-hd"
						}
					],
					"base_resp": {"status_code": 0, "status_msg": "success"}
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

	// Verify coding-plan is matched (no MiniMax-M* exists)
	require.Equal(t, "coding-plan-vlm", quota.RawData["matched_model"])
}

func TestMiniMaxQuotaChecker_FirstEntryFallback(t *testing.T) {
	httpClient := httpclient.NewHttpClientWithClient(&http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     make(http.Header),
				Body: io.NopCloser(strings.NewReader(`{
					"model_remains": [
						{
							"start_time": 1777204800000,
							"end_time": 1777219200000,
							"current_interval_total_count": 19000,
							"current_interval_usage_count": 19000,
							"model_name": "speech-hd"
						}
					],
					"base_resp": {"status_code": 0, "status_msg": "success"}
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

	// Verify first entry is matched (no MiniMax-M* or coding-plan)
	require.Equal(t, "speech-hd", quota.RawData["matched_model"])
}

func TestMiniMaxQuotaChecker_NonZeroBaseRespStatus(t *testing.T) {
	httpClient := httpclient.NewHttpClientWithClient(&http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     make(http.Header),
				Body: io.NopCloser(strings.NewReader(`{
					"model_remains": [
						{
							"start_time": 1777204800000,
							"end_time": 1777219200000,
							"current_interval_total_count": 4500,
							"current_interval_usage_count": 4393,
							"model_name": "MiniMax-M2.5"
						}
					],
					"base_resp": {"status_code": 1, "status_msg": "some warning"}
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
	// Should still parse model data if available
	require.Equal(t, "available", quota.Status)
	require.Equal(t, "MiniMax-M2.5", quota.RawData["matched_model"])
}

func TestMiniMaxQuotaChecker_WeeklyDataIncluded(t *testing.T) {
	httpClient := httpclient.NewHttpClientWithClient(&http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     make(http.Header),
				Body: io.NopCloser(strings.NewReader(`{
					"model_remains": [
						{
							"start_time": 1777204800000,
							"end_time": 1777219200000,
							"current_interval_total_count": 4500,
							"current_interval_usage_count": 4393,
							"model_name": "MiniMax-M2.5",
							"current_weekly_total_count": 10000,
							"current_weekly_usage_count": 9500,
							"weekly_start_time": 1776614400000,
							"weekly_end_time": 1777219200000,
							"weekly_remains_time": 5225062
						}
					],
					"base_resp": {"status_code": 0, "status_msg": "success"}
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

	// Weekly data should be included in rawData
	require.Contains(t, quota.RawData, "weekly")
	weekly := quota.RawData["weekly"].(map[string]any)
	require.Equal(t, int64(10000), weekly["current_weekly_total_count"])
	require.Equal(t, int64(9500), weekly["current_weekly_usage_count"])
}

func TestMsToTime(t *testing.T) {
	// Test millisecond to time.Time conversion
	ms := int64(1777219200000)
	result := msToTime(ms)
	expected := time.Unix(0, ms*int64(time.Millisecond))
	require.Equal(t, expected, result)
}