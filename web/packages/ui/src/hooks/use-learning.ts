import { useMutation, useQuery, useQueryClient, type QueryClient } from "@tanstack/react-query";

import type {
  ApproveProposalParams,
  GateFinding,
  JobView,
  LearnedSkillDetail,
  LearnedSkillStatus,
  LearnedSkillView,
  LearningMode,
  LearningStatus,
  ProposalDetail,
  ProposalStatus,
  ProposalView,
} from "@/lib/learning-types";
import { useDaemonConnection } from "@/lib/query/daemon-query-provider";
import { queryKeys } from "@/lib/query-keys";
import { RpcError, type WsClient } from "@/lib/ws-client";

/**
 * Self-learning skills (ADR-0026) on the shared React Query cache (ADR-0032): one entry per
 * `learning.*` RPC and its params, kept live by the `learnedSkill.*` / `learning.job` events
 * through the event bridge (`lib/query/event-bridge.ts`), never by components subscribing to
 * refetch. Mutations invalidate exactly the keys they can change; the event the daemon also
 * publishes then settles any other client's view.
 */

function useClient(): WsClient | null {
  return useDaemonConnection().client;
}

function requireClient(client: WsClient | null): WsClient {
  if (!client) throw new Error("no daemon connection");
  return client;
}

/** Global enabled flag plus per-workspace mode and counts. */
export function useLearningStatus() {
  const client = useClient();
  return useQuery({
    queryKey: queryKeys.learningStatus(),
    enabled: client !== null,
    retry: false,
    queryFn: (): Promise<LearningStatus> => requireClient(client).call<LearningStatus>("learning.status", {}),
  });
}

/** The workspace's learned skills in the given statuses (all when omitted). */
export function useLearnedSkills(
  workspaceId: number | null,
  statuses?: readonly LearnedSkillStatus[],
  options: { includeBody?: boolean } = {},
) {
  const client = useClient();
  const includeBody = options.includeBody === true;
  return useQuery({
    queryKey: queryKeys.learnedSkills(workspaceId ?? 0, statuses, includeBody),
    enabled: client !== null && workspaceId !== null,
    retry: false,
    queryFn: async (): Promise<LearnedSkillView[]> =>
      (await requireClient(client).call<LearnedSkillView[] | null>("learning.skills.list", {
        workspaceId,
        ...(statuses ? { statuses } : {}),
        ...(includeBody ? { includeBody } : {}),
      })) ?? [],
  });
}

/** One skill with its versions and ledger. */
export function useLearnedSkill(id: number | null) {
  const client = useClient();
  return useQuery({
    queryKey: queryKeys.learnedSkill(id ?? 0),
    enabled: client !== null && id !== null,
    retry: false,
    queryFn: (): Promise<LearnedSkillDetail> => requireClient(client).call<LearnedSkillDetail>("learning.skills.get", { id }),
  });
}

/** The workspace's proposals, optionally filtered by status. */
export function useProposals(workspaceId: number | null, status?: ProposalStatus) {
  const client = useClient();
  return useQuery({
    queryKey: queryKeys.proposals(workspaceId ?? 0, status),
    enabled: client !== null && workspaceId !== null,
    retry: false,
    queryFn: async (): Promise<ProposalView[]> =>
      (await requireClient(client).call<ProposalView[] | null>("learning.proposals.list", {
        workspaceId,
        ...(status ? { status } : {}),
      })) ?? [],
  });
}

/** One proposal plus, for a pending create that duplicates a learned skill, the merge target. */
export function useProposal(id: number | null) {
  const client = useClient();
  return useQuery({
    queryKey: queryKeys.proposal(id ?? 0),
    enabled: client !== null && id !== null,
    retry: false,
    queryFn: (): Promise<ProposalDetail> => requireClient(client).call<ProposalDetail>("learning.proposals.get", { id }),
  });
}

/** Recent reflect/curate jobs of a workspace. */
export function useLearningJobs(workspaceId: number | null, limit?: number) {
  const client = useClient();
  return useQuery({
    queryKey: queryKeys.learningJobs(workspaceId ?? 0, limit),
    enabled: client !== null && workspaceId !== null,
    retry: false,
    queryFn: async (): Promise<JobView[]> =>
      (await requireClient(client).call<JobView[] | null>("learning.jobs.list", {
        workspaceId,
        ...(limit ? { limit } : {}),
      })) ?? [],
  });
}

/**
 * Pending proposals across workspaces (or one) from the shared status entry: what the Settings
 * badge shows. 0 while unknown, so a badge never flashes in on load.
 */
export function usePendingProposalCount(workspaceId?: number): number {
  const status = useLearningStatus().data;
  if (!status || !status.enabled) return 0;
  return status.workspaces
    .filter((w) => workspaceId === undefined || w.id === workspaceId)
    .reduce((sum, w) => sum + w.pendingProposals, 0);
}

/** The `findings` a `gate_rejected` approve carries, or null for any other error. */
export function gateFindings(err: unknown): GateFinding[] | null {
  if (!(err instanceof RpcError) || err.code !== "gate_rejected") return null;
  const findings = (err.data as { findings?: unknown } | undefined)?.findings;
  if (!Array.isArray(findings)) return [];
  return findings
    .filter((f): f is GateFinding => typeof f === "object" && f !== null && typeof (f as GateFinding).reason === "string")
    .map((f) => ({ reason: f.reason, detail: typeof f.detail === "string" ? f.detail : "" }));
}

/** True for the `stale` / `conflict` kinds a landing re-check returns. */
export function isStaleError(err: unknown): boolean {
  return err instanceof RpcError && (err.code === "stale" || err.code === "conflict");
}

function invalidateSkill(queryClient: QueryClient, workspaceId: number | undefined, skillId?: number): void {
  void queryClient.invalidateQueries({ queryKey: queryKeys.learningStatus() });
  void queryClient.invalidateQueries({
    queryKey: workspaceId === undefined ? queryKeys.learnedSkillsAll() : queryKeys.learnedSkillsOf(workspaceId),
  });
  void queryClient.invalidateQueries({
    queryKey: skillId === undefined ? queryKeys.learnedSkillAll() : queryKeys.learnedSkill(skillId),
  });
}

type SkillAction = "pin" | "unpin" | "disable" | "enable" | "archive" | "restore" | "delete";

/** `learning.skills.<action> {id}` for one skill; `workspaceId` scopes the list invalidation. */
export function useSkillAction(action: SkillAction) {
  const client = useClient();
  const queryClient = useQueryClient();
  return useMutation<unknown, Error, { id: number; workspaceId?: number }>({
    mutationFn: ({ id }) => requireClient(client).call(`learning.skills.${action}`, { id }),
    onSuccess: (_result, { id, workspaceId }) => invalidateSkill(queryClient, workspaceId, id),
  });
}

/** `learning.skills.undo {id, version}`: restore the body/description of an earlier version as a new one. */
export function useUndoSkill() {
  const client = useClient();
  const queryClient = useQueryClient();
  return useMutation<unknown, Error, { id: number; version: number; workspaceId?: number }>({
    mutationFn: ({ id, version }) => requireClient(client).call("learning.skills.undo", { id, version }),
    onSuccess: (_result, { id, workspaceId }) => invalidateSkill(queryClient, workspaceId, id),
  });
}

function invalidateProposals(queryClient: QueryClient, workspaceId: number | undefined, proposalId: number): void {
  void queryClient.invalidateQueries({ queryKey: queryKeys.learningStatus() });
  void queryClient.invalidateQueries({
    queryKey: workspaceId === undefined ? queryKeys.proposalsAll() : queryKeys.proposalsOf(workspaceId),
  });
  void queryClient.invalidateQueries({ queryKey: queryKeys.proposal(proposalId) });
}

/**
 * `learning.proposals.approve`. A `gate_rejected` / `stale` error leaves the proposal pending and
 * is surfaced to the caller (`gateFindings`, `isStaleError`); success also changes the skills.
 */
export function useApproveProposal() {
  const client = useClient();
  const queryClient = useQueryClient();
  return useMutation<unknown, Error, ApproveProposalParams & { workspaceId?: number }>({
    mutationFn: ({ workspaceId: _workspaceId, ...params }) => requireClient(client).call("learning.proposals.approve", params),
    onSuccess: (_result, { id, workspaceId }) => {
      invalidateProposals(queryClient, workspaceId, id);
      invalidateSkill(queryClient, workspaceId);
    },
    onError: (err, { id, workspaceId }) => {
      // A stale landing means the target moved: re-read the proposal and its skills so the pane says so.
      if (isStaleError(err)) {
        invalidateProposals(queryClient, workspaceId, id);
        invalidateSkill(queryClient, workspaceId);
      }
    },
  });
}

/** `learning.proposals.reject {id, note?}`. */
export function useRejectProposal() {
  const client = useClient();
  const queryClient = useQueryClient();
  return useMutation<unknown, Error, { id: number; note?: string; workspaceId?: number }>({
    mutationFn: ({ id, note }) => requireClient(client).call("learning.proposals.reject", { id, ...(note ? { note } : {}) }),
    onSuccess: (_result, { id, workspaceId }) => invalidateProposals(queryClient, workspaceId, id),
  });
}

/** `learning.setMode {workspaceId, mode}`. */
export function useSetLearningMode() {
  const client = useClient();
  const queryClient = useQueryClient();
  return useMutation<unknown, Error, { workspaceId: number; mode: LearningMode }>({
    mutationFn: (vars) => requireClient(client).call("learning.setMode", vars),
    onSuccess: () => void queryClient.invalidateQueries({ queryKey: queryKeys.learningStatus() }),
  });
}

/** `learning.forget {workspaceId}`: hard-deletes everything learned for the workspace. */
export function useForgetLearning() {
  const client = useClient();
  const queryClient = useQueryClient();
  return useMutation<unknown, Error, { workspaceId: number }>({
    mutationFn: (vars) => requireClient(client).call("learning.forget", vars),
    onSuccess: (_result, { workspaceId }) => {
      void queryClient.invalidateQueries({ queryKey: queryKeys.learningStatus() });
      void queryClient.invalidateQueries({ queryKey: queryKeys.learnedSkillsOf(workspaceId) });
      void queryClient.invalidateQueries({ queryKey: queryKeys.proposalsOf(workspaceId) });
      void queryClient.invalidateQueries({ queryKey: queryKeys.learningJobsOf(workspaceId) });
      void queryClient.removeQueries({ queryKey: queryKeys.learnedSkillAll() });
      void queryClient.removeQueries({ queryKey: queryKeys.proposalAll() });
    },
  });
}

/** `learning.reflect {chatId}`: run the reflector on one chat now. */
export function useReflect(workspaceId?: number) {
  const client = useClient();
  const queryClient = useQueryClient();
  return useMutation<unknown, Error, { chatId: number }>({
    mutationFn: (vars) => requireClient(client).call("learning.reflect", vars),
    onSuccess: () =>
      void queryClient.invalidateQueries({
        queryKey: workspaceId === undefined ? queryKeys.learningJobsAll() : queryKeys.learningJobsOf(workspaceId),
      }),
  });
}

/** `learning.curate {workspaceId}`: run the curator now. */
export function useCurate() {
  const client = useClient();
  const queryClient = useQueryClient();
  return useMutation<unknown, Error, { workspaceId: number }>({
    mutationFn: (vars) => requireClient(client).call("learning.curate", vars),
    onSuccess: (_result, { workspaceId }) =>
      void queryClient.invalidateQueries({ queryKey: queryKeys.learningJobsOf(workspaceId) }),
  });
}
