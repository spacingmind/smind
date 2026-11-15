import { forwardRef, useCallback, useImperativeHandle, useRef, type KeyboardEvent } from "react";

import { ProviderIcon, useHasBrandMark } from "@/components/provider-icon";
import { StatusDot } from "@/components/ui/status-dot";
import { Icons } from "@/lib/icons";
import { cn } from "@/lib/utils";

/**
 * The model picker's provider rail (E-b): a vertical icon tab strip left of
 * the selected provider's models. Roving tabindex -- only the selected tab is
 * in the tab order -- with Arrow/End/Home moving selection (and focus) like
 * any tablist. A provider without a ready account stays selectable; its view
 * explains why, and the rail carries the reason in the accessible name, the
 * tooltip and a warning dot.
 */
/** The id the rail reports for its leading "Profiles" tab (the picker's cross-provider profiles view). */
export const PROFILES_TAB_ID = "@profiles";

export interface ProviderRailProps {
  providers: { id: string; label: string; unavailableReason?: string }[];
  /** Show a leading Profiles tab (every saved profile across providers, the old root's band); `selectedId` is PROFILES_TAB_ID when it is the view. */
  profilesTab?: boolean;
  selectedId: string;
  onSelect: (providerId: string) => void;
}

export interface ProviderRailHandle {
  focusSelected(): void;
}

/** The first letter or digit of a label, upper-cased ("GLM" -> "G", "my-acp" -> "M"); "" when it has none. */
export function providerInitial(label: string): string {
  const match = label.match(/[\p{L}\p{N}]/u);
  return match ? match[0].toUpperCase() : "";
}

/**
 * A provider with a brand mark keeps it; one without (GLM, Kimi, a custom ACP agent: they all share one generic glyph) gets its
 * initial in a small tile, so the rail's tabs can be told apart without hovering. The accessible name is on the tab, not here.
 */
function RailGlyph({ provider, label }: { provider: string; label: string }) {
  const branded = useHasBrandMark(provider);
  const initial = providerInitial(label);
  if (branded || initial === "") return <ProviderIcon provider={provider} />;
  return (
    <span
      aria-hidden
      data-testid={`model-rail-initial-${provider}`}
      className="flex size-4 items-center justify-center rounded bg-foreground-muted/20 text-ui-xs leading-none font-semibold"
    >
      {initial}
    </span>
  );
}

export const ProviderRail = forwardRef<ProviderRailHandle, ProviderRailProps>(function ProviderRail(
  { providers, profilesTab = false, selectedId, onSelect },
  ref,
) {
  const buttonRefs = useRef(new Map<string, HTMLButtonElement>());

  const focusSelected = useCallback(() => {
    buttonRefs.current.get(selectedId)?.focus();
  }, [selectedId]);
  useImperativeHandle(ref, () => ({ focusSelected }), [focusSelected]);

  const tabIds = profilesTab ? [PROFILES_TAB_ID, ...providers.map((p) => p.id)] : providers.map((p) => p.id);

  const onKeyDown = useCallback(
    (event: KeyboardEvent<HTMLButtonElement>, index: number) => {
      const count = tabIds.length;
      if (count === 0) return;
      let next: number;
      if (event.key === "ArrowDown") next = (index + 1) % count;
      else if (event.key === "ArrowUp") next = (index - 1 + count) % count;
      else if (event.key === "Home") next = 0;
      else if (event.key === "End") next = count - 1;
      else return;
      event.preventDefault();
      const nextId = tabIds[next];
      if (nextId === undefined || nextId === selectedId) return;
      onSelect(nextId);
      buttonRefs.current.get(nextId)?.focus();
    },
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [tabIds.join("|"), selectedId, onSelect],
  );

  return (
    <div
      role="tablist"
      aria-orientation="vertical"
      aria-label="Providers"
      data-testid="model-picker-rail"
      className="flex w-11 shrink-0 flex-col items-center gap-1 overflow-y-auto border-r border-border bg-surface py-2"
    >
      {profilesTab && (
        <button
          ref={(el) => {
            if (el) buttonRefs.current.set(PROFILES_TAB_ID, el);
            else buttonRefs.current.delete(PROFILES_TAB_ID);
          }}
          type="button"
          role="tab"
          aria-selected={selectedId === PROFILES_TAB_ID}
          tabIndex={selectedId === PROFILES_TAB_ID ? 0 : -1}
          data-testid="model-profiles-tab"
          aria-label="Profiles"
          title="Profiles"
          onClick={() => onSelect(PROFILES_TAB_ID)}
          onKeyDown={(event) => onKeyDown(event, 0)}
          className={cn(
            "relative flex size-8 items-center justify-center rounded-lg",
            selectedId === PROFILES_TAB_ID ? "bg-selected text-foreground" : "text-foreground-muted hover:bg-hover hover:text-foreground",
          )}
        >
          <Icons.agent aria-hidden className="size-4" />
        </button>
      )}
      {providers.map((provider, providerIndex) => {
        const index = providerIndex + (profilesTab ? 1 : 0);
        const selected = provider.id === selectedId;
        const reason = provider.unavailableReason;
        return (
          <button
            key={provider.id}
            ref={(el) => {
              if (el) buttonRefs.current.set(provider.id, el);
              else buttonRefs.current.delete(provider.id);
            }}
            type="button"
            role="tab"
            aria-selected={selected}
            tabIndex={selected ? 0 : -1}
            data-testid={`model-provider-${provider.id}`}
            data-unavailable={reason ? "true" : undefined}
            aria-label={reason ? `${provider.label}, unavailable: ${reason}` : provider.label}
            title={reason ? `${provider.label}: ${reason}` : provider.label}
            onClick={() => onSelect(provider.id)}
            onKeyDown={(event) => onKeyDown(event, index)}
            className={cn(
              "relative flex size-8 items-center justify-center rounded-lg",
              selected ? "bg-selected text-foreground" : "text-foreground-muted hover:bg-hover hover:text-foreground",
              reason && "opacity-60",
            )}
          >
            <RailGlyph provider={provider.id} label={provider.label} />
            {reason && (
              <StatusDot
                status="warning"
                aria-hidden
                data-testid={`model-provider-warning-${provider.id}`}
                className="absolute bottom-0.5 right-0.5 size-1.5"
              />
            )}
          </button>
        );
      })}
    </div>
  );
});
