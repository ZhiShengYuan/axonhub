package provider_quota

import (
	"bytes"
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

// ZhipuQuotaLimitAPIResponse is the outer wrapper returned by the quota/limit endpoint.
type ZhipuQuotaLimitAPIResponse struct {
	Code    int                       `json:"code"`
	Msg     string                    `json:"msg"`
	Data    ZhipuQuotaLimitData       `json:"data"`
	Success bool                      `json:"success"`
}

// ZhipuQuotaLimitData contains the actual quota limit data (inside the "data" wrapper).
type ZhipuQuotaLimitData struct {
	Limits []ZhipuQuotaLimitEntry `json:"limits"`
	Level  string                 `json:"level"`
}

// ZhipuQuotaLimitEntry represents a single quota limit entry (TIME_LIMIT or TOKENS_LIMIT).
type ZhipuQuotaLimitEntry struct {
	Type         string              `json:"type"`
	Unit         int                 `json:"unit"`
	Number       int                 `json:"number"`
	Usage        int                 `json:"usage"`               // may be 0/missing for TOKENS_LIMIT
	CurrentValue int                 `json:"currentValue"`        // present in TIME_LIMIT
	Remaining    int                 `json:"remaining"`           // may be 0/missing for TOKENS_LIMIT
	Percentage   int                 `json:"percentage"`
	NextResetMs  int64               `json:"nextResetTime"`
	UsageDetails []ZhipuUsageDetail  `json:"usageDetails,omitempty"` // present in TIME_LIMIT
}

// ZhipuUsageDetail is the per-model usage breakdown in TIME_LIMIT.
type ZhipuUsageDetail struct {
	ModelCode string `json:"modelCode"`
	Usage     int    `json:"usage"`
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
	keys := ch.Credentials.GetAllAPIKeys()
	if len(keys) == 0 {
		return QuotaData{}, fmt.Errorf("channel has no API key")
	}
	apiKey := strings.TrimSpace(keys[0])

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

	if len(bytes.TrimSpace(resp.Body)) == 0 {
		return nil, nil
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

	if len(bytes.TrimSpace(resp.Body)) == 0 {
		return nil, nil
	}

	var result ZhipuToolUsageResponse
	if err := json.Unmarshal(resp.Body, &result); err != nil {
		return nil, fmt.Errorf("tool-usage parse failed: %w", err)
	}

	return &result, nil
}

func (c *ZhipuQuotaChecker) callQuotaLimit(ctx context.Context, httpClient *httpclient.HttpClient, baseURL, apiKey string) (*ZhipuQuotaLimitData, error) {
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

	var apiResp ZhipuQuotaLimitAPIResponse
	if err := json.Unmarshal(resp.Body, &apiResp); err != nil {
		return nil, fmt.Errorf("quota-limit parse failed: %w", err)
	}

	return &apiResp.Data, nil
}

func (c *ZhipuQuotaChecker) buildQuotaData(
	modelUsage *ZhipuModelUsageResponse, modelErr error,
	toolUsage *ZhipuToolUsageResponse, toolErr error,
	quotaLimit *ZhipuQuotaLimitData, limitErr error,
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

func (c *ZhipuQuotaChecker) buildSummaryFromLimit(limitResp *ZhipuQuotaLimitData, displayStatusReason string) (*QuotaSummary, *time.Time, string) {
	var tokensLimit *ZhipuQuotaLimitEntry

	// First pass: look for TOKENS_LIMIT with Percentage >= 0 (accept NextResetMs == 0)
	for i := range limitResp.Limits {
		rec := &limitResp.Limits[i]
		if rec.Type == "TOKENS_LIMIT" && rec.Percentage >= 0 && rec.NextResetMs >= 0 {
			tokensLimit = rec
			break
		}
	}

	// Fallback: if no TOKENS_LIMIT found, look for TIME_LIMIT with Percentage >= 0 and NextResetMs >= 0
	if tokensLimit == nil {
		for i := range limitResp.Limits {
			rec := &limitResp.Limits[i]
			if rec.Type == "TIME_LIMIT" && rec.Percentage >= 0 && rec.NextResetMs >= 0 {
				tokensLimit = rec
				break
			}
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
	// Exhausted when Remaining <= 0 and Usage > 0, OR when Remaining == 0 && Usage == 0 && Percentage >= 100
	if tokensLimit.Remaining <= 0 && tokensLimit.Usage > 0 {
		normalizedStatus = "exhausted"
	} else if tokensLimit.Remaining == 0 && tokensLimit.Usage == 0 && tokensLimit.Percentage >= 100 {
		normalizedStatus = "exhausted"
	} else if tokensLimit.Percentage >= 80 {
		normalizedStatus = "warning"
	}

	var usedCount, totalCount, remainingCount *int64
	// Derive counts from Usage/Remaining if available
	if tokensLimit.Usage > 0 || tokensLimit.Remaining > 0 {
		uc := int64(tokensLimit.Usage)
		tc := int64(tokensLimit.Number)
		rc := int64(tokensLimit.Remaining)
		usedCount = &uc
		totalCount = &tc
		remainingCount = &rc
	} else if tokensLimit.Number > 0 && tokensLimit.Percentage >= 0 {
		// Derive counts from Number and Percentage when Usage/Remaining are both zero/missing
		tc := int64(tokensLimit.Number)
		uc := int64(float64(tokensLimit.Number) * float64(tokensLimit.Percentage) / 100.0)
		rc := tc - uc
		usedCount = &uc
		totalCount = &tc
		remainingCount = &rc
	}
	usedPercentage := float64(tokensLimit.Percentage)

	summary := &QuotaSummary{
		WindowKind:              "tokens_limit",
		PeriodEnd:               periodEnd,
		ProviderUsedCount:       usedCount,
		ProviderTotalCount:      totalCount,
		ProviderRemainingCount:   remainingCount,
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