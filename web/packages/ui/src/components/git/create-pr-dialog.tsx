import { useId, useRef, useState } from "react";
import { Switch } from "radix-ui";

import { ForgeSettingsLink, isForgeSetupCode } from "@/components/git/forge-settings-link";
import { describePrError } from "@/components/git/pr-error";
import { PrErrorLine } from "@/components/git/pr-error-line";
import { Alert } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { InlineSpinner } from "@/components/ui/inline-spinner";
import { Input } from "@/components/ui/input";
import { MiddleText } from "@/components/ui/middle-text";
import { useCreatePr, usePrPreview } from "@/hooks/use-pr-actions";
import { useWorkspaceBranches } from "@/hooks/use-workspace-branches";
import { useTaskGit } from "@/hooks/use-task-git";
import { loadDraftPr, saveDraftPr } from "@/lib/git-prefs";
import { Icons } from "@/lib/icons";

const MAX_TITLE = 256;
const MAX_BODY = 64 * 1024;

/** The commit message box of the diff pane: "Commit staged first" hands focus to it. */
const COMMIT_MESSAGE_SELECTOR = '[data-testid="commit-message"]';

export function CreatePrDialog({
  taskId,
  workspaceId,
  open,
  onOpenChange,
}: {
  taskId: number;
  workspaceId: number | null;
  open: boolean;
  onOpenChange: (open: boolean) => void;
}) {
  const focusCommitBox = useRef(false);
  const pending = useRef(false);
  return (
    <Dialog open={open} onOpenChange={(next) => (next || !pending.current) && onOpenChange(next)}>
      <DialogContent
        className="top-[10vh] translate-y-0 gap-3 p-5 sm:max-w-xl"
        // Top-anchored: the preview loads after open and a centred dialog would jump as its height settles.
        data-testid="create-pr-dialog"
        onCloseAutoFocus={(e) => {
          if (!focusCommitBox.current) return;
          focusCommitBox.current = false;
          e.preventDefault();
          document.querySelector<HTMLElement>(COMMIT_MESSAGE_SELECTOR)?.focus();
        }}
      >
        <CreatePrForm
          taskId={taskId}
          workspaceId={workspaceId}
          onClose={() => onOpenChange(false)}
          onCommitFirst={() => {
            focusCommitBox.current = true;
            onOpenChange(false);
          }}
          onPending={(p) => (pending.current = p)}
        />
      </DialogContent>
    </Dialog>
  );
}

/** Mounted only while the dialog is open, so every open starts from the preview's defaults. */
function CreatePrForm({
  taskId,
  workspaceId,
  onClose,
  onCommitFirst,
  onPending,
}: {
  taskId: number;
  workspaceId: number | null;
  onClose: () => void;
  onCommitFirst: () => void;
  onPending: (pending: boolean) => void;
}) {
  const git = useTaskGit(taskId);
  // The base the daemon would use for a PR with no override (`base_ref`), and the one the field starts at (`prDefault`).
  const storedBase = git?.base?.ref ?? "";
  const taskBase = git?.base?.prDefault ?? storedBase;
  const [baseInput, setBaseInput] = useState(taskBase);
  const [committedBase, setCommittedBase] = useState(taskBase);
  const baseOverride = committedBase === storedBase ? "" : committedBase;
  const preview = usePrPreview(taskId, baseOverride, true);
  const branches = useWorkspaceBranches({ workspaceId: workspaceId ?? -1, limit: 50, enabled: workspaceId !== null });
  const create = useCreatePr();

  // `null` = the person hasn't typed: the field shows the preview's default (so a new base's defaults flow in).
  const [titleEdit, setTitleEdit] = useState<string | null>(null);
  const [bodyEdit, setBodyEdit] = useState<string | null>(null);
  const [draft, setDraft] = useState(() => (workspaceId === null ? true : loadDraftPr(workspaceId)));

  const ids = { title: useId(), body: useId(), base: useId(), branches: useId(), draft: useId() };
  const data = preview.data;
  const title = titleEdit ?? data?.defaults.title ?? "";
  const body = bodyEdit ?? data?.defaults.body ?? "";
  const blocker = data?.blockers[0];
  const uncommitted = data ? data.uncommitted.staged + data.uncommitted.unstaged + data.uncommitted.untracked : 0;
  const titleInvalid = title.trim() === "" || title.length > MAX_TITLE || /[\n\r\0]/.test(title);
  const bodyInvalid = body.length > MAX_BODY;
  const loading = !data && preview.isPending;
  const canCreate = !!data && !blocker && !create.isPending && !titleInvalid && !bodyInvalid;
  const headBranch = data?.head.branch ?? git?.branch?.name ?? "";
  // The PR's own change (committed work only); uncommitted files are listed separately as "not included".
  const diff = data?.diff;
  const error = create.error ? describePrError(create.error, "create") : null;

  const submit = () => {
    if (!canCreate) return;
    onPending(true);
    create.mutate(
      { taskId, ...(baseOverride ? { baseBranch: baseOverride } : {}), title: title.trim(), body, draft },
      {
        onSuccess: () => {
          onPending(false);
          onClose();
        },
        onError: () => onPending(false),
      },
    );
  };

  const commitBase = () => setCommittedBase(baseInput.trim() || taskBase);

  return (
    <form
      className="grid gap-3"
      onSubmit={(e) => {
        e.preventDefault();
        submit();
      }}
    >
      <DialogHeader>
        <DialogTitle>Create pull request</DialogTitle>
        <DialogDescription className="sr-only">Review the title and description, then open the pull request on GitHub.</DialogDescription>
      </DialogHeader>

      <div className="flex min-h-7 flex-wrap items-center gap-x-2 gap-y-1 text-ui-base" data-testid="create-pr-branches">
        <Icons.branch aria-hidden className="size-3.5 shrink-0 text-foreground-muted" />
        <MiddleText text={headBranch} className="max-w-[11rem] font-mono text-ui-sm" />
        {/* The arrow stays with the field; a long base wraps the pair to its own line instead of cutting the name. */}
        <span className="flex min-w-[19rem] flex-1 items-center gap-2">
          <Icons.arrowRight aria-hidden className="size-3.5 shrink-0 text-foreground-muted" />
          <label htmlFor={ids.base} className="sr-only">
            Base branch
          </label>
          <Input
            id={ids.base}
            list={branches.data ? ids.branches : undefined}
            value={baseInput}
            onChange={(e) => setBaseInput(e.target.value)}
            onBlur={commitBase}
            onKeyDown={(e) => {
              if (e.key === "Enter") {
                e.preventDefault();
                commitBase();
              }
            }}
            spellCheck={false}
            autoComplete="off"
            className="h-7 min-w-0 flex-1 font-mono text-ui-xs"
            title={baseInput}
            data-testid="create-pr-base"
          />
        </span>
        {branches.data && (
          <datalist id={ids.branches}>
            {branches.data.branches.map((b) => (
              <option key={`${b.remote}/${b.name}`} value={b.name} />
            ))}
          </datalist>
        )}
        {diff && (
          <span className="ml-auto text-ui-sm" data-testid="create-pr-diffstat">
            <span className="text-diff-added">+{diff.insertions}</span> <span className="text-diff-removed">−{diff.deletions}</span>
          </span>
        )}
      </div>

      {git?.base?.parentBranch && (
        // One line reserved for a stacked task, so switching the base never moves the fields below.
        <p className="min-h-5 text-ui-sm text-foreground-muted">
          {committedBase === git.base.parentBranch ? (
            <span data-testid="create-pr-stacked-hint">
              Stacked on <span className="font-mono text-foreground">{git.base.parentBranch}</span>
            </span>
          ) : git.base.prDefault && git.base.prDefault !== git.base.parentBranch && committedBase === git.base.prDefault ? (
            <span data-testid="create-pr-fallback-hint">Parent branch not pushed yet; PR targets {git.base.prDefault}</span>
          ) : null}
        </p>
      )}

      {blocker && (
        <Alert variant="warning" description={blocker.reason} testId="create-pr-blocker">
          {isForgeSetupCode(blocker.code) && <ForgeSettingsLink onNavigate={onClose} testId="create-pr-blocker-settings-link" />}
        </Alert>
      )}

      <div className="grid gap-1">
        <label htmlFor={ids.title} className="text-ui-sm text-foreground-muted">
          Title
        </label>
        <Input
          id={ids.title}
          value={title}
          onChange={(e) => setTitleEdit(e.target.value)}
          readOnly={loading}
          maxLength={MAX_TITLE}
          autoFocus
          data-testid="create-pr-title"
        />
      </div>

      <div className="grid gap-1">
        <label htmlFor={ids.body} className="text-ui-sm text-foreground-muted">
          Description
        </label>
        <div className="rounded-lg border border-input-border bg-input focus-within:border-input-border-focused">
          <textarea
            id={ids.body}
            value={body}
            onChange={(e) => setBodyEdit(e.target.value)}
            readOnly={loading}
            rows={9}
            spellCheck={false}
            className="block max-h-72 min-h-40 w-full resize-none border-0 bg-transparent p-2.5 font-mono text-ui-sm outline-none read-only:opacity-60"
            data-testid="create-pr-body"
          />
        </div>
      </div>

      <div className="flex min-h-7 items-center justify-between gap-3">
        <div className="flex items-center gap-2">
          <Switch.Root
            id={ids.draft}
            checked={draft}
            onCheckedChange={(next) => {
              setDraft(next);
              if (workspaceId !== null) saveDraftPr(workspaceId, next);
            }}
            className="relative h-5 w-9 shrink-0 rounded-full border border-input-border bg-input transition-colors focus-visible:ring-2 focus-visible:ring-ring/50 data-[state=checked]:bg-primary"
            data-testid="create-pr-draft"
          >
            <Switch.Thumb className="block size-3.5 translate-x-0.5 rounded-full bg-foreground-muted transition-transform data-[state=checked]:translate-x-[18px] data-[state=checked]:bg-primary-foreground" />
          </Switch.Root>
          <label htmlFor={ids.draft} className="text-ui-base">
            Draft
          </label>
        </div>
        <div className="flex min-w-0 items-center gap-2 text-ui-sm text-foreground-muted" data-testid="create-pr-summary">
          {loading ? (
            <InlineSpinner label="Loading preview…" />
          ) : data ? (
            <>
              <span className="truncate">
                {data.commitCount} commit{data.commitCount === 1 ? "" : "s"}
                {uncommitted > 0 && ` · ${uncommitted} file${uncommitted === 1 ? "" : "s"} uncommitted (not included)`}
              </span>
              {data.uncommitted.staged > 0 && (
                <Button type="button" variant="outline" size="sm" onClick={onCommitFirst} data-testid="create-pr-commit-first">
                  Commit staged first
                </Button>
              )}
            </>
          ) : null}
        </div>
      </div>

      {data?.head.strategy === "cherry-pick" && (
        <p className="text-ui-sm text-foreground-muted" data-testid="create-pr-cherry-pick-note">
          smind will open this from a rebased copy (<code className="font-mono">smind/pr-task-{taskId}</code>); later updates aren&apos;t automatic.
        </p>
      )}

      {preview.isError && !data && <PrErrorLine error={describePrError(preview.error, "preview")} testId="create-pr-preview-error" onNavigate={onClose} />}
      {error && (
        <PrErrorLine error={error} testId="create-pr-error" onNavigate={onClose}>
          {error.existing && /^https:\/\//.test(error.existing.url) && (
            <a
              href={error.existing.url}
              target="_blank"
              rel="noopener noreferrer"
              className="inline-flex items-center gap-1 text-ui-sm text-foreground underline"
              data-testid="create-pr-error-view"
            >
              View PR #{error.existing.number}
              <Icons.externalLink aria-hidden className="size-3" />
            </a>
          )}
        </PrErrorLine>
      )}

      <DialogFooter className="gap-2">
        <Button type="button" variant="ghost" onClick={onClose} disabled={create.isPending}>
          Cancel
        </Button>
        <Button type="submit" disabled={!canCreate} data-testid="create-pr-submit">
          {create.isPending ? (
            <>
              <Icons.spinner aria-hidden className="size-3.5 animate-spin" /> Creating…
            </>
          ) : (
            "Create PR"
          )}
        </Button>
      </DialogFooter>
    </form>
  );
}
