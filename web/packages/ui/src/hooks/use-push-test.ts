import { useMutation } from "@tanstack/react-query";

import { useDaemonConnection } from "@/lib/query/daemon-query-provider";

/**
 * `push.test` (internal/wsapi/push.go): one test push to every registered device. `results` is empty when
 * no device is registered -- that is not an error. `error` is the daemon's code (`not_registered`,
 * `not_approved`, `seal_failed`, `send_failed`) or the provider's own (e.g. `DeviceNotRegistered`); the
 * wire carries no token, key or provider payload, and none is echoed.
 */
export interface PushTestDeviceResult {
  deviceId: string;
  sent: boolean;
  error?: string;
}

export interface PushTestResult {
  results: PushTestDeviceResult[];
}

/**
 * A result code as one sentence. The daemon's four codes name the step that failed; anything else
 * (a provider code) is kept in the sentence verbatim so the person can search it; no code is no words.
 */
export function pushTestSentence(code: string | undefined): string {
  switch (code) {
    case undefined:
    case "":
      return "";
    case "not_registered":
      return "This device has no push registration any more.";
    case "not_approved":
      return "This device is not approved.";
    case "seal_failed":
      return "The notification could not be encrypted for this device.";
    case "send_failed":
      return "The push service did not accept the notification.";
    case "DeviceNotRegistered":
      return "The push service no longer knows this device.";
    default:
      return `The push service refused it (${code}).`;
  }
}

/**
 * One `push.test` per `mutate()`: the person asks for it, so a mutation and never a query (nothing
 * changes on its own, so there is nothing to poll). Local-only like the rest of the `push.*` surface:
 * a relay session is refused with `forbidden`, which the group turns into read-only copy.
 */
export function usePushTest() {
  const { client } = useDaemonConnection();
  return useMutation({
    mutationFn: async () => {
      if (!client) throw new Error("no daemon connection");
      return client.call<PushTestResult>("push.test", {});
    },
  });
}
