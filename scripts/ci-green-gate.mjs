#!/usr/bin/env node
// CI-green gate for a release commit (docs/plans/active/
// desktop-release-hygiene.md, item 2): the --job job of the NEWEST push run
// of --workflow for exactly --sha must have concluded success, and --tag
// (if given) must still resolve to --sha. The GitHub API is reached only
// through `gh api <path>` (never a shell; the token stays in the
// environment and is never read or printed here). No dependencies.

import { execFileSync } from "node:child_process";
import process from "node:process";

const USAGE = `Usage: node scripts/ci-green-gate.mjs --repo <owner/name> --sha <40 hex> [--tag <vX.Y.Z>] [--workflow <file>] [--job <name>] [--timeout <seconds>] [--grace <seconds>] [--interval <seconds>] [--dry-run]

The newest push run of the workflow for exactly --sha decides (older runs
never mask newer ones); its --job job must conclude success. --tag must
resolve to --sha. Exit: 0 pass (any verdict under --dry-run), 1 refuse,
2 usage error, 3 persistent GitHub API error. Times are seconds:
--timeout waiting for the job to finish, --grace waiting for the run to
appear, --interval between polls.`;

const SHA_RE = /^[0-9a-f]{40}$/;
const REPO_RE = /^[\w.-]+\/[\w.-]+$/;
const NOT_FOUND_RE = /Not Found|HTTP 404|\b404\b/;
const API_ATTEMPTS = 3;
// With --interval 0 the tracked clock never advances, so each waiting phase
// is also capped by poll count to keep runs bounded.
const MAX_POLLS_AT_ZERO_INTERVAL = 5;

class UsageError extends Error {}
class ApiError extends Error {
  constructor(message, notFound) {
    super(message);
    this.notFound = notFound;
  }
}
class TagNotFoundError extends Error {}

const STRING_OPTIONS = new Set(["repo", "sha", "tag", "workflow", "job"]);
const NUMBER_OPTIONS = new Set(["timeout", "grace", "interval"]);

export function parseArgs(argv) {
  const args = { help: false, "dry-run": false };
  for (let i = 0; i < argv.length; i++) {
    const arg = argv[i];
    if (arg === "--help" || arg === "-h") {
      args.help = true;
      continue;
    }
    const eq = arg.indexOf("=");
    const name = (eq === -1 ? arg : arg.slice(0, eq)).replace(/^--/, "");
    if (name === "dry-run") {
      if (eq !== -1) throw new UsageError("option --dry-run takes no value");
      args["dry-run"] = true;
    } else if (STRING_OPTIONS.has(name) || NUMBER_OPTIONS.has(name)) {
      const value = eq === -1 ? argv[++i] : arg.slice(eq + 1);
      if (value === undefined) throw new UsageError(`missing value for --${name}`);
      if (NUMBER_OPTIONS.has(name)) {
        if (!/^\d+$/.test(value)) {
          throw new UsageError(`--${name} expects a number of seconds, found ${JSON.stringify(value)}`);
        }
        args[name] = Number(value);
      } else {
        args[name] = value;
      }
    } else {
      throw new UsageError(`unknown option ${arg}`);
    }
  }
  return args;
}

const oneLine = (text) => String(text).replace(/\s+/g, " ").trim();
const firstLine = (err) =>
  (String(err?.shortMessage ?? err?.message ?? err).split("\n")[0] || "").trim() || "gh api failed";

function newestRun(runs) {
  let newest = null;
  for (const run of runs) {
    if (
      newest === null ||
      Date.parse(run.created_at) > Date.parse(newest.created_at) ||
      (Date.parse(run.created_at) === Date.parse(newest.created_at) && run.id > newest.id)
    ) {
      newest = run;
    }
  }
  return newest;
}

// opts: {repo, sha, tag?, workflow?, job?, timeout?, grace?, interval?, dryRun?}
// deps: {gh(args) -> stdout string (throws on failure), sleep(ms), now() -> ms}
// Returns {pass, reason, exit}; prints the waiting lines and the final verdict.
export function gate(opts, deps) {
  const o = { workflow: "ci.yml", job: "ci", timeout: 2400, grace: 600, interval: 30, dryRun: false, ...opts };
  const say = (line) => process.stdout.write(`${line}\n`);
  const finish = (pass, reason, exit = pass ? 0 : 1) => {
    const text = oneLine(reason);
    const verdict = o.dryRun ? (pass ? "WOULD PASS" : "WOULD REFUSE") : pass ? "PASS" : "REFUSE";
    say(`${verdict}: ${text}`);
    if (!pass && process.env.GITHUB_ACTIONS) say(`::error::${text}`);
    return { pass, reason: text, exit: o.dryRun && exit === 1 ? 0 : exit };
  };
  const api = (path, notFoundIsFinal = false) => {
    let lastErr = null;
    for (let attempt = 1; attempt <= API_ATTEMPTS; attempt++) {
      if (attempt > 1) deps.sleep(o.interval * 1000);
      try {
        return JSON.parse(deps.gh(["api", path]));
      } catch (err) {
        lastErr = err;
        if (notFoundIsFinal && NOT_FOUND_RE.test(String(err?.message))) break;
      }
    }
    const notFound = notFoundIsFinal && NOT_FOUND_RE.test(String(lastErr?.message));
    throw new ApiError(`GitHub API error: ${firstLine(lastErr)}`, notFound);
  };
  const resolveTag = () => {
    try {
      let obj = api(`repos/${o.repo}/git/ref/tags/${o.tag}`, true).object;
      if (obj?.type === "tag") obj = api(`repos/${o.repo}/git/tags/${obj.sha}`, true).object;
      return obj;
    } catch (err) {
      if (err instanceof ApiError && err.notFound) throw new TagNotFoundError();
      throw err;
    }
  };
  try {
    if (o.tag) {
      const obj = resolveTag();
      if (obj?.type !== "commit" || obj.sha !== o.sha) {
        return finish(
          false,
          `tag ${o.tag} points at ${obj?.type ?? "no object"} ${obj?.sha ?? "?"}, expected commit ${o.sha}`
        );
      }
    }
    const runsPath = `repos/${o.repo}/actions/workflows/${o.workflow}/runs?head_sha=${o.sha}&event=push&per_page=50`;
    const startedAt = deps.now();
    let waited = 0;
    let polls = 0;
    let phase = "run-lookup";
    const elapsed = () => Math.max(deps.now() - startedAt, waited * 1000) / 1000;
    const outOfTime = (limit) => elapsed() >= limit || (o.interval === 0 && polls >= MAX_POLLS_AT_ZERO_INTERVAL);
    for (;;) {
      // Re-fetched every iteration: a re-run changes state, a newer run may appear.
      const newest = newestRun((api(runsPath).workflow_runs ?? []).filter((r) => r.head_sha === o.sha));
      if (phase !== (newest ? "job" : "run-lookup")) {
        phase = newest ? "job" : "run-lookup";
        polls = 0;
      }
      if (!newest) {
        const reason = `no ${o.workflow} push run found for ${o.sha}`;
        if (o.dryRun || outOfTime(o.grace)) return finish(false, reason);
        polls += 1;
        waited += o.interval;
        say(`waiting: ${reason} (${Math.round(elapsed())}s/${o.grace}s)`);
        deps.sleep(o.interval * 1000);
        continue;
      }
      const job = (api(`repos/${o.repo}/actions/runs/${newest.id}/jobs?per_page=100`).jobs ?? []).find(
        (j) => j.name === o.job
      );
      if (!job) return finish(false, `no job named ${o.job} in run ${newest.id}`);
      if (job.status !== "completed") {
        if (o.dryRun) return finish(false, `still running: ${o.job} job ${job.status} in run ${newest.id}`);
        if (outOfTime(o.timeout)) {
          return finish(false, `still running after ${o.timeout}s: ${o.job} job ${job.status} in run ${newest.id}`);
        }
        polls += 1;
        waited += o.interval;
        say(`waiting: ${o.job} job ${job.status} in run ${newest.id} (${Math.round(elapsed())}s/${o.timeout}s)`);
        deps.sleep(o.interval * 1000);
        continue;
      }
      return job.conclusion === "success"
        ? finish(true, `${o.job} job succeeded in run ${newest.id}`)
        : finish(false, `${o.job} job concluded ${job.conclusion} in run ${newest.id}`);
    }
  } catch (err) {
    if (err instanceof TagNotFoundError) return finish(false, `tag ${o.tag} not found`);
    if (err instanceof ApiError) return finish(false, err.message, 3);
    throw err;
  }
}

function realDeps() {
  return {
    gh: (args) => execFileSync("gh", args, { encoding: "utf8", maxBuffer: 32 * 1024 * 1024 }),
    sleep: (ms) => Atomics.wait(new Int32Array(new SharedArrayBuffer(4)), 0, 0, ms),
    now: () => Date.now(),
  };
}

function main(argv) {
  const args = parseArgs(argv);
  if (args.help) {
    process.stdout.write(`${USAGE}\n`);
    return 0;
  }
  const missing = ["repo", "sha"].filter((key) => !args[key]);
  if (missing.length > 0) {
    throw new UsageError(`missing required option${missing.length > 1 ? "s" : ""}: ${missing.map((k) => `--${k}`).join(" ")}`);
  }
  if (!SHA_RE.test(args.sha)) throw new UsageError(`--sha must be 40 hex characters, found ${JSON.stringify(args.sha)}`);
  if (!REPO_RE.test(args.repo)) throw new UsageError(`--repo must be owner/name, found ${JSON.stringify(args.repo)}`);
  return gate(
    {
      repo: args.repo,
      sha: args.sha,
      tag: args.tag,
      workflow: args.workflow ?? "ci.yml",
      job: args.job ?? "ci",
      timeout: args.timeout ?? 2400,
      grace: args.grace ?? 600,
      interval: args.interval ?? 30,
      dryRun: args["dry-run"],
    },
    realDeps()
  ).exit;
}

if (process.argv[1] && process.argv[1].endsWith("ci-green-gate.mjs")) {
  try {
    process.exitCode = main(process.argv.slice(2));
  } catch (err) {
    process.stderr.write(`ci-green-gate: ${err.message}\n`);
    process.exitCode = err instanceof UsageError ? 2 : 1;
  }
}
