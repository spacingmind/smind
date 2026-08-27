import { WsClient, daemonWsUrl } from "@/lib/ws-client";

/**
 * Fetches the daemon's current auth token from the same-origin GET
 * /api/token endpoint. That endpoint carries no auth check of its own --
 * see the endpoint's own doc comment (internal/server/server.go) for why
 * that doesn't introduce a new trust boundary -- so the page's own JS can
 * always read it, letting the web UI open /ws without the user pasting a
 * token in manually.
 */
export async function fetchToken(): Promise<string> {
  const res = await fetch("/api/token");
  if (!res.ok) {
    throw new Error(`/api/token: unexpected status ${res.status}`);
  }
  const body = (await res.json()) as { token: string };
  return body.token;
}

/** Fetches the daemon's token and opens a ready-to-use /ws connection. */
export async function connectDaemon(): Promise<WsClient> {
  const token = await fetchToken();
  return WsClient.connect(daemonWsUrl(token));
}
