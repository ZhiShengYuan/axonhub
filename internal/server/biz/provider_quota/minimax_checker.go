package provider_quota

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/looplj/axonhub/internal/ent"
	"github.com/looplj/axonhub/internal/ent/channel"
	"github.com/looplj/axonhub/llm/httpclient"
)

// MiniMaxUsageResponse matches the unofficial MiniMax remains endpoint response.
// Endpoint: GET https://www.minimaxi.com/v1/api/openplatform/coding_plan/remains
// NOTE: This is an UNOFFICIAL/REVERSE-ENGINEERED endpoint - use with caution.
type MiniMaxUsageResponse struct {
	Interval *MiniMaxUsageWindow `json:"interval,omitempty"`
	Weekly   *MiniMaxUsageWindow `json:"weekly,omitempty"`
}

type MiniMaxUsageWindow struct {
	TotalCount    int64   `json:"current_interval_total_count,omitempty"`
	UsageCount    int64   `json:"current_interval_usage_count,omitempty"`
	WeeklyTotal   int64   `json:"weekly_total_count,omitempty"`
	WeeklyUsage   int64   `json:"weekly_usage_count,omitempty"`
	StartTime     string  `json:"start_time,omitempty"`
	EndTime       string  `json:"end_time,omitempty"`
	UsageRatio    float64 `json:"-"`
}

func (w *MiniMaxUsageWindow) computeUsageRatio() {
	if w.TotalCount > 0 {
		w.UsageRatio = float64(w.UsageCount) / float64(w.TotalCount)
	}
}

type MiniMaxQuotaChecker struct {
	httpClient *httpclient.HttpClient
}

func NewMiniMaxQuotaChecker(httpClient *httpclient.HttpClient) *MiniMaxQuotaChecker {
	return &MiniMaxQuotaChecker{
		httpClient: httpClient,
	}
}

func (c *MiniMaxQuotaChecker) CheckQuota(ctx context.Context, ch *ent.Channel) (QuotaData, error) {
	apiKey := ch.Credentials.APIKey
	if apiKey == "" {
		return QuotaData{}, fmt.Errorf("channel has no credentials")
	}

	httpRequest := httpclient.NewRequestBuilder().
		WithMethod("GET").
		WithURL("https://www.minimaxi.com/v1/api/openplatform/coding_plan/remains").
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

func (c *MiniMaxQuotaChecker) parseResponse(body []byte) (QuotaData, error) {
	var response MiniMaxUsageResponse

	if err := json.Unmarshal(body, &response); err != nil {
		return QuotaData{}, fmt.Errorf("failed to parse minimax usage response: %w", err)
	}

	rawData := map[string]any{
		"remains": body,
	}

	var intervalData, weeklyData map[string]any
	if response.Interval != nil {
		intervalData = convertMiniMaxWindowToMap(response.Interval)
		rawData["interval"] = intervalData
	}
	if response.Weekly != nil {
		weeklyData = convertMiniMaxWindowToMap(response.Weekly)
		rawData["weekly"] = weeklyData
	}

	normalizedStatus := "unknown"
	var nextResetAt *time.Time
	var usageRatio *float64
	partial := true

	if response.Interval != nil && response.Interval.TotalCount > 0 {
		partial = false
		response.Interval.computeUsageRatio()

		remaining := response.Interval.TotalCount - response.Interval.UsageCount

		if remaining <= 0 {
			normalizedStatus = "exhausted"
			usageRatio = &response.Interval.UsageRatio
		} else if response.Interval.UsageRatio >= 0.8 {
			normalizedStatus = "warning"
			usageRatio = &response.Interval.UsageRatio
		} else {
			normalizedStatus = "available"
			usageRatio = &response.Interval.UsageRatio
		}

		if response.Interval.EndTime != "" {
			if t, err := time.Parse(time.RFC3339, response.Interval.EndTime); err == nil {
				nextResetAt = &t
			}
		}
	}

	var periodStartAt *time.Time
	if response.Interval != nil && response.Interval.StartTime != "" {
		if t, err := time.Parse(time.RFC3339, response.Interval.StartTime); err == nil {
			periodStartAt = &t
		}
	}

	displayStatusReason := "complete"
	if partial {
		displayStatusReason = "usage_only"
	}

	var providerUsedCount, providerTotalCount, providerRemainingCount *int64
	var providerUsedPercentage *float64

	if response.Interval != nil && response.Interval.TotalCount > 0 {
		providerUsedCount = &response.Interval.UsageCount
		providerTotalCount = &response.Interval.TotalCount
		remaining := int64(response.Interval.TotalCount - response.Interval.UsageCount)
		providerRemainingCount = &remaining
		pct := response.Interval.UsageRatio * 100
		providerUsedPercentage = &pct
	}

	summary := &QuotaSummary{
		WindowKind:              "interval",
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
		ProviderType: "minimax",
		RawData:      rawData,
		NextResetAt:  nextResetAt,
		Ready:        normalizedStatus == "available" || normalizedStatus == "warning",
		Summary:      summary,
	}, nil
}

func (c *MiniMaxQuotaChecker) SupportsChannel(ch *ent.Channel) bool {
	return ch.Type == channel.TypeMinimax || ch.Type == channel.TypeMinimaxAnthropic
}

func convertMiniMaxWindowToMap(window *MiniMaxUsageWindow) map[string]any {
	result := make(map[string]any)

	if window.TotalCount > 0 {
		result["current_interval_total_count"] = window.TotalCount
	}

	if window.UsageCount > 0 {
		result["current_interval_usage_count"] = window.UsageCount
	}

	if window.WeeklyTotal > 0 {
		result["weekly_total_count"] = window.WeeklyTotal
	}

	if window.WeeklyUsage > 0 {
		result["weekly_usage_count"] = window.WeeklyUsage
	}

	if window.StartTime != "" {
		result["start_time"] = window.StartTime
	}

	if window.EndTime != "" {
		result["end_time"] = window.EndTime
	}

	return result
}