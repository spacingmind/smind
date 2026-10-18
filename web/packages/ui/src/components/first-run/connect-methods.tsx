import { useCallback, useState } from "react";

import { AddRelayLink } from "@/components/settings/add-relay-link";
import { SshConnectForm } from "@/components/first-run/ssh-form";
import { UrlConnectForm, type ConnectMethodId } from "@/components/first-run/url-form";
import type { Connection } from "@/lib/platform";

export type { ConnectMethodId };

export const CONNECT_METHOD_LABELS: Record<ConnectMethodId, string> = {
  url: "Connect to a daemon by URL",
  ssh: "Connect over SSH",
  relay: "Pair via relay link",
};

/** The relay flow's hand-over state: a pairing link pasted into the URL form, `key` remounting the flow for each. */
export interface RelayHandover {
  key: number;
  link: string;
}

/**
 * Owns which "add a connection" method is showing and the relay hand-over, for hosts that
 * render the methods in separate places (first-run's radio cards, Settings' Add menu).
 */
export function useConnectMethod(initial: ConnectMethodId | null = null) {
  const [method, setMethod] = useState<ConnectMethodId | null>(initial);
  const [handover, setHandover] = useState<RelayHandover | null>(null);
  const [notice, setNotice] = useState(false);

  const handOverLink = useCallback((link: string) => {
    setHandover({ key: Date.now(), link });
    setNotice(true);
    setMethod("relay");
  }, []);

  return {
    method,
    setMethod,
    handover,
    notice,
    handOverLink,
    clearNotice: useCallback(() => setNotice(false), []),
    linkTaken: useCallback(() => setHandover((h) => (h ? { ...h, link: "" } : h)), []),
  };
}

export type ConnectMethodState = ReturnType<typeof useConnectMethod>;

/**
 * The body of one add-a-connection method -- the SAME components first-run and
 * Settings -> Connections both show (ADR-0031 D7: "same components as first-run").
 */
export function ConnectMethodPanel({
  method,
  state,
  onConnected,
}: {
  method: ConnectMethodId;
  state: ConnectMethodState;
  /** Called once the connection exists (saved; for relay also selected). The host decides what happens next. */
  onConnected: (conn: Connection) => void | Promise<void>;
}) {
  if (method === "ssh") return <SshConnectForm onConnected={onConnected} />;
  if (method === "relay") {
    return (
      <div className="flex flex-col gap-2" data-testid="connect-method-relay">
        {state.notice && (
          <p role="status" data-testid="connection-relay-handover" className="text-ui-base text-muted-foreground">
            That is a relay pairing link, not a daemon URL. Nothing was saved from the URL form; review it here instead.
          </p>
        )}
        <AddRelayLink
          key={state.handover?.key ?? "paste"}
          initialLink={state.handover?.link}
          onCancel={state.clearNotice}
          onInitialLinkTaken={state.linkTaken}
          onAdded={(conn) => {
            state.clearNotice();
            return onConnected(conn);
          }}
        />
      </div>
    );
  }
  return <UrlConnectForm onConnected={onConnected} onRelayLink={state.handOverLink} onPickMethod={state.setMethod} />;
}
