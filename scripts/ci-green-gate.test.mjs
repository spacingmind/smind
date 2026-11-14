// Tests for scripts/ci-green-gate.mjs (docs/plans/active/
// desktop-release-hygiene.md, item 2). A fake `gh` on PATH serves canned
// GitHub API responses from a temp dir; the CLI is exercised end to end
// with tiny --interval/--timeout/--grace values so polling stays bounded.

import { test } from "node:test";
import assert from "node:assert/strict";
import { spawnSync } from "node:child_process";
import { chmodSync, mkdirSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

const scriptDir = dirname(fileURLToPath(import.meta.url));
const cli = join(scriptDir, "ci-green-gate.mjs");
const REPO = "spacingmind/smind";
const SHA = "a".repeat(40);
const OTHER_SHA = "b".repeat(40);
const TAG_SHA = "c".repeat(40);
const TAG = "v1.2.3";
const RUNS = `repos/${REPO}/actions/workflows/ci.yml/runs?head_sha=${SHA}&event=push&per_page=50`;
const jobsOf = (id) => `repos/${REPO}/actions/runs/${id}/jobs?per_page=100`;
const refOf = (tag) => `repos/${REPO}/git/ref/tags/${tag}`;
const tagObj = (sha) => `repos/${REPO}/git/tags/${sha}`;

// The fake gh: every call's key (the args after `api`, joined by a space)
// is appended to calls.log; responses.json maps a key to [resp, resp, ...]
// where each call returns the next element and the last one repeats. A resp
// is a JSON object to print on stdout, or {__error, __exit} to fail with.
// A key with no entry answers "Not Found (HTTP 404)".
const FAKE_GH = `#!/usr/bin/env node
import { appendFileSync, readFileSync } from "node:fs";
import { join } from "node:path";
const dir = process.env.FAKE_GH_DIR;
const key = process.argv.slice(3).join(" ");
appendFileSync(join(dir, "calls.log"), key + "\\n");
const list = JSON.parse(readFileSync(join(dir, "responses.json"), "utf8"))[key] ?? [];
const prior = readFileSync(join(dir, "calls.log"), "utf8").split("\\n").filter((line) => line === key).length - 1;
const resp = list[Math.min(Math.max(prior, 0), list.length - 1)];
if (resp === undefined) {
  process.stderr.write("gh: Not Found (HTTP 404)\\n");
  process.exit(1);
}
if (resp.__error !== undefined) {
  process.stderr.write(resp.__error + "\\n");
  process.exit(resp.__exit ?? 1);
}
process.stdout.write(JSON.stringify(resp) + "\\n");
`;

function withGh(responses, fn) {
  const root = mkdtempSync(join(tmpdir(), "gate-gh-"));
  try {
    writeFileSync(join(root, "responses.json"), JSON.stringify(responses));
    writeFileSync(join(root, "calls.log"), "");
    const bin = join(root, "bin");
    mkdirSync(bin);
    writeFileSync(join(bin, "gh"), FAKE_GH);
    chmodSync(join(bin, "gh"), 0o755);
    const calls = () => readFileSync(join(root, "calls.log"), "utf8").split("\n").filter((line) => line !== "");
    fn({ root, bin, calls });
  } finally {
    rmSync(root, { recursive: true, force: true });
  }
}

const BASE = ["--repo", REPO, "--sha", SHA, "--interval", "0", "--timeout", "1", "--grace", "1"];

function spawnGate(gh, args) {
  return spawnSync(process.execPath, [cli, ...args], {
    encoding: "utf8",
    env: {
      ...process.env,
      PATH: `${gh.bin}:${process.env.PATH}`,
      FAKE_GH_DIR: gh.root,
      GH_TOKEN: "sekret-token-123",
    },
  });
}

const runGate = (gh, extra = []) => spawnGate(gh, [...BASE, ...extra]);

const run = (over = {}) => ({
  id: 111,
  status: "completed",
  conclusion: "success",
  created_at: "2026-11-14T00:00:00Z",
  head_sha: SHA,
  head_branch: "develop",
  run_attempt: 1,
  ...over,
});
const runsList = (...rs) => ({ workflow_runs: rs });
const job = (over = {}) => ({ name: "ci", status: "completed", conclusion: "success", ...over });
const jobsList = (...js) => ({ jobs: js });

const green = { [RUNS]: [runsList(run())], [jobsOf(111)]: [jobsList(job())] };
const red = { [RUNS]: [runsList(run())], [jobsOf(111)]: [jobsList(job({ conclusion: "failure" }))] };

test("gate-green-passes", () => {
  withGh(green, (gh) => {
    const res = runGate(gh);
    assert.equal(res.status, 0);
    assert.match(res.stdout, /^PASS: ci job succeeded in run 111$/m);
  });
});

test("gate-failed-run-refuses", () => {
  withGh(red, (gh) => {
    const res = runGate(gh);
    assert.equal(res.status, 1);
    assert.match(res.stdout, /^REFUSE: ci job concluded failure in run 111$/m);
  });
});

test("gate-cancelled-refuses", () => {
  withGh({ [RUNS]: [runsList(run())], [jobsOf(111)]: [jobsList(job({ conclusion: "cancelled" }))] }, (gh) => {
    const res = runGate(gh);
    assert.equal(res.status, 1);
    assert.match(res.stdout, /^REFUSE: ci job concluded cancelled in run 111$/m);
  });
});

test("gate-skipped-job-refuses", () => {
  withGh({ [RUNS]: [runsList(run())], [jobsOf(111)]: [jobsList(job({ conclusion: "skipped" }))] }, (gh) => {
    const res = runGate(gh);
    assert.equal(res.status, 1);
    assert.match(res.stdout, /^REFUSE: ci job concluded skipped in run 111$/m);
  });
});

test("gate-bad-sha-usage-error", () => {
  withGh(green, (gh) => {
    let res = spawnGate(gh, ["--repo", REPO, "--sha", "deadbeef"]);
    assert.equal(res.status, 2);
    assert.match(res.stderr, /--sha/);
    res = spawnGate(gh, ["--repo", "bad repo", "--sha", SHA]);
    assert.equal(res.status, 2);
    assert.match(res.stderr, /--repo/);
  });
});

test("gate-path-shaped-input-usage-error", () => {
  withGh(green, (gh) => {
    for (const [opt, value] of [["--tag", "v1/../x"], ["--tag", "v1?x=y"], ["--workflow", "a/b.yml"], ["--workflow", "ci.yml?x"]]) {
      const res = spawnGate(gh, ["--repo", REPO, "--sha", SHA, opt, value]);
      assert.equal(res.status, 2, `${opt} ${value}`);
      assert.match(res.stderr, new RegExp(opt));
      assert.deepEqual(gh.calls(), [], "no API call was made for a bad option");
    }
  });
});

test("gate-missing-args-usage-error", () => {
  withGh(green, (gh) => {
    let res = spawnGate(gh, []);
    assert.equal(res.status, 2);
    assert.match(res.stderr, /missing required options: --repo --sha/);
    res = spawnGate(gh, ["--repo", REPO]);
    assert.equal(res.status, 2);
    assert.match(res.stderr, /missing required option: --sha/);
    res = spawnGate(gh, ["--frobnicate"]);
    assert.equal(res.status, 2);
    assert.match(res.stderr, /unknown option --frobnicate/);
    res = spawnGate(gh, ["--repo", REPO, "--sha", SHA, "--timeout", "soon"]);
    assert.equal(res.status, 2);
    assert.match(res.stderr, /--timeout/);
  });
});

test("gate-tag-moved-refuses", () => {
  withGh({ [refOf(TAG)]: [{ object: { type: "commit", sha: OTHER_SHA } }], ...green }, (gh) => {
    const res = runGate(gh, ["--tag", TAG]);
    assert.equal(res.status, 1);
    assert.match(res.stdout, new RegExp(`^REFUSE: tag ${TAG} points at commit ${OTHER_SHA}, expected commit ${SHA}$`, "m"));
    assert.deepEqual(gh.calls(), [refOf(TAG)]); // refused before any polling
  });
});

test("gate-annotated-tag-resolves-and-passes", () => {
  withGh(
    {
      [refOf(TAG)]: [{ object: { type: "tag", sha: TAG_SHA } }],
      [tagObj(TAG_SHA)]: [{ object: { type: "commit", sha: SHA } }],
      ...green,
    },
    (gh) => {
      const res = runGate(gh, ["--tag", TAG]);
      assert.equal(res.status, 0);
      assert.match(res.stdout, /^PASS: ci job succeeded in run 111$/m);
      assert.deepEqual(gh.calls(), [refOf(TAG), tagObj(TAG_SHA), RUNS, jobsOf(111)]);
    }
  );
});

test("gate-tag-not-found-refuses", () => {
  withGh(green, (gh) => {
    const res = runGate(gh, ["--tag", TAG]);
    assert.equal(res.status, 1);
    assert.match(res.stdout, /^REFUSE: tag v1\.2\.3 not found$/m);
    assert.deepEqual(gh.calls(), [refOf(TAG)]); // refused before any polling
  });
});

test("gate-missing-run-refuses-after-grace", () => {
  withGh({ [RUNS]: [runsList()] }, (gh) => {
    const res = runGate(gh);
    assert.equal(res.status, 1);
    assert.match(res.stdout, new RegExp(`^REFUSE: no ci\\.yml push run found for ${SHA}$`, "m"));
  });
});

test("gate-in-progress-then-green-passes", () => {
  withGh(
    {
      [RUNS]: [runsList(run({ status: "in_progress", conclusion: null }))],
      [jobsOf(111)]: [jobsList(job({ status: "in_progress", conclusion: null })), jobsList(job())],
    },
    (gh) => {
      const res = runGate(gh);
      assert.equal(res.status, 0);
      assert.match(res.stdout, /^PASS: ci job succeeded in run 111$/m);
    }
  );
});

test("gate-in-progress-timeout-refuses", () => {
  withGh(
    {
      [RUNS]: [runsList(run({ status: "in_progress", conclusion: null }))],
      [jobsOf(111)]: [jobsList(job({ status: "in_progress", conclusion: null }))],
    },
    (gh) => {
      const res = runGate(gh);
      assert.equal(res.status, 1);
      assert.match(res.stdout, /^REFUSE: still running after 1s: ci job in_progress in run 111$/m);
    }
  );
});

test("gate-rerun-after-failure-passes", () => {
  withGh(
    {
      [RUNS]: [
        runsList(
          run({ id: 110, conclusion: "failure", created_at: "2026-11-14T00:00:00Z" }),
          run({ id: 111, created_at: "2026-11-14T01:00:00Z" })
        ),
      ],
      [jobsOf(110)]: [jobsList(job({ conclusion: "failure" }))],
      [jobsOf(111)]: [jobsList(job())],
    },
    (gh) => {
      const res = runGate(gh);
      assert.equal(res.status, 0);
      assert.match(res.stdout, /^PASS: ci job succeeded in run 111$/m);
      assert.ok(!gh.calls().includes(jobsOf(110)), "the older failed run must not be consulted");
    }
  );
});

test("gate-newest-run-wins-failure", () => {
  withGh(
    {
      [RUNS]: [
        runsList(
          run({ id: 110, created_at: "2026-11-14T00:00:00Z" }),
          run({ id: 111, conclusion: "failure", created_at: "2026-11-14T01:00:00Z" })
        ),
      ],
      [jobsOf(110)]: [jobsList(job())],
      [jobsOf(111)]: [jobsList(job({ conclusion: "failure" }))],
    },
    (gh) => {
      const res = runGate(gh);
      assert.equal(res.status, 1);
      assert.match(res.stdout, /^REFUSE: ci job concluded failure in run 111$/m);
    }
  );
});

test("gate-other-sha-run-ignored", () => {
  withGh({ [RUNS]: [runsList(run({ head_sha: OTHER_SHA }))] }, (gh) => {
    const res = runGate(gh);
    assert.equal(res.status, 1);
    assert.match(res.stdout, new RegExp(`^REFUSE: no ci\\.yml push run found for ${SHA}$`, "m"));
  });
});

test("gate-job-missing-refuses", () => {
  withGh({ [RUNS]: [runsList(run())], [jobsOf(111)]: [jobsList(job({ name: "build" }))] }, (gh) => {
    const res = runGate(gh);
    assert.equal(res.status, 1);
    assert.match(res.stdout, /^REFUSE: no job named ci in run 111$/m);
  });
});

test("gate-api-error-retries-then-passes", () => {
  withGh(
    {
      [RUNS]: [{ __error: "gh: temporary (HTTP 502)", __exit: 1 }, runsList(run())],
      [jobsOf(111)]: [jobsList(job())],
    },
    (gh) => {
      const res = runGate(gh);
      assert.equal(res.status, 0);
      assert.match(res.stdout, /^PASS: ci job succeeded in run 111$/m);
      assert.equal(gh.calls().filter((call) => call === RUNS).length, 2, "the runs call must be retried exactly once");
    }
  );
});

test("gate-api-error-persistent-refuses", () => {
  withGh({ [RUNS]: [{ __error: "gh: sustained (HTTP 502)", __exit: 1 }] }, (gh) => {
    const res = runGate(gh);
    assert.equal(res.status, 3);
    assert.match(res.stdout, /^REFUSE: GitHub API error: /m);
  });
});

test("gate-dry-run-never-fails", () => {
  withGh(red, (gh) => {
    const res = runGate(gh, ["--dry-run"]);
    assert.equal(res.status, 0);
    assert.match(res.stdout, /^WOULD REFUSE: ci job concluded failure in run 111$/m);
  });
  withGh(green, (gh) => {
    const res = runGate(gh, ["--dry-run"]);
    assert.equal(res.status, 0);
    assert.match(res.stdout, /^WOULD PASS: ci job succeeded in run 111$/m);
  });
});

test("gate-dry-run-does-not-wait", () => {
  withGh(
    {
      [RUNS]: [runsList(run({ status: "in_progress", conclusion: null }))],
      [jobsOf(111)]: [jobsList(job({ status: "in_progress", conclusion: null }))],
    },
    (gh) => {
      const res = runGate(gh, ["--dry-run"]);
      assert.equal(res.status, 0);
      assert.match(res.stdout, /^WOULD REFUSE: still running: ci job in_progress in run 111$/m);
      assert.deepEqual(gh.calls(), [RUNS, jobsOf(111)], "one pass only, no polling");
    }
  );
});

test("gate-token-never-printed", () => {
  withGh(red, (gh) => {
    const res = runGate(gh);
    assert.equal(res.status, 1);
    assert.ok(!`${res.stdout}${res.stderr}`.includes("sekret-token-123"));
  });
  withGh(green, (gh) => {
    const res = runGate(gh);
    assert.equal(res.status, 0);
    assert.ok(!`${res.stdout}${res.stderr}`.includes("sekret-token-123"));
  });
});

test("gate-other-branch-run-ignored", () => {
  withGh({ [RUNS]: [runsList(run({ id: 120, head_branch: "master" }))] }, (gh) => {
    const res = runGate(gh);
    assert.equal(res.status, 1);
    assert.match(res.stdout, /no ci\.yml push run/);
  });
});

test("gate-equal-timestamps-higher-run-id-wins", () => {
  const same = "2026-11-14T00:00:00Z";
  withGh(
    {
      [RUNS]: [runsList(run({ id: 131, created_at: same }), run({ id: 130, created_at: same }))],
      [`repos/${REPO}/actions/runs/131/jobs?per_page=100`]: [{ jobs: [{ name: "ci", status: "completed", conclusion: "failure" }] }],
      [`repos/${REPO}/actions/runs/130/jobs?per_page=100`]: [{ jobs: [{ name: "ci", status: "completed", conclusion: "success" }] }],
    },
    (gh) => {
      const res = runGate(gh);
      assert.equal(res.status, 1, "run 131 (higher id) decides");
      assert.match(res.stdout, /run 131/);
    }
  );
});

test("gate-unparsable-created-at-never-wins", () => {
  withGh(
    {
      [RUNS]: [runsList(run({ id: 141, created_at: "garbage" }), run({ id: 140, created_at: "2026-11-14T00:00:00Z" }))],
      [`repos/${REPO}/actions/runs/141/jobs?per_page=100`]: [{ jobs: [{ name: "ci", status: "completed", conclusion: "failure" }] }],
      [`repos/${REPO}/actions/runs/140/jobs?per_page=100`]: [{ jobs: [{ name: "ci", status: "completed", conclusion: "success" }] }],
    },
    (gh) => {
      const res = runGate(gh);
      assert.equal(res.status, 0);
      assert.match(res.stdout, /run 140/);
    }
  );
});

test("gate-404-in-tag-name-is-not-not-found", () => {
  // a transient error on a tag whose name contains "404" must be retried, not read as a missing tag
  const tag = "v0.404.0";
  withGh({ [`repos/${REPO}/git/ref/tags/${tag}`]: [{ __error: "HTTP 502 Bad Gateway", __exit: 1 }] }, (gh) => {
    const res = runGate(gh, ["--tag", tag]);
    assert.equal(res.status, 3);
    assert.equal(gh.calls().filter((c) => c.includes(`git/ref/tags/${tag}`)).length, 3, "retried 3 times");
  });
});

