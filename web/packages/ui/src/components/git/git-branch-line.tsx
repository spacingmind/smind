import { MiddleText } from "@/components/ui/middle-text";
import { Icons } from "@/lib/icons";

/**
 * `<branch> → Base: <base ref>` (just the branch while the base is unknown), for the sidebar and header hover cards. Both
 * names truncate in the middle so the distinguishing tail stays visible; `gone` marks a base that resolves nowhere;
 * `stackedOn` is the parent task's branch when the base is it.
 */
export function GitBranchLine({ branch, base, gone, stackedOn, testId }: { branch: string; base?: string; gone?: boolean; stackedOn?: string; testId?: string }) {
  const baseText = base && gone ? `${base} (gone)` : base;
  const title = [baseText ? `${branch} → ${baseText}` : branch, stackedOn && `stacked on ${stackedOn}`].filter(Boolean).join(" · ");
  return (
    <div className="flex items-center gap-1.5 text-ui-sm text-muted-foreground">
      <Icons.branch className="size-3 shrink-0" />
      <span data-testid={testId} className="flex min-w-0 flex-1 gap-1" title={title}>
        <MiddleText text={branch} className="flex-1" />
        {base && (
          <span className="flex min-w-0 max-w-[60%] shrink-0 gap-1">
            <span className="shrink-0">{"→ Base: "}</span>
            <MiddleText text={base} />
            {gone && <span className="shrink-0">{" (gone)"}</span>}
          </span>
        )}
      </span>
    </div>
  );
}
