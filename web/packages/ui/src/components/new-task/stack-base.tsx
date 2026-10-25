import { SegmentedControl } from "@/components/ui/segmented-control";
import { useWorkspaceBranches } from "@/hooks/use-workspace-branches";
import type { Task } from "@/lib/types";

export type StackBase = "parent" | "default";

/** The parent's branch, or "" when it has none yet. */
export function parentBranchOf(parent: Task): string {
  return parent.Branch ?? "";
}

/** The New sub-task dialog's "Base" control: stack on the parent's branch or branch from the default base. */
export function StackBaseControl({
  workspaceId,
  parent,
  value,
  onChange,
}: {
  workspaceId: number;
  parent: Task;
  value: StackBase;
  onChange: (value: StackBase) => void;
}) {
  const { defaultBase } = useStackDefaultBase(workspaceId);
  const parentBranch = parentBranchOf(parent);
  const base = defaultBase.name || "the workspace default";

  const options: { value: StackBase; label: string }[] = [];
  if (parentBranch) options.push({ value: "parent", label: `Parent branch (${parentBranch})` });
  options.push({ value: "default", label: `Default base (${base})` });

  const selected: StackBase = parentBranch ? value : "default";
  const helper = !parentBranch
    ? `Parent has no branch yet; branching from ${base}.`
    : selected === "parent"
      ? "Starts at the parent's last commit; uncommitted work in the parent is not included."
      : `Starts at ${base}, not stacked.`;

  return (
    <div className="grid gap-2" data-testid="stack-base">
      <span className="text-ui-base font-medium">Base</span>
      <SegmentedControl label="Base" options={options} value={selected} onChange={onChange} className="w-fit" instant />
      {/* Fixed height: every state is one or two lines, so switching options does not move the dialog. */}
      <p data-testid="stack-base-helper" className="min-h-10 text-ui-sm text-foreground-muted">
        {helper}
      </p>
    </div>
  );
}

/** The default base's `baseRef` (`origin/<name>` when only the remote has it, as `startFromParams` maps a picked branch) and display name. */
export function useStackDefaultBase(workspaceId: number): { defaultBase: { name: string; ref: string } } {
  const data = useWorkspaceBranches({ workspaceId }).data;
  const name = data?.defaultBase ?? "";
  const originOnly = !!data?.branches.some((b) => b.name === name && b.remote === "origin") && !data?.branches.some((b) => b.name === name && b.remote === "local");
  return { defaultBase: { name, ref: originOnly ? `origin/${name}` : name } };
}
