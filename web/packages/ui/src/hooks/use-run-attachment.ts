import { useQuery } from "@tanstack/react-query";
import { useEffect, useState } from "react";

import { useDaemonConnection } from "@/lib/query/daemon-query-provider";
import { queryKeys } from "@/lib/query-keys";
import type { RunAttachmentResult } from "@/lib/types";

/** Decoded once into a Blob, so the cache holds bytes rather than a 13 MB base64 string. */
function toBlob(result: RunAttachmentResult): Blob {
  const binary = atob(result.data);
  const bytes = new Uint8Array(binary.length);
  for (let i = 0; i < binary.length; i++) bytes[i] = binary.charCodeAt(i);
  return new Blob([bytes], { type: result.mimeType });
}

/**
 * The bytes of one stored image attachment (`run.attachment`), as an object URL for a thumbnail. An attachment is
 * immutable, so it is cached forever per (run, id) (`staleTime: Infinity`, 10 min after the last thumbnail
 * unmounts); `enabled` is the caller's "on screen" latch, so nothing is fetched for a thumbnail nobody scrolled to.
 * The object URL is made and revoked here, per mounted thumbnail.
 */
export function useRunAttachment(runId: string, id: string, enabled: boolean): { url: string | null; loading: boolean; error: boolean } {
  const { client } = useDaemonConnection();
  const query = useQuery({
    queryKey: queryKeys.runAttachment(runId, id),
    enabled: enabled && client !== null,
    staleTime: Infinity,
    gcTime: 10 * 60 * 1000,
    retry: false,
    queryFn: async () => {
      if (!client) throw new Error("no daemon connection");
      return toBlob(await client.call<RunAttachmentResult>("run.attachment", { runId, id }));
    },
  });
  const blob = query.data ?? null;
  const [url, setUrl] = useState<string | null>(null);
  useEffect(() => {
    if (!blob) return;
    const next = URL.createObjectURL(blob);
    setUrl(next);
    return () => {
      URL.revokeObjectURL(next);
      setUrl(null);
    };
  }, [blob]);
  return { url, loading: enabled && !query.isError && (query.isPending || (blob !== null && url === null)), error: query.isError };
}
