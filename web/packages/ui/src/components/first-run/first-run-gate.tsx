import { Fragment, useCallback, useEffect, useState, type ReactNode } from "react";

import { FirstRun } from "@/components/first-run/first-run";
import { RUN_SETUP_EVENT } from "@/components/first-run/first-run-events";
import { desktop } from "@/lib/platform";

type Phase = "loading" | "firstRun" | "app";

/**
 * Desktop only (main.tsx imports it behind `isDesktop`): decides whether the workspace UI
 * or first-run is on screen (ADR-0031 D7). Renders nothing while the settings load (no
 * flash of the wrong screen); a read failure falls back to the normal app. A successful
 * choice remounts the app so it connects fresh to whatever was selected; "Skip for now"
 * only hides first-run for this session.
 */
export default function FirstRunGate({ children }: { children: ReactNode }) {
  const [phase, setPhase] = useState<Phase>("loading");
  const [rerun, setRerun] = useState(false);
  const [epoch, setEpoch] = useState(0);

  useEffect(() => {
    let cancelled = false;
    desktop
      .appSettingsGet()
      .then(
        (s) => (s.onboardingCompleted ? "app" : "firstRun"),
        () => "app",
      )
      .then((next) => {
        if (!cancelled) setPhase((cur) => (cur === "loading" ? (next as Phase) : cur));
      });
    return () => {
      cancelled = true;
    };
  }, []);

  const reopen = useCallback(() => {
    setRerun(true);
    setPhase("firstRun");
  }, []);

  useEffect(() => {
    window.addEventListener(RUN_SETUP_EVENT, reopen);
    const unlisten = desktop.onRunSetupRequested(reopen);
    return () => {
      window.removeEventListener(RUN_SETUP_EVENT, reopen);
      unlisten();
    };
  }, [reopen]);

  if (phase === "loading") return null;
  if (phase === "firstRun") {
    return (
      <FirstRun
        rerun={rerun}
        onSkip={() => setPhase("app")}
        onDone={() => {
          setEpoch((n) => n + 1);
          setPhase("app");
        }}
      />
    );
  }
  return <Fragment key={epoch}>{children}</Fragment>;
}
