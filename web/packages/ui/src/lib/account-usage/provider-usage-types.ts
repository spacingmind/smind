import type { AccountUsage, UsageMode } from "@/lib/account-usage/types";

/** One provider instance's plan usage: the `account.usage` row shape plus the instance it belongs to (`accountId` is always 0). */
export interface ProviderUsageRow extends AccountUsage {
  instanceId: string;
  /** RFC 3339: when the provider may be routed again after a rate limit; absent or past = none. */
  cooldownUntil?: string | null;
}

export interface ProviderUsageParams {
  instanceIds?: string[];
  mode?: UsageMode;
}

export interface ProviderUsageResult {
  enabled: boolean;
  ttlSeconds: number;
  reports: ProviderUsageRow[];
}
