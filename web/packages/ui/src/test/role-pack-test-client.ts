import type { RolePackCheck, RolePackField, RolePackSeat } from "@/lib/role-pack-types";
import { FakeUsageDaemon } from "@/test/usage-test-client";

/** What a scripted `profile.rolePack.*` handler returns. Throw an `RpcError` to fail a call. */
export type RolePackHandler = (method: string, params: unknown) => unknown;

/**
 * The usage fake with `profile.rolePack.*` RPCs answered by a handler, every call recorded in `calls`
 * (so tests assert exactly which RPCs ran and with what params). Everything else (profile.list,
 * provider.list, ...) falls through to the base fake.
 */
export function rolePackDaemon(handler: RolePackHandler): FakeUsageDaemon {
  const daemon = new FakeUsageDaemon([]);
  const base = daemon.client.call.bind(daemon.client);
  (daemon.client as { call: unknown }).call = async (method: string, params?: unknown): Promise<unknown> => {
    if (!method.startsWith("profile.rolePack.")) return base(method, params);
    daemon.calls.push({ method, params });
    return handler(method, params);
  };
  return daemon;
}

/** A field as the daemon answers it: `name` picks the kind the engine assigns (`model` is a capability). */
export function rolePackField(name: string, partial: Partial<RolePackField> = {}): RolePackField {
  const kind: RolePackField["kind"] =
    name === "instructions" || name === "notes" || name === "skills" ? "prompt" : "capability";
  return { name, kind, state: "current", diff: "", ...partial };
}

/** A seat as the daemon answers it: pack order, every synced field `current`, untouched, no updates. */
export function rolePackSeat(partial: Partial<RolePackSeat> = {}): RolePackSeat {
  return {
    name: "lead",
    id: 12,
    status: "ok",
    updatedAt: "2026-01-01T00:00:00Z",
    untouched: true,
    hasUpdates: false,
    fields: [
      rolePackField("instructions"),
      rolePackField("notes"),
      rolePackField("skills"),
      rolePackField("model"),
      rolePackField("permissionMode"),
      rolePackField("orchestrationTools"),
      rolePackField("childPolicy"),
      rolePackField("onRateLimit"),
    ],
    ...partial,
  };
}

/** A check as the daemon answers it: pack v9, every seat current. */
export function rolePackCheck(partial: Partial<RolePackCheck> = {}): RolePackCheck {
  return { packVersion: 9, seatsWithUpdates: 0, seats: [rolePackSeat()], ...partial };
}

/**
 * The check the review tests drive: lead.instructions stale (a three-line diff), lead.permissionMode a
 * stale capability (default -> plan), and a validator whose notes the Human edited (customised).
 */
export function staleLeadCheck(): RolePackCheck {
  return rolePackCheck({
    seatsWithUpdates: 1,
    seats: [
      rolePackSeat({
        hasUpdates: true,
        fields: [
          rolePackField("instructions", { state: "stale", diff: " line one\n-old line\n+new line\n line three" }),
          rolePackField("notes"),
          rolePackField("skills"),
          rolePackField("model"),
          rolePackField("permissionMode", { state: "stale", stored: "default", want: "plan" }),
          rolePackField("orchestrationTools"),
          rolePackField("childPolicy"),
          rolePackField("onRateLimit"),
        ],
      }),
      rolePackSeat({
        name: "validator",
        id: 13,
        untouched: false,
        fields: [
          rolePackField("instructions"),
          rolePackField("notes", { state: "customised", diff: "-mine\n+pack" }),
          rolePackField("skills"),
          rolePackField("model"),
          rolePackField("permissionMode"),
          rolePackField("orchestrationTools"),
          rolePackField("childPolicy"),
          rolePackField("onRateLimit"),
        ],
      }),
    ],
  });
}
