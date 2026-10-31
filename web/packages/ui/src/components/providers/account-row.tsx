import type { JSX } from "react";

import { Icons } from "@/lib/icons";
import type { UsageDisplayAs } from "@/lib/account-usage/format";
import type { UsageRefresh } from "@/lib/account-usage/model";
import type { Account } from "@/lib/types";
import type { AccountRowData } from "@/hooks/use-account-rows";
import type { HealthBadge } from "@/lib/providers-view";
import { AccountUsageLines } from "@/components/providers/account-usage-lines";
import {
  RemoveAccountDialog,
  RenameForm,
  TestButton,
  type RowAction,
  UpdateCredentialForm,
  authTypeLabel,
} from "@/components/providers/account-actions";
import { UsageRefreshButton } from "@/components/account-usage/usage-refresh-button";
import { Button } from "@/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { StatusBadge } from "@/components/ui/status-badge";
import { StatusDot } from "@/components/ui/status-dot";

// The dot is the health badge's tone, so dot, badge, nav and summary share one fold.
const DOT_STATUS: Record<HealthBadge["tone"], "success" | "danger" | "warning" | "neutral"> = {
  ok: "success",
  danger: "danger",
  warning: "warning",
  neutral: "neutral",
};

const DOT_STATE: Record<HealthBadge["tone"], string> = {
  ok: "ok",
  danger: "failed",
  warning: "warning",
  neutral: "unknown",
};

function healthBadge(badge: HealthBadge, id: number): JSX.Element {
  if (badge.tone === "neutral") {
    return (
      <span
        data-testid={`provider-row-health-${id}`}
        className="rounded-full border px-2 py-0.5 text-ui-xs text-muted-foreground"
      >
        {badge.label}
      </span>
    );
  }
  return (
    <StatusBadge
      status={badge.tone === "ok" ? "success" : badge.tone === "warning" ? "warning" : "danger"}
      data-testid={`provider-row-health-${id}`}
    >
      {badge.label}
    </StatusBadge>
  );
}

export function AccountRow(props: {
  data: AccountRowData;
  mostHeadroom: boolean;
  displayAs: UsageDisplayAs;
  testing: boolean;
  onTest: (providerKey: string) => void;
  refresh: { state: UsageRefresh; pressedAt?: number; onRefresh: () => void };
  action: RowAction | null;
  onAction: (a: RowAction | null) => void;
  onRelogin: (account: Account) => void;
}): JSX.Element {
  const { data, mostHeadroom, displayAs, testing, onTest, refresh, action, onAction, onRelogin } = props;
  const { account, usage, status, badge } = data;
  const id = account.id;
  const informational = usage?.report?.status === "unavailable" && usage.report.problem?.kind === "no_quota";
  const rawDetail = badge.detail ?? status.reason;
  const detail = rawDetail && data.test && rawDetail === data.test.detail ? `Provider test: ${rawDetail}` : rawDetail;
  const detailTone = badge.tone === "warning" ? "text-warning-text" : badge.tone === "danger" ? "text-destructive" : "";

  return (
    <li
      data-testid={`provider-row-${id}`}
      className="flex flex-col gap-2 rounded-lg border bg-card px-3 py-2.5"
    >
      {action?.kind === "rename" ? (
        <RenameForm account={account} onSaved={() => onAction(null)} onCancel={() => onAction(null)} />
      ) : (
        <div className="flex flex-wrap items-center gap-x-2 gap-y-1">
          <StatusDot
            status={DOT_STATUS[badge.tone]}
            className="size-2"
            role="img"
            aria-label={badge.label}
            title={badge.label}
            data-testid={`provider-row-dot-${id}`}
            data-status={DOT_STATE[badge.tone]}
          />
          <span className="min-w-0 truncate text-ui-base font-medium text-foreground">{status.name}</span>
          <StatusBadge status="neutral">
            {authTypeLabel(account.credentialType)}
          </StatusBadge>
          {usage?.report?.planLabel ? (
            <StatusBadge status="neutral">
              {usage.report.planLabel}
            </StatusBadge>
          ) : null}
          {mostHeadroom ? (
            <StatusBadge status="success" data-testid="provider-row-headroom">
              Most headroom
            </StatusBadge>
          ) : null}
          <span className="flex-1" />
          {healthBadge(badge, id)}
          <span className="flex shrink-0 items-center gap-1.5">
          {badge.kind === "relogin" && account.credentialType === "oauth" ? (
            <Button
              variant="outline"
              size="xs"
              data-testid={`provider-row-relogin-${id}`}
              onClick={() => onRelogin(account)}
            >
              Re-login
            </Button>
          ) : null}
          <TestButton providerId={account.provider} testing={testing} onTest={onTest} />
          {usage && !informational ? (
            <UsageRefreshButton
              usage={usage}
              state={refresh.state}
              pressedAt={refresh.pressedAt}
              onRefresh={refresh.onRefresh}
            />
          ) : null}
          <DropdownMenu>
            <DropdownMenuTrigger asChild>
              <Button
                variant="ghost"
                size="icon-sm"
                aria-label={`Account actions for ${status.name}`}
                data-testid={`provider-row-menu-${id}`}
              >
                <Icons.more />
              </Button>
            </DropdownMenuTrigger>
            <DropdownMenuContent align="end">
              <DropdownMenuItem
                data-testid={`provider-row-rename-${id}`}
                onSelect={() => onAction({ kind: "rename", account })}
              >
                Rename
              </DropdownMenuItem>
              <DropdownMenuItem
                data-testid={`provider-row-update-credential-${id}`}
                onSelect={() => onAction({ kind: "credential", account })}
              >
                Update credential
              </DropdownMenuItem>
              <DropdownMenuItem
                className="text-destructive focus:bg-menu-hover"
                data-testid={`provider-row-remove-${id}`}
                onSelect={() => onAction({ kind: "remove", account })}
              >
                Remove
              </DropdownMenuItem>
            </DropdownMenuContent>
          </DropdownMenu>
          </span>
        </div>
      )}
      {action?.kind === "credential" ? (
        <UpdateCredentialForm account={account} onSaved={() => onAction(null)} onCancel={() => onAction(null)} />
      ) : null}
      {usage ? <AccountUsageLines usage={usage} displayAs={displayAs} /> : null}
      {detail ? (
        <p className={`text-ui-sm ${detailTone}`} data-testid={`provider-row-test-result-${id}`}>
          {detail}
        </p>
      ) : null}
    </li>
  );
}

export { RemoveAccountDialog };
