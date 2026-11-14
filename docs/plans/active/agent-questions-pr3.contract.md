---
v: 1
milestone: "Agent questions PR3: question push kind (title only) and the mobile question card"
base: origin/develop
planRef: docs/plans/active/agent-questions.md
acs:
  - id: AC1
    text: "A question.pending bus event becomes a push of kind question whose body is a fixed sentence with no question text; resolved events never push; the id is the dedupe key."
    covers: { tests: [TestPush_QuestionTitleOnly, TestPushInput_QuestionPendingMapsToQuestionKind, TestPushInput_QuestionResolvedNeverNotifies, TestPush_QuestionDedupedByRequestID], mutations: [M1, M2] }
  - id: AC2
    text: "Only a device subscribed to question receives it; the kind is accepted by push.register."
    covers: { tests: [TestPush_QuestionKindIsSubscribable], mutations: [M3] }
  - id: AC3
    text: "The mobile board tracks questions from history and live events; an event beats a tap; a failed answer rolls back only without an event."
    covers: { tests: ["questionRequests.test.ts"], mutations: [M4] }
  - id: AC4
    text: "The answer payload and the submit rule: no preselection, submit disabled with a reason, empty fields omitted, decline note trimmed."
    covers: { tests: ["questionAnswer.test.ts"], mutations: [M5] }
  - id: AC5
    text: "Every resolution has a words; adversarial text and a long ask are handled as inert text and a collapse."
    covers: { tests: ["questionView.test.ts"], mutations: [M6] }
tests:
  - { name: TestPush_QuestionTitleOnly, run: "go test ./internal/push -run '^TestPush_QuestionTitleOnly$' -count=1", timeoutSec: 300 }
  - { name: TestPushInput_QuestionPendingMapsToQuestionKind, run: "go test ./internal/wsapi -run '^TestPushInput_QuestionPendingMapsToQuestionKind$' -count=1", timeoutSec: 300 }
  - { name: TestPushInput_QuestionResolvedNeverNotifies, run: "go test ./internal/wsapi -run '^TestPushInput_QuestionResolvedNeverNotifies$' -count=1", timeoutSec: 300 }
  - { name: TestPush_QuestionDedupedByRequestID, run: "go test ./internal/push -run '^TestPush_QuestionDedupedByRequestID$' -count=1", timeoutSec: 300 }
  - { name: TestPush_QuestionKindIsSubscribable, run: "go test ./internal/push ./internal/wsapi -run '^TestPush_QuestionKindIsSubscribable$' -count=1", timeoutSec: 300 }
  - { name: questionRequests.test.ts, run: "cd mobile && npx vitest run src/__tests__/questionRequests.test.ts", timeoutSec: 300 }
  - { name: questionAnswer.test.ts, run: "cd mobile && npx vitest run src/__tests__/questionAnswer.test.ts", timeoutSec: 300 }
  - { name: questionView.test.ts, run: "cd mobile && npx vitest run src/__tests__/questionView.test.ts", timeoutSec: 300 }
mutations:
  - { id: M1, file: "internal/push/notifier.go", change: "put the question text into the push body", expectFail: [TestPush_QuestionTitleOnly] }
  - { id: M2, file: "internal/wsapi/push.go", change: "map TopicQuestionResolved to a push too", expectFail: [TestPushInput_QuestionResolvedNeverNotifies] }
  - { id: M3, file: "internal/push/notifier.go", change: "drop question from AllKinds", expectFail: [TestPush_QuestionKindIsSubscribable] }
  - { id: M4, file: "mobile/src/questionRequests.ts", change: "a tap failure rolls back even after a resolved event", expectFail: ["questionRequests.test.ts"] }
  - { id: M5, file: "mobile/src/questionAnswer.ts", change: "preselect the first option", expectFail: ["questionAnswer.test.ts"] }
  - { id: M6, file: "mobile/src/questionView.ts", change: "raise the collapse threshold so a long ask never collapses", expectFail: ["questionView.test.ts"] }
---

# Contract: agent questions PR3

See the plan for the scenarios. The frames (390 light and dark, component-level) are judged by a human, not by this contract.
