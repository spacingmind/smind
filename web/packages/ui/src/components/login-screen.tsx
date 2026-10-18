import { useEffect, useId, useRef, useState, type FormEvent } from "react";

import { Button } from "@/components/ui/button";
import { InlineSpinner } from "@/components/ui/inline-spinner";
import { Input } from "@/components/ui/input";
import { LoginError, loginWithPassword, type LoginReason, type Session } from "@/lib/auth";

/** What the form says under the field after a failed attempt. */
type Failure =
  | { kind: "wrong-password" }
  | { kind: "disabled" }
  | { kind: "network" }
  | { kind: "server" };

const FAILURE_TEXT: Record<Failure["kind"], string> = {
  "wrong-password": "Wrong password",
  disabled: "Password sign-in isn't enabled on this daemon. Open smind from the machine it runs on.",
  network: "Can't reach the daemon. Check the connection and try again.",
  server: "The daemon couldn't sign you in. Try again.",
};

const REASON_TEXT: Record<LoginReason, string | null> = {
  required: null,
  expired: "Your session expired. Sign in again.",
  rejected: "The daemon ended your session. Sign in again.",
};

/**
 * The password gate shown instead of the app when the daemon wants a
 * password (a browser on another machine; loopback never sees this). A
 * single field, Enter submits, Button disabled with a spinner while the
 * request is pending and, after a 429, until `retryAfterSeconds` elapses.
 *
 * The password lives only in the DOM input: it is read from the form on
 * submit (uncontrolled -- no React state, ref or storage ever holds it) and
 * the field is cleared as soon as the attempt resolves, success or not.
 */
export function LoginScreen({
  onLoggedIn,
  reason = "required",
  login = loginWithPassword,
}: {
  /** Called once a session is saved; the app starts a fresh connect. */
  onLoggedIn: () => void;
  reason?: LoginReason;
  /** Overridable for tests. */
  login?: (password: string) => Promise<Session>;
}) {
  const inputRef = useRef<HTMLInputElement>(null);
  const errorId = useId();
  const [pending, setPending] = useState(false);
  const [failure, setFailure] = useState<Failure | null>(null);
  const [retryIn, setRetryIn] = useState(0);
  const notice = REASON_TEXT[reason];

  // Whole-second countdown while rate limited; the button re-enables at 0.
  useEffect(() => {
    if (retryIn <= 0) return;
    const t = setTimeout(() => setRetryIn((n) => n - 1), 1000);
    return () => clearTimeout(t);
  }, [retryIn]);

  async function submit(e: FormEvent<HTMLFormElement>) {
    e.preventDefault();
    const input = inputRef.current;
    if (!input || pending || retryIn > 0) return;
    const password = input.value;
    if (password === "") return;
    setPending(true);
    setFailure(null);
    try {
      await login(password);
      input.value = "";
      onLoggedIn();
    } catch (err) {
      input.value = "";
      if (err instanceof LoginError) {
        const f = err.failure;
        if (f.kind === "rate-limited") {
          setRetryIn(f.retryAfterSeconds);
        } else {
          setFailure({ kind: f.kind });
        }
      } else {
        setFailure({ kind: "server" });
      }
      setPending(false);
      input.focus();
    }
  }

  const message = retryIn > 0 ? `Too many attempts. Try again in ${retryIn}s.` : failure ? FAILURE_TEXT[failure.kind] : null;

  return (
    <main
      data-testid="login-screen"
      className="flex min-h-svh items-center justify-center bg-background p-6 text-foreground"
    >
      <form onSubmit={submit} className="flex w-full max-w-xs flex-col gap-4" noValidate>
        <div className="flex flex-col gap-1">
          <h1 className="text-ui-lg font-medium">Sign in to smind</h1>
          <p className="text-ui-base text-muted-foreground">
            {notice ?? "This daemon requires a password."}
          </p>
        </div>
        <div className="flex flex-col gap-1.5">
          <label htmlFor="smind-login-password" className="text-ui-base font-medium">
            Password
          </label>
          <Input
            ref={inputRef}
            id="smind-login-password"
            name="password"
            type="password"
            autoComplete="current-password"
            autoFocus
            required
            readOnly={pending}
            aria-invalid={message !== null && retryIn === 0}
            aria-describedby={message ? errorId : undefined}
            data-testid="login-password"
          />
          <p
            id={errorId}
            role={retryIn > 0 ? "status" : "alert"}
            data-testid="login-error"
            className="min-h-4 text-ui-sm text-destructive"
          >
            {message}
          </p>
        </div>
        <Button type="submit" disabled={pending || retryIn > 0} data-testid="login-submit">
          {pending ? <InlineSpinner label="Signing in…" className="text-inherit" /> : "Sign in"}
        </Button>
      </form>
    </main>
  );
}
