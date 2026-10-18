import { useEffect, useState } from "react";

import { requestFirstRun } from "@/components/first-run/first-run-events";
import { Button } from "@/components/ui/button";
import { desktop } from "@/lib/platform";

/** "Run setup again" / "Finish setup": reopens first-run without touching any saved connection or setting. */
export function SetupSection() {
  const [pending, setPending] = useState(false);

  useEffect(() => {
    let cancelled = false;
    Promise.resolve()
      .then(() => desktop.appSettingsGet())
      .then((s) => !cancelled && setPending(!s.onboardingCompleted))
      .catch(() => {});
    return () => {
      cancelled = true;
    };
  }, []);

  return (
    <section className="flex items-center justify-between gap-4" data-testid="connection-setup-section">
      <div className="flex flex-col gap-0.5">
        <h3 className="text-ui-base font-medium text-foreground">Setup</h3>
        <p className="max-w-sm text-ui-base text-muted-foreground">
          {pending
            ? "You skipped the first-run setup. Finish it to choose how this app gets a daemon."
            : "Reopen the first-run choices. Saved connections stay as they are until you pick something."}
        </p>
      </div>
      <Button variant="outline" size="sm" data-testid="connection-run-setup" onClick={requestFirstRun}>
        {pending ? "Finish setup" : "Run setup again"}
      </Button>
    </section>
  );
}
