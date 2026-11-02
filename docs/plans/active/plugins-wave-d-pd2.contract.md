---
v: 1
milestone: "Plugins Wave D PD2: scrubbed confined launch, secrets at launch, probe, consent"
base: origin/develop
planRef: docs/plans/active/plugins-wave-d.md
acs:
  - id: AC-D1
    text: "The agent child of a plugin provider gets a BUILT environment, never the daemon's: fixed PATH, private HOME (or the real HOME only with inheritHome), the manifest literals and the resolved secrets, no SMIND_*, no daemon token, no passthrough; a plugin cannot override PATH or HOME."
    covers: { tests: [TestPluginAgent_BaseEnv_BuiltNeverInherited, TestPluginProvider_AgentEnv_ScrubbedAndBuilt, TestPluginProvider_InheritHome_OnlyHome, TestPluginProvider_AgentEnv_NoPassthrough] }
  - id: AC-D2
    text: "The command is confined at LAUNCH, not only at registration: a plugin-relative path is re-resolved and re-checked (inside, regular, no symlink, executable) before every spawn; a bare name is looked up on the fixed system PATH only and resolved to an absolute path before the spawn."
    covers: { tests: [TestPluginAgent_ConfinedExecutable, TestPluginAgent_ResolveBareName_FixedPathOnly, TestPluginProvider_Launch_RechecksConfinedCommand, TestPluginProvider_BareName_LooksOnFixedPathOnly] }
  - id: AC-D3
    text: "Secrets are resolved from provider_secrets into the agent child env only, never argv, logs, events, provider.list, errors, transcript or the plugin process; a required unset slot refuses the launch naming the slot; removal deletes them, disable keeps them."
    covers: { tests: [TestPluginProvider_Secret_ResolvedIntoChildEnvOnly, TestPluginProvider_RequiredSecretUnset_LaunchRefusedNamingSlot, TestPluginProvider_Secret_NeverInArgvLogsEventsTranscript, TestPluginProvider_Secret_NotInPluginProcessEnvOrInitialize, TestPluginRemove_DeletesProviderSecrets_DisableKeeps] }
  - id: AC-D4
    text: "A plugin provider runs through runACP with events, usage, model selection and modes unchanged, and its errors are classified exactly as a custom ACP provider's."
    covers: { tests: [TestPluginProvider_RunsViaACP_FakeAgent_UsageAndEvents, TestPluginProvider_ModelSelectionAndModes, TestPluginProvider_FailureClasses_SameClassifier] }
  - id: AC-D5
    text: "provider.test and the ACP probes never execute a plugin command outside the scrubbed launch: the version probe runs only through the same scrubbed path (built env, plugin-dir cwd, timeout, no shell), and no other site can run the command."
    covers: { tests: [TestPluginProvider_NoExecutionSite, TestProviderTest_PluginProvider_NeverExecutesTheCommand, TestProviderTest_PluginProvider_VersionProbe_ScrubbedEnv, TestRunScopedMethods_NoProviderExecOrWriteMethod] }
  - id: AC-D6
    text: "Consent shows the exact argv, how argv[0] resolves, env KEYS (never values), secret slot names and inheritHome; any of them, and a literal env value, changes the fingerprint; a changed update stays pending with the old version serving; a linked reload that changes it disables the plugin."
    covers: { tests: [TestConsent_Provider_FingerprintCoversArgvEnvValuesAndInheritHome, TestConsent_Provider_ValueNeverInSummaryOrLogs, TestPluginUpdate_ProviderCommandChange_PendingKeepsOldRegistered, TestPluginUpdate_ProviderSameFingerprint_Applies, TestPluginReload_Linked_ProviderChange_DisablesUntilConfirm] }
  - id: AC-D7
    text: "The provider id is never used as a path component or git ref, and every plugin-provider error/log line names the plugin."
    covers: { tests: [TestPluginProvider_SlashID_NeverUsedAsPathOrRef, TestPluginProvider_ErrorsAndLogs_LabeledByPlugin] }
tests:
  - { name: TestPluginAgent_BaseEnv_BuiltNeverInherited, run: "go test ./internal/pluginagent -run '^TestPluginAgent_BaseEnv_BuiltNeverInherited$' -count=1", timeoutSec: 300 }
  - { name: TestPluginAgent_ConfinedExecutable, run: "go test ./internal/pluginagent -run '^TestPluginAgent_ConfinedExecutable$' -count=1", timeoutSec: 300 }
  - { name: TestPluginAgent_ResolveBareName_FixedPathOnly, run: "go test ./internal/pluginagent -run '^TestPluginAgent_ResolveBareName_FixedPathOnly$' -count=1", timeoutSec: 300 }
  - { name: TestPluginProvider_AgentEnv_ScrubbedAndBuilt, run: "go test ./internal/taskrunner -run '^TestPluginProvider_AgentEnv_ScrubbedAndBuilt$' -count=1", timeoutSec: 300 }
  - { name: TestPluginProvider_InheritHome_OnlyHome, run: "go test ./internal/taskrunner -run '^TestPluginProvider_InheritHome_OnlyHome$' -count=1", timeoutSec: 300 }
  - { name: TestPluginProvider_AgentEnv_NoPassthrough, run: "go test ./internal/taskrunner -run '^TestPluginProvider_AgentEnv_NoPassthrough$' -count=1", timeoutSec: 300 }
  - { name: TestPluginProvider_Launch_RechecksConfinedCommand, run: "go test ./internal/taskrunner -run '^TestPluginProvider_Launch_RechecksConfinedCommand$' -count=1", timeoutSec: 300 }
  - { name: TestPluginProvider_BareName_LooksOnFixedPathOnly, run: "go test ./internal/taskrunner -run '^TestPluginProvider_BareName_LooksOnFixedPathOnly$' -count=1", timeoutSec: 300 }
  - { name: TestPluginProvider_Secret_ResolvedIntoChildEnvOnly, run: "go test ./internal/taskrunner -run '^TestPluginProvider_Secret_ResolvedIntoChildEnvOnly$' -count=1", timeoutSec: 300 }
  - { name: TestPluginProvider_RequiredSecretUnset_LaunchRefusedNamingSlot, run: "go test ./internal/taskrunner -run '^TestPluginProvider_RequiredSecretUnset_LaunchRefusedNamingSlot$' -count=1", timeoutSec: 300 }
  - { name: TestPluginProvider_RunsViaACP_FakeAgent_UsageAndEvents, run: "go test ./internal/taskrunner -run '^TestPluginProvider_RunsViaACP_FakeAgent_UsageAndEvents$' -count=1", timeoutSec: 300 }
  - { name: TestPluginProvider_ModelSelectionAndModes, run: "go test ./internal/taskrunner -run '^TestPluginProvider_ModelSelectionAndModes$' -count=1", timeoutSec: 300 }
  - { name: TestPluginProvider_FailureClasses_SameClassifier, run: "go test ./internal/taskrunner -run '^TestPluginProvider_FailureClasses_SameClassifier$' -count=1", timeoutSec: 300 }
  - { name: TestPluginProvider_NoExecutionSite, run: "go test ./internal/taskrunner -run '^TestPluginProvider_NoExecutionSite$' -count=1", timeoutSec: 300 }
  - { name: TestProviderTest_PluginProvider_NeverExecutesTheCommand, run: "go test ./internal/wsapi -run '^TestProviderTest_PluginProvider_NeverExecutesTheCommand$' -count=1", timeoutSec: 300 }
  - { name: TestProviderTest_PluginProvider_VersionProbe_ScrubbedEnv, run: "go test ./internal/wsapi -run '^TestProviderTest_PluginProvider_VersionProbe_ScrubbedEnv$' -count=1", timeoutSec: 300 }
  - { name: TestPluginProvider_Secret_NeverInArgvLogsEventsTranscript, run: "go test ./internal/wsapi -run '^TestPluginProvider_Secret_NeverInArgvLogsEventsTranscript$' -count=1", timeoutSec: 300 }
  - { name: TestPluginProvider_Secret_NotInPluginProcessEnvOrInitialize, run: "go test ./internal/plugins -run '^TestPluginProvider_Secret_NotInPluginProcessEnvOrInitialize$' -count=1", timeoutSec: 300 }
  - { name: TestPluginRemove_DeletesProviderSecrets_DisableKeeps, run: "go test ./internal/plugins -run '^TestPluginRemove_DeletesProviderSecrets_DisableKeeps$' -count=1", timeoutSec: 300 }
  - { name: TestConsent_Provider_FingerprintCoversArgvEnvValuesAndInheritHome, run: "go test ./internal/plugins -run '^TestConsent_Provider_FingerprintCoversArgvEnvValuesAndInheritHome$' -count=1", timeoutSec: 300 }
  - { name: TestConsent_Provider_ValueNeverInSummaryOrLogs, run: "go test ./internal/plugins -run '^TestConsent_Provider_ValueNeverInSummaryOrLogs$' -count=1", timeoutSec: 300 }
  - { name: TestPluginUpdate_ProviderCommandChange_PendingKeepsOldRegistered, run: "go test ./internal/plugins -run '^TestPluginUpdate_ProviderCommandChange_PendingKeepsOldRegistered$' -count=1", timeoutSec: 300 }
  - { name: TestPluginUpdate_ProviderSameFingerprint_Applies, run: "go test ./internal/plugins -run '^TestPluginUpdate_ProviderSameFingerprint_Applies$' -count=1", timeoutSec: 300 }
  - { name: TestPluginReload_Linked_ProviderChange_DisablesUntilConfirm, run: "go test ./internal/plugins -run '^TestPluginReload_Linked_ProviderChange_DisablesUntilConfirm$' -count=1", timeoutSec: 300 }
  - { name: TestPluginProvider_SlashID_NeverUsedAsPathOrRef, run: "go test ./internal/taskrunner -run '^TestPluginProvider_SlashID_NeverUsedAsPathOrRef$' -count=1", timeoutSec: 300 }
  - { name: TestPluginProvider_ErrorsAndLogs_LabeledByPlugin, run: "go test ./internal/wsapi -run '^TestPluginProvider_ErrorsAndLogs_LabeledByPlugin$' -count=1", timeoutSec: 300 }
  - { name: TestRunScopedMethods_NoProviderExecOrWriteMethod, run: "go test ./internal/wsapi -run '^TestRunScopedMethods_NoProviderExecOrWriteMethod$' -count=1", timeoutSec: 300 }
mutations:
  - { id: M1, file: internal/pluginagent/env.go, change: "BaseEnv copies the daemon's os.Environ() instead of building the env", expectFail: [TestPluginAgent_BaseEnv_BuiltNeverInherited, TestPluginProvider_AgentEnv_ScrubbedAndBuilt] }
  - { id: M2, file: internal/taskrunner/plugin_provider_launch.go, change: "skip the launch-time confinement re-check", expectFail: [TestPluginProvider_Launch_RechecksConfinedCommand] }
  - { id: M3, file: internal/taskrunner/plugin_provider_launch.go, change: "resolve a bare name on the daemon's PATH instead of the fixed PATH", expectFail: [TestPluginProvider_BareName_LooksOnFixedPathOnly] }
  - { id: M4, file: internal/taskrunner/plugin_provider_launch.go, change: "inheritHome also passes the daemon environment through", expectFail: [TestPluginProvider_InheritHome_OnlyHome] }
  - { id: M5, file: internal/taskrunner/plugin_provider_launch.go, change: "pass the daemon's env names listed as passthrough (re-add passthrough for plugins)", expectFail: [TestPluginProvider_AgentEnv_NoPassthrough] }
  - { id: M6, file: internal/taskrunner/plugin_provider_launch.go, change: "launch with an unset required secret instead of refusing", expectFail: [TestPluginProvider_RequiredSecretUnset_LaunchRefusedNamingSlot] }
  - { id: M7, file: internal/wsapi/provider_health.go, change: "the version probe runs with the daemon's environment", expectFail: [TestProviderTest_PluginProvider_VersionProbe_ScrubbedEnv] }
  - { id: M8, file: internal/plugins/manager.go, change: "Remove leaves the plugin's provider secrets behind", expectFail: [TestPluginRemove_DeletesProviderSecrets_DisableKeeps] }
  - { id: M9, file: internal/plugins/consent.go, change: "leave the literal env digest out of the fingerprint", expectFail: [TestConsent_Provider_FingerprintCoversArgvEnvValuesAndInheritHome] }
  - { id: M10, file: internal/taskrunner/provider_secrets.go, change: "remove the Plugin guard that routes plugin specs to the scrubbed launch (use the generic launch env)", expectFail: [TestPluginProvider_AgentEnv_ScrubbedAndBuilt, TestPluginProvider_NoExecutionSite] }
commands:
  - { id: C1, run: "task lint", timeoutSec: 900 }
  - { id: C2, run: "task test", timeoutSec: 1500 }
limits: { budgetUsd: 2, maxMinutes: 45 }
writeScope:
  paths: []
---

# PD2 validation contract

Security-sensitive: the daemon now EXECUTES a command a plugin supplies. The validator must attack: (1) can any environment variable of the daemon (sentinel in os env, SMIND_*) reach the agent child or the version probe; (2) can the command resolve outside the plugin directory (symlink swapped after registration, `..`, absolute path, PATH hijack, a bare name found outside the fixed PATH); (3) can a secret reach argv, a log, an event, provider.list, an error, the transcript or the plugin process; (4) is there any other site that executes the command (grep for exec of Spec.Command / HealthCommand); (5) is the provider id used as a path or ref. No model call, isolated temp dirs, fake ACP agent only.
Spec: `docs/plans/active/plugins-wave-d.md` (PD2a; the gates, send guard, kill on disable and failover are PD2b with their own contract).
