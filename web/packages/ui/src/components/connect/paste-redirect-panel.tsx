import { useEffect, useRef, useState, type JSX } from "react";

import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";

/**
 * The paste-the-redirect fallback of an OAuth connect (ADR-0033): after the
 * browser lands on a loopback page the remote daemon can't hear, the user
 * copies that page's full address back here. Presentational -- the caller
 * performs account.oauthComplete and owns clearing the input on success
 * (by re-rendering with a fresh form).
 */
export function PasteRedirectPanel(props: {
  onSubmit: (redirectUrl: string) => void;
  pending: boolean;
  error: string | null;
  autoFocus?: boolean;
}): JSX.Element {
  const [value, setValue] = useState("");
  const trimmed = value.trim();
  const disabled = props.pending || trimmed === "";
  // An edited input invalidates the shown error; the next paste re-arms it.
  const [dismissedError, setDismissedError] = useState(false);
  const error = props.error && !dismissedError ? props.error : null;
  // The pasted address carries the OAuth code and is spent the moment it
  // is submitted -- cleared on submit (the daemon now holds it) and again
  // on an accepted paste (`pending` true -> false with no error).
  const wasPending = useRef(false);
  useEffect(() => {
    if (wasPending.current && !props.pending && !props.error) setValue("");
    wasPending.current = props.pending;
  }, [props.pending, props.error]);

  function submit() {
    if (disabled) return;
    props.onSubmit(trimmed);
    setValue("");
  }

  return (
    <div data-testid="paste-redirect-panel" className="grid gap-2">
      <p className="text-ui-base">
        After you sign in, the browser may show a page that fails to load. Copy that
        page's full address from the address bar and paste it here.
      </p>
      <Input
        id="paste-redirect-input"
        aria-label="Sign-in redirect address"
        placeholder="http://localhost:54545/callback?code=…"
        autoComplete="off"
        spellCheck={false}
        type="text"
        autoFocus={props.autoFocus}
        value={value}
        onChange={(e) => {
          setDismissedError(true);
          setValue(e.target.value);
        }}
        onKeyDown={(e) => {
          if (e.key === "Enter") submit();
        }}
      />
      <div>
        <Button
          type="button"
          data-testid="paste-redirect-submit"
          disabled={disabled}
          onClick={submit}
        >
          {props.pending ? "Finishing…" : "Finish"}
        </Button>
      </div>
      {error && (
        <p role="alert" data-testid="paste-redirect-error" className="text-ui-sm text-destructive">
          {error}
        </p>
      )}
    </div>
  );
}
