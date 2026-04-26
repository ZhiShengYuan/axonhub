package provider_quota

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/looplj/axonhub/internal/ent"
	"github.com/looplj/axonhub/internal/ent/channel"
	"github.com/looplj/axonhub/llm/httpclient"
)

// NOTE: These monitor endpoints are UNOFFICIAL/reverse-engineered based on QuotaHub reference.
// They are not officially documented by Zhipu and may change without notice.

type ZhipuModelUsageResponse struct {
	ModelDataList []struct {
		Model string `json:"model,omitempty"`
	} `json:"modelDataList,omitempty"`
	TotalUsage struct {
		TotalModelCallCount int `json:"totalModelCallCount,omitempty"`
		TotalTokensUsage    int `json:"totalTokensUsage,omitempty"`
	} `json:"totalUsage,omitempty"`
}

type ZhipuToolUsageResponse struct {
	ToolDataList []struct {
		Tool string `json:"tool,omitempty"`
	} `json:"toolDataList,omitempty"`
	TotalUsage struct {
		TotalNetworkSearchCount int `json:"totalNetworkSearchCount,omitempty"`
		TotalWebReadMcpCount    int `json:"totalWebReadMcpCount,omitempty"`
	} `json:"totalUsage,omitempty"`
}

type ZhipuQuotaLimitResponse struct {
	Limits []struct {
		Type        string `json:"type,omitempty"`
		Unit        int    `json:"unit,omitempty"`
		Number      int    `json:"number,omitempty"`
		Usage       int    `json:"usage,omitempty"`
		Remaining   int    `json:"remaining,omitempty"`
		Percentage  int    `json:"percentage,omitempty"`
		NextResetMs int64  `json:"nextResetTime,omitempty"`
	} `json:"limits,omitempty"`
	Level string `json:"level,omitempty"`
}

type ZhipuQuotaChecker struct {
	httpClient *httpclient.HttpClient
}

func NewZhipuQuotaChecker(httpClient *httpclient.HttpClient) *ZhipuQuotaChecker {
	return &ZhipuQuotaChecker{
		httpClient: httpClient,
	}
}

func getBaseURL(ch *ent.Channel) string {
	defaultBase := "https://open.bigmodel.cn"

	if ch.BaseURL == "" {
		return defaultBase
	}

	if strings.Contains(ch.BaseURL, "bigmodel.cn") {
		base := strings.TrimSuffix(ch.BaseURL, "/")
		base = strings.TrimSuffix(base, "/api/paas/v4")
		base = strings.TrimSuffix(base, "/api/paas")
		base = strings.TrimSuffix(base, "/v4")
		base = strings.TrimSuffix(base, "/api")
		return base
	}

	return defaultBase
}

func (c *ZhipuQuotaChecker) CheckQuota(ctx context.Context, ch *ent.Channel) (QuotaData, error) {
	apiKey := strings.TrimSpace(ch.Credentials.APIKey)
	if apiKey == "" {
		return QuotaData{}, fmt.Errorf("channel has no API key")
	}

	baseURL := getBaseURL(ch)

	httpClient := c.httpClient
	if ch.Settings != nil && ch.Settings.Proxy != nil {
		httpClient = c.httpClient.WithProxy(ch.Settings.Proxy)
	}

	modelUsage, modelErr := c.callModelUsage(ctx, httpClient, baseURL, apiKey)
	toolUsage, toolErr := c.callToolUsage(ctx, httpClient, baseURL, apiKey)
	quotaLimit, limitErr := c.callQuotaLimit(ctx, httpClient, baseURL, apiKey)

	if modelErr != nil && toolErr != nil && limitErr != nil {
		return QuotaData{}, fmt.Errorf("all Zhipu quota endpoints failed: model-usage=%w, tool-usage=%w, quota-limit=%w", modelErr, toolErr, limitErr)
	}

	return c.buildQuotaData(modelUsage, modelErr, toolUsage, toolErr, quotaLimit, limitErr)
}

func (c *ZhipuQuotaChecker) callModelUsage(ctx context.Context, httpClient *httpclient.HttpClient, baseURL, apiKey string) (*ZhipuModelUsageResponse, error) {
	url := baseURL + "/api/monitor/usage/model-usage"

	httpReq := httpclient.NewRequestBuilder().
		WithMethod("GET").
		WithURL(url).
		WithBearerToken(apiKey).
		WithHeader("Content-Type", "application/json").
		Build()

	resp, err := httpClient.Do(ctx, httpReq)
	if err != nil {
		return nil, fmt.Errorf("model-usage request failed: %w", err)
	}

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("model-usage HTTP %d: %s", resp.StatusCode, string(resp.Body))
	}

	var result ZhipuModelUsageResponse
	if err := json.Unmarshal(resp.Body, &result); err != nil {
		return nil, fmt.Errorf("model-usage parse failed: %w", err)
	}

	return &result, nil
}

func (c *ZhipuQuotaChecker) callToolUsage(ctx context.Context, httpClient *httpclient.HttpClient, baseURL, apiKey string) (*ZhipuToolUsageResponse, error) {
	url := baseURL + "/api/monitor/usage/tool-usage"

	httpReq := httpclient.NewRequestBuilder().
		WithMethod("GET").
		WithURL(url).
		WithBearerToken(apiKey).
		WithHeader("Content-Type", "application/json").
		Build()

	resp, err := httpClient.Do(ctx, httpReq)
	if err != nil {
		return nil, fmt.Errorf("tool-usage request failed: %w", err)
	}

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("tool-usage HTTP %d: %s", resp.StatusCode, string(resp.Body))
	}

	var result ZhipuToolUsageResponse
	if err := json.Unmarshal(resp.Body, &result); err != nil {
		return nil, fmt.Errorf("tool-usage parse failed: %w", err)
	}

	return &result, nil
}

func (c *ZhipuQuotaChecker) callQuotaLimit(ctx context.Context, httpClient *httpclient.HttpClient, baseURL, apiKey string) (*ZhipuQuotaLimitResponse, error) {
	url := baseURL + "/api/monitor/usage/quota/limit"

	httpReq := httpclient.NewRequestBuilder().
		WithMethod("GET").
		WithURL(url).
		WithBearerToken(apiKey).
		WithHeader("Content-Type", "application/json").
		Build()

	resp, err := httpClient.Do(ctx, httpReq)
	if err != nil {
		return nil, fmt.Errorf("quota-limit request failed: %w", err)
	}

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("quota-limit HTTP %d: %s", resp.StatusCode, string(resp.Body))
	}

	var result ZhipuQuotaLimitResponse
	if err := json.Unmarshal(resp.Body, &result); err != nil {
		return nil, fmt.Errorf("quota-limit parse failed: %w", err)
	}

	return &result, nil
}

func (c *ZhipuQuotaChecker) buildQuotaData(
	modelUsage *ZhipuModelUsageResponse, modelErr error,
	toolUsage *ZhipuToolUsageResponse, toolErr error,
	quotaLimit *ZhipuQuotaLimitResponse, limitErr error,
) (QuotaData, error) {
	rawData := map[string]any{}

	if modelUsage != nil {
		rawData["model_usage"] = modelUsage
	}
	if toolUsage != nil {
		rawData["tool_usage"] = toolUsage
	}
	if quotaLimit != nil {
		rawData["quota_limit"] = quotaLimit
	}

	displayStatusReason := "complete"
	if limitErr != nil {
		if modelErr != nil && toolErr != nil {
			displayStatusReason = "missing_limit"
		} else {
			displayStatusReason = "usage_only"
		}
	} else if modelErr != nil || toolErr != nil {
		displayStatusReason = "usage_only"
	}

	rawData["display_status_reason"] = displayStatusReason

	var summary *QuotaSummary
	var nextResetAt *time.Time
	var normalizedStatus string

	if quotaLimit != nil && limitErr == nil {
		summary, nextResetAt, normalizedStatus = c.buildSummaryFromLimit(quotaLimit, displayStatusReason)
	} else {
		normalizedStatus = c.normalizeStatusFromUsage(modelUsage, toolUsage)
		summary = &QuotaSummary{
			WindowKind:          "usage_only",
			DisplayStatusReason: displayStatusReason,
			UsageRatio:          nil,
			PeriodLabel:         "",
			PeriodEnd:           nil,
			Partial:             true,
		}
	}

	return QuotaData{
		Status:       normalizedStatus,
		ProviderType: "zhipu",
		RawData:      rawData,
		NextResetAt:  nextResetAt,
		Ready:        normalizedStatus == "available" || normalizedStatus == "warning",
		Summary:      summary,
	}, nil
}

func (c *ZhipuQuotaChecker) buildSummaryFromLimit(limitResp *ZhipuQuotaLimitResponse, displayStatusReason string) (*QuotaSummary, *time.Time, string) {
	var tokensLimit *struct {
		Type        string
		Unit        int
		Number      int
		Usage       int
		Remaining   int
		Percentage  int
		NextResetMs int64
	}

	for i := range limitResp.Limits {
		rec := &limitResp.Limits[i]
		if rec.Type == "TOKENS_LIMIT" && rec.Remaining >= 0 && rec.Percentage >= 0 && rec.NextResetMs > 0 {
			tokensLimit = (*struct {
				Type        string
				Unit        int
				Number      int
				Usage       int
				Remaining   int
				Percentage  int
				NextResetMs int64
			})(rec)
			break
		}
	}

	if tokensLimit == nil {
		return &QuotaSummary{
			WindowKind:          "tokens_limit",
			DisplayStatusReason: displayStatusReason,
			UsageRatio:          nil,
			PeriodLabel:        "",
			PeriodEnd:           nil,
			Partial:            false,
		}, nil, "unknown"
	}

	periodLabel := ""
	if tokensLimit.Unit == 5 {
		periodLabel = "Monthly"
	} else if tokensLimit.Unit == 3 {
		periodLabel = "Hourly"
	}

	usageRatio := float64(tokensLimit.Percentage) / 100.0

	var periodEnd *time.Time
	if tokensLimit.NextResetMs > 0 {
		t := time.UnixMilli(tokensLimit.NextResetMs)
		periodEnd = &t
	}

	normalizedStatus := "available"
	if tokensLimit.Remaining <= 0 {
		normalizedStatus = "exhausted"
	} else if tokensLimit.Percentage >= 80 {
		normalizedStatus = "warning"
	}

	usedCount := int64(tokensLimit.Usage)
	totalCount := int64(tokensLimit.Number)
	remainingCount := int64(tokensLimit.Remaining)
	usedPercentage := float64(tokensLimit.Percentage)

	summary := &QuotaSummary{
		WindowKind:              "tokens_limit",
		PeriodEnd:               periodEnd,
		ProviderUsedCount:       &usedCount,
		ProviderTotalCount:      &totalCount,
		ProviderRemainingCount:   &remainingCount,
		ProviderUsedPercentage:  &usedPercentage,
		DisplayStatusReason:      displayStatusReason,
		UsageRatio:              &usageRatio,
		PeriodLabel:             periodLabel,
		Partial:                 false,
	}

	return summary, periodEnd, normalizedStatus
}

func (c *ZhipuQuotaChecker) normalizeStatusFromUsage(modelUsage *ZhipuModelUsageResponse, toolUsage *ZhipuToolUsageResponse) string {
	if modelUsage != nil || toolUsage != nil {
		return "available"
	}
	return "unknown"
}

func (c *ZhipuQuotaChecker) SupportsChannel(ch *ent.Channel) bool {
	return ch.Type == channel.TypeZhipu || ch.Type == channel.TypeZhipuAnthropic
}