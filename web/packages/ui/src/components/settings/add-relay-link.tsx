import { useEffect, useRef, useState, type FormEvent } from "react";

import { Alert } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { InlineSpinner } from "@/components/ui/inline-spinner";
import { Input } from "@/components/ui/input";
import { desktop, MAX_RELAY_LINK_LENGTH, type Connection, type RelayPreview } from "@/lib/platform";

/**
 * ADR-0031 P3: add a daemon through a relay pairing link, as paste -> preview
 * -> confirm -> save -> switch. Ported from Paseo's host-confirmation step
 * (`runtime/host-confirmation.ts`): nothing is saved until the user has seen
 * the relay address, the relay host id and the daemon-key fingerprint, and a
 * relay host that comes back with a different key or relay is a warning that
 * defaults to Cancel.
 *
 * The link carries the relay host's admission secret, so it is treated like a
 * password: masked while typed, never logged, never echoed in an error (Rust's
 * errors don't contain it), and dropped from state on success, failure,
 * cancel and unmount. The webview only ever sees `RelayPreview`'s non-secret
 * fields; the secret and key bytes stay in Rust.
 *
 * Shared by Settings -> Connections now and the first-run flow later
 * (ADR-0031 P5), hence the callbacks rather than any settings coupling.
 */
export function AddRelayLink({
  initialLink,
  onInitialLinkTaken,
  onAdded,
}: {
  /** A link already pasted elsewhere (the plain URL form); previewed straight away. */
  initialLink?: string;
  /** Called once the flow has copied `initialLink`, so the owner can stop holding the secret. */
  onInitialLinkTaken?: () => void;
  /** Called after the connection is saved and selected (or saved, if switching failed). */
  onAdded?: (conn: Connection) => void;
}) {
  const [link, setLink] = useState(initialLink ?? "");
  const [label, setLabel] = useState("");
  const [preview, setPreview] = useState<RelayPreview | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const mounted = useRef(true);

  useEffect(() => {
    mounted.current = true;
    return () => {
      mounted.current = false;
    };
  }, []);

  async function review(candidate: string) {
    setError(null);
    if (candidate.trim().length > MAX_RELAY_LINK_LENGTH) {
      setLink("");
      setError(`That is not a relay pairing link: it is longer than ${MAX_RELAY_LINK_LENGTH} characters.`);
      return;
    }
    setBusy(true);
    try {
      const result = await desktop.previewRelayLink(candidate);
      if (mounted.current) setPreview(result);
    } catch (err) {
      if (mounted.current) {
        setLink("");
        setError(err instanceof Error ? err.message : String(err));
      }
    } finally {
      if (mounted.current) setBusy(false);
    }
  }

  const started = useRef(false);
  useEffect(() => {
    if (started.current || !initialLink) return;
    started.current = true;
    onInitialLinkTaken?.();
    void review(initialLink);
    // eslint-disable-next-line react-hooks/exhaustive-deps -- once, for the link handed in at mount
  }, []);

  function cancel() {
    setLink("");
    setPreview(null);
    setError(null);
  }

  async function confirm() {
    setError(null);
    setBusy(true);
    let conn: Connection;
    try {
      conn = await desktop.addRelayConnection(label, link);
    } catch (err) {
      if (mounted.current) {
        setLink("");
        setPreview(null);
        setError(err instanceof Error ? err.message : String(err));
        setBusy(false);
      }
      return;
    }
    // Saved: the link is spent whatever happens next.
    if (mounted.current) {
      setLink("");
      setPreview(null);
    }
    try {
      await desktop.selectConnection(conn.id);
    } catch (err) {
      if (mounted.current) {
        setError(`Added "${conn.label}", but switching to it failed: ${err instanceof Error ? err.message : String(err)}`);
      }
    }
    if (mounted.current) {
      setLabel("");
      setBusy(false);
    }
    onAdded?.(conn);
  }

  function handleReview(e: FormEvent<HTMLFormElement>) {
    e.preventDefault();
    void review(link);
  }

  const errorNode = error && (
    <p role="alert" data-testid="relay-link-error" className="text-ui-base text-destructive">
      {error}
    </p>
  );

  if (preview) {
    const changed = preview.kind === "changedConnection";
    return (
      <div className="flex flex-col gap-3" data-testid="relay-link-confirm">
        {changed && (
          <Alert
            variant="warning"
            testId="relay-link-changed"
            title="This relay host's key or relay changed"
            description="A saved connection already uses this relay host id, but this link has a different daemon key or relay. Only continue if you re-paired the daemon yourself."
          />
        )}
        {preview.kind === "alreadySaved" && (
          <p data-testid="relay-link-already-saved" className="text-ui-base text-muted-foreground">
            This relay host is already saved with the same key and relay. Adding refreshes its stored link.
          </p>
        )}
        <dl className="grid grid-cols-[max-content_1fr] gap-x-4 gap-y-1 rounded-xl border bg-card p-3 text-ui-base">
          <dt className="text-muted-foreground">Relay address</dt>
          <dd data-testid="relay-link-address" className="truncate font-mono text-ui-sm">
            {preview.relayAddress}
          </dd>
          <dt className="text-muted-foreground">Relay host id</dt>
          <dd data-testid="relay-link-host-id" className="truncate font-mono text-ui-sm">
            {preview.relayHostId}
          </dd>
          <dt className="text-muted-foreground">Daemon key</dt>
          <dd data-testid="relay-link-fingerprint" className="font-mono text-ui-sm">
            {preview.keyFingerprint}
          </dd>
          <dt className="text-muted-foreground">Relay certificate</dt>
          <dd data-testid="relay-link-trust">{preview.pinned ? "Pinned by this link" : "Verified by a certificate authority"}</dd>
          <dt className="text-muted-foreground">Label</dt>
          <dd data-testid="relay-link-label" className="truncate">
            {label.trim() || `Relay (${preview.relayHostId})`}
          </dd>
        </dl>
        {preview.kind === "newHost" && (
          <p className="text-ui-base text-muted-foreground">
            Check that the daemon key matches what your daemon shows. Traffic to it is end-to-end encrypted; the relay cannot read it.
          </p>
        )}
        {errorNode}
        <div className="flex items-center gap-2">
          <Button
            variant={changed ? "default" : "outline"}
            size="sm"
            autoFocus={changed}
            data-testid="relay-link-cancel"
            disabled={busy}
            onClick={cancel}
          >
            Cancel
          </Button>
          <Button
            variant={changed ? "outline" : "default"}
            size="sm"
            data-testid="relay-link-add"
            disabled={busy}
            onClick={() => void confirm()}
          >
            {changed ? "Add anyway" : "Add"}
          </Button>
          {busy && <InlineSpinner />}
        </div>
      </div>
    );
  }

  return (
    <form className="flex flex-col gap-2" onSubmit={handleReview} data-testid="relay-link-form" autoComplete="off">
      <p className="text-ui-base text-muted-foreground">
        Paste the pairing link that <code className="font-mono text-ui-sm">smind relay offer</code> prints for your relay host.
      </p>
      <Alert
        variant="warning"
        testId="relay-link-warning"
        description="Treat this link like a password: anyone who has it can connect to that relay host. It is masked here, never logged, and discarded after use."
      />
      <Input
        aria-label="Relay pairing link"
        data-testid="relay-link-input"
        type="password"
        autoComplete="off"
        autoCorrect="off"
        autoCapitalize="off"
        spellCheck={false}
        data-1p-ignore
        data-lpignore="true"
        placeholder="Paste relay pairing link"
        value={link}
        onChange={(e) => setLink(e.target.value)}
      />
      <Input
        aria-label="Relay host label"
        data-testid="relay-link-label-input"
        autoComplete="off"
        placeholder="Label (optional)"
        value={label}
        onChange={(e) => setLabel(e.target.value)}
      />
      {errorNode}
      <div className="flex items-center gap-2">
        <Button type="submit" size="sm" data-testid="relay-link-review" disabled={busy || !link.trim()}>
          Review
        </Button>
        {busy && <InlineSpinner />}
      </div>
    </form>
  );
}
