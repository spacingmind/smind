import type { JSX } from "react";

import { providerConsumer } from "@/lib/providers-groups";
import { Icons } from "@/lib/icons";
import type { ProviderNavItem, NavTone } from "@/lib/providers-view";
import { ProviderIcon } from "@/components/provider-icon";
import { StatusDot } from "@/components/ui/status-dot";

const TONE_STATUS: Record<NavTone, "success" | "warning" | "danger" | "neutral"> = {
  ok: "success",
  warning: "warning",
  danger: "danger",
  neutral: "neutral",
};

function toneLabel(item: ProviderNavItem): string {
  switch (item.tone) {
    case "ok":
      return "All accounts healthy";
    case "warning":
      return "Needs attention";
    case "danger":
      return "Problem";
    default:
      if (item.connected) return "Not tested yet";
      return item.accountProvider && providerConsumer(item.accountProvider) === "proxy" ? "No proxy account" : "Not connected";
  }
}

function accessibleName(item: ProviderNavItem): string {
  const count = item.accountIds.length;
  return `${item.label}${count > 0 ? `, ${count} accounts` : ""}, ${toneLabel(item)}`;
}

export function ProviderNav(props: {
  items: ProviderNavItem[];
  selectedId: string | null;
  onSelect: (id: string) => void;
  mode: "full" | "rail";
}): JSX.Element {
  const { items, selectedId, onSelect, mode } = props;
  return (
    <nav aria-label="Providers" data-testid="provider-nav" data-mode={mode}>
      <ul className="flex flex-col gap-0.5">
        {items.map((item) => {
          const selected = item.id === selectedId;
          const selectedClasses = selected ? "bg-selected text-foreground" : "";
          if (mode === "rail") {
            return (
              <li key={item.id}>
                <button
                  type="button"
                  data-testid={`provider-nav-item-${item.id}`}
                  aria-current={selected ? "true" : undefined}
                  title={item.label}
                  aria-label={accessibleName(item)}
                  onClick={() => onSelect(item.id)}
                  className={`relative size-10 grid place-items-center rounded-lg hover:bg-hover focus-visible:ring-2 focus-visible:ring-ring/50 outline-none ${selectedClasses}`}
                >
                  {item.id === "other" ? <Icons.credentials className="size-4" /> : <ProviderIcon provider={item.id} size={16} />}
                  <StatusDot
                    status={TONE_STATUS[item.tone]}
                    title={toneLabel(item)}
                    aria-hidden
                    className="absolute right-1 top-1 size-2"
                  />
                </button>
              </li>
            );
          }
          return (
            <li key={item.id}>
              <button
                type="button"
                data-testid={`provider-nav-item-${item.id}`}
                aria-current={selected ? "true" : undefined}
                aria-label={accessibleName(item)}
                onClick={() => onSelect(item.id)}
                className={`h-8 flex w-full items-center gap-2 rounded-lg px-2 hover:bg-hover focus-visible:ring-2 focus-visible:ring-ring/50 outline-none ${selectedClasses}`}
              >
                {item.id === "other" ? (
                  <Icons.credentials className="size-4 shrink-0" />
                ) : (
                  <ProviderIcon provider={item.id} size={16} />
                )}
                <span className="truncate text-ui-base">{item.label}</span>
                {item.recipe ? (
                  <span className="text-ui-xs text-muted-foreground" data-testid={`provider-nav-recipe-${item.id}`}>
                    recipe
                  </span>
                ) : null}
                {item.unverified ? (
                  <span className="text-ui-xs text-warning-text" data-testid={`provider-nav-unverified-${item.id}`}>
                    unverified
                  </span>
                ) : null}
                {item.accountIds.length > 0 && (
                  <span className="text-ui-xs text-muted-foreground">{item.accountIds.length}</span>
                )}
                <StatusDot
                  status={TONE_STATUS[item.tone]}
                  title={toneLabel(item)}
                  aria-hidden
                  className="size-2"
                />
              </button>
            </li>
          );
        })}
      </ul>
    </nav>
  );
}
