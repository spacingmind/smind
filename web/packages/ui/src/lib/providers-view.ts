import type { Account, ProviderInfo } from "@/lib/types";

export type NavTone = "ok" | "warning" | "danger" | "neutral";

export interface ProviderNavItem {
  /** ProviderInfo.id, or "other" for the bucket of accounts no provider maps. */
  id: string;
  label: string;
  accountProvider?: string;
  kind: "oauth" | "api-key" | "cli" | "other";
  accountIds: number[];
  /** Worst account tone; "neutral" when there are no accounts or the provider is CLI-managed. */
  tone: NavTone;
  connected: boolean;
}

const TONE_ORDER: Record<NavTone, number> = { neutral: 2, ok: 3, warning: 1, danger: 0 };

export function buildProviderNav(input: {
  providers: ProviderInfo[];
  accounts: Account[];
  toneOf: (accountId: number) => NavTone | undefined;
}): ProviderNavItem[] {
  const items = input.providers.map((info): ProviderNavItem => {
    const isCli = info.kind === "cli";
    const accountIds = isCli
      ? []
      : input.accounts.filter((account) => account.provider === info.accountProvider).map((a) => a.id);
    return {
      id: info.id,
      label: info.label ?? info.id,
      accountProvider: info.accountProvider,
      kind: isCli ? "cli" : (info.credentialKind ?? "other"),
      accountIds,
      tone: worstTone(accountIds.map((id) => input.toneOf(id) ?? "neutral")),
      connected: accountIds.length > 0,
    };
  });
  const mapped = new Set(input.providers.map((info) => info.accountProvider));
  const others = input.accounts.filter((account) => !mapped.has(account.provider));
  if (others.length > 0) {
    items.push({
      id: "other",
      label: "Other accounts",
      kind: "other",
      accountIds: others.map((a) => a.id),
      tone: worstTone(others.map((a) => input.toneOf(a.id) ?? "neutral")),
      connected: true,
    });
  }
  return items;
}

function worstTone(tones: NavTone[]): NavTone {
  if (tones.length === 0) return "neutral";
  return tones.reduce((worst, tone) => (TONE_ORDER[tone] < TONE_ORDER[worst] ? tone : worst));
}
