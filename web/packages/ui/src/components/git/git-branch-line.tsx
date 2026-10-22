import { Icons } from "@/lib/icons";

/** `<branch> → <base ref>` (just the branch while the base is unknown), for the sidebar and header hover cards. */
export function GitBranchLine({ branch, base, testId }: { branch: string; base?: string; testId?: string }) {
  return (
    <div className="flex items-center gap-1.5 text-ui-sm text-muted-foreground">
      <Icons.branch className="size-3 shrink-0" />
      {/* The branch name truncates; the base stays visible: "→ main" is the part that matters. */}
      <span data-testid={testId} className="flex min-w-0 gap-1" title={base ? `${branch} → ${base}` : branch}>
        <span className="min-w-0 truncate">{branch}</span>
        {base && <span className="shrink-0">{` → ${base}`}</span>}
      </span>
    </div>
  );
}
