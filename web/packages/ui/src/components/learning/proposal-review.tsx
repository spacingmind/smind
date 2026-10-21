import { useState } from "react";

import { LoadingRows } from "@/components/ui/loading-rows";
import { DuplicateBanner, DuplicateCompare } from "@/components/learning/duplicate-compare";
import { EvidenceList } from "@/components/learning/evidence-list";
import { ProposalPrevalenceLine } from "@/components/learning/proposal-prevalence";
import { Chip, SourceChip } from "@/components/learning/learning-chips";
import { ProposalItemView, staleReason } from "@/components/learning/proposal-items";
import { SkillEditor } from "@/components/learning/skill-editor";
import { Alert } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import {
  gateFindings,
  isStaleError,
  useApproveProposal,
  useLearnedSkillDetails,
  useProposal,
  useRejectProposal,
} from "@/hooks/use-learning";
import { editorProblems } from "@/lib/learning-format";
import type { ProposalItem } from "@/lib/learning-types";
import { formatRelativeTime } from "@/lib/relative-time";
import { Icons } from "@/lib/icons";

type Mode = "review" | "edit" | "merge" | "reject";

const TEXTAREA =
  "w-full resize-y rounded-lg border border-input-border bg-input px-2.5 py-1.5 text-ui-base outline-none hover:border-input-border-hover focus-visible:border-input-border-focused focus-visible:bg-input-focused";

/**
 * One proposal opened for review: what it would do (the full skill, or the diff against the
 * current body), why, the evidence, a possible-duplicate comparison, and the decision.
 * Approve, edit-then-approve, merge-into-existing and reject all go through the shared cache's
 * mutations; a gate rejection lists its findings and the proposal stays pending.
 */
export function ProposalReview({ id, onBack }: { id: number; onBack: () => void }) {
  const detail = useProposal(id);
  const approve = useApproveProposal();
  const reject = useRejectProposal();
  const proposal = detail.data?.proposal;
  const merge = detail.data?.merge;

  const patchTargets = (proposal?.items ?? []).flatMap((i) => (i.action === "patch" && i.targetSkillId !== undefined ? [i.targetSkillId] : []));
  const targets = useLearnedSkillDetails(patchTargets);

  const [mode, setMode] = useState<Mode>("review");
  const [draft, setDraft] = useState<{ description: string; body: string }>({ description: "", body: "" });
  const [mergedBody, setMergedBody] = useState("");
  const [note, setNote] = useState("");

  const back = (
    <Button type="button" variant="ghost" size="sm" data-testid="learning-detail-back" onClick={onBack} className="self-start">
      <Icons.back aria-hidden /> Proposals
    </Button>
  );
  if (!proposal) {
    return (
      <div className="flex flex-col gap-3" data-testid="proposal-review">
        {back}
        {detail.error ? (
          <p role="alert" className="text-ui-base text-destructive">
            {detail.error.message}
          </p>
        ) : (
          <LoadingRows rows={2} />
        )}
      </div>
    );
  }

  const ctx = { id: proposal.id, workspaceId: proposal.workspaceId };
  const pending = proposal.status === "pending";
  const busy = approve.isPending || reject.isPending;
  const first: ProposalItem | undefined = proposal.items[0];
  const umbrella = proposal.items.filter((i) => i.action !== "delete");
  const archived = proposal.items.filter((i) => i.action === "delete");

  const staleReasons = proposal.items.flatMap((item) => {
    const index = patchTargets.indexOf(item.targetSkillId ?? -1);
    const query = index >= 0 ? targets[index] : undefined;
    const missing = query?.isError === true;
    const reason = staleReason(item, query?.data, missing);
    return reason ? [reason] : [];
  });
  // A stale answer from the daemon on approve is as blocking as one we detect from the target's version.
  const daemonStale = isStaleError(approve.error) ? (approve.error as Error).message : null;
  const stale = staleReasons.length > 0 || daemonStale !== null;
  const findings = gateFindings(approve.error);
  const otherError = approve.error && findings === null && !daemonStale ? approve.error.message : null;

  const finish = { onSuccess: () => onBack() };
  const editing = mode === "edit";
  const merging = mode === "merge";
  const editProblems = editorProblems({ description: draft.description, body: draft.body });
  const mergeProblems = editorProblems({ body: mergedBody });
  const invalid = editing ? Object.keys(editProblems).length > 0 : merging ? Object.keys(mergeProblems).length > 0 : false;

  const startEdit = () => {
    setDraft({ description: first?.description ?? "", body: first?.body ?? "" });
    approve.reset();
    setMode("edit");
  };
  const startMerge = () => {
    setMergedBody(merge?.target.body ?? "");
    approve.reset();
    setMode("merge");
  };
  const cancelMode = () => {
    approve.reset();
    setMode("review");
  };

  return (
    <div className="flex flex-col gap-5" data-testid="proposal-review">
      {back}
      <header className="flex flex-col gap-1.5">
        <span className="flex min-w-0 flex-wrap items-center gap-2">
          <h2 className="truncate text-ui-lg font-medium text-foreground">{proposal.name}</h2>
          <Chip testId="proposal-kind">{proposal.kind}</Chip>
          <SourceChip source={proposal.source} />
          {!pending && <Chip testId="proposal-status">{proposal.status}</Chip>}
        </span>
        <span className="text-ui-sm text-foreground-subtle">proposed {formatRelativeTime(proposal.createdAt)}</span>
        {first?.reason && (
          <p data-testid="proposal-reason" className="text-ui-base text-foreground-muted">
            {first.reason}
          </p>
        )}
        {proposal.note && <p className="text-ui-sm text-muted-foreground">Note: {proposal.note}</p>}
      </header>

      {proposal.malformed && (
        <Alert variant="error" testId="proposal-malformed" description="This proposal could not be read, so it cannot be approved. Reject it to clear it." />
      )}
      {proposal.possibleDuplicateOf && (
        <DuplicateBanner name={proposal.possibleDuplicateOf} similarity={proposal.similarity} human={!merge} />
      )}
      {staleReasons.length > 0 && (
        <Alert
          variant="warning"
          testId="proposal-stale"
          title="This proposal is out of date"
          description={`${staleReasons.join(" ")} Approving it would overwrite newer changes, so reject it; the next reflection can propose again.`}
        />
      )}
      {daemonStale && staleReasons.length === 0 && (
        <Alert variant="warning" testId="proposal-stale" title="This proposal is out of date" description={daemonStale} />
      )}
      {findings && (
        <Alert variant="error" testId="gate-findings" title="The safety check rejected this proposal; it stays pending">
          <ul className="mt-1 list-disc pl-5 text-ui-base text-foreground-muted">
            {findings.map((f, i) => (
              <li key={`${f.reason}:${i}`}>
                <span className="font-medium text-foreground">{f.reason}</span>
                {f.detail ? `: ${f.detail}` : ""}
              </li>
            ))}
          </ul>
        </Alert>
      )}
      {otherError && <Alert variant="error" testId="proposal-error" description={otherError} />}
      {reject.error && <Alert variant="error" testId="proposal-error" description={reject.error.message} />}

      {merge && first ? (
        <DuplicateCompare proposed={first} existing={merge.target} />
      ) : (
        <section className="flex flex-col gap-3" data-testid="proposal-items">
          {umbrella.map((item, i) => {
            const index = patchTargets.indexOf(item.targetSkillId ?? -1);
            return <ProposalItemView key={`${item.name}:${i}`} item={item} target={index >= 0 ? targets[index]?.data : undefined} />;
          })}
          {archived.length > 0 && (
            <div className="flex flex-col gap-1" data-testid="proposal-archived">
              <h3 className="text-ui-base font-medium text-foreground">Will be archived</h3>
              <ul className="flex list-disc flex-col gap-1 pl-5">
                {archived.map((item, i) => (
                  <ProposalItemView key={`${item.name}:${i}`} item={item} />
                ))}
              </ul>
            </div>
          )}
        </section>
      )}

      <section className="flex flex-col gap-2" data-testid="proposal-evidence">
        <h3 className="text-ui-base font-medium text-foreground">Evidence</h3>
        <EvidenceList evidence={proposal.evidence} workspaceId={proposal.workspaceId} />
        <ProposalPrevalenceLine id={proposal.id} pending={pending} />
      </section>

      {pending && editing && (
        <SkillEditor
          idPrefix="proposal-edit"
          description={draft.description}
          body={draft.body}
          onChange={(next) => setDraft({ description: next.description ?? "", body: next.body })}
        />
      )}
      {pending && merging && merge && (
        <div className="flex flex-col gap-1">
          <p className="text-ui-sm text-muted-foreground">
            Edit the merged body of <span className="font-medium text-foreground">{merge.target.name}</span>. It starts from the existing
            body; use the proposed one above for reference.
          </p>
          <SkillEditor idPrefix="proposal-merge" body={mergedBody} bodyLabel="Merged body" onChange={(next) => setMergedBody(next.body)} />
        </div>
      )}
      {pending && mode === "reject" && (
        <label className="flex flex-col gap-1 text-ui-sm text-foreground-muted">
          Note (optional): why this is not wanted. The reflector learns from it.
          <textarea
            data-testid="proposal-reject-note"
            rows={3}
            className={TEXTAREA}
            value={note}
            onChange={(e) => setNote(e.target.value)}
          />
        </label>
      )}

      {pending && !proposal.malformed && (
        <div className="flex flex-wrap items-center gap-2" data-testid="proposal-actions">
          {mode === "review" && merge && (
            <>
              <Button type="button" disabled={busy || stale} data-testid="proposal-merge" onClick={startMerge}>
                Merge into this skill
              </Button>
              <Button
                type="button"
                variant="outline"
                disabled={busy || stale}
                data-testid="proposal-keep-separate"
                onClick={() => approve.mutate(ctx, finish)}
              >
                Keep as separate skill
              </Button>
            </>
          )}
          {mode === "review" && !merge && (
            <>
              <Button type="button" disabled={busy || stale} data-testid="proposal-approve" onClick={() => approve.mutate(ctx, finish)}>
                Approve
              </Button>
              {proposal.kind === "create" && (
                <Button type="button" variant="outline" disabled={busy || stale} data-testid="proposal-edit-start" onClick={startEdit}>
                  Edit then approve
                </Button>
              )}
            </>
          )}
          {editing && (
            <Button
              type="button"
              disabled={busy || stale || invalid}
              data-testid="proposal-approve-edited"
              onClick={() => approve.mutate({ ...ctx, description: draft.description.trim(), body: draft.body }, finish)}
            >
              Approve with edits
            </Button>
          )}
          {merging && merge && (
            <Button
              type="button"
              disabled={busy || stale || invalid}
              data-testid="proposal-approve-merge"
              onClick={() => approve.mutate({ ...ctx, mergeInto: merge.target.name, body: mergedBody }, finish)}
            >
              Approve merge
            </Button>
          )}
          {mode === "reject" ? (
            <Button
              type="button"
              variant="destructive"
              disabled={busy}
              data-testid="proposal-reject-confirm"
              onClick={() => reject.mutate({ ...ctx, note: note.trim() || undefined }, finish)}
            >
              Reject
            </Button>
          ) : mode === "review" ? (
            <Button type="button" variant="ghost" disabled={busy} data-testid="proposal-reject" onClick={() => setMode("reject")}>
              Reject…
            </Button>
          ) : null}
          {mode !== "review" && (
            <Button type="button" variant="ghost" disabled={busy} data-testid="proposal-cancel" onClick={cancelMode}>
              Cancel
            </Button>
          )}
        </div>
      )}
    </div>
  );
}
