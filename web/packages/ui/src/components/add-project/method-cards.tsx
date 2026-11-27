import { Icons, type IconComponent } from "@/lib/icons";

/**
 * The "Add project" dialog's How step (C1 D2). "Open a folder" is live and
 * preselected (a real button, `aria-pressed`); the other three show the shape
 * of the flow but are disabled with a visible "Coming soon" badge -- plain
 * divs with no click handler and no button semantics, so nothing that looks
 * live is dead.
 */
export function MethodCards() {
  return (
    <div className="grid grid-cols-1 gap-2 sm:grid-cols-2">
      <button
        type="button"
        data-testid="add-project-method-open"
        aria-pressed="true"
        className="flex items-center gap-2.5 rounded-xl border border-foreground/30 bg-card p-3 text-left outline-none transition-colors focus-visible:ring-3 focus-visible:ring-ring/50"
      >
        <Icons.folderOpen className="size-4 shrink-0 text-foreground-muted" />
        <span className="min-w-0 text-ui-base font-medium text-foreground">Open a folder</span>
      </button>
      <ComingSoonCard testId="add-project-method-clone" icon={Icons.branch} title="Clone from Git" />
      <ComingSoonCard testId="add-project-method-new" icon={Icons.folderAdd} title="New project" />
      <ComingSoonCard testId="add-project-method-session" icon={Icons.history} title="From a past session" />
    </div>
  );
}

function ComingSoonCard({ testId, icon: Icon, title }: { testId: string; icon: IconComponent; title: string }) {
  return (
    <div
      data-testid={testId}
      aria-disabled="true"
      className="flex items-center gap-2.5 rounded-xl border border-border bg-card p-3 text-foreground-muted"
    >
      <Icon className="size-4 shrink-0" />
      <span className="min-w-0 text-ui-base">{title}</span>
      <span
        className="ml-auto shrink-0 rounded-full bg-muted px-1.5 py-0.5 text-ui-xs text-muted-foreground"
      >
        Coming soon
      </span>
    </div>
  );
}
