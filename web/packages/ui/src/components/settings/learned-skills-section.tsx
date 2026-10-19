import { useState, type ReactNode } from "react";

import { ConfirmDialog } from "@/components/learning/confirm-dialog";
import { LearnedSkillDetail } from "@/components/learning/learned-skill-detail";
import { ProposalReview } from "@/components/learning/proposal-review";
import { RunLinkProvider } from "@/components/learning/run-link";
import { LearningOptIn } from "@/components/learning/learning-optin";
import { PendingProposalBadge } from "@/components/learning/pending-badge";
import { LearnedSkillRow, ProposalRow } from "@/components/learning/skill-row";
import { registerSettingsSection, type SettingsSectionContext } from "@/components/settings/settings-registry";
import { Alert } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { EmptyState } from "@/components/ui/empty-state";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { useForgetLearning, useLearnedSkills, useLearningStatus, useProposals } from "@/hooks/use-learning";
import { LEARNED_SKILLS_SECTION_ID } from "@/lib/learning-section";
import type { LearningWorkspaceStatus } from "@/lib/learning-types";
import { Icons } from "@/lib/icons";

type Tab = "proposed" | "active" | "archived";

/** Keeps the list area the same height whichever tab is open, so switching tabs never shifts the page. */
const LIST_MIN_HEIGHT = "min-h-[26rem]";

function ListFrame({ children }: { children: ReactNode }) {
  return <div className={`flex flex-col rounded-xl border bg-card p-3 ${LIST_MIN_HEIGHT}`}>{children}</div>;
}

function LoadState({
  loading,
  error,
  onRetry,
}: {
  loading: boolean;
  error: Error | null;
  onRetry: () => void;
}) {
  if (error) {
    return (
      <div className="flex items-center gap-2">
        <p role="alert" data-testid="learning-load-error" className="text-ui-base text-destructive">
          {error.message}
        </p>
        <Button type="button" variant="ghost" size="sm" onClick={onRetry}>
          Retry
        </Button>
      </div>
    );
  }
  return loading ? <p className="text-ui-base text-muted-foreground">Loading…</p> : null;
}

function ProposedTab({ workspace, onOpen, openId }: { workspace: LearningWorkspaceStatus; onOpen?: (id: number) => void; openId?: number | null }) {
  const proposals = useProposals(workspace.id, "pending");
  const list = proposals.data ?? [];
  return (
    <ListFrame>
      <LoadState loading={proposals.isPending} error={proposals.error} onRetry={() => void proposals.refetch()} />
      {proposals.data && list.length === 0 && (
        <EmptyState
          testId="learning-empty-proposed"
          title="No proposals waiting"
          description="When a run shows a reusable pattern, the skill smind would learn from it appears here for you to approve."
        />
      )}
      {list.length > 0 && (
        <ul className="flex flex-col gap-1" aria-label="Proposed skills">
          {list.map((p) => (
            <ProposalRow key={p.id} proposal={p} selected={openId === p.id} onOpen={onOpen ? () => onOpen(p.id) : undefined} />
          ))}
        </ul>
      )}
    </ListFrame>
  );
}

function SkillsTab({
  workspace,
  tab,
  onOpen,
  openId,
}: {
  workspace: LearningWorkspaceStatus;
  tab: "active" | "archived";
  onOpen?: (id: number) => void;
  openId?: number | null;
}) {
  const skills = useLearnedSkills(workspace.id, tab === "active" ? ["active", "disabled"] : ["archived"]);
  const list = skills.data ?? [];
  return (
    <ListFrame>
      <LoadState loading={skills.isPending} error={skills.error} onRetry={() => void skills.refetch()} />
      {skills.data && list.length === 0 && (
        <EmptyState
          testId={`learning-empty-${tab}`}
          title={tab === "active" ? "No learned skills yet" : "Nothing archived"}
          description={
            tab === "active"
              ? workspace.learningMode === "off"
                ? "Turn learning on for this workspace to start collecting skill proposals."
                : "Approved proposals become skills that agents in this workspace can load."
              : "Skills you archive, or that smind merges into another, are kept here."
          }
        />
      )}
      {list.length > 0 && (
        <ul className="flex flex-col gap-1" aria-label={tab === "active" ? "Active skills" : "Archived skills"}>
          {list.map((s) => (
            <LearnedSkillRow key={s.id} skill={s} selected={openId === s.id} onOpen={onOpen ? () => onOpen(s.id) : undefined} />
          ))}
        </ul>
      )}
    </ListFrame>
  );
}

/**
 * Settings -> Skills (learned) (ADR-0026 §8): per-workspace learning opt-in, and the workspace's
 * skill proposals (the inbox), active skills and archive. Everything is read through the shared
 * query cache; the `learnedSkill.*` events keep it live.
 */
function LearnedSkillsSection({ closeSettings }: SettingsSectionContext) {
  const status = useLearningStatus();
  const [chosen, setChosen] = useState<number | null>(null);
  const [tab, setTab] = useState<Tab>("proposed");
  const [openSkill, setOpenSkill] = useState<number | null>(null);
  const [openProposal, setOpenProposal] = useState<number | null>(null);
  const [forgetting, setForgetting] = useState(false);
  const forget = useForgetLearning();

  const workspaces = status.data?.workspaces ?? [];
  const workspace = workspaces.find((w) => w.id === chosen) ?? workspaces[0] ?? null;

  if (!status.data) {
    return (
      <div className="flex flex-col gap-3" data-testid="settings-section-learned-skills">
        <LoadState loading={status.isPending} error={status.error} onRetry={() => void status.refetch()} />
      </div>
    );
  }
  if (!workspace) {
    return (
      <div data-testid="settings-section-learned-skills">
        <EmptyState testId="learning-no-workspaces" title="No workspaces yet" description="Add a workspace to start learning from its runs." />
      </div>
    );
  }

  if (openSkill !== null || openProposal !== null) {
    return (
      <RunLinkProvider closeSettings={closeSettings}>
        <div data-testid="settings-section-learned-skills">
          {openSkill !== null ? (
            <LearnedSkillDetail id={openSkill} onBack={() => setOpenSkill(null)} />
          ) : (
            <ProposalReview id={openProposal!} onBack={() => setOpenProposal(null)} />
          )}
        </div>
      </RunLinkProvider>
    );
  }

  return (
    <div className="flex flex-col gap-4" data-testid="settings-section-learned-skills">
      <Select
        value={String(workspace.id)}
        onValueChange={(v) => {
          setChosen(Number(v));
          setOpenSkill(null);
          setOpenProposal(null);
        }}
      >
        <SelectTrigger aria-label="Workspace" data-testid="learning-workspace-select" className="w-full max-w-xs">
          <SelectValue />
        </SelectTrigger>
        <SelectContent>
          {workspaces.map((w) => (
            <SelectItem key={w.id} value={String(w.id)}>
              {w.title}
            </SelectItem>
          ))}
        </SelectContent>
      </Select>

      <LearningOptIn workspace={workspace} enabled={status.data.enabled} />

      <Tabs value={tab} onValueChange={(v) => setTab(v as Tab)}>
        <TabsList variant="flat" aria-label="Learned skills">
          <TabsTrigger value="proposed" data-testid="learning-tab-proposed">
            Proposed
            {workspace.pendingProposals > 0 && (
              <span data-testid="learning-pending-count" className="tabular-nums text-foreground-subtle">
                {workspace.pendingProposals}
              </span>
            )}
          </TabsTrigger>
          <TabsTrigger value="active" data-testid="learning-tab-active">
            Active
          </TabsTrigger>
          <TabsTrigger value="archived" data-testid="learning-tab-archived">
            Archived
          </TabsTrigger>
        </TabsList>
        <TabsContent value="proposed">
          <ProposedTab workspace={workspace} onOpen={setOpenProposal} />
        </TabsContent>
        <TabsContent value="active">
          <SkillsTab workspace={workspace} tab="active" onOpen={setOpenSkill} />
        </TabsContent>
        <TabsContent value="archived">
          <SkillsTab workspace={workspace} tab="archived" onOpen={setOpenSkill} />
        </TabsContent>
      </Tabs>
      {status.error && <Alert variant="error" description={status.error.message} />}

      <div className="flex items-center justify-between gap-3 border-t pt-3">
        <p className="text-ui-sm text-muted-foreground">
          Forgetting removes every proposal, skill, version and record learned in {workspace.title}.
        </p>
        <Button type="button" variant="destructive" size="sm" data-testid="learning-forget" onClick={() => setForgetting(true)}>
          Forget everything…
        </Button>
      </div>
      <ConfirmDialog
        open={forgetting}
        onOpenChange={(open) => {
          setForgetting(open);
          if (!open) forget.reset();
        }}
        title={`Forget everything learned in ${workspace.title}?`}
        description="This permanently deletes the workspace's proposals, learned skills, versions, history and learning jobs. Skills you wrote yourself are not touched. It cannot be undone."
        confirmLabel="Forget everything"
        pendingLabel="Forgetting…"
        requireText={workspace.title}
        pending={forget.isPending}
        error={forget.error?.message}
        testId="learning-forget-dialog"
        onConfirm={() => forget.mutate({ workspaceId: workspace.id }, { onSuccess: () => setForgetting(false) })}
      />
    </div>
  );
}

registerSettingsSection({
  id: LEARNED_SKILLS_SECTION_ID,
  label: "Skills (learned)",
  groupLabel: "Agents & providers",
  order: 178,
  icon: <Icons.skills aria-hidden className="size-3.5" />,
  badge: <PendingProposalBadge />,
  render: (ctx) => <LearnedSkillsSection {...ctx} />,
});
