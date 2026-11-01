---
v: 1
milestone: "Steward seat S3b: mobile review rows, review screen, permission rule line, read-only policy screen"
base: origin/develop
planRef: docs/plans/active/steward-seat.md
acs:
  - id: AC28
    text: "Mobile: inbox rows of kind review (verdict chip icon+text, summary without a verdict prefix, unknown kind never crashes), a review screen (gates failing first, findings by file with a severity or 'unrated' chip, evidence frames inline with tap-to-expand, copy findings, open PR, resolve; no merge/approve/always-allow anywhere), the permission card rule line ('Matched rule <id>' + why, or 'No rule matched') with the FULL normalised command wrapped, and a read-only Permission policy screen (validity banner, rules and audit as stacked rows, path); one column, 44 pt, no hover-only action; the client sends only steward.get/frame, policy.get/audit, inbox.resolve."
    covers: { tests: ["review row model", "steward verdict chip never throws", "steward gates failing first", "steward findings grouped by file with unrated", "steward findings prompt", "permission card rule line", "permission card full normalised command", "policy audit read-only", "policy view stacks rules and audit", "steward client sends only read methods", "steward mobile sources have no merge or approve control"], mutations: [M1, M2] }
tests:
  - { name: "review row model", run: "cd mobile && npx vitest run src/__tests__/inboxView.test.ts -t 'review row model'", timeoutSec: 600 }
  - { name: "steward verdict chip never throws", run: "cd mobile && npx vitest run src/__tests__/stewardView.test.ts -t 'steward verdict chip never throws'", timeoutSec: 600 }
  - { name: "steward gates failing first", run: "cd mobile && npx vitest run src/__tests__/stewardView.test.ts -t 'steward gates failing first'", timeoutSec: 600 }
  - { name: "steward findings grouped by file with unrated", run: "cd mobile && npx vitest run src/__tests__/stewardView.test.ts -t 'steward findings grouped by file with unrated'", timeoutSec: 600 }
  - { name: "steward findings prompt", run: "cd mobile && npx vitest run src/__tests__/stewardView.test.ts -t 'steward findings prompt'", timeoutSec: 600 }
  - { name: "permission card rule line", run: "cd mobile && npx vitest run src/__tests__/permissionPolicyLine.test.ts -t 'permission card rule line'", timeoutSec: 600 }
  - { name: "permission card full normalised command", run: "cd mobile && npx vitest run src/__tests__/permissionPolicyLine.test.ts -t 'permission card full normalised command'", timeoutSec: 600 }
  - { name: "policy audit read-only", run: "cd mobile && npx vitest run src/__tests__/policyView.test.ts -t 'policy audit read-only'", timeoutSec: 600 }
  - { name: "policy view stacks rules and audit", run: "cd mobile && npx vitest run src/__tests__/policyView.test.ts -t 'policy view stacks rules and audit'", timeoutSec: 600 }
  - { name: "steward client sends only read methods", run: "cd mobile && npx vitest run src/__tests__/stewardSafety.test.ts -t 'steward client sends only read methods'", timeoutSec: 600 }
  - { name: "steward mobile sources have no merge or approve control", run: "cd mobile && npx vitest run src/__tests__/stewardSafety.test.ts -t 'steward mobile sources have no merge or approve control'", timeoutSec: 600 }
mutations:
  - { id: M1, file: "mobile/src/inbox/view.ts", change: "keep the verdict row semantics wrong: map kind review to the verdict meta (label 'Verdict', section 'Validator verdicts')", expectFail: ["review row model"] }
  - { id: M2, file: "mobile/src/screens/task/PermissionPolicyLine.tsx", change: "render an 'Always allow' button", expectFail: ["steward mobile sources have no merge or approve control"] }
commands:
  - { id: C1, run: "cd mobile && npx tsc --noEmit", timeoutSec: 900 }
  - { id: C2, run: "cd mobile && npx vitest run --exclude '**/*.node.test.ts'", timeoutSec: 1800 }
  - { id: C3, run: "task lint", timeoutSec: 900 }
writeScope:
  paths: []
limits: { budgetUsd: 2, maxMinutes: 60 }
---

# S3b validation contract

Judge the branch head against AC28, ADR-0047 sections 5 and 9, and S3a's web card. Attack targets: (1) any mobile control or RPC that approves, merges, always-allows or answers in bulk; (2) the permission command clipped, ellipsised or in a horizontal-only scroller; (3) a crash on an unknown inbox kind, verdict, gate state or rule action; (4) the policy screen offering any write; (5) a push screen without the stock header and back arrow; (6) anything wider than a 390 px viewport. Frames come from the react-native-web export against an isolated daemon with fake data; native device behaviour is the Human's group E and is not verified.
