package provider_quota

import (
	"context"
	"time"

	"github.com/looplj/axonhub/internal/ent"
)

// QuotaChecker checks quota status for a provider.
type QuotaChecker interface {
	// CheckQuota makes a minimal API request to get quota information and returns parsed quota data
	CheckQuota(ctx context.Context, channel *ent.Channel) (QuotaData, error)

	// SupportsChannel returns true if this checker supports the channel
	SupportsChannel(channel *ent.Channel) bool
}

// QuotaSummary is the canonical contract for frontend consumption.
type QuotaSummary struct {
	// WindowKind describes the type of quota window: "interval", "weekly", "tokens_limit", "usage_only"
	WindowKind string `json:"window_kind,omitempty"`
	// PeriodStartAt is the start of the quota period from provider metadata. Nil when unknown.
	PeriodStartAt *time.Time `json:"period_start_at,omitempty"`
	// When the current quota period ends. Nil when unknown.
	PeriodEnd *time.Time `json:"period_end_at,omitempty"`
	// ProviderUsedCount is the used count from provider. Nil when unknown.
	ProviderUsedCount *int64 `json:"provider_used_count,omitempty"`
	// ProviderTotalCount is the total count from provider. Nil when unknown.
	ProviderTotalCount *int64 `json:"provider_total_count,omitempty"`
	// ProviderRemainingCount is the remaining count from provider. Nil when unknown.
	ProviderRemainingCount *int64 `json:"provider_remaining_count,omitempty"`
	// ProviderUsedPercentage is the usage percentage from provider (0-100 scale). Nil when unknown.
	ProviderUsedPercentage *float64 `json:"provider_used_percentage,omitempty"`
	// ChannelRequestCount is the local UsageLog count. Nil when unknown.
	ChannelRequestCount *int64 `json:"channel_request_count,omitempty"`
	// DisplayStatusReason describes the status: "complete", "usage_only", "missing_limit", "error"
	DisplayStatusReason string `json:"display_status_reason,omitempty"`
	// Normalized usage ratio (0.0-1.0). Nil when unknown.
	UsageRatio *float64 `json:"usage_ratio,omitempty"`
	// Human-readable period label, e.g. "Monthly", "Weekly", "Hourly". Empty when unknown.
	PeriodLabel string `json:"period_label,omitempty"`
	// Whether the summary contains only partial data (e.g. usage known but limit unknown).
	Partial bool `json:"partial,omitempty"`
}

// QuotaData is the unified quota data structure.
type QuotaData struct {
	Status       string         `json:"status"` // available, warning, exhausted, unknown
	ProviderType string         `json:"provider_type"`
	RawData      map[string]any `json:"raw_data"`
	NextResetAt  *time.Time     `json:"next_reset_at"` // Next quota reset timestamp
	Ready        bool           `json:"ready"`         // True if status is available or warning
	Summary      *QuotaSummary  `json:"summary,omitempty"`
}
