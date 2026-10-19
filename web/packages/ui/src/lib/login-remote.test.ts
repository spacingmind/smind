import { describe, expect, it } from "vitest";
import { isLoopbackHostname, isRemoteLogin } from "@/lib/login-remote";

describe("login-remote", () => {
  it("remote is derived from connection kind and hostname", () => {
    expect(isRemoteLogin({ connection: { kind: "local" }, hostname: "anything", override: null })).toBe(false);
    expect(isRemoteLogin({ connection: { kind: "url" }, hostname: "localhost", override: null })).toBe(true);
    expect(isRemoteLogin({ connection: { kind: "ssh" }, hostname: "localhost", override: null })).toBe(true);
    expect(isRemoteLogin({ connection: { kind: "relay" }, hostname: "localhost", override: null })).toBe(true);

    // Web build: connection is null, so the page hostname decides.
    expect(isRemoteLogin({ connection: null, hostname: "localhost", override: null })).toBe(false);
    expect(isRemoteLogin({ connection: null, hostname: "127.0.0.1", override: null })).toBe(false);
    expect(isRemoteLogin({ connection: null, hostname: "::1", override: null })).toBe(false);
    expect(isRemoteLogin({ connection: null, hostname: "homelab.example.com", override: null })).toBe(true);
    expect(isRemoteLogin({ connection: null, hostname: "192.168.1.5", override: null })).toBe(true);

    expect(isLoopbackHostname("localhost")).toBe(true);
    expect(isLoopbackHostname("app.localhost")).toBe(true);
    expect(isLoopbackHostname("127.0.0.1")).toBe(true);
    expect(isLoopbackHostname("127.255.0.3")).toBe(true);
    expect(isLoopbackHostname("::1")).toBe(true);
    expect(isLoopbackHostname("[::1]")).toBe(true);
    expect(isLoopbackHostname("LOCALHOST")).toBe(true);
    expect(isLoopbackHostname("192.168.1.5")).toBe(false);
    expect(isLoopbackHostname("example.com")).toBe(false);
  });

  it("manual override beats the derived remote flag", () => {
    expect(isRemoteLogin({ connection: { kind: "local" }, hostname: "localhost", override: "other-device" })).toBe(true);
    expect(isRemoteLogin({ connection: { kind: "relay" }, hostname: "example.com", override: "this-device" })).toBe(false);
  });
});
