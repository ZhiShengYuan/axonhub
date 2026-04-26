import { useRef, useEffect } from 'react';
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { graphqlRequest } from '@/gql/graphql';

const CHECK_PROVIDER_QUOTAS_QUERY = `
  mutation CheckProviderQuotas {
    checkProviderQuotas
  }
`;

const PROVIDER_QUOTA_STATUSES_QUERY = `
  query ProviderQuotaStatuses($input: QueryChannelInput!) {
    queryChannels(input: $input) {
      edges {
        node {
          id
          name
          type
          providerQuotaStatus {
            status
            nextResetAt
            ready
            quotaData
          }
        }
      }
    }
  }
`;

export async function checkProviderQuotas() {
  return graphqlRequest(CHECK_PROVIDER_QUOTAS_QUERY);
}

export type ProviderQuotaChannel = {
  id: string;
  name: string;
  type: string;
  quotaStatus?: {
    status: string;
    nextResetAt: string | null;
    ready: boolean;
    quotaData: any;
  };
};

export function useProviderQuotaStatuses() {
  const queryClient = useQueryClient();
  const hasTriggeredRefresh = useRef(false);

  const { data, error } = useQuery({
    queryKey: ['provider-quotas'],
    queryFn: async () => {
      const input = {
        where: {
          statusIn: ['enabled']
        }
      };
      return graphqlRequest<any>(PROVIDER_QUOTA_STATUSES_QUERY, { input });
    },
    refetchInterval: 60000, // Refetch every minute
  });

  const refreshMutation = useMutation({
    mutationFn: checkProviderQuotas,
    onSuccess: () => {
      void queryClient.refetchQueries({ queryKey: ['provider-quotas'] });
    },
    onError: (err) => {
      console.error('[ProviderQuota] Auto-refresh failed:', err);
    },
  });

  const channels = data?.queryChannels?.edges?.map((e: any) => e.node) || [];

  // Filter for OAuth channels (claudecode, codex, minimax, zhipu) - check both lowercase and PascalCase
  const oauthChannels = channels.filter((c: any) => {
    const type = c.type?.toLowerCase();
    const match = ['claudecode', 'codex', 'minimax', 'minimax_anthropic', 'zhipu', 'zhipu_anthropic', 'kimi', 'kimi_anthropic'].includes(type);
    return match;
  });

  // Map to standard format - providerQuotaStatus is a single object, not an edge/node structure
  const result = oauthChannels.map((channel: any): ProviderQuotaChannel => {
    const quotaStatus = channel.providerQuotaStatus;
    return {
      id: channel.id,
      name: channel.name,
      type: channel.type,
      quotaStatus,
    };
  });

  const hasUnavailable = result.some(c =>
    !c.quotaStatus || c.quotaStatus.status === 'unknown' || c.quotaStatus.ready === false
  );

  useEffect(() => {
    if (hasUnavailable && !hasTriggeredRefresh.current && !refreshMutation.isPending) {
      hasTriggeredRefresh.current = true;
      refreshMutation.mutate();
    }
    if (!hasUnavailable) {
      hasTriggeredRefresh.current = false;
    }
  }, [hasUnavailable, refreshMutation.isPending]);

  return result;
}
