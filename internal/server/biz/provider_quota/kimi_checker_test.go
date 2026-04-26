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

func TestKimiQuotaChecker_Success(t *testing.T) {
	httpClient := httpclient.NewHttpClientWithClient(&http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			require.Equal(t, "Bearer test-api-key", req.Header.Get("Authorization"))

			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     make(http.Header),
				Body: io.NopCloser(strings.NewReader(`{
					"user": {
						"userId": "d7ienj3id4meg9jcogl0",
						"region": "REGION_CN",
						"membership": {"level": "LEVEL_INTERMEDIATE"},
						"businessId": ""
					},
					"usage": {
						"limit": "100",
						"remaining": "100",
						"resetTime": "2026-05-03T11:03:48.011825Z"
					},
					"limits": [
						{
							"window": {"duration": 300, "timeUnit": "TIME_UNIT_MINUTE"},
							"detail": {
								"limit": "100",
								"remaining": "100",
								"resetTime": "2026-04-26T16:03:48.011825Z"
							}
						}
					],
					"parallel": {"limit": "20"},
					"totalQuota": {"limit": "100", "remaining": "100"},
					"authentication": {"method": "METHOD_API_KEY", "scope": "FEATURE_CODING"},
					"subType": "TYPE_PURCHASE"
				}`)),
			}, nil
		}),
	})

	checker := NewKimiQuotaChecker(httpClient)

	quota, err := checker.CheckQuota(context.Background(), &ent.Channel{
		Type: channel.TypeKimi,
		Credentials: objects.ChannelCredentials{
			APIKey: "test-api-key",
		},
	})
	require.NoError(t, err)
	require.Equal(t, "available", quota.Status)
	require.Equal(t, "kimi", quota.ProviderType)
	require.True(t, quota.Ready)
	require.NotNil(t, quota.Summary)

	require.InDelta(t, 0.0, *quota.Summary.UsageRatio, 0.001)
	require.Equal(t, "Interval", quota.Summary.PeriodLabel)
	require.False(t, quota.Summary.Partial)

	require.NotNil(t, quota.Summary.ProviderUsedCount)
	require.Equal(t, int64(0), *quota.Summary.ProviderUsedCount)

	require.NotNil(t, quota.Summary.ProviderTotalCount)
	require.Equal(t, int64(100), *quota.Summary.ProviderTotalCount)

	require.NotNil(t, quota.Summary.ProviderRemainingCount)
	require.Equal(t, int64(100), *quota.Summary.ProviderRemainingCount)

	require.NotNil(t, quota.Summary.ProviderUsedPercentage)
	require.InDelta(t, 0.0, *quota.Summary.ProviderUsedPercentage, 0.1)

	require.NotNil(t, quota.NextResetAt)
	expectedReset, _ := time.Parse(time.RFC3339, "2026-05-03T11:03:48.011825Z")
	require.Equal(t, expectedReset, *quota.NextResetAt)

	require.NotNil(t, quota.RawData)
	require.Contains(t, quota.RawData, "usages")
	require.Contains(t, quota.RawData, "totalQuota")
}

func TestKimiQuotaChecker_Warning(t *testing.T) {
	httpClient := httpclient.NewHttpClientWithClient(&http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     make(http.Header),
				Body: io.NopCloser(strings.NewReader(`{
					"user": {"userId": "test", "region": "REGION_CN", "membership": {"level": "LEVEL_BASIC"}, "businessId": ""},
					"usage": {
						"limit": "100",
						"remaining": "15",
						"resetTime": "2026-05-03T11:03:48.011825Z"
					},
					"limits": [],
					"parallel": {"limit": "20"},
					"totalQuota": {"limit": "100", "remaining": "15"},
					"authentication": {"method": "METHOD_API_KEY", "scope": "FEATURE_CODING"},
					"subType": "TYPE_PURCHASE"
				}`)),
			}, nil
		}),
	})

	checker := NewKimiQuotaChecker(httpClient)

	quota, err := checker.CheckQuota(context.Background(), &ent.Channel{
		Type: channel.TypeKimi,
		Credentials: objects.ChannelCredentials{
			APIKey: "test-api-key",
		},
	})
	require.NoError(t, err)
	require.Equal(t, "warning", quota.Status)
	require.True(t, quota.Ready)
	require.NotNil(t, quota.Summary)

	require.Equal(t, int64(85), *quota.Summary.ProviderUsedCount)
	require.Equal(t, int64(15), *quota.Summary.ProviderRemainingCount)
	require.InDelta(t, 0.85, *quota.Summary.UsageRatio, 0.001)
}

func TestKimiQuotaChecker_Exhausted(t *testing.T) {
	httpClient := httpclient.NewHttpClientWithClient(&http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     make(http.Header),
				Body: io.NopCloser(strings.NewReader(`{
					"user": {"userId": "test", "region": "REGION_CN", "membership": {"level": "LEVEL_BASIC"}, "businessId": ""},
					"usage": {
						"limit": "100",
						"remaining": "0",
						"resetTime": "2026-05-03T11:03:48.011825Z"
					},
					"limits": [],
					"parallel": {"limit": "20"},
					"totalQuota": {"limit": "100", "remaining": "0"},
					"authentication": {"method": "METHOD_API_KEY", "scope": "FEATURE_CODING"},
					"subType": "TYPE_PURCHASE"
				}`)),
			}, nil
		}),
	})

	checker := NewKimiQuotaChecker(httpClient)

	quota, err := checker.CheckQuota(context.Background(), &ent.Channel{
		Type: channel.TypeKimi,
		Credentials: objects.ChannelCredentials{
			APIKey: "test-api-key",
		},
	})
	require.NoError(t, err)
	require.Equal(t, "exhausted", quota.Status)
	require.False(t, quota.Ready)
	require.NotNil(t, quota.Summary)

	require.InDelta(t, 1.0, *quota.Summary.UsageRatio, 0.001)
}

func TestKimiQuotaChecker_MissingCredentials(t *testing.T) {
	httpClient := httpclient.NewHttpClientWithClient(&http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			t.Fatal("HTTP request should not be made without credentials")
			return nil, nil
		}),
	})

	checker := NewKimiQuotaChecker(httpClient)

	_, err := checker.CheckQuota(context.Background(), &ent.Channel{
		Type: channel.TypeKimi,
		Credentials: objects.ChannelCredentials{
			APIKey: "",
		},
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "no API key")
}

func TestKimiQuotaChecker_MalformedResponse(t *testing.T) {
	httpClient := httpclient.NewHttpClientWithClient(&http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     make(http.Header),
				Body:       io.NopCloser(strings.NewReader(`{invalid json`)),
			}, nil
		}),
	})

	checker := NewKimiQuotaChecker(httpClient)

	_, err := checker.CheckQuota(context.Background(), &ent.Channel{
		Type: channel.TypeKimi,
		Credentials: objects.ChannelCredentials{
			APIKey: "test-api-key",
		},
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "failed to parse")
}

func TestKimiQuotaChecker_HTTPError(t *testing.T) {
	httpClient := httpclient.NewHttpClientWithClient(&http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusUnauthorized,
				Header:     make(http.Header),
				Body:       io.NopCloser(strings.NewReader(`{"error": "unauthorized"}`)),
			}, nil
		}),
	})

	checker := NewKimiQuotaChecker(httpClient)

	_, err := checker.CheckQuota(context.Background(), &ent.Channel{
		Type: channel.TypeKimi,
		Credentials: objects.ChannelCredentials{
			APIKey: "test-api-key",
		},
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "quota request failed")
}

func TestKimiQuotaChecker_StringLimitParsed(t *testing.T) {
	httpClient := httpclient.NewHttpClientWithClient(&http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     make(http.Header),
				Body: io.NopCloser(strings.NewReader(`{
					"user": {"userId": "test", "region": "REGION_CN", "membership": {"level": "LEVEL_INTERMEDIATE"}, "businessId": ""},
					"usage": {
						"limit": "1000",
						"remaining": "750",
						"resetTime": "2026-05-03T11:03:48.011825Z"
					},
					"limits": [],
					"parallel": {"limit": "20"},
					"totalQuota": {"limit": "1000", "remaining": "750"},
					"authentication": {"method": "METHOD_API_KEY", "scope": "FEATURE_CODING"},
					"subType": "TYPE_PURCHASE"
				}`)),
			}, nil
		}),
	})

	checker := NewKimiQuotaChecker(httpClient)

	quota, err := checker.CheckQuota(context.Background(), &ent.Channel{
		Type: channel.TypeKimi,
		Credentials: objects.ChannelCredentials{
			APIKey: "test-api-key",
		},
	})
	require.NoError(t, err)
	require.Equal(t, "available", quota.Status)

	require.Equal(t, int64(250), *quota.Summary.ProviderUsedCount)
	require.Equal(t, int64(1000), *quota.Summary.ProviderTotalCount)
	require.Equal(t, int64(750), *quota.Summary.ProviderRemainingCount)
	require.InDelta(t, 0.25, *quota.Summary.UsageRatio, 0.001)
	require.InDelta(t, 25.0, *quota.Summary.ProviderUsedPercentage, 0.1)
}

func TestKimiQuotaChecker_SupportsChannel(t *testing.T) {
	httpClient := httpclient.NewHttpClientWithClient(&http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     make(http.Header),
				Body: io.NopCloser(strings.NewReader(`{
					"user": {"userId": "test", "region": "REGION_CN", "membership": {"level": "LEVEL_BASIC"}, "businessId": ""},
					"usage": {"limit": "100", "remaining": "100", "resetTime": "2026-05-03T11:03:48.011825Z"},
					"limits": [],
					"parallel": {"limit": "20"},
					"totalQuota": {"limit": "100", "remaining": "100"},
					"authentication": {"method": "METHOD_API_KEY", "scope": "FEATURE_CODING"},
					"subType": "TYPE_PURCHASE"
				}`)),
			}, nil
		}),
	})

	checker := NewKimiQuotaChecker(httpClient)

	require.True(t, checker.SupportsChannel(&ent.Channel{Type: channel.TypeKimi}))
	require.True(t, checker.SupportsChannel(&ent.Channel{Type: channel.TypeKimiAnthropic}))
	require.False(t, checker.SupportsChannel(&ent.Channel{Type: channel.TypeCodex}))
	require.False(t, checker.SupportsChannel(&ent.Channel{Type: channel.TypeClaudecode}))
}

func TestKimiQuotaChecker_KimiAnthropic(t *testing.T) {
	httpClient := httpclient.NewHttpClientWithClient(&http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     make(http.Header),
				Body: io.NopCloser(strings.NewReader(`{
					"user": {"userId": "test", "region": "REGION_CN", "membership": {"level": "LEVEL_INTERMEDIATE"}, "businessId": ""},
					"usage": {
						"limit": "100",
						"remaining": "50",
						"resetTime": "2026-05-03T11:03:48.011825Z"
					},
					"limits": [],
					"parallel": {"limit": "20"},
					"totalQuota": {"limit": "100", "remaining": "50"},
					"authentication": {"method": "METHOD_API_KEY", "scope": "FEATURE_CODING"},
					"subType": "TYPE_PURCHASE"
				}`)),
			}, nil
		}),
	})

	checker := NewKimiQuotaChecker(httpClient)

	quota, err := checker.CheckQuota(context.Background(), &ent.Channel{
		Type: channel.TypeKimiAnthropic,
		Credentials: objects.ChannelCredentials{
			APIKey: "test-api-key",
		},
	})
	require.NoError(t, err)
	require.Equal(t, "available", quota.Status)
	require.True(t, checker.SupportsChannel(&ent.Channel{Type: channel.TypeKimiAnthropic}))
}

func TestKimiQuotaChecker_PartialMissingLimit(t *testing.T) {
	httpClient := httpclient.NewHttpClientWithClient(&http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     make(http.Header),
				Body: io.NopCloser(strings.NewReader(`{
					"user": {"userId": "test", "region": "REGION_CN", "membership": {"level": "LEVEL_INTERMEDIATE"}, "businessId": ""},
					"usage": {
						"limit": "",
						"remaining": "",
						"resetTime": "2026-05-03T11:03:48.011825Z"
					},
					"limits": [],
					"parallel": {"limit": "20"},
					"totalQuota": {"limit": "", "remaining": ""},
					"authentication": {"method": "METHOD_API_KEY", "scope": "FEATURE_CODING"},
					"subType": "TYPE_PURCHASE"
				}`)),
			}, nil
		}),
	})

	checker := NewKimiQuotaChecker(httpClient)

	quota, err := checker.CheckQuota(context.Background(), &ent.Channel{
		Type: channel.TypeKimi,
		Credentials: objects.ChannelCredentials{
			APIKey: "test-api-key",
		},
	})
	require.NoError(t, err)
	require.Equal(t, "unknown", quota.Status)
	require.False(t, quota.Ready)
	require.NotNil(t, quota.Summary)
	require.True(t, quota.Summary.Partial)
	require.Nil(t, quota.Summary.UsageRatio)
	require.Nil(t, quota.Summary.ProviderUsedCount)
	require.Nil(t, quota.Summary.ProviderTotalCount)
	require.Nil(t, quota.Summary.ProviderRemainingCount)
	require.Nil(t, quota.Summary.ProviderUsedPercentage)
}

func TestKimiQuotaChecker_MalformedNumeric(t *testing.T) {
	httpClient := httpclient.NewHttpClientWithClient(&http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     make(http.Header),
				Body: io.NopCloser(strings.NewReader(`{
					"user": {"userId": "test", "region": "REGION_CN", "membership": {"level": "LEVEL_INTERMEDIATE"}, "businessId": ""},
					"usage": {
						"limit": "not-a-number",
						"remaining": "also-bad",
						"resetTime": "2026-05-03T11:03:48.011825Z"
					},
					"limits": [],
					"parallel": {"limit": "20"},
					"totalQuota": {"limit": "not-a-number", "remaining": "also-bad"},
					"authentication": {"method": "METHOD_API_KEY", "scope": "FEATURE_CODING"},
					"subType": "TYPE_PURCHASE"
				}`)),
			}, nil
		}),
	})

	checker := NewKimiQuotaChecker(httpClient)

	quota, err := checker.CheckQuota(context.Background(), &ent.Channel{
		Type: channel.TypeKimi,
		Credentials: objects.ChannelCredentials{
			APIKey: "test-api-key",
		},
	})
	require.NoError(t, err)
	require.Equal(t, "unknown", quota.Status)
	require.False(t, quota.Ready)
	require.NotNil(t, quota.Summary)
	require.True(t, quota.Summary.Partial)
	require.Nil(t, quota.Summary.UsageRatio)
	require.Nil(t, quota.Summary.ProviderUsedCount)
	require.Nil(t, quota.Summary.ProviderTotalCount)
	require.Nil(t, quota.Summary.ProviderRemainingCount)
	require.Nil(t, quota.Summary.ProviderUsedPercentage)
}