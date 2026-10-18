import { type JSX } from "react";

import { Button } from "@/components/ui/button";
import { Alert } from "@/components/ui/alert";

function DismissButton(props: { onDismiss: () => void }): JSX.Element {
  return (
    <Button
      type="button"
      variant="ghost"
      size="xs"
      data-testid="login-notice-dismiss"
      onClick={props.onDismiss}
    >
      Dismiss
    </Button>
  );
}

/**
 * The done state of an account login (ADR-0033 D6): a new account row was
 * created, an existing one was merely re-linked, or an existing row was
 * adopted into the signed-in identity. Presentational.
 */
export function LoginDoneNotice(props: {
  result: { label: string; created: boolean; adopted?: boolean };
  onDismiss: () => void;
}): JSX.Element {
  const { label, created, adopted } = props.result;
  const message = created
    ? `Connected as '${label}'.`
    : adopted
      ? `Updated '${label}' — now linked to the account you signed in with.`
      : `Updated '${label}' — already connected.`;
  return (
    <Alert variant="success" title={message} testId="login-done-notice">
      <DismissButton onDismiss={props.onDismiss} />
    </Alert>
  );
}

/**
 * The error state of an account login: `soft` errors (a cancelled login,
 * a stale code) get a single quiet line; everything else gets the
 * destructive alert. Presentational.
 */
export function LoginErrorNotice(props: {
  message: string;
  soft: boolean;
  onDismiss: () => void;
}): JSX.Element {
  if (props.soft) {
    return (
      <p
        role="alert"
        data-testid="login-error-notice"
        className="flex items-center gap-2 text-ui-sm text-muted-foreground"
      >
        {props.message}
        <DismissButton onDismiss={props.onDismiss} />
      </p>
    );
  }
  return (
    <Alert variant="error" title={props.message} testId="login-error-notice">
      <DismissButton onDismiss={props.onDismiss} />
    </Alert>
  );
}
