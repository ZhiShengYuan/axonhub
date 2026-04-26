package provider_quota

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/looplj/axonhub/internal/ent"
	"github.com/looplj/axonhub/internal/objects"
	"github.com/looplj/axonhub/llm/httpclient"
)

type zhipuRoundTripFunc func(req *http.Request) (*http.Response, error)

func (f zhipuRoundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestZhipuQuotaChecker_SupportsChannel(t *testing.T) {
	checker := NewZhipuQuotaChecker(nil)

	require.True(t, checker.SupportsChannel(&ent.Channel{Type: "zhipu"}))
	require.True(t, checker.SupportsChannel(&ent.Channel{Type: "zhipu_anthropic"}))
	require.False(t, checker.SupportsChannel(&ent.Channel{Type: "claudecode"}))
	require.False(t, checker.SupportsChannel(&ent.Channel{Type: "codex"}))
	require.False(t, checker.SupportsChannel(&ent.Channel{Type: "minimax"}))
}

func TestZhipuQuotaChecker_MissingCredentials(t *testing.T) {
	httpClient := httpclient.NewHttpClientWithClient(&http.Client{
		Transport: zhipuRoundTripFunc(func(req *http.Request) (*http.Response, error) {
			t.Fatal("should not be called")
			return nil, nil
		}),
	})

	checker := NewZhipuQuotaChecker(httpClient)

	_, err := checker.CheckQuota(context.Background(), &ent.Channel{
		Type:        "zhipu",
		Credentials: objects.ChannelCredentials{},
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "no API key")
}

func TestZhipuQuotaChecker_CompleteLimitAndUsage(t *testing.T) {
	requestCount := 0
	var capturedReqs []*http.Request

	httpClient := httpclient.NewHttpClientWithClient(&http.Client{
		Transport: zhipuRoundTripFunc(func(req *http.Request) (*http.Response, error) {
			capturedReqs = append(capturedReqs, req)
			requestCount++

			if req.URL.Path == "/api/monitor/usage/model-usage" {
				return &http.Response{
					StatusCode: http.StatusOK,
					Header:     make(http.Header),
					Body: io.NopCloser(strings.NewReader(`{
						"modelDataList": [{"model": "glm-4"}],
						"totalUsage": {"totalModelCallCount": 100, "totalTokensUsage": 50000}
					}`)),
				}, nil
			}
			if req.URL.Path == "/api/monitor/usage/tool-usage" {
				return &http.Response{
					StatusCode: http.StatusOK,
					Header:     make(http.Header),
					Body: io.NopCloser(strings.NewReader(`{
						"toolDataList": [{"tool": "web_search"}],
						"totalUsage": {"totalNetworkSearchCount": 10, "totalWebReadMcpCount": 5}
					}`)),
				}, nil
			}
			if req.URL.Path == "/api/monitor/usage/quota/limit" {
				return &http.Response{
					StatusCode: http.StatusOK,
					Header:     make(http.Header),
					Body: io.NopCloser(strings.NewReader(`{
						"code": 200,
						"msg": "操作成功",
						"data": {
							"limits": [{
								"type": "TOKENS_LIMIT",
								"unit": 5,
								"number": 1,
								"usage": 50000,
								"remaining": 150000,
								"percentage": 25,
								"nextResetTime": 1704067200000
							}],
							"level": "default"
						},
						"success": true
					}`)),
				}, nil
			}

			t.Fatalf("unexpected request: %s", req.URL.Path)
			return nil, nil
		}),
	})

	checker := NewZhipuQuotaChecker(httpClient)

	quota, err := checker.CheckQuota(context.Background(), &ent.Channel{
		Type: "zhipu",
		Credentials: objects.ChannelCredentials{
			APIKey: "test-api-key",
		},
		BaseURL: "https://open.bigmodel.cn/api/paas/v4",
	})

	require.NoError(t, err)
	require.Equal(t, "available", quota.Status)
	require.Equal(t, "zhipu", quota.ProviderType)
	require.True(t, quota.Ready)
	require.NotNil(t, quota.Summary)
	require.False(t, quota.Summary.Partial)
	require.NotNil(t, quota.Summary.UsageRatio)
	require.Equal(t, 0.25, *quota.Summary.UsageRatio)
	require.Equal(t, "Monthly", quota.Summary.PeriodLabel)
	require.NotNil(t, quota.Summary.PeriodEnd)
	require.Equal(t, time.UnixMilli(1704067200000), *quota.Summary.PeriodEnd)
	require.Equal(t, "complete", quota.RawData["display_status_reason"])

	require.Len(t, capturedReqs, 3)
	for _, req := range capturedReqs {
		require.Equal(t, "Bearer test-api-key", req.Header.Get("Authorization"))
	}
}

func TestZhipuQuotaChecker_UsageOnlyFallback(t *testing.T) {
	requestCount := 0

	httpClient := httpclient.NewHttpClientWithClient(&http.Client{
		Transport: zhipuRoundTripFunc(func(req *http.Request) (*http.Response, error) {
			requestCount++

			if req.URL.Path == "/api/monitor/usage/model-usage" {
				return &http.Response{
					StatusCode: http.StatusOK,
					Header:     make(http.Header),
					Body: io.NopCloser(strings.NewReader(`{
						"modelDataList": [{"model": "glm-4"}],
						"totalUsage": {"totalModelCallCount": 100, "totalTokensUsage": 50000}
					}`)),
				}, nil
			}
			if req.URL.Path == "/api/monitor/usage/tool-usage" {
				return &http.Response{
					StatusCode: http.StatusOK,
					Header:     make(http.Header),
					Body: io.NopCloser(strings.NewReader(`{
						"toolDataList": [{"tool": "web_search"}],
						"totalUsage": {"totalNetworkSearchCount": 10, "totalWebReadMcpCount": 5}
					}`)),
				}, nil
			}
			if req.URL.Path == "/api/monitor/usage/quota/limit" {
				return nil, errors.New("quota limit endpoint unavailable")
			}

			t.Fatalf("unexpected request: %s", req.URL.Path)
			return nil, nil
		}),
	})

	checker := NewZhipuQuotaChecker(httpClient)

	quota, err := checker.CheckQuota(context.Background(), &ent.Channel{
		Type: "zhipu",
		Credentials: objects.ChannelCredentials{
			APIKey: "test-api-key",
		},
	})

	require.NoError(t, err)
	require.Equal(t, "available", quota.Status)
	require.True(t, quota.Ready)
	require.NotNil(t, quota.Summary)
	require.True(t, quota.Summary.Partial)
	require.Nil(t, quota.Summary.UsageRatio)
	require.Equal(t, "", quota.Summary.PeriodLabel)
	require.Nil(t, quota.Summary.PeriodEnd)
	require.Equal(t, "usage_only", quota.RawData["display_status_reason"])
}

func TestZhipuQuotaChecker_AllEndpointsFail(t *testing.T) {
	httpClient := httpclient.NewHttpClientWithClient(&http.Client{
		Transport: zhipuRoundTripFunc(func(req *http.Request) (*http.Response, error) {
			return nil, errors.New("all endpoints failing")
		}),
	})

	checker := NewZhipuQuotaChecker(httpClient)

	_, err := checker.CheckQuota(context.Background(), &ent.Channel{
		Type: "zhipu",
		Credentials: objects.ChannelCredentials{
			APIKey: "test-api-key",
		},
	})

	require.Error(t, err)
	require.Contains(t, err.Error(), "all Zhipu quota endpoints failed")
}

func TestZhipuQuotaChecker_MalformedJSON(t *testing.T) {
	requestCount := 0

	httpClient := httpclient.NewHttpClientWithClient(&http.Client{
		Transport: zhipuRoundTripFunc(func(req *http.Request) (*http.Response, error) {
			requestCount++

			if req.URL.Path == "/api/monitor/usage/model-usage" {
				return &http.Response{
					StatusCode: http.StatusOK,
					Header:     make(http.Header),
					Body:       io.NopCloser(strings.NewReader(`{invalid json`)),
				}, nil
			}
			if req.URL.Path == "/api/monitor/usage/tool-usage" {
				return &http.Response{
					StatusCode: http.StatusOK,
					Header:     make(http.Header),
					Body:       io.NopCloser(strings.NewReader(`{}`)),
				}, nil
			}
			if req.URL.Path == "/api/monitor/usage/quota/limit" {
				return &http.Response{
					StatusCode: http.StatusOK,
					Header:     make(http.Header),
					Body:       io.NopCloser(strings.NewReader(`{"code":200,"data":{"limits":[],"level":""}}`)),
				}, nil
			}

			t.Fatalf("unexpected request: %s", req.URL.Path)
			return nil, nil
		}),
	})

	checker := NewZhipuQuotaChecker(httpClient)

	quota, err := checker.CheckQuota(context.Background(), &ent.Channel{
		Type: "zhipu",
		Credentials: objects.ChannelCredentials{
			APIKey: "test-api-key",
		},
	})

	require.NoError(t, err)
	require.Equal(t, "unknown", quota.Status)
	require.Equal(t, "usage_only", quota.RawData["display_status_reason"])
}

func TestZhipuQuotaChecker_Exhausted(t *testing.T) {
	httpClient := httpclient.NewHttpClientWithClient(&http.Client{
		Transport: zhipuRoundTripFunc(func(req *http.Request) (*http.Response, error) {
			if req.URL.Path == "/api/monitor/usage/quota/limit" {
				return &http.Response{
					StatusCode: http.StatusOK,
					Header:     make(http.Header),
					Body: io.NopCloser(strings.NewReader(`{
						"code": 200,
						"msg": "操作成功",
						"data": {
							"limits": [{
								"type": "TOKENS_LIMIT",
								"unit": 5,
								"number": 1,
								"usage": 200000,
								"remaining": 0,
								"percentage": 100,
								"nextResetTime": 1704067200000
							}],
							"level": "default"
						},
						"success": true
					}`)),
				}, nil
			}

			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     make(http.Header),
				Body:       io.NopCloser(strings.NewReader(`{}`)),
			}, nil
		}),
	})

	checker := NewZhipuQuotaChecker(httpClient)

	quota, err := checker.CheckQuota(context.Background(), &ent.Channel{
		Type: "zhipu",
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

func TestZhipuQuotaChecker_Warning(t *testing.T) {
	httpClient := httpclient.NewHttpClientWithClient(&http.Client{
		Transport: zhipuRoundTripFunc(func(req *http.Request) (*http.Response, error) {
			if req.URL.Path == "/api/monitor/usage/quota/limit" {
				return &http.Response{
					StatusCode: http.StatusOK,
					Header:     make(http.Header),
					Body: io.NopCloser(strings.NewReader(`{
						"code": 200,
						"msg": "操作成功",
						"data": {
							"limits": [{
								"type": "TOKENS_LIMIT",
								"unit": 5,
								"number": 1,
								"usage": 170000,
								"remaining": 30000,
								"percentage": 85,
								"nextResetTime": 1704067200000
							}],
							"level": "default"
						},
						"success": true
					}`)),
				}, nil
			}

			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     make(http.Header),
				Body:       io.NopCloser(strings.NewReader(`{}`)),
			}, nil
		}),
	})

	checker := NewZhipuQuotaChecker(httpClient)

	quota, err := checker.CheckQuota(context.Background(), &ent.Channel{
		Type: "zhipu",
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

func TestZhipuQuotaChecker_HourlyUnit(t *testing.T) {
	httpClient := httpclient.NewHttpClientWithClient(&http.Client{
		Transport: zhipuRoundTripFunc(func(req *http.Request) (*http.Response, error) {
			if req.URL.Path == "/api/monitor/usage/quota/limit" {
				return &http.Response{
					StatusCode: http.StatusOK,
					Header:     make(http.Header),
					Body: io.NopCloser(strings.NewReader(`{
						"code": 200,
						"msg": "操作成功",
						"data": {
							"limits": [{
								"type": "TOKENS_LIMIT",
								"unit": 3,
								"number": 1,
								"usage": 80,
								"remaining": 20,
								"percentage": 80,
								"nextResetTime": 1704067200000
							}],
							"level": "default"
						},
						"success": true
					}`)),
				}, nil
			}

			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     make(http.Header),
				Body:       io.NopCloser(strings.NewReader(`{}`)),
			}, nil
		}),
	})

	checker := NewZhipuQuotaChecker(httpClient)

	quota, err := checker.CheckQuota(context.Background(), &ent.Channel{
		Type: "zhipu",
		Credentials: objects.ChannelCredentials{
			APIKey: "test-api-key",
		},
	})

	require.NoError(t, err)
	require.Equal(t, "warning", quota.Status)
	require.Equal(t, "Hourly", quota.Summary.PeriodLabel)
}

func TestZhipuQuotaChecker_BaseURLDerivation(t *testing.T) {
	tests := []struct {
		name     string
		baseURL  string
		expected string
	}{
		{
			name:     "full path with api/paas/v4",
			baseURL:  "https://open.bigmodel.cn/api/paas/v4",
			expected: "https://open.bigmodel.cn",
		},
		{
			name:     "with trailing slash",
			baseURL:  "https://open.bigmodel.cn/api/paas/v4/",
			expected: "https://open.bigmodel.cn",
		},
		{
			name:     "base only",
			baseURL:  "https://open.bigmodel.cn",
			expected: "https://open.bigmodel.cn",
		},
		{
			name:     "empty uses default",
			baseURL:  "",
			expected: "https://open.bigmodel.cn",
		},
		{
			name:     "non-zhipu uses default",
			baseURL:  "https://other.example.com",
			expected: "https://open.bigmodel.cn",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			httpClient := httpclient.NewHttpClientWithClient(&http.Client{
				Transport: zhipuRoundTripFunc(func(req *http.Request) (*http.Response, error) {
					require.Equal(t, tt.expected, req.URL.Scheme+"://"+req.URL.Host)
					return &http.Response{
						StatusCode: http.StatusOK,
						Header:     make(http.Header),
						Body:       io.NopCloser(strings.NewReader(`{}`)),
					}, nil
				}),
			})

			checker := NewZhipuQuotaChecker(httpClient)

			_, err := checker.CheckQuota(context.Background(), &ent.Channel{
				Type: "zhipu",
				Credentials: objects.ChannelCredentials{
					APIKey: "test-api-key",
				},
				BaseURL: tt.baseURL,
			})

			require.NoError(t, err)
		})
	}
}

func TestZhipuQuotaChecker_TokensLimitWithoutUsageAndRemaining(t *testing.T) {
	httpClient := httpclient.NewHttpClientWithClient(&http.Client{
		Transport: zhipuRoundTripFunc(func(req *http.Request) (*http.Response, error) {
			if req.URL.Path == "/api/monitor/usage/quota/limit" {
				return &http.Response{
					StatusCode: http.StatusOK,
					Header:     make(http.Header),
					Body: io.NopCloser(strings.NewReader(`{
						"code": 200,
						"msg": "操作成功",
						"data": {
							"limits": [
								{
									"type": "TIME_LIMIT",
									"unit": 5,
									"number": 1,
									"usage": 100,
									"currentValue": 0,
									"remaining": 100,
									"percentage": 0,
									"nextResetTime": 1779428140978,
									"usageDetails": [
										{"modelCode": "search-prime", "usage": 0},
										{"modelCode": "web-reader", "usage": 0}
									]
								},
								{
									"type": "TOKENS_LIMIT",
									"unit": 3,
									"number": 5,
									"percentage": 1,
									"nextResetTime": 1777231845666
								}
							],
							"level": "lite"
						},
						"success": true
					}`)),
				}, nil
			}

			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     make(http.Header),
				Body:       io.NopCloser(strings.NewReader(`{}`)),
			}, nil
		}),
	})

	checker := NewZhipuQuotaChecker(httpClient)

	quota, err := checker.CheckQuota(context.Background(), &ent.Channel{
		Type: "zhipu",
		Credentials: objects.ChannelCredentials{
			APIKey: "test-api-key",
		},
	})

	require.NoError(t, err)
	require.Equal(t, "available", quota.Status)
	require.True(t, quota.Ready)
	require.NotNil(t, quota.Summary)
	require.NotNil(t, quota.Summary.UsageRatio)
	require.Equal(t, 0.01, *quota.Summary.UsageRatio)
	require.Nil(t, quota.Summary.ProviderUsedCount)
	require.Nil(t, quota.Summary.ProviderTotalCount)
	require.Nil(t, quota.Summary.ProviderRemainingCount)
	require.NotNil(t, quota.Summary.ProviderUsedPercentage)
	require.Equal(t, 1.0, *quota.Summary.ProviderUsedPercentage)
	require.Equal(t, "Hourly", quota.Summary.PeriodLabel)
	require.Equal(t, time.UnixMilli(1777231845666), *quota.Summary.PeriodEnd)
}

func TestZhipuQuotaChecker_EmptyModelAndToolUsage(t *testing.T) {
	httpClient := httpclient.NewHttpClientWithClient(&http.Client{
		Transport: zhipuRoundTripFunc(func(req *http.Request) (*http.Response, error) {
			if req.URL.Path == "/api/monitor/usage/model-usage" {
				return &http.Response{
					StatusCode: http.StatusOK,
					Header:     make(http.Header),
					Body:       io.NopCloser(strings.NewReader(``)),
				}, nil
			}
			if req.URL.Path == "/api/monitor/usage/tool-usage" {
				return &http.Response{
					StatusCode: http.StatusOK,
					Header:     make(http.Header),
					Body:       io.NopCloser(strings.NewReader(`   `)),
				}, nil
			}
			if req.URL.Path == "/api/monitor/usage/quota/limit" {
				return &http.Response{
					StatusCode: http.StatusOK,
					Header:     make(http.Header),
					Body: io.NopCloser(strings.NewReader(`{
						"code": 200,
						"msg": "操作成功",
						"data": {
							"limits": [{
								"type": "TOKENS_LIMIT",
								"unit": 5,
								"number": 1,
								"usage": 50000,
								"remaining": 150000,
								"percentage": 25,
								"nextResetTime": 1704067200000
							}],
							"level": "default"
						},
						"success": true
					}`)),
				}, nil
			}

			t.Fatalf("unexpected request: %s", req.URL.Path)
			return nil, nil
		}),
	})

	checker := NewZhipuQuotaChecker(httpClient)

	quota, err := checker.CheckQuota(context.Background(), &ent.Channel{
		Type: "zhipu",
		Credentials: objects.ChannelCredentials{
			APIKey: "test-api-key",
		},
	})

	require.NoError(t, err)
	require.Equal(t, "available", quota.Status)
	require.True(t, quota.Ready)
	require.NotNil(t, quota.Summary)
	require.Equal(t, 0.25, *quota.Summary.UsageRatio)
	require.Equal(t, "complete", quota.RawData["display_status_reason"])
}

func TestZhipuQuotaChecker_TimeLimitWithUsageDetails(t *testing.T) {
	httpClient := httpclient.NewHttpClientWithClient(&http.Client{
		Transport: zhipuRoundTripFunc(func(req *http.Request) (*http.Response, error) {
			if req.URL.Path == "/api/monitor/usage/quota/limit" {
				return &http.Response{
					StatusCode: http.StatusOK,
					Header:     make(http.Header),
					Body: io.NopCloser(strings.NewReader(`{
						"code": 200,
						"msg": "操作成功",
						"data": {
							"limits": [
								{
									"type": "TIME_LIMIT",
									"unit": 5,
									"number": 1,
									"usage": 50,
									"currentValue": 0,
									"remaining": 50,
									"percentage": 50,
									"nextResetTime": 1779428140978,
									"usageDetails": [
										{"modelCode": "search-prime", "usage": 30},
										{"modelCode": "web-reader", "usage": 20}
									]
								},
								{
									"type": "TOKENS_LIMIT",
									"unit": 3,
									"number": 5,
									"percentage": 1,
									"nextResetTime": 1777231845666
								}
							],
							"level": "lite"
						},
						"success": true
					}`)),
				}, nil
			}

			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     make(http.Header),
				Body:       io.NopCloser(strings.NewReader(`{}`)),
			}, nil
		}),
	})

	checker := NewZhipuQuotaChecker(httpClient)

	quota, err := checker.CheckQuota(context.Background(), &ent.Channel{
		Type: "zhipu",
		Credentials: objects.ChannelCredentials{
			APIKey: "test-api-key",
		},
	})

	require.NoError(t, err)
	require.Equal(t, "available", quota.Status)
	require.NotNil(t, quota.Summary)
	require.Equal(t, 0.01, *quota.Summary.UsageRatio)
	require.Equal(t, "Hourly", quota.Summary.PeriodLabel)
	require.Equal(t, time.UnixMilli(1777231845666), *quota.Summary.PeriodEnd)
}