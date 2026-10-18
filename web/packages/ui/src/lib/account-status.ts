import { usageCopy } from "@/lib/account-usage/copy";
import { activeCooldown } from "@/lib/account-usage/model";
import type { AccountUsage } from "@/lib/account-usage/types";
import type { Account, ProviderTestResult } from "@/lib/types";

export type AccountStatusTone = "ok" | "warning" | "danger" | "neutral";

export interface AccountStatus {
  /** The account's name everywhere: the usage report's displayName, else the label (AC58). */
  name: string;
  tone: AccountStatusTone;
  label: string;
  reason: string | null;
}

/**
 * The one account-status fold every surface shows (ADR-0032): the shared
 * usage report (cached, never fetched here), the local `provider.test`
 * result and the account itself become one tone/label/reason so Providers
 * and Plan usage can no longer disagree. Priority: an expired/rejected
 * login beats a passing local test; a failing local test beats cooldown and
 * usage-unavailable; `no_quota` is not a failure (API keys have no windows)
 * and falls through to the test/connected states.
 */
export function accountStatus({
  account,
  usage,
  test,
  now = Date.now(),
}: {
  account: Account;
  usage: AccountUsage | undefined;
  test: ProviderTestResult | undefined;
  now?: number;
}): AccountStatus {
  const name = usage?.displayName || account.label;
  const problem = usage?.report?.problem?.kind;
  if (usage && (problem === "expired" || problem === "rejected")) {
    return {
      name,
      tone: "danger",
      label: problem === "expired" ? "Login expired" : "Login rejected",
      reason: usageCopy.problem(usage, now),
    };
  }
  if (test && !test.ok) {
    return { name, tone: "danger", label: "Connection failed", reason: test.detail };
  }
  const cooldown = activeCooldown(usage, now);
  if (usage && cooldown) {
    return {
      name,
      tone: "warning",
      label: "Rate limited",
      reason: usageCopy.cooldown(cooldown, now).headline,
    };
  }
  if (usage?.report?.status === "error") {
    return { name, tone: "warning", label: "Usage unavailable", reason: usageCopy.problem(usage, now) };
  }
  if (test?.ok) {
    return { name, tone: "ok", label: "Connection ok", reason: test.detail };
  }
  if (usage?.report?.status === "available") {
    return { name, tone: "ok", label: "Connected", reason: null };
  }
  return { name, tone: "neutral", label: "Not tested yet", reason: null };
}
