import { useDefaultAgentStore } from "@/stores/default-agent-store";

/** Settings -> Agents' ★ default agent, from the shared store (see `stores/default-agent-store.ts`). */
export function useDefaultAgentId(): {
  defaultAgentId: string | null;
  setDefaultAgentId: (id: string | null) => void;
} {
  const defaultAgentId = useDefaultAgentStore((s) => s.value);
  const setDefaultAgentId = useDefaultAgentStore((s) => s.set);
  return { defaultAgentId, setDefaultAgentId };
}
