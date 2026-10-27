# Recipe: nightly "Windows full suite" failed on develop

`automation-windows-full-suite-failed.json` (next to this file) is the pilot
automation of ADR-0044. It is an example: nothing creates or enables it.

1. Make sure `gh` is signed in on the machine that runs the daemon
   (`gh auth status`), and that the repository has a workflow named
   `Windows full suite` (`.github/workflows/windows-full-suite.yml`).
2. Turn the engine on with a human edit of `config.yaml`:
   ```yaml
   automations:
     enabled: true
   ```
3. Find your ids: `smind workspace ls`, `smind profile ls` (use the Lead profile,
   `smind profile seed roles`). Copy the JSON, set `workspaceId` and
   `template.profileId`, and keep or change `template.model`.
4. `smind automation create --file <copy>.json`, then `smind automation enable <id>`.
5. Rehearse: `smind automation fire <id>` starts a task from the latest failed
   run (one forge call, same caps as a real signal, skips only the dedupe window).
6. Watch: `smind automation audit <id>`, Settings -> Automations, or the phone.

Rails in the example: 2 runs an hour, identical commit deduped for 24 h, auto-pause
after 3 failed runs in a row, $3 per run on the task tree, 45 minute wall clock,
memory on (16 KiB). Permission mode `default`: nothing is auto-approved.

`scripts/automation-pilot-live.sh` replays the whole story against an isolated
daemon with a scripted `gh` and a fake agent (no network, no LLM).
