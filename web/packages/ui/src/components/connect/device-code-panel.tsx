import { useEffect, useRef, useState, type JSX } from "react";

import { Button } from "@/components/ui/button";
import { Icons } from "@/lib/icons";
import { externalLinkProps } from "@/lib/external-link";

/** "Expires in m:ss" from a remaining-ms count; negative counts render as 0. */
function formatRemaining(ms: number): string {
  const total = Math.max(0, Math.floor(ms / 1000));
  const m = Math.floor(total / 60);
  const s = total % 60;
  return `${m}:${String(s).padStart(2, "0")}`;
}

/**
 * The device-code half of an OAuth connect (ADR-0033): the short user code
 * to enter at the verification page, a copy affordance, a live countdown to
 * expiresAt, and an anti-phishing reminder. Presentational -- the caller
 * owns opening the link and cancelling the login.
 */
export function DeviceCodePanel(props: {
  userCode: string;
  verificationUrl: string;
  /** RFC3339 timestamp of when the code stops being accepted. */
  expiresAt: string;
  now?: () => number;
  onCancel: () => void;
}): JSX.Element {
  const { userCode, verificationUrl, expiresAt, onCancel } = props;
  const now = props.now ?? Date.now;
  const [copied, setCopied] = useState(false);
  // NaN (an unparseable expiresAt) means "no countdown" rather than a
  // permanently-expired panel.
  const expiryMs = Date.parse(expiresAt);
  const [remainingMs, setRemainingMs] = useState(() =>
    Number.isNaN(expiryMs) ? NaN : Math.max(0, expiryMs - now()),
  );

  useEffect(() => {
    if (Number.isNaN(expiryMs)) return;
    const update = () => setRemainingMs(Math.max(0, expiryMs - now()));
    update();
    if (expiryMs - now() <= 0) return;
    const timer = setInterval(update, 1000);
    return () => clearInterval(timer);
  }, [expiryMs, now]);

  const expired = !Number.isNaN(remainingMs) && remainingMs <= 0;

  const copiedTimer = useRef<ReturnType<typeof setTimeout> | null>(null);
  useEffect(
    () => () => {
      if (copiedTimer.current) clearTimeout(copiedTimer.current);
    },
    [],
  );

  function copy() {
    navigator.clipboard
      .writeText(userCode)
      .then(() => {
        setCopied(true);
        if (copiedTimer.current) clearTimeout(copiedTimer.current);
        copiedTimer.current = setTimeout(() => setCopied(false), 1500);
      })
      .catch(() => {});
  }

  return (
    <div
      data-testid="device-code-panel"
      className="grid gap-2 rounded-md border bg-muted/50 p-3"
    >
      <p className="text-ui-base">Open the verification page and enter this code:</p>
      <div className="flex items-center gap-2">
        <span data-testid="device-code" className="font-mono text-2xl tracking-widest">
          {userCode}
          {/* Readable as "Device code: X" rather than a bare string; a
              plain span's aria-label is ignored by many screen readers. */}
          <span className="sr-only">{`Device code: ${userCode}`}</span>
        </span>
        <Button
          type="button"
          variant="ghost"
          size="icon-xs"
          aria-label="Copy device code"
          data-testid="device-code-copy"
          onClick={copy}
        >
          {copied ? <Icons.check /> : <Icons.copy />}
        </Button>
      </div>
      <a {...externalLinkProps(verificationUrl)} className="underline" data-testid="device-code-link">
        {verificationUrl}
      </a>
      <p className="text-ui-base" data-testid="device-code-countdown">
        {Number.isNaN(remainingMs)
          ? ""
          : expired
            ? "The code has expired"
            : `Expires in ${formatRemaining(remainingMs)}`}
      </p>
      <p className="text-ui-sm text-muted-foreground">
        Only enter this code on the page above — never share it.
      </p>
      <div>
        <Button
          type="button"
          variant="outline"
          size="xs"
          data-testid="accounts-connect-cancel"
          onClick={onCancel}
        >
          Cancel
        </Button>
      </div>
    </div>
  );
}
