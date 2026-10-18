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
  /** True from `cancel()` until the aborted call's promise settles -- Connect stays disabled meanwhile. */
  cancelling: boolean;
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
  const [cancelling, setCancelling] = useState(false);
  // An accepted account.oauthComplete leaves the login waiting for its
  // result; this flag keeps Finish disabled (as `pasting`) until then, so a
  // second Enter/click can't send a second paste.
  const [pasteSubmitted, setPasteSubmitted] = useState(false);
  const pasteSubmittedRef = useRef(false);
  const genRef = useRef(0);
  const controllerRef = useRef<AbortController | null>(null);
  const openedForRef = useRef<string | null>(null);
  const onConnectedRef = useRef(opts?.onConnected);
  onConnectedRef.current = opts?.onConnected;

  function start(params: OAuthStartParams) {
    const gen = ++genRef.current;
    setCancelling(false);
    controllerRef.current?.abort();
    const controller = new AbortController();
    controllerRef.current = controller;
    setPasteError(null);
    pasteSubmittedRef.current = false;
    setPasteSubmitted(false);
    setPasting(false);
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
          if (gen !== genRef.current) {
            // Cancelled but the account was still created: cache it and
            // report it, without changing the visible (idle) phase.
            if (controller.signal.aborted) {
              upsertAccountInCache(queryClient, result);
              onConnectedRef.current?.(result);
            }
            return;
          }
          upsertAccountInCache(queryClient, result);
          setState({ phase: "done", result });
          onConnectedRef.current?.(result);
        },
        (err) => {
          if (gen !== genRef.current || controller.signal.aborted) return;
          const { code, detail } = parseLoginError(err);
          setState({ phase: "error", code, message: loginErrorMessage(code, detail), soft: isSoftLoginError(code) });
        },
      )
      .finally(() => {
        if (gen !== genRef.current) setCancelling(false);
      });
  }

  async function complete(redirectUrl: string) {
    if (state.phase !== "waiting") return;
    if (pasteSubmittedRef.current) return;
    if (!client) return;
    const gen = genRef.current;
    const { loginId } = state;
    setPasteError(null);
    setPasting(true);
    let resolved = false;
    try {
      await client.call("account.oauthComplete", { loginId, redirectUrl });
      resolved = true;
    } catch (err) {
      // A cancelled or superseded login's late rejection must not touch
      // state -- only a rejection for the still-current waiting login does.
      if (gen !== genRef.current || state.phase !== "waiting" || state.loginId !== loginId) return;
      const { code, detail } = parseLoginError(err);
      if (code === "login_unknown" || code === "login_already_completed") {
        setState({ phase: "error", code, message: loginErrorMessage(code, detail), soft: false });
      } else {
        setPasteError(loginErrorMessage(code, detail));
      }
      setPasting(false);
      return;
    }
    if (gen !== genRef.current || !resolved) return;
    // Accepted: keep Finish disabled (as `pasting`) until the login leaves
    // waiting -- its result is what ends the login. A second paste is a
    // no-op while this is set.
    pasteSubmittedRef.current = true;
    setPasteSubmitted(true);
    setPasting(false);
  }

  function cancel() {
    genRef.current++;
    controllerRef.current?.abort();
    setCancelling(true);
    setState({ phase: "idle" });
    setPasteError(null);
    pasteSubmittedRef.current = false;
    setPasteSubmitted(false);
    setPasting(false);
  }

  function reset() {
    setState({ phase: "idle" });
    setPasteError(null);
    setCancelling(false);
    pasteSubmittedRef.current = false;
    setPasteSubmitted(false);
    setPasting(false);
  }

  useEffect(
    () => () => {
      genRef.current++;
      controllerRef.current?.abort();
    },
    [],
  );

  return { state, pasteError, pasting: pasting || pasteSubmitted, cancelling, start, complete, cancel, reset };
}
