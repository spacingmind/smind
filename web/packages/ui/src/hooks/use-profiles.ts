import { useMutation, useQuery, useQueryClient, type QueryClient } from "@tanstack/react-query";

import { useDaemonConnection } from "@/lib/query/daemon-query-provider";
import { queryKeys } from "@/lib/query-keys";
import type { AgentProfile } from "@/lib/types";

/**
 * The agent-profile list, ONE cache entry per connection (ADR-0032 AC11):
 * sidebar palette entries, task header pills, the composer picker and
 * Settings -> Agents all read the same entry, kept live by
 * `profile.created`/`profile.updated`/`profile.deleted` through the event
 * bridge and by the mutations below (upsert-by-ID, so the event arriving too
 * is idempotent). A profile created, renamed or deleted in Settings therefore
 * reaches every other surface without a reconnect.
 */

/** The profile list. Loads once per connection; events and mutations keep it live. */
export function useProfiles() {
  const { client } = useDaemonConnection();
  return useQuery({
    queryKey: queryKeys.profileList(),
    enabled: client !== null,
    staleTime: Infinity,
    retry: false,
    retryOnMount: false,
    queryFn: async (): Promise<AgentProfile[]> => {
      if (!client) throw new Error("no daemon connection");
      return (await client.call<AgentProfile[]>("profile.list")) ?? [];
    },
  });
}

const NO_PROFILES: AgentProfile[] = [];

/** The cached profiles, `[]` (one stable array) until they load or if the list failed. */
export function useProfileList(): AgentProfile[] {
  return useProfiles().data ?? NO_PROFILES;
}

/** Writes a `profile.*` mutation's or event's profile into the cached list (upsert by ID). */
export function upsertProfileInCache(queryClient: QueryClient, profile: AgentProfile): void {
  queryClient.setQueryData<AgentProfile[]>(queryKeys.profileList(), (held) => {
    // No cached list: leave it to the first read (a one-element list would pass for the whole answer).
    if (!held) return held;
    const index = held.findIndex((existing) => existing.ID === profile.ID);
    if (index === -1) return [...held, profile];
    const next = held.slice();
    next[index] = profile;
    return next;
  });
}

interface ProfileParams {
  name: string;
  provider: string;
  permissionMode: string;
  autoAccept: boolean;
  thinkingLevel: string;
  model: string;
  notes: string;
  /** ADR-0041 seat contract and failover. profile.update is a FULL REPLACE: omitting these clears them, so the editor always sends all of them. */
  onRateLimit?: string;
  fallbackProfileId?: number | null;
  /** null = unmanaged. */
  orchestrationTools?: boolean | null;
  instructions?: string;
  skills?: string[];
  childPolicy?: string;
  childProfileIds?: number[];
}

/** `profile.create` {name, provider, ...} -> the saved profile, upserted into the cache. */
export function useCreateProfile() {
  const { client } = useDaemonConnection();
  const queryClient = useQueryClient();
  return useMutation<AgentProfile, Error, ProfileParams>({
    mutationFn: async (params) => {
      if (!client) throw new Error("no daemon connection");
      return client.call<AgentProfile>("profile.create", params);
    },
    onSuccess: (saved) => upsertProfileInCache(queryClient, saved),
  });
}

/** `profile.update` {id, name, provider, ...} -> the saved profile, upserted into the cache. */
export function useUpdateProfile() {
  const { client } = useDaemonConnection();
  const queryClient = useQueryClient();
  return useMutation<AgentProfile, Error, { id: number } & ProfileParams>({
    mutationFn: async ({ id, ...params }) => {
      if (!client) throw new Error("no daemon connection");
      return client.call<AgentProfile>("profile.update", { id, ...params });
    },
    onSuccess: (saved) => upsertProfileInCache(queryClient, saved),
  });
}

/** `profile.delete` {id}; drops the profile from the cached list. */
export function useDeleteProfile() {
  const { client } = useDaemonConnection();
  const queryClient = useQueryClient();
  return useMutation<null, Error, number>({
    mutationFn: async (id) => {
      if (!client) throw new Error("no daemon connection");
      return client.call<null>("profile.delete", { id });
    },
    onSuccess: (_result, id) => {
      queryClient.setQueryData<AgentProfile[]>(queryKeys.profileList(), (held) =>
        held ? held.filter((p) => p.ID !== id) : held,
      );
    },
  });
}
