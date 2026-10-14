# PSScriptAnalyzer settings for scripts/install.ps1 and scripts/install_test.ps1.
@{
    Severity     = @('Error', 'Warning')
    ExcludeRules = @(
        # The installer runs under `irm | iex` for end users: its progress and
        # result messages are meant for the console, not for a pipeline.
        'PSAvoidUsingWriteHost',
        # Internal helpers of a one-shot script; -WhatIf/-Confirm add nothing.
        'PSUseShouldProcessForStateChangingFunctions'
    )
}
