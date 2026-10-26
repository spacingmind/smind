import { useInfiniteQuery, useQuery, useQueryClient, type InfiniteData, type QueryClient } from "@tanstack/react-query";

import {
  AUTOMATION_PAGE_SIZE,
  type AutomationAuditFilter,
  type AutomationAuditRow,
  type AutomationDef,
  type AutomationListing,
  type AutomationView,
} from "@/lib/automation-types";
import { useDaemonConnection } from "@/lib/query/daemon-query-provider";
import { queryKeys } from "@/lib/query-keys";

/**
 * The automation list and the daemon's two switches (`automation.list`, ADR-0044), ONE cache entry per connection:
 * Settings -> Automations reads it, the event bridge refetches it on `automation.fired/paused/updated`, and the
 * mutations below write the RPC's own answer into it (upsert by id) so a row changes in place before the event
 * arrives. Reads are open to relay sessions.
 */
export function useAutomations() {
  const { client } = useDaemonConnection();
  return useQuery({
    queryKey: queryKeys.automationList(),
    enabled: client !== null,
    staleTime: Infinity,
    retry: false,
    retryOnMount: false,
    queryFn: async (): Promise<AutomationListing> => {
      if (!client) throw new Error("no daemon connection");
      const listing = await client.call<AutomationListing>("automation.list");
      return { enabled: listing?.enabled === true, paused: listing?.paused === true, automations: listing?.automations ?? [] };
    },
  });
}

/**
 * The audit rows of one automation, newest first, paged with `before` = the last loaded row's id. `filter.decision`
 * narrows the loaded rows (the RPC has no decision parameter); `hasMore` is true while the last page was full.
 */
export function useAutomationAudit(id: number, filter: AutomationAuditFilter = {}) {
  const { client } = useDaemonConnection();
  const decision = filter.decision ?? "";
  const query = useInfiniteQuery({
    queryKey: queryKeys.automationAudit(id),
    enabled: client !== null,
    staleTime: Infinity,
    retry: false,
    retryOnMount: false,
    initialPageParam: 0,
    queryFn: async ({ pageParam }): Promise<AutomationAuditRow[]> => {
      if (!client) throw new Error("no daemon connection");
      const answer = await client.call<{ rows: AutomationAuditRow[] }>("automation.audit", {
        id,
        limit: AUTOMATION_PAGE_SIZE,
        before: pageParam,
      });
      return answer?.rows ?? [];
    },
    getNextPageParam: (last) => (last.length >= AUTOMATION_PAGE_SIZE ? last[last.length - 1].id : undefined),
  });
  const all = (query.data as InfiniteData<AutomationAuditRow[]> | undefined)?.pages.flat();
  const rows = all === undefined ? undefined : decision === "" ? all : all.filter((r) => r.decision === decision);
  return { ...query, rows, loadedCount: all?.length ?? 0 };
}

/** `automation.memory.get`: the automation's memory text (empty when it has none). */
export function useAutomationMemory(id: number, enabled: boolean) {
  const { client } = useDaemonConnection();
  return useQuery({
    queryKey: queryKeys.automationMemory(id),
    enabled: client !== null && enabled,
    staleTime: Infinity,
    retry: false,
    retryOnMount: false,
    queryFn: async (): Promise<string> => {
      if (!client) throw new Error("no daemon connection");
      return (await client.call<{ memory: string }>("automation.memory.get", { id }))?.memory ?? "";
    },
  });
}

function upsertAutomation(queryClient: QueryClient, automation: AutomationView): void {
  queryClient.setQueryData<AutomationListing>(queryKeys.automationList(), (held) => {
    if (!held) return held;
    const index = held.automations.findIndex((a) => a.id === automation.id);
    if (index === -1) return { ...held, automations: [...held.automations, automation] };
    const automations = held.automations.slice();
    automations[index] = automation;
    return { ...held, automations };
  });
}

/** The automation.* mutations. Each resolves with the daemon's answer (or rejects with its own typed, human-readable message). */
export function useAutomationActions() {
  const { client } = useDaemonConnection();
  const queryClient = useQueryClient();
  const call = async <T,>(method: string, params: unknown): Promise<T> => {
    if (!client) throw new Error("no daemon connection");
    try {
      return await client.call<T>(method, params);
    } catch (err) {
      // A refusal (`forbidden`) changed nothing, so it costs no round trip; any other failure may have raced a change.
      if ((err as { code?: string } | null)?.code !== "forbidden") void queryClient.invalidateQueries({ queryKey: queryKeys.automationList() });
      throw err;
    }
  };
  const view = async (method: string, params: unknown): Promise<AutomationView> => {
    const automation = await call<AutomationView>(method, params);
    upsertAutomation(queryClient, automation);
    return automation;
  };
  return {
    /** Always creates a DISABLED automation. */
    create: (def: AutomationDef) => view("automation.create", def),
    update: (id: number, def: AutomationDef) => view("automation.update", { id, ...def }),
    delete: async (id: number): Promise<void> => {
      await call("automation.delete", { id });
      queryClient.setQueryData<AutomationListing>(queryKeys.automationList(), (held) =>
        held ? { ...held, automations: held.automations.filter((a) => a.id !== id) } : held,
      );
      queryClient.removeQueries({ queryKey: queryKeys.automationAudit(id) });
      queryClient.removeQueries({ queryKey: queryKeys.automationMemory(id) });
    },
    enable: (id: number) => view("automation.enable", { id }),
    /** Open to a relay session (a brake). */
    disable: (id: number) => view("automation.disable", { id }),
    resume: (id: number) => view("automation.resume", { id }),
    /** The engine's audit row for the manual fire: a capped / deduped / paused answer is a decision, not a failure. */
    fire: async (id: number): Promise<AutomationAuditRow> => {
      const row = await call<AutomationAuditRow>("automation.fire", { id });
      void queryClient.invalidateQueries({ queryKey: queryKeys.automationAudit(id) });
      void queryClient.invalidateQueries({ queryKey: queryKeys.automationList() });
      return row;
    },
    memoryGet: async (id: number): Promise<string> => (await call<{ memory: string }>("automation.memory.get", { id }))?.memory ?? "",
    memoryClear: async (id: number): Promise<void> => {
      await call("automation.memory.clear", { id });
      queryClient.setQueryData<string>(queryKeys.automationMemory(id), "");
    },
  };
}
