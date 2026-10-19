import type { ProviderNavItem } from "@/lib/providers-view";
import type { AccountRowData } from "@/hooks/use-account-rows";
import type { ProviderInfo } from "@/lib/types";

/**
 * Deterministic plain-text diagnostic for bug reports. Built only from
 * cached data (catalog + account rows); never includes credentials,
 * tokens, or identities — see the plan's rate-limit rule.
 */
export function buildProviderDiagnostic(input: {
  provider: ProviderInfo | null;
  label: string;
  navKind: ProviderNavItem["kind"];
  rows: AccountRowData[];
  now?: number;
  generatedAt?: string;
}): string {
  const { provider, label, navKind, rows } = input;
  const lines: string[] = [
    "sMind provider diagnostic",
    `Generated: ${input.generatedAt ?? new Date(input.now ?? Date.now()).toISOString()}`,
    `Provider: ${label} (${provider?.id ?? "unknown"})`,
    `Kind: ${navKind}   Credentials: ${provider?.credentialKind ?? "none"}`,
    `Models: ${(provider?.models ?? []).length} (${provider?.modelsDiscovered ? "reported by the agent" : "built-in catalog"})`,
    `Accounts: ${rows.length}`,
  ];
  for (const row of rows) {
    const badge = row.badge;
    lines.push(
      `- ${row.account.label ?? `Account ${row.account.id}`} [${row.account.credentialType}] health=${badge.label} (${badge.kind})${badge.detail ? `; detail: ${badge.detail}` : ""}`,
    );
    lines.push(`  test: ${row.test ? (row.test.ok ? "ok" : "failed") : "not run"} ${row.test?.detail ?? ""}`.trimEnd());
    const report = row.usage?.report;
    lines.push(
      `  usage: ${report ? report.status : "none"}, plan=${report?.planLabel ?? "-"}, updated=${report?.fetchedAt ?? "-"}`,
    );
  }
  return `${lines.join("\n")}\n`;
}
