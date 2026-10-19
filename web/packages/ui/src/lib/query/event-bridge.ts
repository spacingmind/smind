import type { QueryClient } from "@tanstack/react-query";

import { upsertProfileInCache } from "@/hooks/use-profiles";
import { upsertReport } from "@/lib/account-usage/model";
import type { AccountUsage, AccountUsageEventPayload, UsageQueryData } from "@/lib/account-usage/types";
import { LEARNED_SKILLS_SECTION_ID } from "@/lib/learning-section";
import type {
  LearnedSkillChangedPayload,
  LearnedSkillProposedPayload,
  LearningJobPayload,
} from "@/lib/learning-types";
import { queryKeys } from "@/lib/query-keys";
import type { Account, AgentProfile } from "@/lib/types";
import { toast } from "@/components/ui/toast";

/**
 * The daemon-event -> query-cache bridge (ADR-0032 decision 1). It is the one
 * place events touch the cache: `useDaemonEvents` calls it for every
 * notification before dispatching to listeners, so components never subscribe
 * to an event just to refetch.
 *
 * Rules: a payload that carries the whole entity is written with
 * `setQueryData` (no round trip); anything else invalidates. Invalidating the
 * usage query is safe -- it only ever re-reads the daemon's *cached* reports
 * (`usageQueryOptions`), never a provider -- and the usage query is never
 * invalidated from a focus, mount or retry path.
 */
export function applyDaemonEvent(queryClient: QueryClient, topic: string, payload: unknown): void {
  switch (topic) {
    case "account.updated": {
      const account = (payload as { account?: Account } | null)?.account;
      if (account && typeof account.id === "number") {
        queryClient.setQueryData<Account[]>(queryKeys.accountList(), (held) =>
          held ? upsertById(held, account) : held,
        );
      } else {
        void queryClient.invalidateQueries({ queryKey: queryKeys.accountList() });
      }
      // The credential may have changed, so the provider's earlier health answer is stale: drop it (the
      // footer's ensure() re-tests it; a local check, never a provider call).
      if (account && typeof account.provider === "string") {
        queryClient.removeQueries({ queryKey: queryKeys.providerTest(account.provider) });
      } else {
        queryClient.removeQueries({ queryKey: queryKeys.providerTestAll() });
      }
      // A new account has no usage row yet and a renamed one has a new displayName: one more
      // cached read picks both up.
      void queryClient.invalidateQueries({ queryKey: queryKeys.accountUsage() });
      void queryClient.invalidateQueries({ queryKey: queryKeys.providerList() });
      return;
    }
    case "account.removed": {
      const id = (payload as { id?: unknown } | null)?.id;
      if (typeof id !== "number") {
        void queryClient.invalidateQueries({ queryKey: queryKeys.accountList() });
        void queryClient.invalidateQueries({ queryKey: queryKeys.accountUsage() });
        return;
      }
      const removed = queryClient.getQueryData<Account[]>(queryKeys.accountList())?.find((a) => a.id === id);
      if (removed) queryClient.removeQueries({ queryKey: queryKeys.providerTest(removed.provider) });
      else queryClient.removeQueries({ queryKey: queryKeys.providerTestAll() });
      queryClient.setQueryData<Account[]>(queryKeys.accountList(), (held) => held?.filter((a) => a.id !== id));
      queryClient.setQueryData<UsageQueryData>(queryKeys.accountUsage(), (held) =>
        held ? { ...held, reports: held.reports.filter((r) => r.accountId !== id) } : held,
      );
      void queryClient.invalidateQueries({ queryKey: queryKeys.providerList() });
      return;
    }
    case "account.usage": {
      const usage = (payload as AccountUsageEventPayload | null)?.usage;
      if (typeof usage !== "object" || usage === null || typeof (usage as AccountUsage).accountId !== "number") return;
      const held = queryClient.getQueryData<UsageQueryData>(queryKeys.accountUsage());
      if (!held) return;
      // An account the usage cache has not seen is kept only when the account list says it exists (a
      // new account's `account.updated` arrives first); otherwise a report published just after the
      // account's removal would resurrect it. Anything else is settled by one more cached read.
      const known = held.reports.some((r) => r.accountId === usage.accountId);
      const listed = queryClient
        .getQueryData<Account[]>(queryKeys.accountList())
        ?.some((a) => a.id === usage.accountId);
      if (!known && listed !== true) {
        void queryClient.invalidateQueries({ queryKey: queryKeys.accountUsage() });
        return;
      }
      queryClient.setQueryData<UsageQueryData>(queryKeys.accountUsage(), (current) =>
        current ? { ...current, reports: upsertReport(current.reports, usage) } : current,
      );
      return;
    }
    case "profile.created":
    case "profile.updated": {
      const profile = (payload as { profile?: AgentProfile } | null)?.profile;
      // Upsert by ID is idempotent with the mutation's own write; a malformed
      // payload (or a list not yet cached) falls back to one re-read.
      const held = queryClient.getQueryData<AgentProfile[]>(queryKeys.profileList());
      // An *update* for a profile the cache does not hold (it was deleted just before, or the list is
      // stale) is settled by one re-read instead of resurrecting it, as with account.usage.
      const known = topic === "profile.created" || held?.some((p) => isProfile(profile) && p.ID === profile.ID);
      if (isProfile(profile) && held !== undefined && known) {
        upsertProfileInCache(queryClient, profile);
      } else {
        void queryClient.invalidateQueries({ queryKey: queryKeys.profileList() });
      }
      return;
    }
    case "profile.deleted": {
      const id = (payload as { id?: unknown } | null)?.id;
      if (typeof id === "number" && queryClient.getQueryData(queryKeys.profileList()) !== undefined) {
        queryClient.setQueryData<AgentProfile[]>(queryKeys.profileList(), (held) => held?.filter((p) => p.ID !== id));
      } else {
        void queryClient.invalidateQueries({ queryKey: queryKeys.profileList() });
      }
      return;
    }
    case "learnedSkill.proposed": {
      const p = payload as Partial<LearnedSkillProposedPayload> | null;
      invalidateLearning(queryClient, p?.workspaceId);
      // Announced, never interrupting: a run's completion is not blocked by it (ADR-0026 AC20).
      toast({
        title: `New skill proposal: ${p?.name ?? "untitled"}`,
        durationMs: 8000,
        action: {
          label: "Review",
          onClick: () =>
            window.dispatchEvent(
              new CustomEvent("smind:open-settings", { detail: { sectionId: LEARNED_SKILLS_SECTION_ID } }),
            ),
        },
      });
      return;
    }
    case "learnedSkill.changed": {
      const p = payload as Partial<LearnedSkillChangedPayload> | null;
      invalidateLearning(queryClient, p?.workspaceId);
      if (typeof p?.skillId === "number") {
        void queryClient.invalidateQueries({ queryKey: queryKeys.learnedSkill(p.skillId) });
      } else {
        void queryClient.invalidateQueries({ queryKey: queryKeys.learnedSkillAll() });
      }
      if (typeof p?.proposalId === "number") {
        void queryClient.invalidateQueries({ queryKey: queryKeys.proposal(p.proposalId) });
      } else {
        void queryClient.invalidateQueries({ queryKey: queryKeys.proposalAll() });
      }
      return;
    }
    case "learning.job": {
      const p = payload as Partial<LearningJobPayload> | null;
      // A finished job changes what the workspace has spent, which the status entry carries.
      void queryClient.invalidateQueries({ queryKey: queryKeys.learningStatus() });
      void queryClient.invalidateQueries({
        queryKey: typeof p?.workspaceId === "number" ? queryKeys.learningJobsOf(p.workspaceId) : queryKeys.learningJobsAll(),
      });
      return;
    }
    default:
      return;
  }
}

function isProfile(value: unknown): value is AgentProfile {
  return typeof value === "object" && value !== null && typeof (value as AgentProfile).ID === "number";
}

/** The workspace's skills, proposals and status counts; every workspace's when the payload names none. */
function invalidateLearning(queryClient: QueryClient, workspaceId: number | undefined): void {
  void queryClient.invalidateQueries({ queryKey: queryKeys.learningStatus() });
  if (typeof workspaceId === "number") {
    void queryClient.invalidateQueries({ queryKey: queryKeys.learnedSkillsOf(workspaceId) });
    void queryClient.invalidateQueries({ queryKey: queryKeys.proposalsOf(workspaceId) });
  } else {
    void queryClient.invalidateQueries({ queryKey: queryKeys.learnedSkillsAll() });
    void queryClient.invalidateQueries({ queryKey: queryKeys.proposalsAll() });
  }
}

function upsertById(list: Account[], account: Account): Account[] {
  const index = list.findIndex((existing) => existing.id === account.id);
  if (index === -1) return [...list, account];
  const next = list.slice();
  next[index] = account;
  return next;
}
