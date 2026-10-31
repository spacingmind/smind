package permpolicy

// Template is the file `smind policy init` writes. Its only active lines are
// `version: 1` and `rules: []`: the example rules sit behind `#> ` so a
// human opts in to each one, and un-commenting them is a mechanical strip of
// that prefix. Zero active allow rules is what lets a fresh install turn the
// engine on without it approving anything.
const Template = `# smind permission policy (human-edited; nothing in smind writes this file).
#
# The engine is OFF until config.yaml has:
#   steward:
#     policy:
#       enabled: true
#
# Evaluation order is deny > ask > allow, by action and not by file order.
# A request no rule allows is "ask": it goes to a human, and an ask nobody
# answers within 5 minutes is denied. Every decision is audited; read the
# trail with ` + "`smind policy audit`" + `.
#
# The floor is compiled in and can never be overridden by any rule here:
#   no-stash  no-force-push  no-merge  no-install-state  no-secrets
#   no-destructive-outside  no-curl-sh  no-real-home-tests  no-add-all
#
# Every allow needs a tool, a path scope (match.paths.within) and a why of
# at most 160 characters. To opt in to an example, delete its leading "#> "
# and put the rule under "rules:" (replace the "[]").
version: 1
rules: []

#> - id: go-test
#>   action: allow
#>   why: "Run Go tests inside the task worktree"
#>   match:
#>     tool: Bash
#>     command: {argv0: go, subcommand: test}
#>     paths: {within: ["{worktree}"]}
#> - id: task-test
#>   action: allow
#>   why: "Run task test inside the task worktree"
#>   match:
#>     tool: Bash
#>     command: {argv0: task, subcommand: test}
#>     paths: {within: ["{worktree}"]}
#> - id: task-lint
#>   action: allow
#>   why: "Run task lint inside the task worktree"
#>   match:
#>     tool: Bash
#>     command: {argv0: task, subcommand: lint}
#>     paths: {within: ["{worktree}"]}
#> - id: bunx-tsc
#>   action: allow
#>   why: "Type-check the web UI inside the task worktree"
#>   match:
#>     tool: Bash
#>     command: {argv0: bunx, subcommand: tsc}
#>     paths: {within: ["{worktree}"]}
#> - id: gh-pr-view
#>   action: allow
#>   why: "Read-only: view a pull request"
#>   match:
#>     tool: Bash
#>     command: {argv0: gh, subcommand: "pr view"}
#>     paths: {within: ["{worktree}"]}
#> - id: gh-pr-checks
#>   action: allow
#>   why: "Read-only: list CI checks of a pull request"
#>   match:
#>     tool: Bash
#>     command: {argv0: gh, subcommand: "pr checks"}
#>     paths: {within: ["{worktree}"]}
#> - id: gh-pr-diff
#>   action: allow
#>   why: "Read-only: show the diff of a pull request"
#>   match:
#>     tool: Bash
#>     command: {argv0: gh, subcommand: "pr diff"}
#>     paths: {within: ["{worktree}"]}
#> - id: lead-push-own-branch
#>   action: allow
#>   why: "A Lead pushes its own task branch, never forced"
#>   match:
#>     seats: [lead]
#>     tool: Bash
#>     command:
#>       argv0: git
#>       subcommand: push
#>       args:
#>         allow_only: ["origin", "{taskBranch}"]
#>         deny_flags: ["-f", "--force", "--force-with-lease", "--delete", "--mirror", "--all", "--tags"]
#>     paths: {within: ["{worktree}"]}
#> - id: deny-curl
#>   action: deny
#>   why: "No agent fetches from the network with curl"
#>   match:
#>     tool: Bash
#>     command: {argv0: curl}
#> - id: ask-pr-create
#>   action: ask
#>   why: "A human reads the title and body before a PR is opened"
#>   match:
#>     tool: Bash
#>     command: {argv0: gh, subcommand: "pr create"}
`
