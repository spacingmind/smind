import { Icons } from "@/lib/icons";

/** `<branch> → <base ref>` (just the branch while the base is unknown), for the sidebar and header hover cards. */
export function GitBranchLine({ branch, base, testId }: { branch: string; base?: string; testId?: string }) {
  return (
    <div className="flex items-center gap-1.5 text-ui-sm text-muted-foreground">
      <Icons.branch className="size-3 shrink-0" />
      <span data-testid={testId} className="min-w-0 truncate">
        {base ? `${branch} → ${base}` : branch}
      </span>
    </div>
  );
}
