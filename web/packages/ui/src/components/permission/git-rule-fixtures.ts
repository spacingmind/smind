import type { PermissionDetail } from "@/lib/types";

/** Sample git-rule permission details (G13) for tests and screenshot seeding. */

export const gitPushDetail: PermissionDetail = {
  kind: "execute",
  command: "git push origin smind/task-x",
  gitRule: {
    id: "git-push",
    class: "always-ask",
    label: "git push always needs your approval",
    command: "git push origin smind/task-x",
    remote: "origin",
    branch: "smind/task-x",
    perCall: true,
  },
};

export const gitCommitDetail: PermissionDetail = {
  kind: "execute",
  command: 'git commit -m "feat: add parser"',
  gitRule: {
    id: "git-commit",
    class: "commit",
    label: "git commit",
    command: 'git commit -m "feat: add parser"',
    branch: "smind/task-x",
    perCall: true,
    stagedFiles: ["internal/parser/parse.go", "internal/parser/parse_test.go", "docs/plans/active/parser.md", "README.md"],
  },
};

export const gitForcePushDetail: PermissionDetail = {
  kind: "execute",
  command: "git push --force origin smind/task-x",
  gitRule: {
    id: "git-push-force",
    class: "always-ask",
    label: "git push --force always needs your approval",
    command: "git push --force origin smind/task-x",
    remote: "origin",
    branch: "smind/task-x",
    perCall: true,
    note: "Force push rewrites remote history.",
  },
};

export const gitPipelineDetail: PermissionDetail = {
  kind: "execute",
  command: `sh -c 'cd sub && git push origin HEAD'`,
  gitRule: {
    id: "git-push",
    class: "always-ask",
    label: "git push always needs your approval",
    command: "git push origin HEAD",
    remote: "origin",
    branch: "HEAD",
    perCall: true,
    note: "Found inside a shell command: cd sub && git push origin HEAD",
  },
};
