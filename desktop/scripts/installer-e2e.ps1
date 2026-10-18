# Windows installer end-to-end QA, run on a clean windows-latest GitHub runner.
#
# Phase A: silently install the OLD release installer, drive the app's own
# "Install daemon" command headlessly over CDP, create user data, and check
# that quitting the app leaves the managed daemon running. Phases B (upgrade
# over the old install) and C (uninstall) come in later briefs.
#
#   pwsh -NoProfile -File desktop/scripts/installer-e2e.ps1 -NewInstaller <path-to-new-setup.exe>
param(
    [Parameter(Mandatory)][string]$NewInstaller,  # freshly built *-setup.exe (phases B/C; validated now)
    [string]$OldVersion = '0.12.0',               # release to install first
    [string]$Repo = 'spacingmind/smind',
    [int]$CdpPort = 9222,
    [int]$DaemonPort = 4648
)
$ErrorActionPreference = 'Stop'
$PSStyle.OutputRendering = 'PlainText'

# NTFS is case-insensitive: %LOCALAPPDATA%\sMind is the app dir AND the
# managed-daemon dir (bin\smind.exe, managed.json, smind.log live there too).
$appDir = Join-Path $env:LOCALAPPDATA 'sMind'
$appExe = "$appDir\smind-desktop.exe"
$sidecar = "$appDir\smind.exe"          # bundled daemon shipped by the installer
$binExe = "$appDir\bin\smind.exe"       # daemon installed by the app itself
$managedJson = "$appDir\managed.json"
$dataDir = Join-Path $env:USERPROFILE '.smind'

function Step($m) { Write-Host ""; Write-Host "== $m" }
function Fail($m) { throw "FAIL: $m" }
function Check($cond, $m) { if (-not $cond) { Fail $m } }
function WaitFor($what, [scriptblock]$cond, [int]$seconds = 60) {
    for ($i = 0; $i -lt $seconds; $i++) {
        if (& $cond) { return }
        Start-Sleep 1
    }
    Fail "timed out after ${seconds}s waiting for $what"
}
function Get-Sha($path) { (Get-FileHash $path -Algorithm SHA256).Hash }

# The app ignores WEBVIEW2_ADDITIONAL_BROWSER_ARGUMENTS; the WebView2 policy
# registry value, keyed by the host exe name and set before launch, works.
function Enable-Cdp {
    foreach ($hive in 'HKLM:\Software\Policies\Microsoft\Edge\WebView2\AdditionalBrowserArguments', 'HKCU:\Software\Policies\Microsoft\Edge\WebView2\AdditionalBrowserArguments') {
        New-Item -Path $hive -Force | Out-Null
        New-ItemProperty -Path $hive -Name 'smind-desktop.exe' -Value ('--remote-debugging-port=' + $CdpPort) -PropertyType String -Force | Out-Null
    }
}
function Disable-Cdp {
    foreach ($hive in 'HKLM:\Software\Policies\Microsoft\Edge\WebView2\AdditionalBrowserArguments', 'HKCU:\Software\Policies\Microsoft\Edge\WebView2\AdditionalBrowserArguments') {
        if (Test-Path $hive) {
            Remove-ItemProperty -Path $hive -Name 'smind-desktop.exe' -ErrorAction SilentlyContinue
        }
    }
}

function Start-App {
    Enable-Cdp
    Start-Process $appExe -PassThru | Out-Null
    WaitFor 'the app CDP endpoint' { & node desktop/scripts/cdp-invoke.mjs $CdpPort 'typeof window.__TAURI_INTERNALS__' 90 *> $null; ($LASTEXITCODE -eq 0) } 90
}
function Stop-App {
    # Only ever the desktop app; smind.exe is the daemon and must survive.
    Get-Process smind-desktop -ErrorAction SilentlyContinue | Stop-Process -Force -ErrorAction SilentlyContinue
    WaitFor 'the app to exit' { -not (Get-Process smind-desktop -ErrorAction SilentlyContinue) } 30
}
function Invoke-Tauri($cmd) {
    $out = & node desktop/scripts/cdp-invoke.mjs $CdpPort "window.__TAURI_INTERNALS__.invoke('$cmd')" 120 2>$null
    if ($LASTEXITCODE -ne 0) {
        $err = & node desktop/scripts/cdp-invoke.mjs $CdpPort "window.__TAURI_INTERNALS__.invoke('$cmd')" 10 2>&1 | Out-String
        Fail "invoke '$cmd' exited $LASTEXITCODE`: $err"
    }
    return ($out | Out-String) | ConvertFrom-Json
}
# Used only inside string concatenations below, which PSScriptAnalyzer's
# unused-parameter check does not see; reference them once so it stays quiet.
$CdpPort | Out-Null
$DaemonPort | Out-Null
$healthzUrl = 'http://127.0.0.1:' + $DaemonPort + '/healthz'
function Get-Healthz { try { Invoke-RestMethod $healthzUrl -TimeoutSec 2 } catch { $null } }
$printedDaemonProc = $false
function Get-DaemonProc {
    $p = Get-CimInstance Win32_Process -Filter "Name='smind.exe'" |
        Where-Object { $_.CommandLine -match ' serve' -or $_.ExecutablePath -eq $binExe } |
        Select-Object -First 1
    if (-not $script:printedDaemonProc) {
        Write-Host "daemon process: $($p | Select-Object ProcessId, ExecutablePath, CommandLine | ConvertTo-Json -Compress)"
        $script:printedDaemonProc = $true
    }
    return $p
}

Check (Test-Path $NewInstaller) "the new installer '$NewInstaller' does not exist"

$summary = [System.Collections.Generic.List[string]]::new()

try {
    Step 'phase 0: clean machine'
    Check (-not (Test-Path $appDir)) "the app dir '$appDir' already exists; the job must start clean"
    Check ($null -eq (Get-Healthz)) 'a daemon is already answering on port 4648'
    Check (-not (Test-Path $dataDir)) "the data dir '$dataDir' already exists"
    $summary.Add('phase 0: machine was clean')

    # TODO phase B: silent upgrade with the new installer, daemon replaced, data survives
    # TODO phase C: uninstall, data survives

    Step "phase A1: silent install of v$OldVersion"
    New-Item -ItemType Directory -Path old | Out-Null
    gh release download "v$OldVersion" --repo $Repo -p '*x64-setup.exe' -D old
    $setup = (Get-ChildItem old -Filter *x64-setup.exe | Select-Object -First 1).FullName
    Write-Host "installing $setup"
    $p = Start-Process $setup -ArgumentList '/S' -Wait -PassThru
    Check ($p.ExitCode -eq 0) "the old installer exited $($p.ExitCode)"
    Check (Test-Path $appExe) 'the app exe was not installed'
    Check (Test-Path $sidecar) 'the bundled daemon sidecar was not installed'
    Check ((Get-Item $sidecar).Length -gt 1MB) 'the bundled daemon sidecar is smaller than 1 MB'
    Check (Test-Path "$appDir\uninstall.exe") 'uninstall.exe was not installed'
    Check (-not (Test-Path $binExe)) 'the installer itself installed the managed daemon'
    $oldSidecarHash = Get-Sha $sidecar
    $oldVersionOut = (& $sidecar version) -join ' '
    Check ($oldVersionOut -match [regex]::Escape($OldVersion)) "the sidecar reports '$oldVersionOut', want it to contain $OldVersion"
    $oldCommit = if ($oldVersionOut -match '\((.+)\)') { $Matches[1] } else { '?' }
    $summary.Add("phase A1: v$OldVersion ($oldCommit) silently installed, sidecar sha256 $oldSidecarHash")
    Write-Host "sidecar: $oldVersionOut"

    Step 'phase A2: the app installs its bundled daemon'
    Start-App
    $st = Invoke-Tauri 'daemon_status'
    Write-Host ($st | ConvertTo-Json -Compress)
    Check ($st.managedState -eq 'notRunning') "initial managedState is '$($st.managedState)', want notRunning"
    Check ($st.reachable -eq $false) 'a daemon is already reachable before install'
    Check ($st.binaryInstalled -eq $false) 'binaryInstalled is already true'
    Check ($st.appVersion -eq $OldVersion) "appVersion is '$($st.appVersion)', want $OldVersion"
    $r = Invoke-Tauri 'daemon_install'
    Write-Host ($r | ConvertTo-Json -Compress)
    WaitFor 'the daemon healthz' { Get-Healthz } 60
    $h = Get-Healthz
    Check ($h.version -eq $OldVersion) "healthz version is '$($h.version)', want $OldVersion"
    Check (Test-Path $binExe) 'bin\smind.exe was not installed'
    Check ((Get-Sha $binExe) -eq $oldSidecarHash) 'the installed daemon is not the bundled sidecar (hash mismatch)'
    Check (Test-Path $managedJson) 'managed.json was not written'
    [void](Get-Content $managedJson -Raw | ConvertFrom-Json)
    $proc = Get-DaemonProc
    Check ($null -ne $proc) 'no daemon process found'
    Check ($proc.ExecutablePath -eq $binExe) "the daemon runs from '$($proc.ExecutablePath)', want $binExe"
    Check ($r.managedState -eq 'managed') "daemon_install returned managedState '$($r.managedState)', want managed"
    Check (Test-Path "$dataDir\smind.db") "the daemon did not create $dataDir\smind.db"
    $summary.Add("phase A2: app installed its bundled daemon (pid $($proc.ProcessId)), managed.json present, healthz v$($h.version)")

    Step 'phase A3: user data'
    $ws = Join-Path ([IO.Path]::GetTempPath()) ('smind-e2e-ws-' + [guid]::NewGuid().ToString('N').Substring(0, 8))
    New-Item -ItemType Directory -Path $ws | Out-Null
    git -C $ws init | Out-Null
    git -C $ws -c user.name=e2e -c user.email=e2e@example.com commit --allow-empty -m init | Out-Null
    $out = (& $binExe workspace create $ws e2e-ws e2e) 2>&1 | Out-String
    Check ($LASTEXITCODE -eq 0) "workspace create exited $LASTEXITCODE`: $out"
    $ls = (& $binExe workspace ls) 2>&1 | Out-String
    Check ($ls -match 'e2e-ws') "workspace ls does not list e2e-ws: $ls"
    Set-Content -Path "$dataDir\e2e-marker.txt" -Value 'keep me' -Encoding ascii
    $tokenHash = Get-Sha "$dataDir\token"
    $dbSize = (Get-Item "$dataDir\smind.db").Length
    $summary.Add("phase A3: workspace e2e-ws created, marker written, token sha256 $tokenHash, smind.db $dbSize bytes")

    Step 'phase A4: closing the app leaves the daemon running'
    $procIdBefore = (Get-DaemonProc).ProcessId
    Stop-App
    Start-Sleep 3
    Check ($null -ne (Get-Healthz)) 'the daemon stopped answering after the app quit'
    Check ((Get-DaemonProc).ProcessId -eq $procIdBefore) 'the daemon was replaced after the app quit'
    $summary.Add("phase A4: app quit, daemon still running (pid $procIdBefore)")

    Step 'phase B1: silent upgrade with the new installer, over the running old daemon'
    $newAppVersion = (Get-Content desktop/src-tauri/tauri.conf.json -Raw | ConvertFrom-Json).version
    $p = Start-Process $NewInstaller -ArgumentList '/S' -Wait -PassThru
    Check ($p.ExitCode -eq 0) "the new installer exited $($p.ExitCode)"
    Check (Test-Path $appExe) 'the upgrade removed the app exe'
    Check (Test-Path $sidecar) 'the upgrade removed the bundled sidecar'
    Check (Test-Path "$appDir\uninstall.exe") 'uninstall.exe is gone after the upgrade'
    $newSidecarHash = Get-Sha $sidecar
    Check ($newSidecarHash -ne $oldSidecarHash) 'the upgrade did not replace the bundled sidecar (same sha256)'
    $newVersionOut = (& $sidecar version) -join ' '
    Check ($newVersionOut -match [regex]::Escape($newAppVersion)) "the new sidecar reports '$newVersionOut', want it to contain $newAppVersion"
    Write-Host "new sidecar: $newVersionOut"
    $newCommit = if ($newVersionOut -match '\((.+)\)') { $Matches[1] } else { '?' }
    Check ($newCommit -ne $oldCommit) "the new sidecar reports the same build commit ($newCommit) as the old one ($oldCommit)"
    Check ((Get-Sha $binExe) -eq $oldSidecarHash) 'the installer touched the managed bin\smind.exe'
    Check ((Get-DaemonProc).ProcessId -eq $procIdBefore) 'the running daemon was disturbed by the installer'
    Check ($null -ne (Get-Healthz)) 'the daemon stopped answering after the upgrade install'
    Check ((Get-Content "$dataDir\e2e-marker.txt" -Raw).Trim() -eq 'keep me') 'the installer deleted the e2e marker'
    Check ((Get-Sha "$dataDir\token") -eq $tokenHash) 'the installer replaced the auth token'
    Check ((Get-Item "$dataDir\smind.db").Length -ge $dbSize) 'the installer shrank smind.db'
    $summary.Add("phase B1: upgraded to $newAppVersion ($newCommit), sidecar sha256 $newSidecarHash, daemon pid $procIdBefore undisturbed, data intact")

    Step 'phase B2: the new app replaces the old daemon (Update)'
    Start-App
    $st = Invoke-Tauri 'daemon_status'
    Write-Host ($st | ConvertTo-Json -Compress)
    Check ($st.managedState -eq 'managed') "managedState is '$($st.managedState)', want managed"
    Check ($st.reachable -eq $true) 'the daemon is not reachable from the new app'
    Check ($st.appVersion -eq $newAppVersion) "appVersion is '$($st.appVersion)', want $newAppVersion"
    Check ($st.daemonVersion -eq $OldVersion) "daemonVersion is '$($st.daemonVersion)', want $OldVersion (the old daemon)"
    $r = Invoke-Tauri 'daemon_update'
    Write-Host ($r | ConvertTo-Json -Compress)
    WaitFor 'the replaced daemon' { $d = Get-DaemonProc; $d -and $d.ProcessId -ne $procIdBefore -and (Get-Healthz) } 90
    $newProc = Get-DaemonProc
    Check ((Get-Sha $binExe) -eq $newSidecarHash) 'bin\smind.exe is not the new bundled daemon'
    Check ($newProc.ExecutablePath -eq $binExe) "the daemon runs from '$($newProc.ExecutablePath)', want $binExe"
    Check (-not (Get-Process -Id $procIdBefore -ErrorAction SilentlyContinue)) 'the old daemon process is still running'
    Check ((Get-Healthz).version -eq $newAppVersion) "healthz version is '$((Get-Healthz).version)', want $newAppVersion"
    Check ($r.managedState -eq 'managed') "daemon_update returned managedState '$($r.managedState)', want managed"
    $rec = Get-Content $managedJson -Raw | ConvertFrom-Json
    Check ($rec.version -eq $newAppVersion) "managed.json version is '$($rec.version)', want $newAppVersion"
    Check ($rec.pid -eq $newProc.ProcessId) "managed.json pid is '$($rec.pid)', want the running pid $($newProc.ProcessId)"
    Check ((Get-Content "$dataDir\e2e-marker.txt" -Raw).Trim() -eq 'keep me') 'the daemon replacement deleted the e2e marker'
    Check ((Get-Sha "$dataDir\token") -eq $tokenHash) 'the daemon replacement replaced the auth token'
    Check ((Get-Item "$dataDir\smind.db").Length -ge $dbSize) 'the daemon replacement shrank smind.db'
    $ls = (& $binExe workspace ls) 2>&1 | Out-String
    Check ($ls -match 'e2e-ws') "the new daemon does not serve workspace e2e-ws: $ls"
    $summary.Add("phase B2: daemon replaced by daemon_update (pid $procIdBefore -> $($newProc.ProcessId)), bin\smind.exe = new sidecar, data served by the new daemon")

    Step 'phase B3: closing the new app leaves the new daemon running'
    $newProcId = $newProc.ProcessId
    Stop-App
    Start-Sleep 3
    Check ($null -ne (Get-Healthz)) 'the new daemon stopped answering after the app quit'
    Check ((Get-DaemonProc).ProcessId -eq $newProcId) 'the new daemon was replaced after the app quit'
    $summary.Add("phase B3: app quit, new daemon still running (pid $newProcId)")

    # TODO phase C: uninstall, data survives
} finally {
    Disable-Cdp
    Stop-App
    Get-CimInstance Win32_Process -Filter "Name='smind.exe'" |
        Where-Object { $_.ExecutablePath -eq $binExe } |
        ForEach-Object { Stop-Process -Id $_.ProcessId -Force -ErrorAction SilentlyContinue }
    if (Test-Path "$appDir\uninstall.exe") {
        Start-Process "$appDir\uninstall.exe" -ArgumentList '/S' -Wait | Out-Null
    }
    if ($env:GITHUB_STEP_SUMMARY) {
        Add-Content -Path $env:GITHUB_STEP_SUMMARY -Value (($summary | ForEach-Object { "- $_" }) -join "`n") -Encoding utf8
    }
}
Write-Host ''
Write-Host 'installer e2e phase A passed'
exit 0
