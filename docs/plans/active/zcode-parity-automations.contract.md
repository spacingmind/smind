---
v: 1
milestone: "ZCode parity stage 7: Automations page, off-state explainer, task chip"
base: origin/develop
planRef: docs/plans/active/zcode-parity.md
acs:
  - id: AC-4
    text: "smind config set automations.enabled true|false edits config.yaml keeping comments; any other value is rejected naming the key."
    covers: { tests: [TestConfigSetAutomationsEnabled, TestConfigSetAutomationsEnabledRejectsNonBool] }
  - id: AC-5
    text: "task.list and task.get expose automationId/automationName only for a task an automation fired (audit decision fire); every other task has neither field."
    covers: { tests: [TestAutomationLinksForTasks, TestTaskListExposesAutomationLink] }
  - id: AC-3
    text: "Off state shows the explainer with the exact CLI command and restart line; no UI control flips the daemon flag."
    covers: { tests: ["shows the exact command and copies it", "off state shows the card, not the list"] }
  - id: AC-1
    text: "Automations page opens from the sidebar quick action and the palette; Settings only links to it."
    covers: { tests: ["sidebar quick action opens the page", "palette command opens the page", "settings section only links to the page"] }
  - id: AC-6
    text: "Automation chip on sidebar row, header and inbox row; the filter shows only automation tasks."
    covers: { tests: ["renders the name on hover", "filter shows only automation tasks"] }
tests:
  - { name: TestConfigSetAutomationsEnabled, run: "go test ./internal/config -run '^TestConfigSetAutomationsEnabled$' -count=1", timeoutSec: 300 }
  - { name: TestConfigSetAutomationsEnabledRejectsNonBool, run: "go test ./internal/config -run '^TestConfigSetAutomationsEnabledRejectsNonBool$' -count=1", timeoutSec: 300 }
  - { name: TestAutomationLinksForTasks, run: "go test ./internal/store -run '^TestAutomationLinksForTasks$' -count=1", timeoutSec: 300 }
  - { name: TestTaskListExposesAutomationLink, run: "go test ./internal/wsapi -run '^TestTaskListExposesAutomationLink$' -count=1", timeoutSec: 300 }
mutations:
  - { id: M1, file: internal/config/edit.go, change: "accept any string for the bool key", expectFail: [TestConfigSetAutomationsEnabledRejectsNonBool] }
  - { id: M2, file: internal/store/automations.go, change: "drop the decision = 'fire' condition in AutomationLinksForTasks", expectFail: [TestAutomationLinksForTasks] }
  - { id: M3, file: internal/wsapi/task_budget.go, change: "do not fill the link in viewTasks", expectFail: [TestTaskListExposesAutomationLink] }
commands:
  - { id: C1, run: "task lint", timeoutSec: 900 }
  - { id: C2, run: "task test", timeoutSec: 1500 }
writeScope:
  paths: []
limits: { budgetUsd: 2, maxMinutes: 60 }
---

# Stage 7 validation contract

Judge the head against the ACs and `docs/plans/active/zcode-parity.md` Stage 7. Attack targets: (1) the task wire leaking an automation name for a task the automation did not start, or for a deleted automation; (2) `config set` writing a non-bool or dropping comments; (3) any UI path that flips `automations.enabled` (ADR-0044 keeps it file/CLI-only); (4) Settings still rendering a second copy of the list.
