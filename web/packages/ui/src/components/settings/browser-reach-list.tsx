import type { BrowserProviderReach, BrowserReach } from "@/lib/browser-settings-types";

/**
 * Item 8 of `docs/plans/completed/browser-for-agents-settings-ui.md` (AC8): the
 * "Which agents get it" list. Pure renderer of the daemon's `providers` reach
 * table; the list is never hard-coded, so state is carried by the group titles
 * and the per-entry text, never by colour.
 */

const GROUPS: ReadonlyArray<{ reach: BrowserReach; title: string; testId: string }> = [
  { reach: "yes", title: "Browser tools offered", testId: "browser-reach-yes" },
  { reach: "conditional", title: "Offered, not guaranteed", testId: "browser-reach-conditional" },
  { reach: "no", title: "No browser for these agents", testId: "browser-reach-no" },
];

const SCREENSHOT_STATEMENTS: Record<BrowserProviderReach["images"], string> = {
  yes: "Screenshots: yes",
  no: "Screenshots: no, text tools only",
  unknown: "Screenshots: unknown, text tools only",
};

export function BrowserReachList({ providers }: { providers: BrowserProviderReach[] }) {
  if (providers.length === 0) {
    return (
      <p data-testid="browser-reach-empty" className="text-ui-sm text-foreground-muted">
        The daemon did not report which agents get the browser.
      </p>
    );
  }

  return (
    <div className="flex flex-col gap-3">
      {GROUPS.map(({ reach, title, testId }) => {
        const entries = providers.filter((provider) => provider.reach === reach);
        if (entries.length === 0) {
          return null;
        }
        return (
          <section key={reach} data-testid={testId}>
            <h4 className="text-ui-sm font-medium text-foreground">
              {title} ({entries.length})
            </h4>
            <ul className="mt-1.5 flex flex-col gap-2.5">
              {entries.map((provider) => (
                <li key={provider.id} className="flex flex-col">
                  <span className="min-w-0 break-words text-ui-base text-foreground">{provider.label}</span>
                  {provider.why ? (
                    <span className="min-w-0 break-words text-ui-sm text-foreground-muted first-letter:uppercase">{provider.why}</span>
                  ) : null}
                  {/* An agent without the browser gets no screenshots at all: saying so would be noise. */}
                  {provider.reach !== "no" && (
                    <span className="min-w-0 break-words text-ui-sm text-foreground-muted">
                      {SCREENSHOT_STATEMENTS[provider.images]}
                    </span>
                  )}
                </li>
              ))}
            </ul>
          </section>
        );
      })}
    </div>
  );
}
