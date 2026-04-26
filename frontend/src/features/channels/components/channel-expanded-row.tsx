import { memo, useRef, useEffect } from 'react';
import { format } from 'date-fns';
import { useTranslation } from 'react-i18next';
import { useMutation, useQueryClient } from '@tanstack/react-query';
import { Badge } from '@/components/ui/badge';
import { CHANNEL_CONFIGS } from '../data/config_channels';
import { Channel } from '../data/schema';
import { checkProviderQuotas } from '@/features/system/data/quotas';

const MINIMAX_ZHIPU_TYPES = ['minimax', 'minimax_anthropic', 'zhipu', 'zhipu_anthropic'];

interface ChannelExpandedRowProps {
  channel: Channel;
  columnsLength: number;
  getApiFormatLabel: (apiFormat?: string) => string;
}

export const ChannelExpandedRow = memo(({ channel, columnsLength, getApiFormatLabel }: ChannelExpandedRowProps) => {
  const { t } = useTranslation();
  const config = CHANNEL_CONFIGS[channel.type];

  return (
    <div className='bg-muted/30 p-6 hover:bg-muted/50'>
      <div className='space-y-6'>
        <div className='grid grid-cols-1 gap-6 md:grid-cols-2'>
          <div className='space-y-3'>
            <h4 className='text-sm font-semibold'>{t('channels.expandedRow.basic')}</h4>
            <div className='space-y-2 text-sm'>
              <div className='flex items-start gap-2'>
                <span className='text-muted-foreground shrink-0'>{t('channels.columns.baseURL')}:</span>
                <span className='min-w-0 flex-1 text-right font-mono text-xs break-all'>{channel.baseURL}</span>
              </div>
              <div className='flex items-center justify-between'>
                <span className='text-muted-foreground'>{t('channels.columns.type')}:</span>
                <Badge variant='outline' className={config?.color}>
                  {t(`channels.types.${channel.type}`)}
                </Badge>
              </div>
              <div className='flex items-center justify-between'>
                <span className='text-muted-foreground'>{t('channels.expandedRow.apiFormat')}:</span>
                <span className='font-mono text-xs'>{getApiFormatLabel(config?.apiFormat)}</span>
              </div>
              <div className='flex justify-between'>
                <span className='text-muted-foreground'>{t('common.columns.createdAt')}:</span>
                <span>{format(channel.createdAt, 'yyyy-MM-dd HH:mm')}</span>
              </div>
              <div className='flex justify-between'>
                <span className='text-muted-foreground'>{t('common.columns.updatedAt')}:</span>
                <span>{format(channel.updatedAt, 'yyyy-MM-dd HH:mm')}</span>
              </div>
            </div>
          </div>

          <div className='space-y-6'>
            <div className='space-y-3'>
              <h4 className='text-sm font-semibold'>{t('channels.expandedRow.additional')}</h4>
              <div className='space-y-2 text-sm'>
                <div className='flex items-center justify-between'>
                  <span className='text-muted-foreground'>{t('channels.columns.orderingWeight')}:</span>
                  <span className='font-mono text-xs'>{channel.orderingWeight ?? 0}</span>
                </div>
                <div className='flex justify-between'>
                  <span className='text-muted-foreground'>{t('channels.expandedRow.remark')}:</span>
                  <span className='max-w-[200px] truncate text-right' title={channel.remark || undefined}>
                    {channel.remark || '-'}
                  </span>
                </div>
                <div className='flex items-start justify-between'>
                  <span className='text-muted-foreground shrink-0'>{t('channels.expandedRow.tags')}:</span>
                  <div className='flex max-w-[200px] flex-wrap justify-end gap-1'>
                    {channel.tags && channel.tags.length > 0 ? (
                      channel.tags.map((tag) => (
                        <Badge key={tag} variant='outline' className='text-xs'>
                          {tag}
                        </Badge>
                      ))
                    ) : (
                      <span>-</span>
                    )}
                  </div>
                </div>
              </div>
            </div>

          </div>
        </div>

        {channel.supportedModels && channel.supportedModels.length > 0 && (
          <div className='space-y-3'>
            <h4 className='text-sm font-semibold'>{t('channels.expandedRow.supportedModels')}</h4>
            <div className='flex flex-wrap gap-2'>
              {channel.supportedModels.slice(0, 5).map((model) => (
                <Badge key={model} variant='secondary' className='font-mono text-xs'>
                  {model}
                </Badge>
              ))}
              {channel.supportedModels.length > 5 && (
                <span className='text-muted-foreground flex items-center text-xs italic'>
                  {t('channels.expandedRow.moreModels', { count: channel.supportedModels.length - 5 })}
                </span>
              )}
            </div>
          </div>
        )}

        {MINIMAX_ZHIPU_TYPES.includes(channel.type) && (
          <QuotaSummarySection channel={channel} />
        )}
      </div>
    </div>
  );
});

interface QuotaSummarySectionProps {
  channel: Channel;
}

const QuotaSummarySection = memo(({ channel }: QuotaSummarySectionProps) => {
  const { t } = useTranslation();
  const queryClient = useQueryClient();
  const hasTriggeredRefresh = useRef(false);
  const quotaStatus = channel.providerQuotaStatus;
  const summary = quotaStatus?.quotaData?.summary;

  const refreshMutation = useMutation({
    mutationFn: checkProviderQuotas,
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: ['channels'] });
    },
    onError: (err) => {
      console.error('[QuotaSummary] Auto-refresh failed:', err);
    },
  });

  useEffect(() => {
    const isUnavailable = !quotaStatus || quotaStatus.status === 'unknown' || quotaStatus.ready === false;
    if (isUnavailable && !hasTriggeredRefresh.current && !refreshMutation.isPending) {
      hasTriggeredRefresh.current = true;
      refreshMutation.mutate();
    }
    if (quotaStatus && quotaStatus.ready !== false && quotaStatus.status !== 'unknown') {
      hasTriggeredRefresh.current = false;
    }
  }, [quotaStatus, refreshMutation.isPending]);

  if (!quotaStatus) {
    return (
      <div className='space-y-3'>
        <h4 className='text-sm font-semibold'>{t('channels.quota.title')}</h4>
        <p className='text-muted-foreground text-sm'>{t('channels.quota.notAvailable')}</p>
      </div>
    );
  }

  const status = quotaStatus.status || 'unknown';
  // usage_ratio is 0-1 scale; provider_used_percentage is 0-100 scale. Handle each correctly.
  const usageRatio = summary?.usage_ratio;
  const providerUsedPercentage = summary?.provider_used_percentage;
  // Display percentage: usage_ratio * 100 or provider_used_percentage directly
  const displayPercent = usageRatio != null ? usageRatio * 100 : providerUsedPercentage != null ? providerUsedPercentage : null;
  const periodLabel = summary?.period_label;
  const periodEnd = summary?.period_end_at;
  const partial = summary?.partial;
  const channelRequestCount = summary?.channel_request_count;
  const usedCount = summary?.provider_used_count;
  const totalCount = summary?.provider_total_count;
  const remainingCount = summary?.provider_remaining_count;

  const getStatusColor = () => {
    switch (status) {
      case 'available':
        return 'bg-green-100 text-green-800';
      case 'warning':
        return 'bg-amber-100 text-amber-800';
      case 'exhausted':
        return 'bg-red-100 text-red-800';
      default:
        return 'bg-gray-100 text-gray-800';
    }
  };

  return (
    <div className='space-y-3'>
      <h4 className='text-sm font-semibold'>{t('channels.quota.title')}</h4>
      <div className='space-y-2 text-sm'>
        <div className='flex items-center justify-between'>
          <span className='text-muted-foreground'>{t('channels.quota.status')}:</span>
          <span className={`inline-flex items-center rounded-full px-2.5 py-0.5 text-xs font-medium ${getStatusColor()}`}>
            {t(`quota.status.${status}`)}
          </span>
        </div>

        {displayPercent != null && (
          <div className='flex items-center justify-between'>
            <span className='text-muted-foreground'>{t('channels.quota.usage')}:</span>
            <span className='font-mono text-xs'>{Math.round(displayPercent ?? 0)}% {t('channels.quota.used')}</span>
          </div>
        )}

        {usedCount != null && totalCount != null && (
          <div className='flex items-center justify-between'>
            <span className='text-muted-foreground'>{t('channels.quota.allocation')}:</span>
            <span className='font-mono text-xs'>{usedCount} / {totalCount}</span>
          </div>
        )}

        {remainingCount != null && (
          <div className='flex items-center justify-between'>
            <span className='text-muted-foreground'>{t('channels.quota.remaining')}:</span>
            <span className='font-mono text-xs'>{remainingCount}</span>
          </div>
        )}

        {periodLabel && (
          <div className='flex items-center justify-between'>
            <span className='text-muted-foreground'>{t('channels.quota.period')}:</span>
            <span className='text-muted-foreground'>{t('channels.quota.periodType.' + periodLabel, periodLabel)}</span>
          </div>
        )}

        {periodEnd && (
          <div className='flex items-center justify-between'>
            <span className='text-muted-foreground'>{t('channels.quota.resets')}:</span>
            <span className='font-mono text-xs'>
              {format(new Date(periodEnd), 'yyyy-MM-dd HH:mm')}
            </span>
          </div>
        )}

        {channelRequestCount != null && (
          <div className='flex items-center justify-between'>
            <span className='text-muted-foreground'>{t('channels.quota.requests')}:</span>
            <span className='font-mono text-xs'>{channelRequestCount} {t('channels.quota.requestsThisPeriod')}</span>
          </div>
        )}

        {partial && (
          <div className='flex items-center justify-between'>
            <span className='text-muted-foreground'>{t('channels.quota.note')}:</span>
            <span className='text-muted-foreground text-xs italic'>{t('channels.quota.partialData')}</span>
          </div>
        )}

        {displayPercent == null && !periodLabel && !periodEnd && channelRequestCount == null && !partial && usedCount == null && remainingCount == null && (
          <p className='text-muted-foreground text-sm'>—</p>
        )}
      </div>
    </div>
  );
});

QuotaSummarySection.displayName = 'QuotaSummarySection';

ChannelExpandedRow.displayName = 'ChannelExpandedRow';
