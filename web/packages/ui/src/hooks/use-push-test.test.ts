import { describe, expect, it } from "vitest";

import { pushTestSentence } from "@/hooks/use-push-test";

describe("pushTestSentence", () => {
  it("push_test_sentence_maps_every_code", () => {
    const table: [string | undefined, string][] = [
      [undefined, ""],
      ["", ""],
      ["not_registered", "This device has no push registration any more."],
      ["not_approved", "This device is not approved."],
      ["seal_failed", "The notification could not be encrypted for this device."],
      ["send_failed", "The push service did not accept the notification."],
      ["DeviceNotRegistered", "The push service no longer knows this device."],
    ];
    for (const [code, want] of table) expect(pushTestSentence(code), String(code)).toBe(want);
  });

  it("push_test_sentence_keeps_a_provider_code_in_the_sentence", () => {
    expect(pushTestSentence("UnregisteredTopic")).toBe("The push service refused it (UnregisteredTopic).");
  });
});
