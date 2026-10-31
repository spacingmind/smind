import type { MouseEvent, ReactNode } from "react";

import { Icons } from "@/lib/icons";
import { OwnerBadge } from "@/components/plugins/owner-badge";

import { ProfileIcon, ProviderIcon } from "@/components/provider-icon";
import { Button } from "@/components/ui/button";
import { StatusBadge } from "@/components/ui/status-badge";
import { gateActionLabel, gateBadge, gateDetail, gateReasonText, isHardGate } from "@/lib/provider-gate";
import { getProviderModelCount, type BrowserProvider, type ModelRow } from "@/lib/model-browser";
import type { ProviderGate } from "@/lib/types";
import { cn } from "@/lib/utils";

// Row chrome for the combined picker, ported from Paseo's model-browser rows:
// 16px glyph · label · inline description · 16px check slot.

export interface PickerProfileRow {
  id: string;
  name: string;
  summary: string;
  provider: string;
  /** "" = the provider's default model. */
  model: string;
  notes?: string;
  /** The plugin id that contributed this agent (ADR-0042); shown as a "from <plugin>" badge. */
  owner?: string | null;
}

export interface RowShellProps {
  id: string;
  active: boolean;
  disabled: boolean;
  onActivate: () => void;
  onHover: () => void;
  /** The row's live gate (hard or advisory); a hard one blocks selection but keeps the row in the keyboard walk. */
  gate?: ProviderGate;
  /** Runs the gate's action (Enter on a blocked row, or its control); undefined hides the control. */
  onGateAction?: () => void;
  /** Test id suffix of the gate slot (`model-gate-<suffix>`). */
  gateKey?: string;
  /** The picker's clock, so every row of one render agrees on "until". */
  now?: number;
}

/** Keeps focus in the search input so tapping a row cannot blur it (and re-render) before the click lands. */
function keepInputFocus(e: { preventDefault: () => void }) {
  e.preventDefault();
}

/** Shared option chrome: id for aria-activedescendant, active highlight, hover-to-activate. */
export function RowShell({
  id,
  active,
  disabled,
  onActivate,
  onHover,
  gate,
  testId,
  selected,
  className,
  children,
}: RowShellProps & { testId: string; selected?: boolean; className?: string; children: ReactNode }) {
  // A hard gate blocks selection like `disabled`, but the row stays hoverable and in the arrow walk so its action is reachable.
  const blocked = isHardGate(gate);
  return (
    <div
      id={id}
      role="option"
      aria-selected={selected ?? false}
      aria-disabled={disabled || blocked || undefined}
      aria-describedby={gate ? `${id}-gate` : undefined}
      data-testid={testId}
      data-active={active ? "true" : undefined}
      data-gate={gate?.reason}
      onClick={disabled || blocked ? undefined : onActivate}
      onMouseDown={keepInputFocus}
      onPointerDown={keepInputFocus}
      onMouseMove={disabled || active ? undefined : onHover}
      className={cn(
        "group/row flex min-h-9 cursor-pointer items-center gap-2 rounded-md px-2 py-1 max-md:min-h-11",
        active && "bg-hover",
        disabled && "cursor-not-allowed opacity-50",
        blocked && !disabled && "cursor-default",
        className,
      )}
    >
      {children}
    </div>
  );
}

/**
 * The gate's reason (a Badge plus one line) and its one control, in the row's detail slot. The text carries the
 * full sentence in `title`; the control is a tab stop only while its row is the active one.
 */
export function GateSlot({
  gate,
  rowId,
  gateKey,
  active,
  now,
  onAction,
}: {
  gate: ProviderGate;
  rowId: string;
  gateKey: string;
  active: boolean;
  now: number;
  onAction?: () => void;
}) {
  const badge = gateBadge(gate);
  const text = gateReasonText(gate, now);
  const detail = gateDetail(gate, now);
  const actionLabel = onAction ? gateActionLabel(gate) : undefined;
  return (
    <span className="flex min-w-0 flex-1 items-center justify-end gap-1.5">
      <span id={`${rowId}-gate`} data-testid={`model-gate-${gateKey}`} title={text} className="flex min-w-0 items-center gap-1.5">
        <StatusBadge status={badge.status} className="shrink-0">
          {badge.label}
        </StatusBadge>
        {detail && <span className="min-w-0 truncate text-ui-sm text-foreground-muted">{detail}</span>}
      </span>
      {actionLabel && (
        <button
          type="button"
          tabIndex={active ? 0 : -1}
          data-testid={`model-gate-action-${gateKey}`}
          aria-label={`${actionLabel}: ${gate.action?.label ?? text}`}
          title={gate.action?.label}
          onMouseDown={keepInputFocus}
          onPointerDown={keepInputFocus}
          onClick={(e) => {
            e.stopPropagation();
            onAction?.();
          }}
          className="h-6 shrink-0 rounded px-2 text-ui-sm font-medium text-foreground hover:bg-muted max-md:h-10 max-md:px-3"
        >
          {actionLabel}
        </button>
      )}
    </span>
  );
}

function CheckSlot({ show }: { show: boolean }) {
  return (
    <span className="flex size-4 shrink-0 items-center justify-center">
      {show && <Icons.check aria-hidden className="size-4 text-foreground" />}
    </span>
  );
}

export function SectionHeading({ label }: { label: string }) {
  return (
    <div role="presentation" className="px-2 pt-2 pb-1 text-ui-sm font-medium text-foreground-muted">
      {label}
    </div>
  );
}

export function ProfilesHeading({ onEdit }: { onEdit: () => void }) {
  return (
    <div role="presentation" className="flex items-center justify-between pr-1 pl-2">
      <span className="py-1.5 text-ui-sm font-medium text-foreground-muted">Profiles</span>
      <Button
        type="button"
        variant="quiet"
        size="icon-sm"
        aria-label="Edit profiles"
        title="Edit profiles"
        data-testid="model-profiles-edit"
        onMouseDown={keepInputFocus}
        onClick={onEdit}
      >
        <Icons.edit aria-hidden className="size-3.5" />
      </Button>
    </div>
  );
}

export function NoAgentRow(shell: RowShellProps) {
  return (
    <RowShell {...shell} testId="model-no-agent">
      <span className="size-4 shrink-0" />
      <span className="text-ui-base text-foreground">No agent</span>
    </RowShell>
  );
}

export function CreateProfileRow(shell: RowShellProps) {
  return (
    <RowShell {...shell} testId="model-profiles-empty">
      <Icons.add aria-hidden className="size-4 shrink-0 text-foreground-muted" />
      <span className="text-ui-base text-foreground-muted">Create profile</span>
    </RowShell>
  );
}

export function ProfileRow({
  profile,
  checked,
  custom,
  ...shell
}: RowShellProps & { profile: PickerProfileRow; checked: boolean; custom: boolean }) {
  return (
    <RowShell {...shell} testId={`model-profile-row-${profile.id}`} selected={checked}>
      <ProfileIcon className="shrink-0 text-foreground-muted" />
      <span className="max-w-[50%] shrink-0 truncate text-ui-base text-foreground">{profile.name}</span>
      <OwnerBadge owner={profile.owner} link={false} testId={`model-profile-owner-${profile.id}`} />
      {checked && custom && (
        <span className="shrink-0 rounded bg-muted px-1 text-ui-xs text-foreground-muted">custom</span>
      )}
      {shell.gate ? (
        <GateSlot gate={shell.gate} rowId={shell.id} gateKey={shell.gateKey ?? `profile-${profile.id}`} active={shell.active} now={shell.now ?? Date.now()} onAction={shell.onGateAction} />
      ) : (
        <span className="min-w-0 flex-1 truncate text-ui-sm text-foreground-muted">{profile.summary}</span>
      )}
      <CheckSlot show={checked} />
    </RowShell>
  );
}

export function ProviderRow({
  provider,
  separator,
  ...shell
}: RowShellProps & { provider: BrowserProvider; separator: boolean }) {
  const selection = provider.modelSelection;
  let detail: ReactNode;
  if (shell.gate && !provider.unavailableReason) {
    detail = null;
  } else if (provider.unavailableReason) {
    detail = provider.unavailableReason;
  } else if (selection.kind === "loading") {
    detail = (
      <>
        <Icons.spinner aria-hidden className="size-3.5 animate-spin" />
        Loading…
      </>
    );
  } else if (selection.kind === "error") {
    detail = (
      <>
        <Icons.error aria-hidden className="size-3.5" />
        Error
      </>
    );
  } else {
    const count = getProviderModelCount(provider);
    detail = count === 0 ? "Default model" : count === 1 ? "1 model" : `${count} models`;
  }
  return (
    <RowShell {...shell} testId={`model-provider-${provider.id}`} className={cn(separator && "border-t border-border rounded-t-none")}>
      <ProviderIcon provider={provider.id} className={cn("shrink-0 text-foreground-muted", isHardGate(shell.gate) && "opacity-60")} />
      <span className={cn("min-w-0 shrink-0 truncate text-ui-base text-foreground", isHardGate(shell.gate) && "opacity-60")}>{provider.label}</span>
      {shell.gate && !provider.unavailableReason ? (
        <GateSlot gate={shell.gate} rowId={shell.id} gateKey={shell.gateKey ?? provider.id} active={shell.active} now={shell.now ?? Date.now()} onAction={shell.onGateAction} />
      ) : (
        <span className="flex min-w-0 flex-1 items-center justify-end gap-1 truncate text-ui-sm text-foreground-muted">
          {detail}
        </span>
      )}
      {!shell.disabled && !isHardGate(shell.gate) && <Icons.chevronRight aria-hidden className="size-4 shrink-0 text-foreground-subtle" />}
    </RowShell>
  );
}

export type ModelRowAction =
  | { kind: "create" }
  | { kind: "edit"; profile: PickerProfileRow }
  | { kind: "list"; count: number };

/** Paseo's ModelRowProfileAction: create from this model, edit its one profile, or list several. */
function ModelRowProfileButton({ row, action, onAct }: { row: ModelRow; action: ModelRowAction; onAct: () => void }) {
  const suffix = `${row.provider}-${row.modelId}`;
  let testId: string;
  let label: string;
  let icon: ReactNode;
  switch (action.kind) {
    case "create":
      testId = `model-create-profile-${suffix}`;
      label = "Create profile from this model";
      icon = <Icons.add aria-hidden className="size-4" />;
      break;
    case "edit":
      testId = `model-edit-profile-${suffix}`;
      label = `Edit profile ${action.profile.name}`;
      icon = <ProfileIcon />;
      break;
    case "list":
      testId = `model-edit-profiles-${suffix}`;
      label = `Edit profiles (${action.count})`;
      icon = <ProfileIcon />;
      break;
  }
  const stop = (e: MouseEvent) => {
    e.stopPropagation();
    onAct();
  };
  return (
    <button
      type="button"
      tabIndex={-1}
      aria-label={label}
      title={label}
      data-testid={testId}
      onMouseDown={keepInputFocus}
      onPointerDown={keepInputFocus}
      onClick={stop}
      className={cn(
        "flex size-6 shrink-0 items-center justify-center rounded text-foreground-muted hover:bg-muted hover:text-foreground",
        "md:pointer-fine:opacity-0 md:pointer-fine:group-hover/row:opacity-100 md:pointer-fine:group-data-[active=true]/row:opacity-100 md:pointer-fine:group-focus-within/row:opacity-100",
      )}
    >
      {icon}
    </button>
  );
}

export function ModelRowItem({
  row,
  qualified,
  selected,
  action,
  onAction,
  ...shell
}: RowShellProps & {
  row: ModelRow;
  qualified: boolean;
  selected: boolean;
  /** Undefined for the synthetic default row and when no handler is wired. */
  action?: ModelRowAction;
  onAction: () => void;
}) {
  // ADR-0025: the "Default model" row (id "") sends no model; it has no description of its own.
  const description = qualified
    ? row.description
      ? `${row.providerLabel} · ${row.description}`
      : row.providerLabel
    : row.description;
  return (
    <RowShell {...shell} testId={`model-row-${row.key}`} selected={selected}>
      <ProviderIcon provider={row.provider} className={cn("shrink-0 text-foreground-muted", isHardGate(shell.gate) && "opacity-60")} />
      <span className={cn("max-w-[55%] shrink-0 truncate text-ui-base text-foreground", isHardGate(shell.gate) && "opacity-60")}>{row.modelLabel}</span>
      {shell.gate ? (
        <GateSlot gate={shell.gate} rowId={shell.id} gateKey={shell.gateKey ?? row.key} active={shell.active} now={shell.now ?? Date.now()} onAction={shell.onGateAction} />
      ) : (
        <span className="min-w-0 flex-1 truncate text-ui-sm text-foreground-muted">{description}</span>
      )}
      {action && !isHardGate(shell.gate) && <ModelRowProfileButton row={row} action={action} onAct={onAction} />}
      <CheckSlot show={selected} />
    </RowShell>
  );
}

export function EmptyState({ query }: { query: string }) {
  return (
    <div data-testid="model-search-empty" className="px-3 py-6 text-center text-ui-sm text-foreground-muted">
      No models match “{query}”
    </div>
  );
}

export function UnavailableNotice({ reason }: { reason: string }) {
  return (
    <p data-testid="model-provider-unavailable" className="px-3 pb-2 text-ui-sm text-foreground-muted">
      {reason}
    </p>
  );
}

export function ProviderNotice({ kind, message }: { kind: "loading" | "error"; message?: string }) {
  return (
    <div
      data-testid={`model-provider-${kind}`}
      className="flex items-center justify-center gap-2 px-3 py-6 text-center text-ui-sm text-foreground-muted"
    >
      {kind === "loading" ? (
        <>
          <Icons.spinner aria-hidden className="size-4 animate-spin" />
          Loading…
        </>
      ) : (
        <>
          <Icons.error aria-hidden className="size-4 shrink-0" />
          {message}
        </>
      )}
    </div>
  );
}

export function PickerHeader({
  title,
  providerId,
  onBack,
  onProviderSettings,
  inSheet,
}: {
  title: string;
  /** Set in a provider view: shows the provider glyph (and the gear when `onProviderSettings` is wired). */
  providerId?: string;
  onBack?: () => void;
  onProviderSettings?: () => void;
  inSheet: boolean;
}) {
  return (
    <div className={cn("flex h-10 shrink-0 items-center gap-1 px-2", inSheet && "pr-12")}>
      {onBack && (
        <Button
          type="button"
          variant="quiet"
          size="icon-sm"
          aria-label="Back"
          data-testid="model-picker-back"
          onMouseDown={keepInputFocus}
          onPointerDown={keepInputFocus}
          onClick={onBack}
        >
          <Icons.back aria-hidden />
        </Button>
      )}
      {providerId && <ProviderIcon provider={providerId} className={cn("shrink-0 text-foreground", !onBack && "ml-1")} />}
      <span className={cn("min-w-0 flex-1 truncate text-ui-sm font-medium text-foreground", !onBack && !providerId && "px-1")}>
        {title}
      </span>
      {providerId && onProviderSettings && (
        <Button
          type="button"
          variant="quiet"
          size="icon-sm"
          aria-label={`Open ${title} settings`}
          title={`${title} settings`}
          data-testid="model-picker-provider-settings"
          onMouseDown={keepInputFocus}
          onClick={onProviderSettings}
        >
          <Icons.settings aria-hidden className="size-4" />
        </Button>
      )}
    </div>
  );
}
