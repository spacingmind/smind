import { Icons } from "@/lib/icons";

/** `<branch> → <base ref>` (just the branch while the base is unknown), for the sidebar and header hover cards. `gone` marks a base that resolves nowhere; `stackedOn` is the parent task's branch when the base is it. */
export function GitBranchLine({ branch, base, gone, stackedOn, testId }: { branch: string; base?: string; gone?: boolean; stackedOn?: string; testId?: string }) {
  const baseText = base && gone ? `${base} (gone)` : base;
  const title = [baseText ? `${branch} → ${baseText}` : branch, stackedOn && `stacked on ${stackedOn}`].filter(Boolean).join(" · ");
  return (
    <div className="flex items-center gap-1.5 text-ui-sm text-muted-foreground">
      <Icons.branch className="size-3 shrink-0" />
      {/* The branch name truncates; the base stays visible: "→ main" is the part that matters. */}
      <span data-testid={testId} className="flex min-w-0 gap-1" title={title}>
        <span className="min-w-0 truncate">{branch}</span>
        {baseText && <span className="shrink-0">{` → ${baseText}`}</span>}
      </span>
    </div>
  );
}
