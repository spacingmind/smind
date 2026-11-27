import type { ReactNode } from "react";

import { SettingsGroup, SettingsList, SettingsListRow, SettingsRow } from "@/components/settings/settings-layout";
import { Button } from "@/components/ui/button";
import { FormError } from "@/components/ui/form-error";
import { pushTestSentence, usePushTest } from "@/hooks/use-push-test";
import { useRelayDevices } from "@/hooks/use-relay-devices";
import { useViaRelay } from "@/hooks/use-via-relay";
import { daemonSentence } from "@/lib/daemon-sentence";
import { deviceName, relayDeviceUnavailable, shortDeviceId, type RelayDevice } from "@/lib/relay-devices";
import { RpcError } from "@/lib/ws-client";

/**
 * The "Mobile push" group of Settings -> Notifications (B6 D4, both builds): the count of phones registered for
 * push from one `relay.device.list` read, and a "Send test notification" button that calls `push.test` once per
 * click. Over a relay session (or a `forbidden` answer) the group says where to send the test and makes no call;
 * a daemon without the relay bridge simply cannot count. Only device ids, labels and sentences are ever shown:
 * the wire carries no token, key or provider payload, and none is echoed.
 */
export function MobilePushGroup() {
  const { viaRelay, ready } = useViaRelay();
  const devices = useRelayDevices(ready && !viaRelay);
  const test = usePushTest();

  const listUnavailable = devices.isError ? relayDeviceUnavailable(devices.error) : null;
  const testForbidden = test.error instanceof RpcError && test.error.code === "forbidden";
  const readOnly = viaRelay || listUnavailable === "relay" || testForbidden;

  const list = devices.data ?? null;
  const byId = new Map<string, RelayDevice>((list?.devices ?? []).map((d) => [d.device_id, d]));
  const results = test.data?.results ?? null;

  let count: ReactNode;
  if (devices.isError) {
    count = "Not known on this daemon.";
  } else if (list) {
    count = `${list.devices.filter((d) => d.push === true).length} with push on`;
  } else {
    count = null;
  }

  return (
    <div className="flex flex-col gap-2" data-testid="mobile-push-group">
      <SettingsGroup
        title="Mobile push"
        description="Notifications sent to your paired phones when a task finishes or needs you."
      >
        {readOnly ? (
          <p data-testid="push-read-only" className="px-4 py-3 text-ui-base text-muted-foreground">
            Send test notifications from the daemon&apos;s machine.
          </p>
        ) : (
          <>
            <SettingsRow
              label="Registered phones"
              control={
                <div className="flex items-center gap-2">
                  {count !== null && (
                    <span data-testid="push-count" className="text-ui-sm text-foreground-muted">
                      {count}
                    </span>
                  )}
                  <Button
                    size="sm"
                    variant="outline"
                    data-testid="push-test"
                    disabled={!ready || test.isPending}
                    onClick={() => test.mutate()}
                  >
                    {test.isPending ? "Sending..." : "Send test notification"}
                  </Button>
                </div>
              }
            />
            {test.error ? (
              <div className="px-4 pb-3">
                <FormError message={daemonSentence(test.error.message)} testId="push-error" />
              </div>
            ) : null}
            {results && results.length === 0 ? (
              <p data-testid="push-no-device" className="px-4 py-3 text-ui-base text-muted-foreground">
                No device has push set up yet. Open sMind on your phone to register one.
              </p>
            ) : null}
          </>
        )}
      </SettingsGroup>
      {results && results.length > 0 && !readOnly ? (
        <SettingsList testId="push-results">
          {results.map((r) => {
            const device = byId.get(r.deviceId);
            return (
              <SettingsListRow
                key={r.deviceId}
                data-testid={`push-result-${r.deviceId}`}
                className="flex items-center justify-between gap-2"
              >
                <span className="flex min-w-0 items-center gap-2">
                  {device && <span className="truncate text-ui-base font-medium text-foreground">{deviceName(device)}</span>}
                  <span className="shrink-0 font-mono text-ui-sm text-foreground-muted">{shortDeviceId(r.deviceId)}</span>
                </span>
                <span
                  className={`shrink-0 text-ui-sm ${r.sent ? "text-success" : "text-foreground-muted"}`}
                  data-testid={`push-result-state-${r.deviceId}`}
                >
                  {r.sent ? "Sent" : pushTestSentence(r.error)}
                </span>
              </SettingsListRow>
            );
          })}
        </SettingsList>
      ) : null}
    </div>
  );
}
