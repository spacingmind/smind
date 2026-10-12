import { PlanUsageCompact } from "@/components/account-usage/plan-usage-compact";
import { InlineSpinner } from "@/components/ui/inline-spinner";
import type { ChatPlanUsage as ChatPlanUsageState } from "@/hooks/use-chat-plan-usage";
import { openUsageModal } from "@/hooks/use-usage-modal";
import { hasPlanData, planProviderName } from "@/lib/account-usage/chat-provider";

const NO_DATA = "No plan data yet";

/**
 * The Plan usage section of a chat's usage hover card (ADR-0028 PR-E), under
 * the token rows. Reads the shared store only (see useChatPlanUsage); the
 * link opens the Usage modal, where an explicit surface fetches under the
 * daemon's call budget. Renders nothing for a chat whose provider has no plan
 * windows, a daemon without `account.usage`, or a provider with no OAuth
 * account in a store that does hold accounts (API keys have no windows).
 */
export function ChatPlanUsage({ plan, onOpenUsage }: { plan: ChatPlanUsageState; onOpenUsage?: () => void }) {
  const { provider, snapshot, accounts, displayAs } = plan;
  if (provider === null) return null;

  const open = () => {
    onOpenUsage?.();
    openUsageModal();
  };

  let body;
  if (snapshot.phase === "error" && snapshot.reports.length === 0) {
    body = <p className="text-ui-sm text-muted-foreground">Couldn't load plan usage</p>;
  } else if (snapshot.phase !== "ready") {
    body = <InlineSpinner label="Loading plan usage…" />;
  } else if (snapshot.reports.length === 0 || (accounts.length > 0 && !accounts.some(hasPlanData))) {
    body = (
      <p data-testid="plan-usage-empty" className="text-ui-sm text-muted-foreground">
        {NO_DATA}
      </p>
    );
  } else if (accounts.length === 0) {
    // Accounts exist, none an OAuth login of this provider: nothing to show.
    return null;
  } else {
    body = (
      <>
        {accounts.length > 1 ? (
          <p data-testid="plan-usage-note" className="text-ui-sm leading-snug text-muted-foreground">
            This chat's login isn't tracked; showing all {planProviderName(provider)} accounts
          </p>
        ) : null}
        <div className="flex flex-col gap-3">
          {accounts.map((usage) => (
            <PlanUsageCompact key={usage.accountId} usage={usage} displayAs={displayAs} />
          ))}
        </div>
      </>
    );
  }

  return (
    <section data-testid="chat-plan-usage" aria-label="Plan usage" className="mt-3 flex flex-col gap-2 border-t pt-3">
      <div className="flex items-center justify-between gap-2">
        <p className="text-ui-sm font-medium text-foreground">Plan usage</p>
        <button
          type="button"
          data-testid="plan-usage-open"
          onClick={open}
          className="rounded-sm text-ui-sm text-muted-foreground underline-offset-4 transition-colors hover:text-foreground hover:underline focus-visible:outline-2 focus-visible:outline-ring"
        >
          Open Usage
        </button>
      </div>
      {body}
    </section>
  );
}
