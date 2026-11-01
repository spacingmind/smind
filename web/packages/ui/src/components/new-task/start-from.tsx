import type { Dispatch, ReactNode } from "react";

import { ForgePicker } from "@/components/composer/forge-picker";
import { BranchPicker, divergence } from "@/components/new-task/branch-picker";
import type { PickedForgeItem, StartFromAction, StartFromState, StartMode } from "@/components/new-task/start-from-state";
import { Button } from "@/components/ui/button";
import { SegmentedControl } from "@/components/ui/segmented-control";
import { useWorkspaceBranches } from "@/hooks/use-workspace-branches";
import { Icons } from "@/lib/icons";

const MODES: { value: StartMode; label: string; shortLabel: string }[] = [
  { value: "head", label: "Workspace HEAD", shortLabel: "HEAD" },
  { value: "branch", label: "Branch…", shortLabel: "Branch" },
  { value: "pr", label: "Pull request…", shortLabel: "PR" },
  { value: "issue", label: "Issue…", shortLabel: "Issue" },
];

export const FORK_BADGE_TEXT = "From fork — setup scripts blocked until you run them";

/** The fork warning chip: a PR from another repository runs no setup script until the user does (ADR-0039 §7). */
export function ForkBadge() {
  return (
    <span data-testid="fork-badge" className="mt-0.5 inline-flex items-center gap-1 text-ui-xs text-warning-text">
      <Icons.warning aria-hidden className="size-3 shrink-0" />
      {FORK_BADGE_TEXT}
    </span>
  );
}

function PickedRow({ testId, children, onClear }: { testId: string; children: ReactNode; onClear: () => void }) {
  return (
    <div data-testid={testId} className="flex items-start gap-2 rounded-lg border border-input-border px-2 py-1.5">
      <div className="flex min-w-0 flex-1 flex-col">{children}</div>
      <Button type="button" variant="ghost" size="xs" onClick={onClear}>
        Change
      </Button>
    </div>
  );
}

function ForgePicked({ kind, picked, onClear }: { kind: "pr" | "issue"; picked: PickedForgeItem; onClear: () => void }) {
  const Icon = kind === "pr" ? Icons.pullRequest : Icons.issue;
  return (
    <PickedRow testId={`start-from-${kind}`} onClear={onClear}>
      <span className="flex items-center gap-2 text-ui-sm text-foreground">
        <Icon aria-hidden className="size-4 shrink-0 text-foreground-muted" />
        <span className="truncate">
          #{picked.item.number} {picked.item.title}
        </span>
      </span>
      {kind === "pr" && picked.item.fromFork && <ForkBadge />}
    </PickedRow>
  );
}

/** The PR/issue step: the composer's `forge.search` list over the workspace, or the picked item with Change. */
function ForgeStep({
  kind,
  workspaceId,
  picked,
  onPick,
}: {
  kind: "pr" | "issue";
  workspaceId: number;
  picked: PickedForgeItem | undefined;
  onPick: (picked: PickedForgeItem | undefined) => void;
}) {
  if (picked) return <ForgePicked kind={kind} picked={picked} onClear={() => onPick(undefined)} />;
  return (
    <div className="rounded-lg border border-input-border">
      <ForgePicker
        kind={kind}
        scope={{ workspaceId }}
        limits={null}
        onPick={(item, repo) => onPick({ item, repo })}
        renderBadge={(item) => (item.fromFork ? <ForkBadge /> : null)}
      />
    </div>
  );
}

/**
 * The New Task dialog's "Start from" control (ADR-0039 §7): Workspace HEAD / Branch… / Pull request… / Issue…, with the
 * picker for the chosen mode underneath. The PR and issue pickers are the composer's ADR-0038 `forge.search` list,
 * scoped to the workspace; everything the user picked lives in the one `StartFromState` the parent owns.
 */
export function StartFrom({ workspaceId, state, dispatch }: { workspaceId: number; state: StartFromState; dispatch: Dispatch<StartFromAction> }) {
  // The default base for the helper line; the same first page the Branch… picker opens with.
  const base = useWorkspaceBranches({ workspaceId, enabled: state.mode === "head" || state.mode === "branch" });
  const defaultBase = base.data?.defaultBase;

  return (
    <div className="grid gap-2" data-testid="start-from">
      <span id="start-from-label" className="text-ui-base font-medium">
        Start from
      </span>
      <SegmentedControl label="Start from" options={MODES} value={state.mode} onChange={(mode) => dispatch({ type: "mode", mode })} className="w-fit" />

      {state.mode === "head" && (
        <p data-testid="start-from-helper" className="text-ui-sm text-foreground-muted">
          {defaultBase ? (
            <>
              Branch from <code className="font-mono text-foreground">{defaultBase}</code>
            </>
          ) : (
            "Branch from the workspace default"
          )}
        </p>
      )}

      {state.mode === "branch" &&
        (state.branch ? (
          <>
            <PickedRow testId="start-from-branch" onClear={() => dispatch({ type: "branch", branch: undefined })}>
              <span className="flex items-center gap-2 text-ui-sm text-foreground">
                <Icons.branch aria-hidden className="size-4 shrink-0 text-foreground-muted" />
                <span className="truncate font-mono">{state.branch.name}</span>
                {divergence(state.branch).map((t) => (
                  <span key={t} className="font-mono text-ui-xs text-foreground-muted">
                    {t}
                  </span>
                ))}
              </span>
            </PickedRow>
            <SegmentedControl
              label="Use the branch"
              options={[
                { value: "baseRef", label: "New branch from it" },
                { value: "checkout", label: "Check it out" },
              ]}
              value={state.branchUse}
              onChange={(use) => dispatch({ type: "branchUse", use })}
              className="w-fit"
            />
            <p className="text-ui-sm text-foreground-muted">
              {state.branchUse === "checkout" ? "Works in the branch itself; git refuses if it is checked out elsewhere." : "The task gets its own branch, starting at this one."}
            </p>
          </>
        ) : (
          <BranchPicker workspaceId={workspaceId} onPick={(branch) => dispatch({ type: "branch", branch })} />
        ))}

      {state.mode === "pr" && (
        <ForgeStep
          kind="pr"
          workspaceId={workspaceId}
          picked={state.pr}
          onPick={(pr) => dispatch({ type: "pr", pr })}
        />
      )}
      {state.mode === "issue" && (
        <ForgeStep
          kind="issue"
          workspaceId={workspaceId}
          picked={state.issue}
          onPick={(issue) => dispatch({ type: "issue", issue })}
        />
      )}
    </div>
  );
}
