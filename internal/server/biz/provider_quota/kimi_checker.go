package provider_quota

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"github.com/looplj/axonhub/internal/ent"
	"github.com/looplj/axonhub/internal/ent/channel"
	"github.com/looplj/axonhub/llm/httpclient"
)

// KimiUsagesResponse matches the actual KIMI API response from https://api.kimi.com/coding/v1/usages
type KimiUsagesResponse struct {
	User struct {
		UserID     string `json:"userId"`
		Region     string `json:"region"`
		Membership struct {
			Level string `json:"level"`
		} `json:"membership"`
		BusinessID string `json:"businessId"`
	} `json:"user"`
	Usage struct {
		Limit     string `json:"limit"`     // STRING, not int!
		Remaining string `json:"remaining"` // STRING, not int!
		ResetTime string `json:"resetTime"` // RFC3339
	} `json:"usage"`
	Limits []struct {
		Window struct {
			Duration int    `json:"duration"`
			TimeUnit string `json:"timeUnit"`
		} `json:"window"`
		Detail struct {
			Limit     string `json:"limit"`     // STRING
			Remaining string `json:"remaining"` // STRING
			ResetTime string `json:"resetTime"` // RFC3339
		} `json:"detail"`
	} `json:"limits"`
	Parallel struct {
		Limit string `json:"limit"` // STRING
	} `json:"parallel"`
	TotalQuota struct {
		Limit     string `json:"limit"`     // STRING
		Remaining string `json:"remaining"` // STRING
	} `json:"totalQuota"`
	Authentication struct {
		Method string `json:"method"`
		Scope  string `json:"scope"`
	} `json:"authentication"`
	SubType string `json:"subType"`
}

type KimiQuotaChecker struct {
	httpClient *httpclient.HttpClient
}

func NewKimiQuotaChecker(httpClient *httpclient.HttpClient) *KimiQuotaChecker {
	return &KimiQuotaChecker{
		httpClient: httpClient,
	}
}

func (c *KimiQuotaChecker) CheckQuota(ctx context.Context, ch *ent.Channel) (QuotaData, error) {
	keys := ch.Credentials.GetAllAPIKeys()
	if len(keys) == 0 {
		return QuotaData{}, fmt.Errorf("channel has no API key")
	}
	apiKey := keys[0]

	httpRequest := httpclient.NewRequestBuilder().
		WithMethod("GET").
		WithURL("https://api.kimi.com/coding/v1/usages").
		WithBearerToken(apiKey).
		WithHeader("Content-Type", "application/json").
		Build()

	hc := c.httpClient
	if ch.Settings != nil && ch.Settings.Proxy != nil {
		hc = c.httpClient.WithProxy(ch.Settings.Proxy)
	}

	resp, err := hc.Do(ctx, httpRequest)
	if err != nil {
		return QuotaData{}, fmt.Errorf("quota request failed: %w", err)
	}

	return c.parseResponse(resp.Body)
}

func (c *KimiQuotaChecker) parseResponse(body []byte) (QuotaData, error) {
	var response KimiUsagesResponse

	if err := json.Unmarshal(body, &response); err != nil {
		return QuotaData{}, fmt.Errorf("failed to parse kimi usage response: %w", err)
	}

	// Parse limit and remaining as int64 (they are strings in the API response)
	limit, _ := strconv.ParseInt(response.Usage.Limit, 10, 64)
	remaining, _ := strconv.ParseInt(response.Usage.Remaining, 10, 64)

	// Parse totalQuota as well for reference
	totalLimit, _ := strconv.ParseInt(response.TotalQuota.Limit, 10, 64)
	totalRemaining, _ := strconv.ParseInt(response.TotalQuota.Remaining, 10, 64)

	rawData := map[string]any{
		"usages":     body,
		"totalQuota": map[string]any{"limit": totalLimit, "remaining": totalRemaining},
	}

	normalizedStatus := "unknown"
	var nextResetAt *time.Time
	var periodStartAt *time.Time
	var usageRatio *float64
	partial := false

	// Calculate used count and usage ratio
	if limit > 0 {
		usedCount := limit - remaining
		intervalUsageRatio := float64(usedCount) / float64(limit)

		if remaining <= 0 {
			normalizedStatus = "exhausted"
			usageRatio = &intervalUsageRatio
		} else if intervalUsageRatio >= 0.8 {
			normalizedStatus = "warning"
			usageRatio = &intervalUsageRatio
		} else {
			normalizedStatus = "available"
			usageRatio = &intervalUsageRatio
		}
	} else {
		partial = true
	}

	// Parse reset time from RFC3339 format
	if response.Usage.ResetTime != "" {
		t, err := time.Parse(time.RFC3339, response.Usage.ResetTime)
		if err == nil {
			nextResetAt = &t
			// For rolling intervals, we don't have a period start
			periodStartAt = nil
		}
	}

	displayStatusReason := "complete"

	var providerUsedCount, providerTotalCount, providerRemainingCount *int64
	var providerUsedPercentage *float64

	if limit > 0 {
		usedCount := limit - remaining
		providerUsedCount = &usedCount
		providerTotalCount = &limit
		providerRemainingCount = &remaining
		pct := (float64(usedCount) / float64(limit)) * 100
		providerUsedPercentage = &pct
	}

	summary := &QuotaSummary{
		WindowKind:              "coding_plan",
		PeriodStartAt:          periodStartAt,
		PeriodEnd:               nextResetAt,
		ProviderUsedCount:       providerUsedCount,
		ProviderTotalCount:      providerTotalCount,
		ProviderRemainingCount:  providerRemainingCount,
		ProviderUsedPercentage:  providerUsedPercentage,
		DisplayStatusReason:      displayStatusReason,
		UsageRatio:              usageRatio,
		PeriodLabel:             "Interval",
		Partial:                 partial,
	}

	return QuotaData{
		Status:       normalizedStatus,
		ProviderType: "kimi",
		RawData:      rawData,
		NextResetAt:  nextResetAt,
		Ready:        normalizedStatus == "available" || normalizedStatus == "warning",
		Summary:      summary,
	}, nil
}

func (c *KimiQuotaChecker) SupportsChannel(ch *ent.Channel) bool {
	return ch.Type == channel.TypeKimi || ch.Type == channel.TypeKimiAnthropic
}