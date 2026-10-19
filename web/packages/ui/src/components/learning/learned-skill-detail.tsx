import { useState, type ReactNode } from "react";

import { LoadingRows } from "@/components/ui/loading-rows";
import { ConfirmDialog } from "@/components/ui/confirm-dialog";
import { EvidenceList } from "@/components/learning/evidence-list";
import { Chip, SkillBadges, SourceChip } from "@/components/learning/learning-chips";
import { RunLink } from "@/components/learning/run-link";
import { DiffRender } from "@/components/diff-render";
import { Alert } from "@/components/ui/alert";
import { FormError } from "@/components/ui/form-error";

import { Button } from "@/components/ui/button";
import { useLearnedSkill, useSkillAction, useUndoSkill } from "@/hooks/use-learning";
import { formatPercent } from "@/lib/learning-format";
import type { LearnedSkillView, LedgerEntryView, VersionView } from "@/lib/learning-types";
import { formatRelativeTime } from "@/lib/relative-time";
import { unifiedDiff } from "@/lib/text-diff";
import { Icons } from "@/lib/icons";

function Block({ title, testId, children }: { title: string; testId: string; children: ReactNode }) {
  return (
    <section data-testid={testId} className="flex flex-col gap-2">
      <h3 className="text-ui-base font-medium text-foreground">{title}</h3>
      {children}
    </section>
  );
}

/** The body, read-only, in the monospace token. */
export function SkillBody({ body, testId = "skill-body" }: { body: string; testId?: string }) {
  return (
    <pre
      data-testid={testId}
      className="max-h-96 overflow-auto whitespace-pre-wrap break-words rounded-lg border bg-card p-3 font-mono text-ui-sm text-foreground"
    >
      {body}
    </pre>
  );
}

function VersionRow({
  version,
  previous,
  current,
  name,
  busy,
  onRestore,
}: {
  version: VersionView;
  previous?: VersionView;
  current: boolean;
  name: string;
  busy: boolean;
  onRestore: () => void;
}) {
  const [open, setOpen] = useState(false);
  const diff = previous ? unifiedDiff(previous.body, version.body, `${name}.md`) : "";
  const descriptionChanged = previous !== undefined && previous.description !== version.description;
  return (
    <li data-testid={`version-${version.version}`} className="flex flex-col gap-1 rounded-lg px-2 py-1.5">
      <div className="flex flex-wrap items-center gap-2">
        <span className="text-ui-base font-medium tabular-nums text-foreground">v{version.version}</span>
        {current && <Chip testId="version-current">current</Chip>}
        <span className="text-ui-sm text-foreground-subtle">
          {version.actor} · {formatRelativeTime(version.createdAt)}
        </span>
        <span className="ml-auto flex items-center gap-1">
          {previous && (
            <Button
              type="button"
              variant="ghost"
              size="xs"
              aria-expanded={open}
              data-testid={`version-toggle-${version.version}`}
              onClick={() => setOpen((v) => !v)}
            >
              {open ? "Hide changes" : "Show changes"}
            </Button>
          )}
          {!current && (
            <Button type="button" variant="ghost" size="xs" disabled={busy} data-testid={`version-restore-${version.version}`} onClick={onRestore}>
              <Icons.reset aria-hidden /> Restore
            </Button>
          )}
        </span>
      </div>
      {!previous && <span className="text-ui-sm text-muted-foreground">First version</span>}
      {open && (
        <div className="flex flex-col gap-2" data-testid={`version-diff-${version.version}`}>
          {descriptionChanged && (
            <p className="text-ui-sm text-muted-foreground">
              Description: <span className="line-through">{previous!.description}</span> → {version.description}
            </p>
          )}
          {diff ? <DiffRender diff={diff} outputFormat="line-by-line" /> : <p className="text-ui-sm text-muted-foreground">The body did not change.</p>}
        </div>
      )}
    </li>
  );
}

function LedgerRow({ entry, workspaceId }: { entry: LedgerEntryView; workspaceId: number }) {
  return (
    <li data-testid="ledger-entry" className="flex flex-col gap-0.5 text-ui-sm">
      <span className="flex flex-wrap items-center gap-x-2">
        <span className="font-medium text-foreground">{entry.action}</span>
        <span className="text-foreground-subtle">
          {entry.actor} · {formatRelativeTime(entry.createdAt)}
        </span>
        {entry.runId && <RunLink runId={entry.runId} workspaceId={workspaceId} />}
      </span>
      {(entry.reason || entry.absorbedInto) && (
        <span className="text-muted-foreground">
          {entry.reason}
          {entry.absorbedInto ? ` (merged into ${entry.absorbedInto})` : ""}
        </span>
      )}
    </li>
  );
}

function Usage({ skill }: { skill: LearnedSkillView }) {
  const rows: [string, string][] = [
    ["Offered to", `${skill.offeredRuns} runs`],
    ["Loaded in", `${skill.loadedRuns} runs`],
    ["Load rate", formatPercent(skill.loadRate, skill.measured)],
    ["Loads", `${skill.loads} (${skill.loadsViaSkill} via skill, ${skill.loadsViaRead} via read)`],
    ["Last offered", skill.lastOfferedAt ? formatRelativeTime(skill.lastOfferedAt) : "never"],
    ["Last loaded", skill.lastLoadedAt ? formatRelativeTime(skill.lastLoadedAt) : "never"],
    ...(skill.patched ? ([["After last patch", skill.reusedAfterPatch ? "loaded again" : "not loaded yet"]] as [string, string][]) : []),
  ];
  return (
    <dl className="grid grid-cols-[auto_1fr] gap-x-4 gap-y-0.5 text-ui-sm">
      {rows.map(([k, v]) => (
        <div key={k} className="contents">
          <dt className="text-foreground-subtle">{k}</dt>
          <dd className="tabular-nums text-foreground-muted">{v}</dd>
        </div>
      ))}
    </dl>
  );
}

/**
 * One learned skill: body, usage, provenance, version history with diffs, the ledger, and the
 * actions that change it. Every action is a mutation on the shared cache; the buttons disable
 * while one is in flight and its error shows in the alert at the top.
 */
export function LearnedSkillDetail({ id, onBack }: { id: number; onBack: () => void }) {
  const detail = useLearnedSkill(id);
  const pin = useSkillAction("pin");
  const unpin = useSkillAction("unpin");
  const disable = useSkillAction("disable");
  const enable = useSkillAction("enable");
  const archive = useSkillAction("archive");
  const restore = useSkillAction("restore");
  const remove = useSkillAction("delete");
  const undo = useUndoSkill();
  const [confirm, setConfirm] = useState<{ kind: "delete" } | { kind: "undo"; version: number } | null>(null);

  const back = (
    <Button type="button" variant="ghost" size="sm" data-testid="learning-detail-back" onClick={onBack} className="self-start">
      <Icons.back aria-hidden /> Skills
    </Button>
  );
  if (!detail.data) {
    return (
      <div className="flex flex-col gap-3" data-testid="learned-skill-detail">
        {back}
        {detail.error ? (
          <FormError message={detail.error.message} />
        ) : (
          <LoadingRows rows={2} />
        )}
      </div>
    );
  }

  const { skill, versions, ledger, evidence } = detail.data;
  const ctx = { id: skill.id, workspaceId: skill.workspaceId };
  const mutations = [pin, unpin, disable, enable, archive, restore, remove, undo];
  const busy = mutations.some((m) => m.isPending);
  const error = mutations.find((m) => m.error)?.error ?? null;
  const currentVersion = versions.find((v) => v.version === skill.version);
  const body = currentVersion?.body ?? skill.body ?? "";
  const sorted = [...versions].sort((a, b) => b.version - a.version);

  const action = (label: string, run: () => void, testId: string, icon?: ReactNode) => (
    <Button type="button" variant="outline" size="sm" disabled={busy} data-testid={testId} onClick={run}>
      {icon}
      {label}
    </Button>
  );

  return (
    <div className="flex flex-col gap-5" data-testid="learned-skill-detail">
      {back}
      <header className="flex flex-col gap-1.5">
        <span className="flex min-w-0 flex-wrap items-center gap-2">
          <h2 className="truncate text-ui-lg font-medium text-foreground">{skill.name}</h2>
          <SourceChip source={skill.source} />
          {skill.category && <Chip>{skill.category}</Chip>}
          <SkillBadges skill={skill} />
          {skill.status === "archived" && <Chip testId="badge-archived">archived</Chip>}
        </span>
        <p className="text-ui-base text-foreground-muted">{skill.description}</p>
        <div className="flex flex-wrap gap-2 pt-1">
          {skill.status !== "archived" &&
            (skill.pinned
              ? action("Unpin", () => unpin.mutate(ctx), "skill-unpin", <Icons.unpin aria-hidden />)
              : action("Pin", () => pin.mutate(ctx), "skill-pin", <Icons.pin aria-hidden />))}
          {skill.status === "active" && action("Disable", () => disable.mutate(ctx), "skill-disable")}
          {skill.status === "disabled" && action("Enable", () => enable.mutate(ctx), "skill-enable")}
          {skill.status !== "archived"
            ? action("Archive", () => archive.mutate(ctx), "skill-archive", <Icons.archive aria-hidden />)
            : action("Restore", () => restore.mutate(ctx), "skill-restore")}
          <Button type="button" variant="destructive" size="sm" disabled={busy} data-testid="skill-delete" onClick={() => setConfirm({ kind: "delete" })}>
            <Icons.delete aria-hidden /> Delete…
          </Button>
        </div>
        {error && <Alert variant="error" testId="learned-skill-error" description={error.message} />}
      </header>

      <Block title="Instructions" testId="skill-section-body">
        <SkillBody body={body} />
      </Block>

      <Block title="Usage" testId="skill-section-usage">
        <Usage skill={skill} />
      </Block>

      <Block title="Where it came from" testId="skill-section-provenance">
        <p className="text-ui-sm text-foreground-subtle">
          Learned by {skill.source}
          {skill.createdByRun && (
            <>
              {" "}
              from <RunLink runId={skill.createdByRun} workspaceId={skill.workspaceId} />
            </>
          )}
          , {formatRelativeTime(skill.createdAt)}.
        </p>
        {evidence && <EvidenceList evidence={evidence} workspaceId={skill.workspaceId} />}
      </Block>

      <Block title="Versions" testId="skill-section-versions">
        <ul className="flex flex-col gap-1 rounded-xl border bg-card p-2">
          {sorted.map((v) => (
            <VersionRow
              key={v.version}
              version={v}
              previous={versions.find((p) => p.version === v.version - 1)}
              current={v.version === skill.version}
              name={skill.name}
              busy={busy}
              onRestore={() => setConfirm({ kind: "undo", version: v.version })}
            />
          ))}
        </ul>
      </Block>

      <Block title="History" testId="skill-section-ledger">
        {ledger.length === 0 ? (
          <p className="text-ui-sm text-muted-foreground">Nothing recorded yet.</p>
        ) : (
          <ul className="flex flex-col gap-2">
            {ledger.map((entry) => (
              <LedgerRow key={entry.id} entry={entry} workspaceId={skill.workspaceId} />
            ))}
          </ul>
        )}
      </Block>

      <ConfirmDialog
        open={confirm?.kind === "delete"}
        onOpenChange={(open) => !open && setConfirm(null)}
        title="Delete this skill?"
        description={
          <>
            <span className="font-medium text-foreground">{skill.name}</span> and its history are removed for good. To keep it
            but stop agents using it, disable or archive it instead.
          </>
        }
        confirmLabel="Delete"
        pendingLabel="Deleting…"
        pending={remove.isPending}
        error={remove.error?.message}
        testId="skill-delete-dialog"
        onConfirm={() => remove.mutate(ctx, { onSuccess: () => { setConfirm(null); onBack(); } })}
      />
      <ConfirmDialog
        open={confirm?.kind === "undo"}
        onOpenChange={(open) => !open && setConfirm(null)}
        title={confirm?.kind === "undo" ? `Restore version ${confirm.version}?` : "Restore version?"}
        description="The body and description of that version become the current ones, as a new version. Nothing is lost: the versions in between stay in the history."
        confirmLabel="Restore"
        pendingLabel="Restoring…"
        destructive={false}
        pending={undo.isPending}
        error={undo.error?.message}
        testId="skill-undo-dialog"
        onConfirm={() => {
          if (confirm?.kind !== "undo") return;
          undo.mutate({ ...ctx, version: confirm.version }, { onSuccess: () => setConfirm(null) });
        }}
      />
    </div>
  );
}
