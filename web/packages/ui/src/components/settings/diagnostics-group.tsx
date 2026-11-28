import { SettingsGroup, SettingsList, SettingsListRow, SettingsRow } from "@/components/settings/settings-layout";
import { Button } from "@/components/ui/button";
import { FormError } from "@/components/ui/form-error";
import { useDoctorSessions, type DoctorSessions } from "@/hooks/use-doctor-sessions";
import { useViaRelay } from "@/hooks/use-via-relay";
import { daemonSentence } from "@/lib/daemon-sentence";
import { RpcError } from "@/lib/ws-client";

const DESCRIPTION = "Checks what this daemon can still read of the sessions behind your runs.";

/** The six counts `doctor.sessions` answers, in the wire's order, each with the label a person reads. */
const ROWS: { label: string; testId: string; key: keyof DoctorSessions }[] = [
  { label: "Finished runs", testId: "doctor-runs", key: "runs" },
  { label: "With a provider session", testId: "doctor-withref", key: "withRef" },
  { label: "Readable", testId: "doctor-readable", key: "readable" },
  { label: "Too large to read", testId: "doctor-toolarge", key: "tooLarge" },
  { label: "No longer on disk", testId: "doctor-gone", key: "gone" },
  { label: "Checked", testId: "doctor-checked", key: "checked" },
];

/**
 * Settings -> Daemon -> "Diagnostics" (D3): the `doctor.sessions` probe, one call per click of Run checks.
 * The counts are the whole answer (the wire carries no crash report and no role-pack line); a relay
 * connection or a `forbidden` answer means the check is local-human-only, so the group says where to run
 * it and makes no call; any other error is one sentence and the button stays usable.
 */
export function DiagnosticsGroup() {
  const { viaRelay, ready } = useViaRelay();
  const mutation = useDoctorSessions();
  const forbidden = mutation.error instanceof RpcError && mutation.error.code === "forbidden";
  const counts = mutation.data ?? null;

  return (
    <div className="flex flex-col gap-2">
      <SettingsGroup title="Diagnostics" description={DESCRIPTION}>
        {viaRelay || forbidden ? (
          <p data-testid="doctor-read-only" className="px-4 py-3 text-ui-base text-muted-foreground">
            Run diagnostics on the daemon&apos;s machine.
          </p>
        ) : (
          <>
            <SettingsRow
              label="Sessions"
              control={
                <Button
                  size="sm"
                  variant="outline"
                  data-testid="doctor-run"
                  disabled={!ready || mutation.isPending}
                  onClick={() => mutation.mutate()}
                >
                  {mutation.isPending ? "Running..." : "Run checks"}
                </Button>
              }
            />
            {mutation.isIdle ? (
              <p data-testid="doctor-idle" className="px-4 py-3 text-ui-base text-muted-foreground">
                Not run yet.
              </p>
            ) : null}
            {mutation.error ? (
              <div className="px-4 py-3">
                <FormError message={daemonSentence(mutation.error.message)} testId="doctor-error" />
              </div>
            ) : null}
          </>
        )}
      </SettingsGroup>
      {counts && !viaRelay && !forbidden ? (
        <SettingsList testId="doctor-result">
          {ROWS.map((row) => (
            <SettingsListRow key={row.key} testId={row.testId} className="flex items-center justify-between gap-2">
              <span className="text-ui-base text-foreground">{row.label}</span>
              <span className="text-ui-base font-medium tabular-nums text-foreground-muted">{counts[row.key]}</span>
            </SettingsListRow>
          ))}
        </SettingsList>
      ) : null}
    </div>
  );
}
