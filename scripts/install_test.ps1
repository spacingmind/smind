# Tests scripts/install.ps1 without network access or touching the real user
# profile: downloads, the user PATH and %LOCALAPPDATA% are replaced by fakes.
# Plain PowerShell (no Pester), so it runs anywhere pwsh or powershell.exe does.
#
#   pwsh -NoProfile -File scripts/install_test.ps1
#
# The run against a real Release on a Windows runner lives in
# .github/workflows/install-scripts.yml.
$ErrorActionPreference = 'Stop'

Set-Variable -Name SmindInstallNoRun -Value $true
. (Join-Path $PSScriptRoot 'install.ps1')

$work = Join-Path ([IO.Path]::GetTempPath()) ('smind-install-test-' + [guid]::NewGuid().ToString('N'))
New-Item -ItemType Directory -Path $work | Out-Null
Add-Type -AssemblyName System.IO.Compression
Add-Type -AssemblyName System.IO.Compression.FileSystem

# --- fakes -------------------------------------------------------------------

$script:releases = Join-Path $work 'releases'
$script:fakeLocal = Join-Path $work 'LocalAppData'
$script:fakePath = ''
$script:gitInstalled = $true
$script:latest = '9.9.9'
$script:downloads = @()

function Invoke-SmindDownload {
    param([string]$Url, [string]$OutFile)
    $script:downloads += $Url
    $rel = $Url -replace '^https://github.com/spacingmind/smind/releases/download/', ''
    $src = Join-Path $script:releases $rel
    if (-not (Test-Path -LiteralPath $src)) { throw "404: $Url" }
    Copy-Item -LiteralPath $src -Destination $OutFile
}
function Get-SmindLatestVersion { $script:latest }
function Get-SmindLocalAppData { $script:fakeLocal }
function Get-SmindUserPath { $script:fakePath }
function Set-SmindUserPath { param([string]$Value) $script:fakePath = $Value }
function Test-SmindGitInstalled { $script:gitInstalled }
function Get-SmindArch { 'amd64' }

function New-Release {
    # New-Release <version> [-Arch amd64] [-Entries @{name=content}] [-Tamper]
    param([string]$Version, [string]$Arch = 'amd64', [hashtable]$Entries, [switch]$Tamper)
    if (-not $Entries) { $Entries = [ordered]@{ 'smind.exe' = "fake smind $Version"; 'LICENSE' = 'license' } }
    $dir = Join-Path $script:releases "v$Version"
    New-Item -ItemType Directory -Path $dir -Force | Out-Null
    $zipPath = Join-Path $dir "smind_${Version}_windows_${Arch}.zip"
    if (Test-Path $zipPath) { Remove-Item $zipPath -Force }
    $zip = [System.IO.Compression.ZipFile]::Open($zipPath, 'Create')
    try {
        foreach ($name in $Entries.Keys) {
            $entry = $zip.CreateEntry($name)
            $w = New-Object System.IO.StreamWriter($entry.Open())
            try { $w.Write($Entries[$name]) } finally { $w.Dispose() }
        }
    } finally { $zip.Dispose() }
    $sum = (Get-FileHash -LiteralPath $zipPath -Algorithm SHA256).Hash.ToLowerInvariant()
    if ($Tamper) { $sum = '0' * 64 }
    Set-Content -LiteralPath (Join-Path $dir 'checksums.txt') -Encoding ascii -Value @(
        (('1' * 64) + "  smind_${Version}_windows_arm64.zip"),
        "$sum  $(Split-Path $zipPath -Leaf)"
    )
}

function Reset-State {
    $script:fakePath = ''
    $script:gitInstalled = $true
    $script:latest = '9.9.9'
    $script:downloads = @()
    if (Test-Path $script:fakeLocal) { Remove-Item $script:fakeLocal -Recurse -Force }
    New-Item -ItemType Directory -Path $script:fakeLocal | Out-Null
}

# Runs Install-Smind, returning @{ Ok; Error; Warnings; Output }.
function Invoke-Install {
    param([string]$Version, [string]$InstallDir)
    $result = @{ Ok = $true; Error = $null; Warnings = @(); Output = @() }
    try {
        $stream = Install-Smind -Version $Version -InstallDir $InstallDir 3>&1 6>&1
        $result.Warnings = @($stream | Where-Object { $_ -is [System.Management.Automation.WarningRecord] } | ForEach-Object { $_.Message })
        $result.Output = @($stream | Where-Object { $_ -isnot [System.Management.Automation.WarningRecord] } | ForEach-Object { "$_" })
    } catch {
        $result.Ok = $false
        $result.Error = $_.Exception.Message
    }
    return $result
}

# --- harness -----------------------------------------------------------------

$script:passes = 0
$script:fails = 0
function Assert-True {
    param($Condition, [string]$Message)
    if (-not $Condition) { throw "assertion failed: $Message" }
}
function Test-Case {
    param([string]$Name, [scriptblock]$Body)
    Reset-State
    try {
        & $Body
        $script:passes++
        Write-Host "ok   $Name"
    } catch {
        $script:fails++
        Write-Host "FAIL $Name"
        Write-Host "  $($_.Exception.Message)"
        Write-Host "  $($_.ScriptStackTrace)"
    }
}

function Read-Exe { param([string]$Dir) Get-Content -LiteralPath (Join-Path $Dir 'smind.exe') -Raw }

# --- tests -------------------------------------------------------------------

Test-Case 'Select-Arch_x64PowerShellOnArm64Os_ReturnsArm64' {
    # An emulated x64 PowerShell on ARM64 reports AMD64 in the environment but
    # IsWow64Process2 says the native machine is ARM64.
    $arch = Resolve-SmindArch -NativeMachine 0xAA64 -ProcessorArchitecture 'AMD64' -ProcessorArchitectureW6432 ''
    Assert-True ($arch -eq 'arm64') "got $arch"
}

Test-Case 'Select-Arch_NativeAmd64_ReturnsAmd64' {
    Assert-True ((Resolve-SmindArch -NativeMachine 0x8664 -ProcessorArchitecture 'AMD64') -eq 'amd64') 'amd64'
}

Test-Case 'Select-Arch_FallsBackToEnvironment' {
    Assert-True ((Resolve-SmindArch -ProcessorArchitecture 'ARM64') -eq 'arm64') 'ARM64 env'
    Assert-True ((Resolve-SmindArch -ProcessorArchitecture 'x86' -ProcessorArchitectureW6432 'AMD64') -eq 'amd64') '32-bit PowerShell on x64 OS'
}

Test-Case 'Select-Arch_UnsupportedThrows' {
    $threw = $false
    try { Resolve-SmindArch -ProcessorArchitecture 'x86' | Out-Null } catch { $threw = $true }
    Assert-True $threw 'x86 must be rejected'
}

Test-Case 'Install_DefaultDir_InstallsExeAndUsesArchAsset' {
    New-Release '1.2.3'
    $r = Invoke-Install -Version '1.2.3'
    Assert-True $r.Ok "install failed: $($r.Error)"
    $dir = Join-Path (Join-Path $script:fakeLocal 'Programs') 'smind'
    Assert-True ((Read-Exe $dir) -eq 'fake smind 1.2.3') 'smind.exe content'
    Assert-True (-not (Test-Path (Join-Path $dir 'LICENSE'))) 'only smind.exe is extracted'
    Assert-True ($script:downloads -contains 'https://github.com/spacingmind/smind/releases/download/v1.2.3/smind_1.2.3_windows_amd64.zip') 'asset url'
}

Test-Case 'Install_VersionPrefixAndLatest' {
    New-Release '1.2.3'; New-Release '4.5.6'
    $script:latest = '4.5.6'
    Assert-True (Invoke-Install).Ok 'latest'
    $dir = Join-Path (Join-Path $script:fakeLocal 'Programs') 'smind'
    Assert-True ((Read-Exe $dir) -eq 'fake smind 4.5.6') 'latest resolved'
    Assert-True (Invoke-Install -Version 'v1.2.3').Ok 'v-prefix'
    Assert-True ((Read-Exe $dir) -eq 'fake smind 1.2.3') 'v-prefixed version installed'
}

Test-Case 'Install_InvalidVersionRejected' {
    $r = Invoke-Install -Version '1.0;calc'
    Assert-True (-not $r.Ok) 'must fail'
    Assert-True ($r.Error -match 'valid version') $r.Error
    Assert-True ($script:downloads.Count -eq 0) 'nothing downloaded'
}

Test-Case 'Install_MissingRelease_FailsAndLeavesExistingExe' {
    New-Release '1.2.3'
    Assert-True (Invoke-Install -Version '1.2.3').Ok 'seed'
    $r = Invoke-Install -Version '7.7.7'
    Assert-True (-not $r.Ok) 'must fail'
    Assert-True ($r.Error -match 'download failed') $r.Error
    Assert-True ((Read-Exe (Join-Path (Join-Path $script:fakeLocal 'Programs') 'smind')) -eq 'fake smind 1.2.3') 'previous exe kept'
}

Test-Case 'Install_ChecksumMismatch_LeavesExistingExe' {
    New-Release '1.2.3'
    Assert-True (Invoke-Install -Version '1.2.3').Ok 'seed'
    New-Release '6.6.6' -Tamper
    $r = Invoke-Install -Version '6.6.6'
    Assert-True (-not $r.Ok) 'must fail'
    Assert-True ($r.Error -match 'checksum mismatch') $r.Error
    $dir = Join-Path (Join-Path $script:fakeLocal 'Programs') 'smind'
    Assert-True ((Read-Exe $dir) -eq 'fake smind 1.2.3') 'previous exe kept'
    Assert-True (@(Get-ChildItem $dir).Count -eq 1) 'no stray files in install dir'
}

Test-Case 'Install_ChecksumMismatch_FreshInstallInstallsNothing' {
    New-Release '6.6.6' -Tamper
    $r = Invoke-Install -Version '6.6.6'
    Assert-True (-not $r.Ok) 'must fail'
    Assert-True (-not (Test-Path (Join-Path (Join-Path (Join-Path $script:fakeLocal 'Programs') 'smind') 'smind.exe'))) 'no exe'
    Assert-True ($script:fakePath -eq '') 'PATH untouched'
}

Test-Case 'Install_ZipWithPathTraversal_Rejected' {
    New-Release '1.2.3'
    Assert-True (Invoke-Install -Version '1.2.3').Ok 'seed'
    New-Release '2.0.0' -Entries ([ordered]@{ 'smind.exe' = 'fake smind 2.0.0'; '../evil.txt' = 'pwned' })
    $r = Invoke-Install -Version '2.0.0'
    Assert-True (-not $r.Ok) 'must fail'
    Assert-True ($r.Error -match 'unsafe entry') $r.Error
    $dir = Join-Path (Join-Path $script:fakeLocal 'Programs') 'smind'
    Assert-True ((Read-Exe $dir) -eq 'fake smind 1.2.3') 'previous exe kept'
    Assert-True (-not (Test-Path (Join-Path (Split-Path $dir) 'evil.txt'))) 'nothing written outside'
}

Test-Case 'Install_ZipWithoutSmindExe_Rejected' {
    New-Release '2.0.0' -Entries ([ordered]@{ 'LICENSE' = 'x' })
    $r = Invoke-Install -Version '2.0.0'
    Assert-True (-not $r.Ok) 'must fail'
    Assert-True ($r.Error -match 'smind.exe not found') $r.Error
}

Test-Case 'Install_AddsUserPathOnce' {
    New-Release '1.2.3'
    $script:fakePath = '%USERPROFILE%\bin;C:\tools\'
    Assert-True (Invoke-Install -Version '1.2.3').Ok 'first'
    Assert-True (Invoke-Install -Version '1.2.3').Ok 'second'
    $dir = Join-Path (Join-Path $script:fakeLocal 'Programs') 'smind'
    $entries = @($script:fakePath.Split(';') | Where-Object { $_ })
    Assert-True ($entries.Count -eq 3) "entries: $($entries -join '|')"
    Assert-True ($entries[0] -eq '%USERPROFILE%\bin') 'existing unexpanded entry preserved'
    Assert-True ($entries[2] -eq $dir) 'install dir appended'
}

Test-Case 'Install_PathAlreadyHasDirWithTrailingSlash_NotDuplicated' {
    New-Release '1.2.3'
    $dir = Join-Path (Join-Path $script:fakeLocal 'Programs') 'smind'
    $sep = [IO.Path]::DirectorySeparatorChar
    $script:fakePath = $dir.ToUpperInvariant() + $sep
    Assert-True (Invoke-Install -Version '1.2.3').Ok 'install'
    Assert-True ($script:fakePath -eq ($dir.ToUpperInvariant() + $sep)) 'PATH unchanged'
}

Test-Case 'Install_NeverTouchesAppManagedDir' {
    New-Release '1.2.3'
    Assert-True (Invoke-Install -Version '1.2.3').Ok 'default install'
    Assert-True (-not (Test-Path (Join-Path $script:fakeLocal 'smind'))) 'LocalAppData\smind must not exist after a default install'

    $managed = Join-Path $script:fakeLocal 'smind'
    foreach ($target in @($managed, (Join-Path (Join-Path $managed 'bin') 'nested'), ($managed + [IO.Path]::DirectorySeparatorChar))) {
        $r = Invoke-Install -Version '1.2.3' -InstallDir $target
        Assert-True (-not $r.Ok) "must refuse $target"
        Assert-True ($r.Error -match 'managed directory') $r.Error
    }
    Assert-True (-not (Test-Path $managed)) 'refused installs created nothing'

    # A sibling that merely shares the prefix is fine.
    $sibling = Join-Path $script:fakeLocal 'smind-other'
    Assert-True (Invoke-Install -Version '1.2.3' -InstallDir $sibling).Ok 'sibling dir allowed'
}

Test-Case 'Install_CustomInstallDir' {
    New-Release '1.2.3'
    $dir = Join-Path $work 'custom'
    Assert-True (Invoke-Install -Version '1.2.3' -InstallDir $dir).Ok 'install'
    Assert-True ((Read-Exe $dir) -eq 'fake smind 1.2.3') 'content'
    Assert-True ($script:fakePath -eq $dir) 'PATH points at the custom dir'
}

Test-Case 'Install_RerunUpdatesInPlace_NoLeftovers' {
    New-Release '1.2.3'; New-Release '4.5.6'
    Assert-True (Invoke-Install -Version '1.2.3').Ok 'first'
    Assert-True (Invoke-Install -Version '4.5.6').Ok 'update'
    $dir = Join-Path (Join-Path $script:fakeLocal 'Programs') 'smind'
    Assert-True ((Read-Exe $dir) -eq 'fake smind 4.5.6') 'updated'
    Assert-True (@(Get-ChildItem $dir).Count -eq 1) "leftovers: $((Get-ChildItem $dir).Name -join ',')"
}

Test-Case 'Install_WarnsWhenGitMissing' {
    New-Release '1.2.3'
    $script:gitInstalled = $false
    $r = Invoke-Install -Version '1.2.3'
    Assert-True $r.Ok "must not fail: $($r.Error)"
    Assert-True (@($r.Warnings | Where-Object { $_ -match 'Git for Windows' }).Count -eq 1) "warnings: $($r.Warnings -join '|')"
}

Test-Case 'Install_NoGitWarningWhenGitPresent' {
    New-Release '1.2.3'
    $r = Invoke-Install -Version '1.2.3'
    Assert-True ($r.Warnings.Count -eq 0) "warnings: $($r.Warnings -join '|')"
}

Test-Case 'Install_ShellStateNotLeaked' {
    # `irm | iex` runs in the caller's scope: the installer must not change its settings.
    New-Release '1.2.3'
    $eap = $ErrorActionPreference; $pp = $ProgressPreference
    $global:LASTEXITCODE = 42
    Assert-True (Invoke-Install -Version '1.2.3').Ok 'install'
    Assert-True ($ErrorActionPreference -eq $eap -and $ProgressPreference -eq $pp) 'preference variables unchanged'
    $after = $global:LASTEXITCODE
    $global:LASTEXITCODE = 0
    Assert-True ($after -eq 42) "LASTEXITCODE changed to $after"
}

Test-Case 'Install_PathRegistryFailure_IsWarningNotFailure' {
    New-Release '1.2.3'
    function Set-SmindUserPath { param([string]$Value) throw "registry is locked ($Value)" }
    $r = Invoke-Install -Version '1.2.3'
    Assert-True $r.Ok "must not fail: $($r.Error)"
    Assert-True (@($r.Warnings | Where-Object { $_ -match 'user PATH' }).Count -eq 1) "warnings: $($r.Warnings -join '|')"
    Assert-True ((Read-Exe (Join-Path (Join-Path $script:fakeLocal 'Programs') 'smind')) -eq 'fake smind 1.2.3') 'exe installed'
    function Set-SmindUserPath { param([string]$Value) $script:fakePath = $Value }
}

Test-Case 'Install_RelativeAndEnvInstallDir_ResolveAgainstPowerShellLocation' {
    New-Release '1.2.3'
    $base = Join-Path $work 'cwd'
    New-Item -ItemType Directory -Path $base | Out-Null
    Push-Location $base
    try {
        Assert-True (Invoke-Install -Version '1.2.3' -InstallDir 'rel').Ok 'relative'
        Assert-True ((Read-Exe (Join-Path $base 'rel')) -eq 'fake smind 1.2.3') 'installed under the PowerShell location'
        $env:SMIND_TEST_DIR = Join-Path $work 'fromenv'
        Assert-True (Invoke-Install -Version '1.2.3' -InstallDir '%SMIND_TEST_DIR%').Ok 'env var'
        Assert-True ((Read-Exe (Join-Path $work 'fromenv')) -eq 'fake smind 1.2.3') 'env var expanded'
    } finally { Pop-Location; Remove-Item Env:SMIND_TEST_DIR -ErrorAction SilentlyContinue }
}

Remove-Item -LiteralPath $work -Recurse -Force -ErrorAction SilentlyContinue
Write-Host ''
Write-Host "$($script:passes) passed, $($script:fails) failed"
# Explicit: CI wrappers exit with $LASTEXITCODE when the script ends without `exit`.
if ($script:fails -gt 0) { exit 1 }
exit 0
