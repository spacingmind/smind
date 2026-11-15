import { useId, useState } from "react";

import { COMPACT_TOUCH_BUTTON_CLASS } from "@/components/permission/permission-option-button";
import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import { StatusBadge } from "@/components/ui/status-badge";
import { GRANT_BUTTON_LABEL, GRANT_NEEDS_OPT_IN_HINT, GRANT_OPT_IN_FALLBACK, grantClassLabel, grantNeedsOptIn } from "@/lib/grant-copy";
import type { GrantOffer } from "@/lib/types";

/**
 * The scoped "always allow in this project" control of a permission card (saved grants,
 * docs/plans/active/saved-grants.md): it shows the EXACT scope before the human chooses it
 * (this command, this project and folder, how long, which class) and saves nothing wider.
 *
 * A command that runs project-defined code (a "runner": make, npm run, go test, ...) needs an
 * extra opt-in, unticked by default, that says what it means; the save control is disabled until
 * it is ticked. The daemon enforces the same rule, so this is the human's view of it, not the
 * guard. Every agent- or daemon-authored string renders as plain text (whitespace-pre-wrap).
 */
export function GrantControl({
  offer,
  disabled,
  onSave,
}: {
  offer: GrantOffer;
  disabled: boolean;
  onSave: (allowProjectCode: boolean) => void | Promise<void>;
}) {
  const needsOptIn = grantNeedsOptIn(offer);
  const [optIn, setOptIn] = useState(false);
  const optInId = useId();
  const blocked = needsOptIn && !optIn;
  return (
    <div data-testid="grant-control" data-grant-class={offer.class} className="flex w-full min-w-0 flex-col gap-2 border-t border-border pt-2">
      <div className="flex flex-wrap items-center gap-2">
        <span className="text-ui-base font-medium text-foreground">{GRANT_BUTTON_LABEL}</span>
        <StatusBadge status={needsOptIn ? "warning" : "neutral"} data-testid="grant-class">
          {grantClassLabel(offer.class)}
        </StatusBadge>
      </div>
      <dl className="grid grid-cols-[auto_1fr] gap-x-3 gap-y-1 text-ui-base">
        <dt className="text-foreground-muted">Command</dt>
        <dd data-testid="grant-command" className="min-w-0 font-mono whitespace-pre-wrap break-all text-foreground">
          {offer.command}
        </dd>
        <dt className="text-foreground-muted">Project</dt>
        <dd data-testid="grant-project" className="min-w-0 whitespace-pre-wrap break-all text-foreground">
          {offer.project}
          {offer.folder ? <span className="text-foreground-muted">{` (${offer.folder})`}</span> : null}
        </dd>
        <dt className="text-foreground-muted">Lasts</dt>
        <dd data-testid="grant-expiry" className="text-foreground">{`${offer.expiresInDays} days, or until you revoke it`}</dd>
      </dl>
      <p data-testid="grant-note" className="text-ui-base text-foreground-muted whitespace-pre-wrap break-words">
        {offer.note}
      </p>
      {needsOptIn && (
        <label htmlFor={optInId} className="flex items-start gap-2 text-ui-base text-foreground">
          <Checkbox id={optInId} data-testid="grant-optin" checked={optIn} disabled={disabled} onCheckedChange={(v) => setOptIn(v === true)} className="mt-0.5 border-foreground-muted" />
          <span className="whitespace-pre-wrap break-words">{offer.optInLabel || GRANT_OPT_IN_FALLBACK}</span>
        </label>
      )}
      <div className="flex flex-wrap items-center gap-2">
        <Button type="button" variant="outline" size="sm" className={COMPACT_TOUCH_BUTTON_CLASS} data-testid="grant-save" disabled={disabled || blocked} onClick={() => void onSave(needsOptIn && optIn)}>
          {GRANT_BUTTON_LABEL}
        </Button>
        {blocked && !disabled && (
          <span data-testid="grant-hint" className="text-ui-sm text-foreground-muted">
            {GRANT_NEEDS_OPT_IN_HINT}
          </span>
        )}
      </div>
    </div>
  );
}
