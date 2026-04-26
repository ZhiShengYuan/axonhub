import { Loader2, RefreshCw, Battery, BatteryLow, BatteryMedium, BatteryFull, BatteryWarning } from 'lucide-react';
import { Popover, PopoverContent, PopoverTrigger } from '@/components/ui/popover';
import { useProviderQuotaStatuses, ProviderQuotaChannel } from '@/features/system/data/quotas';
import { useTranslation } from 'react-i18next';

const STATUS_COLORS = {
  available: 'bg-green-500 hover:bg-green-600 text-white',
  warning: 'bg-yellow-500 hover:bg-yellow-600 text-white',
  exhausted: 'bg-red-500 hover:bg-red-600 text-white',
  unknown: 'bg-gray-500 hover:bg-gray-600 text-white',
} as const;

const STATUS_LABELS = {
  available: 'quota.status.available',
  warning: 'quota.status.warning',
  exhausted: 'quota.status.exhausted',
  unknown: 'quota.status.unknown',
} as const;

const SUMMARY_TYPES = ['minimax', 'minimax_anthropic', 'zhipu', 'zhipu_anthropic', 'kimi', 'kimi_anthropic'];

function isSummaryType(type: string): boolean {
  return SUMMARY_TYPES.includes(type);
}

type QuotaData = {
  windows?: {
    '5h'?: { utilization?: number; reset?: number; status?: string };
    '7d'?: { utilization?: number; reset?: number; status?: string };
    overage?: { utilization?: number; reset?: number; status?: string };
  };
  representative_claim?: string;
  plan_type?: string;
  rate_limit?: {
    primary_window?: { used_percent?: number; reset_at?: number; limit_window_seconds?: number };
    secondary_window?: { used_percent?: number; reset_at?: number; limit_window_seconds?: number };
  };
  summary?: {
    usage_ratio?: number;
    provider_used_percentage?: number;
    provider_used_count?: number;
    provider_total_count?: number;
    provider_remaining_count?: number;
    period_label?: string;
    period_start_at?: string;
    period_end_at?: string;
    partial?: boolean;
    channel_request_count?: number;
    display_status_reason?: string;
    window_kind?: string;
  };
  error?: string;
};

type BatteryLevel = 'full' | 'medium' | 'low' | 'empty' | 'warning';

function getBatteryIcon(level: BatteryLevel) {
  switch (level) {
    case 'full':
      return BatteryFull;
    case 'medium':
      return BatteryMedium;
    case 'low':
      return BatteryLow;
    case 'warning':
      return BatteryWarning;
    default:
      return Battery;
  }
}

function getBatteryLevel(percentage: number, status: string): BatteryLevel {
  if (status === 'exhausted') return 'warning';
  const remaining = 100 - percentage;
  if (remaining < 5) return 'empty';
  if (remaining < 20) return 'low';
  if (remaining < 80) return 'medium';
  return 'full';
}

function getChannelPercentage(channel: ProviderQuotaChannel, quotaData: QuotaData): number {
  let percentage = 0;
  if (channel.type === 'claudecode') {
    const util5h = quotaData.windows?.['5h']?.utilization || 0;
    const util7d = quotaData.windows?.['7d']?.utilization || 0;
    percentage = Math.max(util5h, util7d) * 100;
  } else if (channel.type === 'codex') {
    percentage = quotaData.rate_limit?.primary_window?.used_percent || 0;
  } else if (isSummaryType(channel.type)) {
    const summary = quotaData?.summary;
    const usageRatio = summary?.usage_ratio;
    const providerUsedPercentage = summary?.provider_used_percentage;
    percentage = usageRatio != null ? usageRatio * 100 : providerUsedPercentage != null ? providerUsedPercentage : 0;
  }
  return percentage;
}

function QuotaRow({ channel }: { channel: ProviderQuotaChannel }) {
  const { t } = useTranslation();
  const quota = channel.quotaStatus;
  if (!quota) return null;

  const status = quota.status || 'unknown';
  const colorClass = STATUS_COLORS[status as keyof typeof STATUS_COLORS] || STATUS_COLORS.unknown;
  const statusLabel = t(STATUS_LABELS[status as keyof typeof STATUS_LABELS]);
  const quotaData = quota.quotaData as QuotaData;

  const percentage = getChannelPercentage(channel, quotaData);
  const batteryLevel = getBatteryLevel(percentage, status);
  const BatteryIcon = getBatteryIcon(batteryLevel);
  const displayPercentage = status === 'unknown' ? '?' : Math.round(percentage);

  const formatWindowDuration = (seconds?: number) => {
    if (!seconds) return t('quota.unknown');
    const hours = Math.floor(seconds / 3600);
    const days = hours >= 24 ? Math.floor(hours / 24) : 0;
    if (days > 0) return t('system.quota.durationDays', { count: days });
    if (hours > 0) return t('system.quota.durationHours', { count: hours });
    return t('system.quota.durationMinutes', { count: Math.floor(seconds / 60) });
  };

  const formatTimeToReset = (resetAt?: string | null) => {
    if (!resetAt) return t('quota.unknown');
    const now = Date.now();
    const reset = new Date(resetAt).getTime();
    const diffMs = reset - now;
    if (diffMs < 0) return t('system.quota.resetNow');
    const diffMins = Math.floor(diffMs / 60000);
    const diffHours = Math.floor(diffMins / 60);
    if (diffHours > 0) return t('system.quota.resetTimeHoursMinutes', { hours: diffHours, minutes: diffMins % 60 });
    return t('system.quota.resetTimeMinutes', { minutes: diffMins });
  };

  return (
    <div className="space-y-2 text-sm py-3 first:pt-0 border-b last:border-0 last:pb-0 pb-3">
      <div className="flex items-center justify-between">
        <div className="flex items-center gap-2">
          <BatteryIcon className={`w-4 h-4 ${status === 'exhausted' ? 'text-red-500' : status === 'warning' ? 'text-yellow-500' : 'text-muted-foreground'}`} />
          <span className="font-medium">{channel.name}</span>
        </div>
        <span className={`text-xs px-2 py-0.5 rounded ${colorClass}`}>{statusLabel}</span>
      </div>

      {quotaData.error && (
        <div className="ml-6 text-xs text-red-500 break-words">
          <span className="font-medium">{t('quota.label.error')}:</span> {quotaData.error}
        </div>
      )}

      {channel.type === 'claudecode' && (
        <div className="ml-6 mt-2">
          <div className="space-y-1.5 text-xs">
            <div className="flex justify-between items-center text-muted-foreground">
              <span>{t('quota.label.used')}</span>
              <span className={`font-medium ${batteryLevel === 'warning' || batteryLevel === 'low' ? 'text-red-500' : 'text-foreground'}`}>{displayPercentage}%</span>
            </div>
            <div className="flex justify-between items-center text-muted-foreground">
              <span>{t('quota.window.5h')}</span>
              <span className="font-medium">{Math.round((quotaData.windows?.['5h']?.utilization || 0) * 100)}%</span>
            </div>
            <div className="flex justify-between items-center text-muted-foreground">
              <span>{t('quota.window.7d')}</span>
              <span className="font-medium">{Math.round((quotaData.windows?.['7d']?.utilization || 0) * 100)}%</span>
            </div>
            {quotaData.representative_claim && (
              <div className="flex justify-between items-center text-muted-foreground">
                <span>{t('quota.label.limiting_bucket')}</span>
                <span>{quotaData.representative_claim === 'five_hour' ? '5h' : '7d'}</span>
              </div>
            )}
            <div className="flex justify-between items-center text-muted-foreground">
              <span>{t('quota.label.reset_in')}</span>
              <span>{formatTimeToReset(quota.nextResetAt)}</span>
            </div>
          </div>
        </div>
      )}

      {channel.type === 'codex' && (
        <div className="ml-6 mt-2">
          <div className="space-y-1.5 text-xs">
            <div className="flex justify-between items-center text-muted-foreground">
              <span>{t('quota.label.used')}</span>
              <span className={`font-medium ${batteryLevel === 'warning' || batteryLevel === 'low' ? 'text-red-500' : 'text-foreground'}`}>{displayPercentage}%</span>
            </div>
            <div className="flex justify-between items-center text-muted-foreground">
              <span>{t('quota.label.primary_window')}</span>
              <span className="font-medium">{Math.round(quotaData.rate_limit?.primary_window?.used_percent || 0)}%</span>
            </div>
            <div className="flex justify-between items-center text-muted-foreground">
              <span>{t('quota.label.primary_duration')}</span>
              <span>{formatWindowDuration(quotaData.rate_limit?.primary_window?.limit_window_seconds)}</span>
            </div>
            {quotaData.rate_limit?.primary_window?.reset_at && (
              <div className="flex justify-between items-center text-muted-foreground">
                <span>{t('quota.label.resets_at')}</span>
                <span>{new Date(quotaData.rate_limit.primary_window.reset_at * 1000).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })}</span>
              </div>
            )}
            {quotaData.plan_type && (
              <div className="flex justify-between items-center text-muted-foreground">
                <span>{t('quota.label.plan')}</span>
                <span>{quotaData.plan_type}</span>
              </div>
            )}
            {quotaData.rate_limit?.secondary_window?.used_percent !== undefined && (
              <>
                <div className="flex justify-between items-center text-muted-foreground">
                  <span>{t('quota.label.secondary_window')}</span>
                  <span className="font-medium">{Math.round(quotaData.rate_limit.secondary_window.used_percent)}%</span>
                </div>
                {quotaData.rate_limit?.secondary_window?.limit_window_seconds && (
                  <div className="flex justify-between items-center text-muted-foreground">
                    <span>{t('quota.label.secondary_duration')}</span>
                    <span>{formatWindowDuration(quotaData.rate_limit.secondary_window.limit_window_seconds)}</span>
                  </div>
                )}
              </>
            )}
          </div>
        </div>
      )}

      {isSummaryType(channel.type) && (
        <div className="ml-6 mt-2">
          {(() => {
            const summary = quotaData?.summary;
            const usageRatio = summary?.usage_ratio;
            const providerUsedPercentage = summary?.provider_used_percentage;
            const displayPercent = usageRatio != null ? usageRatio * 100 : providerUsedPercentage != null ? providerUsedPercentage : null;
            const hasData = displayPercent !== null;
            const isPartial = summary?.partial === true;
            const periodLabel = summary?.period_label;
            const periodEnd = summary?.period_end_at;
            const usedCount = summary?.provider_used_count;
            const totalCount = summary?.provider_total_count;
            const remainingCount = summary?.provider_remaining_count;
            const channelRequestCount = summary?.channel_request_count;

            let iconColor = 'text-muted-foreground';
            if (status === 'exhausted') {
              iconColor = 'text-red-500';
            } else if (status === 'warning') {
              iconColor = 'text-yellow-500';
            } else if (hasData) {
              if (displayPercent >= 95) iconColor = 'text-red-500';
              else if (displayPercent >= 80) iconColor = 'text-yellow-500';
              else iconColor = 'text-green-500';
            }

            return (
              <div className="space-y-1.5 text-xs">
                <div className="flex justify-between items-center text-muted-foreground">
                  <span>{t('quota.label.used')}</span>
                  <span className={`font-medium ${iconColor.includes('red') ? 'text-red-500' : iconColor.includes('yellow') ? 'text-yellow-500' : 'text-foreground'}`}>
                    {displayPercent !== null ? `${Math.round(displayPercent)}%${isPartial ? t('system.quota.partial') : ''}` : '—'}
                  </span>
                </div>
                {usedCount != null && totalCount != null && (
                  <div className="flex justify-between items-center text-muted-foreground">
                    <span>{t('quota.label.usage')}</span>
                    <span className="font-medium">{usedCount} / {totalCount}</span>
                  </div>
                )}
                {remainingCount != null && (
                  <div className="flex justify-between items-center text-muted-foreground">
                    <span>{t('quota.label.remaining')}</span>
                    <span className="font-medium">{remainingCount}</span>
                  </div>
                )}
                {channelRequestCount != null && (
                  <div className="flex justify-between items-center text-muted-foreground">
                    <span>{t('quota.label.requests')}</span>
                    <span className="font-medium">{channelRequestCount}</span>
                  </div>
                )}
                {periodLabel && (
                  <div className="flex justify-between items-center text-muted-foreground">
                    <span>{t('quota.label.period')}</span>
                    <span>{t('system.quota.periodType.' + periodLabel, periodLabel)}</span>
                  </div>
                )}
                {periodEnd && (
                  <div className="flex justify-between items-center text-muted-foreground">
                    <span>{t('quota.label.resets_at')}</span>
                    <span>{new Date(periodEnd).toLocaleDateString()}</span>
                  </div>
                )}
              </div>
            );
          })()}
        </div>
      )}
    </div>
  );
}

function QuotaBadgeTrigger({ channels }: { channels: ProviderQuotaChannel[] }) {
  const highestUsed = Math.max(...channels.map(c => {
    const quota = c.quotaStatus;
    if (!quota) return 0;
    const quotaData = quota.quotaData as QuotaData;
    return getChannelPercentage(c, quotaData);
  }));

  const hasExhausted = channels.some(c => c.quotaStatus?.status === 'exhausted');
  const hasWarning = channels.some(c => c.quotaStatus?.status === 'warning');

  let level: BatteryLevel = 'full';
  if (hasExhausted) level = 'warning';
  else if (hasWarning) level = 'low';
  else level = getBatteryLevel(highestUsed, 'available');

  const BatteryIcon = getBatteryIcon(level);
  const isWarning = level === 'warning';
  const textColor = isWarning ? 'text-red-500' : level === 'low' ? 'text-yellow-500' : 'text-muted-foreground';

  return (
    <BatteryIcon className={`w-5 h-5 ${textColor} transition-colors`} />
  );
}

export function QuotaBadges({ isRefreshing, onRefresh }: { isRefreshing: boolean; onRefresh: () => void }) {
  const { t } = useTranslation();
  const channels = useProviderQuotaStatuses();

  if (channels.length === 0) return null;

  return (
    <Popover>
      <PopoverTrigger asChild>
        <button type="button" data-testid="quota-badge-trigger" className="p-2 hover:bg-muted rounded-md transition-colors relative">
          <QuotaBadgeTrigger channels={channels} />
        </button>
      </PopoverTrigger>
      <PopoverContent className={channels.length > 4 ? "w-[640px]" : "w-80"} align="end">
        <div className="space-y-1">
          <div className="flex items-center justify-between mb-2">
            <div className="text-xs font-medium text-muted-foreground uppercase tracking-wide">
              {t('system.providerQuota.title')}
            </div>
            <button
              onClick={onRefresh}
              disabled={isRefreshing}
              className="text-muted-foreground hover:text-foreground transition-colors"
              aria-label={t('system.quota.refreshQuotas')}
            >
              {isRefreshing ? (
                <Loader2 className="w-4 h-4 animate-spin" />
              ) : (
                <RefreshCw className="w-4 h-4" />
              )}
            </button>
          </div>
          <div className={`max-h-[60vh] overflow-y-auto ${channels.length > 4 ? 'grid grid-cols-2 gap-x-4' : ''}`}>
            {channels.map((channel: ProviderQuotaChannel) => (
              <QuotaRow key={channel.id} channel={channel} />
            ))}
          </div>
        </div>
      </PopoverContent>
    </Popover>
  );
}
