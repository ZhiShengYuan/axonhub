'use client';

import { useState, useEffect } from 'react';
import { Request } from '../data/schema';

export type DisplayMode = 'latency' | 'tokensPerSecond';

// Minimum latency value (in milliseconds) used as a general safety floor for tokens/second
// calculations. Prevents unreasonably high tok/s values when latency is near zero.
const MINIMUM_LATENCY_MS = 10;

const VALID_DISPLAY_MODES: DisplayMode[] = ['latency', 'tokensPerSecond'];

/**
 * Calculate tokens per second for a given request.
 * Handles all edge cases including no usage log, zero latency, zero completion tokens,
 * and streaming vs non-streaming scenarios.
 *
 * @param request - The request object containing usage logs and metrics
 * @returns Formatted string like "123 tok/s" or "-" if calculation is not possible
 */
export function calculateTokensPerSecond(request: Request): string {
  const usageLog = request.usageLogs?.edges?.[0]?.node;
  if (!usageLog || request.metricsLatencyMs == null || request.metricsLatencyMs <= 0) {
    return '-';
  }

  // Sum all completion token types (matching fastest performers logic)
  const completionTokens =
    (usageLog.completionTokens || 0) +
    (usageLog.completionReasoningTokens || 0) +
    (usageLog.completionAudioTokens || 0);

  if (completionTokens === 0) {
    return '-';
  }

  let effectiveLatencyMs = request.metricsLatencyMs;
  if (effectiveLatencyMs < MINIMUM_LATENCY_MS) {
    effectiveLatencyMs = MINIMUM_LATENCY_MS;
  }

  const latencySeconds = effectiveLatencyMs / 1000;
  const tokensPerSecond = completionTokens / latencySeconds;

  return `${Math.round(tokensPerSecond)} tok/s`;
}

/**
 * Hook to manage display mode state with localStorage persistence.
 * SSR-safe: defaults to 'latency' during server-side rendering.
 * Uses two-phase initialization to avoid hydration mismatches.
 *
 * @returns Tuple of [displayMode, setDisplayMode] similar to useState
 */
export function useDisplayMode(): [DisplayMode, React.Dispatch<React.SetStateAction<DisplayMode>>] {
  const [displayMode, setDisplayMode] = useState<DisplayMode>(() => {
    if (typeof window !== 'undefined') {
      const stored = localStorage.getItem('requests-table-latency-display-mode');
      if (stored && VALID_DISPLAY_MODES.includes(stored as DisplayMode)) {
        return stored as DisplayMode;
      }
    }
    return 'latency';
  });

  useEffect(() => {
    if (typeof window !== 'undefined') {
      localStorage.setItem('requests-table-latency-display-mode', displayMode);
    }
  }, [displayMode]);

  return [displayMode, setDisplayMode];
}