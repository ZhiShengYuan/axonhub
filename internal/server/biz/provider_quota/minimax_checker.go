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

// MiniMaxUsageResponse matches the actual MiniMax remains endpoint response.
// Endpoint: GET https://www.minimaxi.com/v1/api/openplatform/coding_plan/remains
type MiniMaxUsageResponse struct {
	ModelRemains []MiniMaxModelRemain `json:"model_remains"`
	BaseResp    MiniMaxBaseResp      `json:"base_resp"`
}

type MiniMaxModelRemain struct {
	StartTime                 int64  `json:"start_time"`                   // milliseconds
	EndTime                   int64  `json:"end_time"`                     // milliseconds
	RemainsTime               int64  `json:"remains_time"`                 // seconds remaining
	CurrentIntervalTotalCount int64  `json:"current_interval_total_count"` // total quota for interval
	CurrentIntervalUsageCount int64  `json:"current_interval_usage_count"` // REMAINING quota (NOT used!)
	ModelName                 string `json:"model_name"`                   // e.g. "MiniMax-M*", "speech-hd", "coding-plan-vlm"
	CurrentWeeklyTotalCount   int64  `json:"current_weekly_total_count"`
	CurrentWeeklyUsageCount   int64  `json:"current_weekly_usage_count"` // REMAINING quota (NOT used!)
	WeeklyStartTime           int64  `json:"weekly_start_time"`          // milliseconds
	WeeklyEndTime             int64  `json:"weekly_end_time"`            // milliseconds
	WeeklyRemainsTime         int64  `json:"weekly_remains_time"`       // seconds remaining
}

type MiniMaxBaseResp struct {
	StatusCode int    `json:"status_code"`
	StatusMsg  string `json:"status_msg"`
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

	// Find the coding plan model entry
	matchedEntry := findMiniMaxCodingPlanModel(response.ModelRemains)

	rawData := map[string]any{
		"remains":        body,
		"matched_model":  "",
	}

	if matchedEntry != nil {
		rawData["matched_model"] = matchedEntry.ModelName
		rawData["interval"] = map[string]any{
			"start_time":                    matchedEntry.StartTime,
			"end_time":                      matchedEntry.EndTime,
			"remains_time":                  matchedEntry.RemainsTime,
			"current_interval_total_count":  matchedEntry.CurrentIntervalTotalCount,
			"current_interval_usage_count":  matchedEntry.CurrentIntervalUsageCount,
			"model_name":                    matchedEntry.ModelName,
		}
		if matchedEntry.CurrentWeeklyTotalCount > 0 {
			rawData["weekly"] = map[string]any{
				"current_weekly_total_count":  matchedEntry.CurrentWeeklyTotalCount,
				"current_weekly_usage_count":  matchedEntry.CurrentWeeklyUsageCount,
				"weekly_start_time":           matchedEntry.WeeklyStartTime,
				"weekly_end_time":             matchedEntry.WeeklyEndTime,
				"weekly_remains_time":         matchedEntry.WeeklyRemainsTime,
			}
		}
	}

	normalizedStatus := "unknown"
	var nextResetAt *time.Time
	var periodStartAt *time.Time
	var usageRatio *float64
	partial := true

	if matchedEntry != nil && matchedEntry.CurrentIntervalTotalCount > 0 {
		partial = false

		// CRITICAL: current_interval_usage_count is REMAINING, not used!
		// Used = total - remaining
		usedCount := matchedEntry.CurrentIntervalTotalCount - matchedEntry.CurrentIntervalUsageCount
		intervalUsageRatio := float64(usedCount) / float64(matchedEntry.CurrentIntervalTotalCount)

		if matchedEntry.CurrentIntervalUsageCount <= 0 {
			normalizedStatus = "exhausted"
			usageRatio = &intervalUsageRatio
		} else if intervalUsageRatio >= 0.8 {
			normalizedStatus = "warning"
			usageRatio = &intervalUsageRatio
		} else {
			normalizedStatus = "available"
			usageRatio = &intervalUsageRatio
		}

		// Convert millisecond timestamps to time.Time
		if matchedEntry.EndTime > 0 {
			t := msToTime(matchedEntry.EndTime)
			nextResetAt = &t
		}
		if matchedEntry.StartTime > 0 {
			t := msToTime(matchedEntry.StartTime)
			periodStartAt = &t
		}
	}

	displayStatusReason := "complete"
	if partial {
		displayStatusReason = "usage_only"
	}

	var providerUsedCount, providerTotalCount, providerRemainingCount *int64
	var providerUsedPercentage *float64

	if matchedEntry != nil && matchedEntry.CurrentIntervalTotalCount > 0 {
		usedCount := matchedEntry.CurrentIntervalTotalCount - matchedEntry.CurrentIntervalUsageCount
		providerUsedCount = &usedCount
		providerTotalCount = &matchedEntry.CurrentIntervalTotalCount
		providerRemainingCount = &matchedEntry.CurrentIntervalUsageCount // THIS IS REMAINING
		if matchedEntry.CurrentIntervalTotalCount > 0 {
			pct := (float64(usedCount) / float64(matchedEntry.CurrentIntervalTotalCount)) * 100
			providerUsedPercentage = &pct
		}
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

// findMiniMaxCodingPlanModel finds the coding plan model entry.
// Priority: model_name starting with "MiniMax-M" > model_name starting with "coding-plan" > first entry
func findMiniMaxCodingPlanModel(entries []MiniMaxModelRemain) *MiniMaxModelRemain {
	if len(entries) == 0 {
		return nil
	}

	// Priority 1: model_name starting with "MiniMax-M"
	for i := range entries {
		if strings.HasPrefix(entries[i].ModelName, "MiniMax-M") {
			return &entries[i]
		}
	}

	// Priority 2: model_name starting with "coding-plan"
	for i := range entries {
		if strings.HasPrefix(entries[i].ModelName, "coding-plan") {
			return &entries[i]
		}
	}

	// Priority 3: first entry
	return &entries[0]
}

// msToTime converts a millisecond timestamp to time.Time
func msToTime(ms int64) time.Time {
	return time.Unix(0, ms*int64(time.Millisecond))
}

func (c *MiniMaxQuotaChecker) SupportsChannel(ch *ent.Channel) bool {
	return ch.Type == channel.TypeMinimax || ch.Type == channel.TypeMinimaxAnthropic
}