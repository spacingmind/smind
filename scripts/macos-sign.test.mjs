// Tests for scripts/macos-sign/*.sh (docs/plans/active/desktop-notarization-prep.md, AC1-AC5).
// The scripts call macOS tools by bare name, so a directory of fake tools goes first on PATH and
// the real scripts run under `bash`. Each fake appends "<tool><US><arg>..." per call to
// $FAKE_LOG; FAKE_* env knobs steer the fakes. Real tools (bash, base64, tar, mktemp, ...)
// stay real so the script's own plumbing is exercised.

import { test } from "node:test";
import assert from "node:assert/strict";
import { spawn, spawnSync } from "node:child_process";
import {
  chmodSync,
  existsSync,
  mkdirSync,
  mkdtempSync,
  readFileSync,
  readdirSync,
  rmSync,
  symlinkSync,
  writeFileSync,
} from "node:fs";
import { tmpdir } from "node:os";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

const ROOT = join(dirname(fileURLToPath(import.meta.url)), "..");
const SIGN = join(ROOT, "scripts", "macos-sign", "sign-notarize.sh");
const VERIFY = join(ROOT, "scripts", "macos-sign", "verify.sh");
const SCRIPTS_DIR = join(ROOT, "scripts", "macos-sign");
const ENTITLEMENTS_DIR = join(ROOT, "desktop", "src-tauri", "entitlements");

const IDENTITY = "Developer ID Application: Test Ltd (ABCDE12345)";
const SECRETS = {
  SMIND_MACOS_CERT_P12_BASE64: Buffer.from("P12-SECRET-BYTES").toString("base64"),
  SMIND_MACOS_CERT_PASSWORD: "hunter2-p12-pass",
  SMIND_MACOS_SIGN_IDENTITY: IDENTITY,
  SMIND_NOTARY_KEY_P8_BASE64: Buffer.from("P8-SECRET-KEY-BYTES").toString("base64"),
  SMIND_NOTARY_KEY_ID: "ABCDE12345",
  SMIND_NOTARY_ISSUER: "12345678-1234-1234-1234-123456789abc",
};
const SECRET_VALUES = [
  SECRETS.SMIND_MACOS_CERT_PASSWORD,
  SECRETS.SMIND_MACOS_CERT_P12_BASE64,
  SECRETS.SMIND_NOTARY_KEY_P8_BASE64,
  "P12-SECRET-BYTES",
  "P8-SECRET-KEY-BYTES",
];

// --- fakes ---------------------------------------------------------------------------------------------

const withLog = (tool, body) =>
  [
    "#!/usr/bin/env bash",
    "US=$(printf '\\037')",
    "log() {",
    `  local line="${tool}" a`,
    '  for a in "$@"; do line+="$US$a"; done',
    '  printf \'%s\\n\' "$line" >> "$FAKE_LOG"',
    "}",
    'log "$@"',
    ...body,
    "exit 0",
  ].join("\n") + "\n";

const EMPTY_PLIST_XML =
  '<?xml version="1.0" encoding="UTF-8"?>\\n<plist version="1.0">\\n<dict/>\\n</plist>\\n';
const JIT_PLIST_XML =
  '<?xml version="1.0" encoding="UTF-8"?>\\n<plist version="1.0">\\n<dict>\\n' +
  "<key>com.apple.security.cs.allow-jit</key>\\n<true/>\\n</dict>\\n</plist>\\n";

const FAKES = {
  uname: ["#!/usr/bin/env bash", 'if [ "${FAKE_UNAME:-}" = "Linux" ]; then echo "Linux"; else echo "Darwin"; fi'].join("\n") + "\n",

  security: withLog("security", [
    'case "$1" in',
    '  create-keychain) touch "${@: -1}" ;;',
    '  delete-keychain) rm -f "${@: -1}" ;;',
    "  find-identity)",
    '    if [ "${FAKE_NO_IDENTITY:-}" = "1" ]; then',
    '      echo "  0 valid identities found"',
    "    else",
    '      echo "  1) ABC123 \\"${SMIND_MACOS_SIGN_IDENTITY:-' + IDENTITY + '}\\""',
    // a long listing after the match, like a keychain with many identities
    "      printf '  2) FFFF \"Some Other Identity\"\\n%.0s' $(seq 1 4000)",
    "    fi",
    "    ;;",
    "esac",
  ]),

  codesign: withLog("codesign", [
    'path="${@: -1}"',
    'if [ "${1:-}" = "--verify" ]; then',
    '  if [ "${FAKE_VERIFY_FAIL:-}" = "1" ]; then exit 1; fi',
    "  exit 0",
    "fi",
    "dv=0",
    "signing=0",
    'for a in "$@"; do',
    '  if [ "$a" = "-dv" ]; then dv=1; fi',
    '  if [ "$a" = "--sign" ]; then signing=1; fi',
    "done",
    'if [ "$signing" = "1" ]; then exit 0; fi',
    "if [ \"$dv\" = \"1\" ]; then",
    "  NL=$'\\n'",
    '  d="Identifier=x"',
    '  if [ "${FAKE_NO_AUTHORITY:-}" != "1" ]; then d="$d${NL}Authority=' + IDENTITY + '"; fi',
    '  if [ "${FAKE_ADHOC:-}" = "1" ]; then d="$d${NL}Signature=adhoc"; fi',
    '  team="ABCDE12345"',
    '  case "$path" in',
    '    */smind) if [ "${FAKE_TEAM_MISMATCH:-}" = "1" ]; then team="ZZZZZZZZZZ"; fi ;;',
    '    *.framework) if [ "${FAKE_TEAM_MISMATCH_FRAMEWORK:-}" = "1" ]; then team="ZZZZZZZZZZ"; fi ;;',
    "  esac",
    '  if [ "${FAKE_ADHOC:-}" = "1" ]; then team="not set"; fi',
    '  d="$d${NL}TeamIdentifier=$team"',
    '  flags="0x10000(runtime)"',
    '  if [ "${FAKE_NO_RUNTIME:-}" = "1" ]; then flags="0x0(none)"; fi',
    '  case "$path" in',
    '    */smind) if [ "${FAKE_NO_RUNTIME_NESTED:-}" = "1" ]; then flags="0x0(none)"; fi ;;',
    '    *.framework) if [ "${FAKE_NO_RUNTIME_FRAMEWORK:-}" = "1" ]; then flags="0x0(none)"; fi ;;',
    "  esac",
    '  d="$d${NL}flags=$flags"',
    // The real codesign prints a long listing; the details line a script greps for comes first and
    // the noise after it, so a `| grep -q` under pipefail (grep exits at the first match, the
    // writer gets SIGPIPE) fails here exactly as it did against real apps.
    '  printf \'%s\\n\' "$d" >&2',
    // printed by the fake's own process (a builtin printf), like the real tool, so the fake itself is
    // killed by SIGPIPE when the reader has gone: its exit status is then 141, as real codesign's is
    "  printf 'CodeDirectory noise line that real codesign prints and nobody greps for\\n%.0s' $(seq 1 4000) >&2",
    "  exit 0",
    "fi",
    'if [ "${1:-}" = "-d" ]; then',
    '  if [ "${FAKE_ENT_MISMATCH:-}" = "1" ]; then printf \'' + JIT_PLIST_XML + "'",
    '  else printf \'' + EMPTY_PLIST_XML + "'",
    "  fi",
    "  exit 0",
    "fi",
  ]),

  xcrun: withLog("xcrun", [
    'if [ "$1" = "notarytool" ] && [ "$2" = "submit" ]; then',
    // real notarytool prints progress text on stdout before the JSON object under --wait
    '  if [ "${FAKE_NOTARY_NOISE:-}" = "1" ]; then echo "Conducting pre-submission checks for app.zip and initiating connection to the Apple notary service..."; echo "Successfully uploaded file"; fi',
    '  if [ -n "${FAKE_NOTARY_SLEEP:-}" ]; then sleep "$FAKE_NOTARY_SLEEP"; fi',
    '  echo "{\\"id\\":\\"sub-123\\",\\"status\\":\\"${FAKE_NOTARY_STATUS:-Accepted}\\"}"',
    "  exit 0",
    "fi",
    'if [ "$1" = "notarytool" ] && [ "$2" = "log" ]; then',
  '  echo \'{"issues":[]}\' > "$4"',
    "  exit 0",
    "fi",
    'if [ "$1" = "stapler" ] && [ "$2" = "validate" ] && [ "${FAKE_STAPLE_FAIL:-}" = "1" ]; then',
    "  exit 1",
    "fi",
  ]),

  ditto: withLog("ditto", [
    'if [ "$1" = "-c" ]; then',
    '  touch "${@: -1}"',
    "else",
    '  cp -R "$1" "$2"',
    "fi",
  ]),

  hdiutil: withLog("hdiutil", ['touch "${@: -1}"']),

  spctl: withLog("spctl", ['if [ "${FAKE_SPCTL_FAIL:-}" = "1" ]; then exit 1; fi']),

  plutil: [
    "#!/usr/bin/env node",
    'const fs = require("node:fs");',
    "const a = process.argv.slice(2);",
    'const out = (s) => process.stdout.write(s + "\\n");',
    'if (a[0] === "-extract") {',
    "  const key = a[1];",
    "  const file = a[a.length - 1];",
    "  let text;",
    '  try { text = fs.readFileSync(file, "utf8"); } catch { process.exit(1); }',
    "  const t = text.trim();",
    '  if (t.startsWith("{")) {',
    "    let v;",
    "    try { v = JSON.parse(t)[key]; } catch { process.exit(1); }",
    "    if (v === undefined || v === null) process.exit(1);",
    "    out(String(v));",
    "    process.exit(0);",
    "  }",
    '  const m = t.match(new RegExp("<key>" + key + "</key>[^<]*<string>([^<]*)</string>"));',
    "  if (m) { out(m[1]); process.exit(0); }",
    "  process.exit(1);",
    "}",
    'if (a[0] === "-p") {',
    "  let t;",
    '  try { t = a[1] === "-" ? fs.readFileSync(0, "utf8") : fs.readFileSync(a[1], "utf8"); } catch { process.exit(1); }',
    "  t = t.trim();",
    '  if (t === "") process.exit(1);',
    '  const keys = [...t.matchAll(/<key>([^<]+)<\\/key>/g)].map((m) => m[1]);',
    '  out("{");',
    "  for (const k of keys) out('\"' + k + '\" => 1');",
    '  out("}");',
    "  process.exit(0);",
    "}",
    'if (a[0] === "-lint") process.exit(0);',
    "process.exit(0);",
  ].join("\n") + "\n",
};

// --- harness -------------------------------------------------------------------------------------------

function makeFakeBin(dir) {
  mkdirSync(dir);
  for (const [name, src] of Object.entries(FAKES)) {
    const p = join(dir, name);
    writeFileSync(p, src);
    chmodSync(p, 0o755);
  }
}

const INFO_PLIST = [
  '<?xml version="1.0" encoding="UTF-8"?>',
  "<plist version=\"1.0\">",
  "<dict>",
  "\t<key>CFBundleExecutable</key>",
  "\t<string>smind-desktop</string>",
  "</dict>",
  "</plist>",
].join("\n");

function makeApp(dir) {
  const app = join(dir, "sMind.app");
  mkdirSync(join(app, "Contents", "MacOS"), { recursive: true });
  writeFileSync(join(app, "Contents", "Info.plist"), INFO_PLIST);
  writeFileSync(join(app, "Contents", "MacOS", "smind-desktop"), "");
  writeFileSync(join(app, "Contents", "MacOS", "smind"), "");
  return app;
}

function harness({ knobs = {}, omit = [], withSecrets = true } = {}) {
  const tmp = mkdtempSync(join(tmpdir(), "smind-sign-test."));
  const bin = join(tmp, "fakebin");
  makeFakeBin(bin);
  const mytmp = mkdtempSync(join(tmpdir(), "smind-sign-tmp."));
  const logFile = join(tmp, "fake.log");
  const env = {
    PATH: bin + ":" + process.env.PATH,
    HOME: tmp,
    TMPDIR: mytmp,
    FAKE_LOG: logFile,
  };
  if (withSecrets) Object.assign(env, SECRETS);
  Object.assign(env, knobs);
  for (const k of omit) delete env[k];
  return {
    tmp,
    mytmp,
    env,
    run: (script, args) => spawnSync("bash", [script, ...args], { env, encoding: "utf8" }),
    log: () => {
      try {
        return readFileSync(logFile, "utf8").split("\n").filter(Boolean).map((l) => l.split("\u001f"));
      } catch {
        return [];
      }
    },
    cleanup: () => {
      rmSync(tmp, { recursive: true, force: true });
      rmSync(mytmp, { recursive: true, force: true });
    },
  };
}

function withHarness(opts, fn) {
  const h = harness(opts);
  try {
    fn(h);
  } finally {
    h.cleanup();
  }
}

function runSign(h, extraArgs = []) {
  const app = makeApp(h.tmp);
  const out = join(h.tmp, "out");
  return { app, out, r: h.run(SIGN, ["--app", app, "--out", out, ...extraArgs]) };
}

const MISSING_SECRET_CASES = Object.keys(SECRETS).map((name) => ({ name, omit: [name] }));
const BAD_SHAPE_CASES = [
  { name: "identity", knobs: { SMIND_MACOS_SIGN_IDENTITY: "Apple Development: x" } },
  { name: "key id", knobs: { SMIND_NOTARY_KEY_ID: "short" } },
  { name: "issuer", knobs: { SMIND_NOTARY_ISSUER: "not-a-uuid" } },
];
const ALL_REFUSAL_CASES = [...MISSING_SECRET_CASES, ...BAD_SHAPE_CASES];

const toolLines = (lines, tool) => lines.filter((f) => f[0] === tool);
const flat = (f) => f.join(" ");

function assertCleanedUp(h) {
  const lines = h.log();
  const created = toolLines(lines, "security").find((f) => f.includes("create-keychain"));
  const deleted = toolLines(lines, "security").find((f) => f.includes("delete-keychain"));
  assert.ok(created, "no security create-keychain call");
  assert.ok(deleted, "no security delete-keychain call");
  assert.equal(deleted[deleted.length - 1], created[created.length - 1], "wrong keychain deleted");
  assert.ok(
    readdirSync(h.mytmp).every((n) => !n.startsWith("smind-macos-sign.")),
    "temp work dir left behind in TMPDIR",
  );
}

// --- the secret gate (AC1) -----------------------------------------------------------------------------

test("sign-refuses-without-each-secret", () => {
  for (const { name, omit } of MISSING_SECRET_CASES) {
    withHarness({ omit }, (h) => {
      const r = h.run(SIGN, ["--app", makeApp(h.tmp), "--out", join(h.tmp, "out")]);
      assert.equal(r.status, 3, `${name}: unexpected exit: ${r.stderr}`);
      assert.ok(r.stderr.includes(name), `${name} not named in: ${r.stderr}`);
      assert.deepEqual(h.log(), [], `${name}: a tool ran before the refusal`);
    });
  }
});

test("sign-refuses-a-bad-identity-shape", () => {
  withHarness(BAD_SHAPE_CASES[0], (h) => {
    const r = h.run(SIGN, ["--app", makeApp(h.tmp)]);
    assert.equal(r.status, 3, r.stderr);
    assert.deepEqual(h.log(), []);
  });
});

test("sign-refuses-bad-key-id-and-issuer", () => {
  for (const c of BAD_SHAPE_CASES.slice(1)) {
    withHarness(c, (h) => {
      const r = h.run(SIGN, ["--app", makeApp(h.tmp)]);
      assert.equal(r.status, 3, `${c.name}: unexpected exit: ${r.stderr}`);
      assert.deepEqual(h.log(), [], `${c.name}: a tool ran before the refusal`);
    });
  }
});

test("sign-refusal-names-variables-never-values", () => {
  for (const c of ALL_REFUSAL_CASES) {
    withHarness(c, (h) => {
      const r = h.run(SIGN, ["--app", makeApp(h.tmp), "--out", join(h.tmp, "out")]);
      assert.equal(r.status, 3, `${c.name}: unexpected exit: ${r.stderr}`);
      const o = r.stdout + r.stderr;
      for (const v of SECRET_VALUES) {
        assert.ok(!o.includes(v), `${c.name}: refusal leaked a secret value`);
      }
    });
  }
});

test("sign-refuses-before-any-tool-runs", () => {
  for (const app of ["fixture", "missing"]) {
    for (const c of ALL_REFUSAL_CASES) {
      withHarness(c, (h) => {
        const appArg = app === "fixture" ? makeApp(h.tmp) : join(h.tmp, "no", "such.app");
        const r = h.run(SIGN, ["--app", appArg, "--out", join(h.tmp, "out")]);
        assert.equal(r.status, 3, `${c.name} (${app}): unexpected exit: ${r.stderr}`);
        assert.deepEqual(h.log(), [], `${c.name} (${app}): a tool ran before the refusal`);
      });
    }
  }
});

test("sign-refuses-off-macos", () => {
  withHarness({ knobs: { FAKE_UNAME: "Linux" } }, (h) => {
    const r = h.run(SIGN, ["--app", makeApp(h.tmp), "--out", join(h.tmp, "out")]);
    assert.equal(r.status, 3, r.stderr);
    const tools = new Set(h.log().map((f) => f[0]));
    assert.ok(!tools.has("codesign"), "codesign ran off macOS");
    assert.ok(!tools.has("security"), "security ran off macOS");
    assert.ok(!tools.has("xcrun"), "xcrun ran off macOS");
  });
});

test("sign-check-mode-signs-nothing", () => {
  withHarness({}, (h) => {
    const r = h.run(SIGN, ["--check"]);
    assert.equal(r.status, 0, r.stderr);
    const sec = toolLines(h.log(), "security").map(flat);
    assert.ok(sec.some((l) => l.includes("create-keychain")), sec.join("\n"));
    assert.ok(sec.some((l) => l.includes("import")), sec.join("\n"));
    assert.ok(sec.some((l) => l.includes("find-identity")), sec.join("\n"));
    assert.deepEqual(toolLines(h.log(), "codesign"), [], "--check signed something");
    assert.deepEqual(toolLines(h.log(), "xcrun"), [], "--check called xcrun");
  });
});

// --- no leakage, cleanup (AC2) -------------------------------------------------------------------------

test("sign-no-secret-in-output", () => {
  withHarness({}, (h) => {
    const { r } = runSign(h);
    assert.equal(r.status, 0, r.stderr);
    const o = r.stdout + r.stderr;
    for (const v of SECRET_VALUES) {
      assert.ok(!o.includes(v), `a secret value appeared in script output: ${v}`);
    }
  });
});

test("sign-cleans-up-keychain-and-temp-dir-on-success", () => {
  withHarness({}, (h) => {
    const { r } = runSign(h);
    assert.equal(r.status, 0, r.stderr);
    assertCleanedUp(h);
  });
});

test("sign-cleans-up-on-notarization-failure", () => {
  withHarness({ knobs: { FAKE_NOTARY_STATUS: "Invalid" } }, (h) => {
    const { r } = runSign(h);
    assert.notEqual(r.status, 0, "notarization failure must fail the script");
    assertCleanedUp(h);
  });
});

// --- signing order and flags (AC3) ---------------------------------------------------------------------

const signCalls = (h) => toolLines(h.log(), "codesign").filter((f) => f.includes("--sign"));

test("sign-nested-code-before-app", () => {
  withHarness({}, (h) => {
    const { r } = runSign(h);
    assert.equal(r.status, 0, r.stderr);
    const calls = signCalls(h);
    const nested = calls.findIndex((f) => f[f.length - 1].endsWith("/Contents/MacOS/smind"));
    const app = calls.findIndex((f) => f[f.length - 1].endsWith(".app"));
    assert.ok(nested !== -1, "the sidecar was not signed");
    assert.ok(app !== -1, "the app bundle was not signed");
    assert.ok(nested < app, `sidecar signed after the app:\n${calls.map(flat).join("\n")}`);
  });
});

test("sign-every-codesign-has-runtime-and-timestamp", () => {
  withHarness({}, (h) => {
    const dmg = join(h.tmp, "out", "sMind.dmg");
    const { r } = runSign(h, ["--dmg", dmg]);
    assert.equal(r.status, 0, r.stderr);
    const calls = signCalls(h);
    assert.ok(calls.length >= 3, calls.map(flat).join("\n"));
    for (const f of calls) {
      assert.ok(f.includes("--timestamp"), `no --timestamp: ${flat(f)}`);
      if (f[f.length - 1].endsWith(".dmg")) continue;
      assert.ok(f.includes("--options") && f.includes("runtime"), `no hardened runtime: ${flat(f)}`);
    }
  });
});

// Frameworks (none today) are signed first, with the runtime and a timestamp, and without the
// app's entitlements; this branch has no other test, the fixture app has none.
test("sign-frameworks-are-signed-with-runtime-and-timestamp-before-the-app", () => {
  withHarness({}, (h) => {
    const app = makeApp(h.tmp);
    mkdirSync(join(app, "Contents", "Frameworks", "Thing.framework"), { recursive: true });
    const r = h.run(SIGN, ["--app", app, "--out", join(h.tmp, "out")]);
    assert.equal(r.status, 0, r.stderr);
    const calls = signCalls(h);
    const fwAt = calls.findIndex((f) => f[f.length - 1].endsWith("Thing.framework"));
    const appAt = calls.findIndex((f) => f[f.length - 1].endsWith("sMind.app"));
    assert.ok(fwAt !== -1 && appAt !== -1 && fwAt < appAt, calls.map(flat).join("\n"));
    const fw = calls[fwAt];
    assert.ok(fw.includes("--timestamp") && fw.includes("--options") && fw.includes("runtime"), flat(fw));
    assert.ok(!fw.includes("--entitlements"), flat(fw));
  });
});

test("sign-never-deep", () => {
  withHarness({}, (h) => {
    const { r } = runSign(h, ["--dmg", join(h.tmp, "out", "sMind.dmg"), "--updater-tarball", join(h.tmp, "out", "sMind.app.tar.gz")]);
    assert.equal(r.status, 0, r.stderr);
    const deep = toolLines(h.log(), "codesign").filter((f) => f.includes("--deep"));
    assert.deepEqual(deep, [], `codesign used --deep:\n${deep.map(flat).join("\n")}`);
  });
});

test("sign-passes-the-entitlements", () => {
  withHarness({}, (h) => {
    const { r } = runSign(h);
    assert.equal(r.status, 0, r.stderr);
    const entOf = (f) => {
      const i = f.indexOf("--entitlements");
      return i === -1 ? null : f[i + 1];
    };
    const sidecar = signCalls(h).find((f) => f[f.length - 1].endsWith("/Contents/MacOS/smind"));
    const app = signCalls(h).find((f) => f[f.length - 1].endsWith(".app"));
    assert.ok(sidecar && entOf(sidecar)?.endsWith("sidecar.entitlements"), flat(sidecar ?? []));
    assert.ok(app && entOf(app)?.endsWith("app.entitlements"), flat(app ?? []));
  });
});

// --- notarization (AC4) --------------------------------------------------------------------------------

test("sign-notarization-not-accepted-fails-and-fetches-log", () => {
  withHarness({ knobs: { FAKE_NOTARY_STATUS: "Invalid" } }, (h) => {
    const { out, r } = runSign(h);
    assert.notEqual(r.status, 0, "a non-Accepted notarization must fail the script");
    assert.ok((r.stdout + r.stderr).includes("Invalid"), r.stdout + r.stderr);
    assert.ok(
      toolLines(h.log(), "xcrun").some((f) => f.includes("notarytool") && f.includes("log") && f.includes("sub-123")),
      "the notarization log was not fetched",
    );
    assert.ok(existsSync(join(out, "notarization-log-app.json")), "notarization-log-app.json missing");
  });
});

test("sign-staples-only-after-accepted", () => {
  withHarness({}, (h) => {
    const { r } = runSign(h);
    assert.equal(r.status, 0, r.stderr);
    const lines = h.log();
    const submit = lines.findIndex((f) => f[0] === "xcrun" && f.includes("notarytool") && f.includes("submit"));
    const staple = lines.findIndex((f) => f[0] === "xcrun" && f.includes("stapler"));
    assert.ok(submit !== -1, "no notarization submit");
    assert.ok(staple !== -1, "no stapler call");
    assert.ok(submit < staple, "stapled before notarization was accepted");
  });
  withHarness({ knobs: { FAKE_NOTARY_STATUS: "Invalid" } }, (h) => {
    const { r } = runSign(h);
    assert.notEqual(r.status, 0);
    assert.deepEqual(toolLines(h.log(), "xcrun").filter((f) => f.includes("stapler")), [], "stapled despite rejection");
  });
});

test("sign-key-by-file-path", () => {
  withHarness({}, (h) => {
    const { r } = runSign(h);
    assert.equal(r.status, 0, r.stderr);
    const submit = toolLines(h.log(), "xcrun").find((f) => f.includes("notarytool") && f.includes("submit"));
    assert.ok(submit, "no notarytool submit");
    const i = submit.indexOf("--key");
    assert.ok(i !== -1, flat(submit));
    const keyPath = submit[i + 1];
    assert.ok(keyPath.includes("AuthKey.p8"), `key not passed by file path: ${keyPath}`);
    for (const v of SECRET_VALUES) assert.ok(!keyPath.includes(v), "key bytes on the command line");
    for (const flag of ["--key-id", "--issuer", "--wait"]) {
      assert.ok(submit.includes(flag), `${flag} missing: ${flat(submit)}`);
    }
  });
});

test("sign-dmg-is-signed-notarized-stapled", () => {
  withHarness({}, (h) => {
    const dmg = join(h.tmp, "out", "sMind.dmg");
    const { r } = runSign(h, ["--dmg", dmg]);
    assert.equal(r.status, 0, r.stderr);
    assert.ok(existsSync(dmg), "no dmg was produced");
    const lines = h.log();
    assert.ok(toolLines(lines, "hdiutil").some((f) => f.includes("create")), "no hdiutil create");
    assert.ok(signCalls(h).some((f) => f[f.length - 1] === dmg), "the dmg was not signed");
    const submits = toolLines(lines, "xcrun").filter((f) => f.includes("notarytool") && f.includes("submit"));
    assert.equal(submits.length, 2, `expected app+dmg submissions:\n${submits.map(flat).join("\n")}`);
    assert.ok(
      toolLines(lines, "xcrun").some((f) => f.includes("stapler") && f.includes("staple") && f[f.length - 1] === dmg),
      "the dmg was not stapled",
    );
  });
});

test("sign-updater-tarball-warns-to-resign", () => {
  withHarness({}, (h) => {
    const tb = join(h.tmp, "out", "sMind.app.tar.gz");
    const { r } = runSign(h, ["--updater-tarball", tb]);
    assert.equal(r.status, 0, r.stderr);
    assert.ok(existsSync(tb), "no updater tarball was produced");
    assert.ok(r.stdout.includes("re-sign"), r.stdout);
    assert.ok(r.stdout.includes("updater key"), r.stdout);
  });
});

test("sign-rejects-app-that-is-a-link", () => {
  withHarness({}, (h) => {
    const app = makeApp(h.tmp);
    const link = join(h.tmp, "link.app");
    symlinkSync(app, link);
    const r = h.run(SIGN, ["--app", link, "--out", join(h.tmp, "out")]);
    assert.notEqual(r.status, 0, "a symlinked --app must be rejected");
    assert.deepEqual(toolLines(h.log(), "codesign"), [], "a link was signed");
  });
});

// --- the verify gate (AC5) -----------------------------------------------------------------------------

function runVerify(h, extraArgs = []) {
  const app = makeApp(h.tmp);
  return { app, r: h.run(VERIFY, ["--app", app, ...extraArgs]) };
}

const failLines = (r) => r.stdout.split("\n").filter((l) => l.startsWith("FAIL"));

function assertFailsWith(h, knobs, text) {
  withHarness({ knobs }, (h2) => {
    const { r } = runVerify(h2);
    assert.equal(r.status, 1, r.stdout + r.stderr);
    assert.ok(
      failLines(r).some((l) => l.toLowerCase().includes(text)),
      `no FAIL line mentioning '${text}':\n${r.stdout}`,
    );
  });
}

test("sign-refuses-a-whitespace-only-secret", () => {
  for (const name of Object.keys(SECRETS)) {
    withHarness({ knobs: { [name]: "   \n " } }, (h) => {
      const r = h.run(SIGN, ["--app", makeApp(h.tmp)]);
      assert.equal(r.status, 3, `${name}: ${r.stderr}`);
      assert.ok(r.stderr.includes(name), r.stderr);
      assert.deepEqual(h.log(), [], `${name}: a tool ran`);
    });
  }
});

test("sign-refuses-a-prefix-only-identity", () => {
  for (const identity of ["Developer ID Application: ", "Developer ID Application: Test Ltd", "Developer ID Application: Test Ltd (short)"]) {
    withHarness({ knobs: { SMIND_MACOS_SIGN_IDENTITY: identity } }, (h) => {
      const r = h.run(SIGN, ["--app", makeApp(h.tmp)]);
      assert.equal(r.status, 3, `${identity}: ${r.stderr}`);
      assert.deepEqual(h.log(), []);
    });
  }
});

test("sign-missing-option-value-exits-2", () => {
  withHarness({}, (h) => {
    for (const opt of ["--app", "--out", "--dmg", "--updater-tarball", "--entitlements-app", "--entitlements-sidecar"]) {
      const r = h.run(SIGN, [opt]);
      assert.equal(r.status, 2, `${opt}: ${r.stderr}`);
      assert.ok(r.stderr.includes("needs a value"), r.stderr);
    }
    assert.deepEqual(h.log(), []);
  });
});

test("help-prints-the-comment-and-no-code", () => {
  withHarness({}, (h) => {
    for (const script of [SIGN, VERIFY]) {
      const r = h.run(script, ["--help"]);
      assert.equal(r.status, 0, r.stderr);
      assert.ok(r.stdout.length > 100);
      for (const l of r.stdout.split("\n").filter(Boolean)) {
        assert.ok(l.startsWith("#"), `--help printed a code line: ${l}`);
      }
    }
  });
});

test("sign-staples-the-app-before-it-is-packed", () => {
  withHarness({}, (h) => {
    const { app, r } = runSign(h, ["--dmg", join(h.tmp, "out", "sMind.dmg"), "--updater-tarball", join(h.tmp, "out", "sMind.app.tar.gz")]);
    assert.equal(r.status, 0, r.stderr);
    const log = h.log();
    const staple = log.findIndex((f) => f[0] === "xcrun" && f.includes("staple") && f[f.length - 1] === app);
    const stage = log.findIndex((f) => f[0] === "ditto" && f[1] !== "-c" && f[1] === app);
    const dmg = log.findIndex((f) => f[0] === "hdiutil");
    assert.ok(staple !== -1, "the app is never stapled");
    assert.ok(stage !== -1 && staple < stage, `the dmg stage is copied from the app before it is stapled: ${log.map(flat).join("\n")}`);
    assert.ok(dmg !== -1 && staple < dmg);
  });
});

test("sign-every-codesign-uses-the-temporary-keychain", () => {
  withHarness({}, (h) => {
    // with a framework too: that goes through the other codesign branch (no entitlements)
    const app = makeApp(h.tmp);
    mkdirSync(join(app, "Contents", "Frameworks", "Thing.framework"), { recursive: true });
    const r = h.run(SIGN, ["--app", app, "--out", join(h.tmp, "out"), "--dmg", join(h.tmp, "out", "sMind.dmg")]);
    assert.equal(r.status, 0, r.stderr);
    const created = toolLines(h.log(), "security").find((f) => f.includes("create-keychain"));
    const keychain = created[created.length - 1];
    const calls = signCalls(h);
    assert.ok(calls.some((f) => f[f.length - 1].endsWith("Thing.framework")), "the framework was not signed");
    assert.ok(calls.length >= 4);
    for (const f of calls) {
      const at = f.indexOf("--keychain");
      assert.ok(at !== -1 && f[at + 1] === keychain, `a codesign does not use the temporary keychain: ${flat(f)}`);
    }
  });
});

test("sign-parses-notarytool-output-with-progress-text", () => {
  withHarness({ knobs: { FAKE_NOTARY_NOISE: "1" } }, (h) => {
    const { r } = runSign(h);
    assert.equal(r.status, 0, r.stdout + r.stderr);
    assert.ok(r.stdout.includes("notarized"), r.stdout);
  });
});

test("sign-notarization-failure-reports-the-exit-status", () => {
  withHarness({ knobs: { FAKE_NOTARY_STATUS: "Invalid" } }, (h) => {
    const { r } = runSign(h);
    assert.notEqual(r.status, 0);
    assert.match(r.stderr, /notarytool exit status \d+/);
  });
});

test("sign-refuses-bundles-with-nested-code-it-does-not-sign", () => {
  for (const dir of ["Helpers", "XPCServices", "PlugIns", "Library/LaunchServices"]) {
    withHarness({}, (h) => {
      const app = makeApp(h.tmp);
      mkdirSync(join(app, "Contents", dir), { recursive: true });
      const r = h.run(SIGN, ["--app", app, "--out", join(h.tmp, "out")]);
      assert.notEqual(r.status, 0, dir);
      assert.ok(r.stderr.includes(dir), r.stderr);
      assert.equal(signCalls(h).length, 0, `${dir}: something was signed`);
    });
  }
});

// A TERM while notarization is waiting must still delete the keychain and the temp dir (the EXIT trap
// runs for the signal handlers `exit 143`).
test("sign-term-signal-still-cleans-up", async () => {
  const h = harness({ knobs: { FAKE_NOTARY_SLEEP: "30" } });
  try {
    const app = makeApp(h.tmp);
    const child = spawn("bash", [SIGN, "--app", app, "--out", join(h.tmp, "out")], { env: h.env, stdio: "ignore" });
    const exited = new Promise((res) => child.on("exit", (code, sig) => res({ code, sig })));
    const deadline = Date.now() + 20000;
    while (!h.log().some((f) => f[0] === "xcrun" && f.includes("submit"))) {
      if (Date.now() > deadline) throw new Error("the script never reached the notarization step");
      await new Promise((r) => setTimeout(r, 100));
    }
    child.kill("SIGTERM");
    await exited;
    // the fake's `sleep` child may still run for a moment: the script's own cleanup is what is asserted
    await new Promise((r) => setTimeout(r, 500));
    assertCleanedUp(h);
  } finally {
    h.cleanup();
  }
});

test("verify-checks-frameworks", () => {
  for (const knob of ["FAKE_NO_RUNTIME_FRAMEWORK", "FAKE_TEAM_MISMATCH_FRAMEWORK"]) {
    withHarness({ knobs: { [knob]: "1" } }, (h) => {
      const app = makeApp(h.tmp);
      mkdirSync(join(app, "Contents", "Frameworks", "Thing.framework"), { recursive: true });
      const r = h.run(VERIFY, ["--app", app]);
      assert.equal(r.status, 1, `${knob}: ${r.stdout}${r.stderr}`);
      assert.ok(failLines(r).some((l) => l.includes("framework Thing.framework")), r.stdout);
    });
  }
});

test("verify-passes-a-good-app", () => {
  withHarness({}, (h) => {
    const { r } = runVerify(h);
    assert.equal(r.status, 0, r.stdout + r.stderr);
    assert.ok(!r.stdout.includes("FAIL"), r.stdout);
    for (const line of r.stdout.split("\n").filter(Boolean)) {
      assert.ok(line.startsWith("ok"), line);
    }
  });
});

test("verify-fails-adhoc", () => assertFailsWith(null, { FAKE_ADHOC: "1" }, "ad-hoc"));
test("verify-fails-without-developer-id", () => assertFailsWith(null, { FAKE_NO_AUTHORITY: "1" }, "authority"));
test("verify-fails-without-runtime", () => assertFailsWith(null, { FAKE_NO_RUNTIME: "1" }, "runtime"));
test("verify-fails-nested-without-runtime", () => assertFailsWith(null, { FAKE_NO_RUNTIME_NESTED: "1" }, "smind"));
test("verify-fails-team-mismatch", () => assertFailsWith(null, { FAKE_TEAM_MISMATCH: "1" }, "team"));
// both the app's and the sidecar's entitlements are compared: each must produce its own FAIL line
// (a check dropped for one of them must not hide behind the other)
test("verify-fails-entitlements-mismatch", () => {
  withHarness({ knobs: { FAKE_ENT_MISMATCH: "1" } }, (h) => {
    const { r } = runVerify(h);
    assert.equal(r.status, 1, r.stdout + r.stderr);
    const lines = failLines(r).map((l) => l.toLowerCase());
    assert.ok(lines.some((l) => l.includes("app: entitlements")), r.stdout);
    assert.ok(lines.some((l) => l.includes("sidecar smind: entitlements")), r.stdout);
  });
});
test("verify-fails-gatekeeper-rejection", () => assertFailsWith(null, { FAKE_SPCTL_FAIL: "1" }, "gatekeeper"));
test("verify-fails-without-staple", () => assertFailsWith(null, { FAKE_STAPLE_FAIL: "1" }, "stapled"));

test("verify-dmg-checks", () => {
  withHarness({}, (h) => {
    const dmg = join(h.tmp, "sMind.dmg");
    writeFileSync(dmg, "dmg-bytes");
    const { r } = runVerify(h, ["--dmg", dmg]);
    assert.equal(r.status, 0, r.stdout + r.stderr);
    const dmgOk = r.stdout.split("\n").filter((l) => l.startsWith("ok") && l.includes("dmg:"));
    assert.ok(dmgOk.length >= 3, r.stdout);
  });
  withHarness({ knobs: { FAKE_SPCTL_FAIL: "1" } }, (h) => {
    const dmg = join(h.tmp, "sMind.dmg");
    writeFileSync(dmg, "dmg-bytes");
    const { r } = runVerify(h, ["--dmg", dmg]);
    assert.equal(r.status, 1, r.stdout + r.stderr);
    assert.ok(failLines(r).some((l) => l.includes("dmg:")), r.stdout);
  });
});

test("verify-needs-no-secrets", () => {
  withHarness({ withSecrets: false }, (h) => {
    const { r } = runVerify(h);
    assert.equal(r.status, 0, r.stdout + r.stderr);
    assert.ok(!r.stdout.includes("FAIL"), r.stdout);
  });
});

// --- static checks (AC2, entitlements files) -----------------------------------------------------------

test("scripts-never-enable-xtrace", () => {
  const files = readdirSync(SCRIPTS_DIR).filter((f) => f.endsWith(".sh"));
  assert.ok(files.length >= 3, files.join(", "));
  for (const f of files) {
    const code = readFileSync(join(SCRIPTS_DIR, f), "utf8")
      .split("\n")
      .map((l) => l.replace(/#.*/, ""))
      .join("\n");
    assert.ok(!code.includes("set -x"), `${f} enables xtrace`);
    assert.ok(!code.includes("bash -x"), `${f} runs bash -x`);
    assert.ok(!code.includes("xtrace"), `${f} mentions xtrace outside a comment`);
  }
});

test("entitlements-files-are-valid-plists", () => {
  const files = readdirSync(ENTITLEMENTS_DIR).filter((f) => f.endsWith(".entitlements"));
  assert.equal(files.length, 2, files.join(", "));
  for (const f of files) {
    const text = readFileSync(join(ENTITLEMENTS_DIR, f), "utf8");
    assert.ok(text.trimStart().startsWith("<?xml"), `${f} is not XML`);
    assert.ok(text.includes("<plist"), `${f} has no plist`);
    assert.ok(text.includes("<dict/>"), `${f} is not an empty dict`);
    const lint = spawnSync("plutil", ["-lint", join(ENTITLEMENTS_DIR, f)], { encoding: "utf8" });
    if (!(lint.error && lint.error.code === "ENOENT")) {
      assert.equal(lint.status, 0, `${f}: ${lint.stdout}${lint.stderr}`);
    }
  }
});
