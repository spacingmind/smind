import { useEffect, useRef, useState } from "react";
import { useQueryClient } from "@tanstack/react-query";

import { upsertAccountInCache } from "@/hooks/use-accounts";
import { openExternalUrl } from "@/lib/external-link";
import { isDesktop } from "@/lib/platform";
import {
  isSoftLoginError,
  loginErrorMessage,
  parseLoginError,
  parseLoginEvent,
} from "@/lib/login-events";
import { useDaemonConnection } from "@/lib/query/daemon-query-provider";
import type { LoginMethod, OAuthStartParams, OAuthStartResult } from "@/lib/types";
import type { WsClientLike } from "@/lib/ws-client";

/**
 * ADR-0033's login state machine as a hook (providers-login-and-tab PR-B4):
 * one `account.oauthStart` streaming request per `start`, its authorize/
 * device events surfaced as `waiting`/`device` phases, `account.oauthComplete`
 * for the pasted redirect address, and a generation counter so a cancelled
 * or superseded login's late events/results can never touch state.
 *
 * The pasted redirect URL is only ever sent to `account.oauthComplete` --
 * it carries the OAuth code, so it is never kept in state or surfaced in
 * any message.
 */
export type LoginState =
  | { phase: "idle" }
  | { phase: "starting"; method: LoginMethod }
  | { phase: "waiting"; loginId: string; url: string; pasteOnly: boolean }
  | {
      phase: "device";
      loginId: string;
      verificationUrl: string;
      userCode: string;
      expiresAt: string;
      intervalSeconds: number;
    }
  | { phase: "done"; result: OAuthStartResult }
  | { phase: "error"; code: string; message: string; soft: boolean };

export interface AccountLogin {
  state: LoginState;
  /** Set when the last paste was rejected (login stays `waiting`); cleared on the next paste/start/cancel. */
  pasteError: string | null;
  pasting: boolean;
  start(params: OAuthStartParams): void;
  complete(redirectUrl: string): Promise<void>;
  cancel(): void;
  reset(): void;
}

export function useAccountLogin(opts?: {
  onConnected?: (result: OAuthStartResult) => void;
  /** An explicit client (the connect panel's prop) instead of the ambient connection; used even when null. */
  client?: WsClientLike | null;
}): AccountLogin {
  const ambient = useDaemonConnection().client;
  const client = opts && "client" in opts ? opts.client : ambient;
  const queryClient = useQueryClient();
  const [state, setState] = useState<LoginState>({ phase: "idle" });
  const [pasteError, setPasteError] = useState<string | null>(null);
  const [pasting, setPasting] = useState(false);
  const genRef = useRef(0);
  const controllerRef = useRef<AbortController | null>(null);
  const openedForRef = useRef<string | null>(null);
  const onConnectedRef = useRef(opts?.onConnected);
  onConnectedRef.current = opts?.onConnected;

  function start(params: OAuthStartParams) {
    const gen = ++genRef.current;
    controllerRef.current?.abort();
    const controller = new AbortController();
    controllerRef.current = controller;
    setPasteError(null);
    setState({ phase: "starting", method: params.method ?? "browser" });
    if (!client) {
      setState({ phase: "error", code: "no_connection", message: "No connection to the daemon.", soft: false });
      return;
    }
    client
      .callStream<OAuthStartResult>(
        "account.oauthStart",
        params,
        (event, p) => {
          if (gen !== genRef.current) return;
          const parsed = parseLoginEvent(event, p);
          if (!parsed) return;
          if (parsed.kind === "authorizeUrl") {
            setState({
              phase: "waiting",
              loginId: parsed.loginId,
              url: parsed.url,
              pasteOnly: parsed.pasteOnly,
            });
            if (isDesktop && openedForRef.current !== parsed.loginId) {
              openedForRef.current = parsed.loginId;
              openExternalUrl(parsed.url);
            }
          } else {
            setState({ phase: "device", loginId: parsed.loginId, verificationUrl: parsed.verificationUrl, userCode: parsed.userCode, expiresAt: parsed.expiresAt, intervalSeconds: parsed.intervalSeconds });
          }
        },
        { signal: controller.signal },
      )
      .then(
        (result) => {
          if (gen !== genRef.current) return;
          upsertAccountInCache(queryClient, result);
          setState({ phase: "done", result });
          onConnectedRef.current?.(result);
        },
        (err) => {
          if (gen !== genRef.current || controller.signal.aborted) return;
          const { code, detail } = parseLoginError(err);
          setState({ phase: "error", code, message: loginErrorMessage(code, detail), soft: isSoftLoginError(code) });
        },
      );
  }

  async function complete(redirectUrl: string) {
    if (state.phase !== "waiting") return;
    if (!client) return;
    setPasteError(null);
    setPasting(true);
    try {
      await client.call("account.oauthComplete", { loginId: state.loginId, redirectUrl });
    } catch (err) {
      const { code, detail } = parseLoginError(err);
      if (code === "login_unknown" || code === "login_already_completed") {
        setState({ phase: "error", code, message: loginErrorMessage(code, detail), soft: false });
      } else {
        setPasteError(loginErrorMessage(code, detail));
      }
    } finally {
      setPasting(false);
    }
  }

  function cancel() {
    genRef.current++;
    controllerRef.current?.abort();
    setState({ phase: "idle" });
    setPasteError(null);
  }

  function reset() {
    setState({ phase: "idle" });
    setPasteError(null);
  }

  useEffect(
    () => () => {
      genRef.current++;
      controllerRef.current?.abort();
    },
    [],
  );

  return { state, pasteError, pasting, start, complete, cancel, reset };
}
