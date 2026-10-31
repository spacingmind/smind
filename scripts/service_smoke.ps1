# Smoke-tests `smind service` against the REAL Windows Task Scheduler: install,
# a hidden launch, an idempotent re-install, logs, a graceful stop, a crash that
# is restarted, the stop fallback, restart, the foreign-daemon guard and
# uninstall. It registers and removes a task named `smind-<user SID>` for the
# current user (and, for the migration step, briefly the old machine-wide `smind`)
# and listens on -Port, so run it only where that is fine: a CI runner or a
# disposable VM. Used by .github/workflows/service-windows.yml.
#
#   pwsh -NoProfile -File scripts/service_smoke.ps1 -Exe C:\path\to\smind.exe
param(
    [Parameter(Mandatory)][string]$Exe,
    [int]$Port = 4999
)
$ErrorActionPreference = 'Stop'
$PSStyle.OutputRendering = 'PlainText'
# Native output is read as UTF-8 (the paths below are not ASCII).
[Console]::OutputEncoding = [Text.UTF8Encoding]::new()
$OutputEncoding = [Text.UTF8Encoding]::new()

# The exe and SMIND_HOME live in directories with spaces and non-ASCII characters
# (a profile like C:\Users\Nguy-en Van with diacritics): they go through the task's command line,
# `schtasks /Query` and PowerShell, which is where such paths break.
$work = Join-Path ([IO.Path]::GetTempPath()) ('smind smoke ' + [char]0xDC + 'n' + [char]0xEF + ' ' + [guid]::NewGuid().ToString('N').Substring(0, 8))
New-Item -ItemType Directory -Path $work | Out-Null
$binDir = Join-Path $work ('Bin D' + [char]0x1EEF + ' li' + [char]0x1EC7 + 'u')
New-Item -ItemType Directory -Path $binDir | Out-Null
Copy-Item $Exe (Join-Path $binDir 'smind.exe')
$Exe = Join-Path $binDir 'smind.exe'

$smindHome = Join-Path $work ('Nguy' + [char]0x1EC5 + 'n V' + [char]0x103 + 'n data')
New-Item -ItemType Directory -Path $smindHome | Out-Null
Set-Content -Path (Join-Path $smindHome 'config.yaml') -Value "server:`n  port: $Port" -Encoding ascii
# `service install` captures SMIND_HOME from this shell into the task (the task
# itself does not inherit this environment).
$env:SMIND_HOME = $smindHome

# A second install of smind.exe, elsewhere, running its own supervisor and daemon
# on another port: `smind service` must never touch it (it pins its own exe).
$decoyDir = Join-Path $work 'decoy'
$decoyHome = Join-Path $work 'decoy-home'
$decoyPort = $Port + 1
New-Item -ItemType Directory -Path $decoyDir, $decoyHome | Out-Null
Copy-Item $Exe (Join-Path $decoyDir 'smind.exe')
Set-Content -Path (Join-Path $decoyHome 'config.yaml') -Value "server:`n  port: $decoyPort" -Encoding ascii
$decoyExe = Join-Path $decoyDir 'smind.exe'

function ApiTokenStatus { try { (Invoke-WebRequest "http://127.0.0.1:$Port/api/token" -TimeoutSec 3 -SkipHttpErrorCheck).StatusCode } catch { 0 } }
function DecoyHealth { try { Invoke-RestMethod "http://127.0.0.1:$decoyPort/healthz" -TimeoutSec 2 } catch { $null } }

function Step($m) { Write-Host ""; Write-Host "== $m" }
function Fail($m) { throw "FAIL: $m" }
function Check($cond, $m) { if (-not $cond) { Fail $m } }

function Svc {
    # Runs `smind service ...`, returns @{ Code; Out } (stdout and stderr together).
    $out = & $Exe service @args 2>&1 | Out-String
    return @{ Code = $LASTEXITCODE; Out = $out }
}
function Health { try { Invoke-RestMethod "http://127.0.0.1:$Port/healthz" -TimeoutSec 2 } catch { $null } }
function WaitFor($what, [scriptblock]$cond, [int]$seconds = 60) {
    for ($i = 0; $i -lt $seconds; $i++) {
        if (& $cond) { return }
        Start-Sleep 1
    }
    Fail "timed out after ${seconds}s waiting for $what"
}
function Procs { Get-CimInstance Win32_Process -Filter "Name='smind.exe'" | Where-Object { $_.ExecutablePath -eq $Exe } }
function DaemonProc { Procs | Where-Object { $_.CommandLine -match ' serve\s*$' } | Select-Object -First 1 }
function SupervisorProcs { Procs | Where-Object { $_.CommandLine -match ' service run( |$)' } }
# The per-user task name (the old machine-wide name was plain `smind`).
$taskName = 'smind-' + [Security.Principal.WindowsIdentity]::GetCurrent().User.Value
function TaskExists { schtasks /Query /TN $taskName 2>&1 | Out-Null; return ($LASTEXITCODE -eq 0) }
function LegacyTaskExists { schtasks /Query /TN smind 2>&1 | Out-Null; return ($LASTEXITCODE -eq 0) }

$cleanedUp = $false
function Cleanup {
    if ($cleanedUp) { return }
    $script:cleanedUp = $true
    & $Exe service uninstall 2>&1 | Out-Null
    Procs | ForEach-Object { Stop-Process -Id $_.ProcessId -Force -ErrorAction SilentlyContinue }
    Get-CimInstance Win32_Process | Where-Object { $_.ExecutablePath -eq $decoyExe } | ForEach-Object { & taskkill.exe /T /F /PID $_.ProcessId | Out-Null }
}

try {
    Step 'a decoy smind.exe (own path, supervisor, port) is running; nothing below may touch it'
    $env:SMIND_HOME = $decoyHome
    $decoySup = Start-Process -FilePath $decoyExe -ArgumentList 'service', 'run' -PassThru -WindowStyle Hidden
    $env:SMIND_HOME = $smindHome
    WaitFor 'the decoy daemon' { DecoyHealth }
    Check (-not $decoySup.HasExited) 'the decoy supervisor exited'

    Step 'install registers a per-user logon task and starts the daemon'
    $r = Svc install
    Write-Host $r.Out
    Check ($r.Code -eq 0) "install exited $($r.Code)"
    WaitFor 'the daemon' { Health }
    $h = Health
    Check ($h.managedBy -eq 'taskscheduler') "healthz managedBy is '$($h.managedBy)', want taskscheduler: $($h | ConvertTo-Json -Compress)"
    $xml = (schtasks /Query /TN $taskName /XML 2>&1 | Out-String)
    Check ($xml -match 'LogonTrigger') 'the task has no logon trigger'
    # Task Scheduler omits RunLevel when it is the default (LeastPrivilege), so assert the absence of the elevated one.
    Check ($xml -notmatch 'HighestAvailable') 'the task must not ask for elevation'
    $r = Svc status
    Write-Host $r.Out
    Check ($r.Code -eq 0) "status exited $($r.Code), want 0 (running)"
    Check ($r.Out -match [regex]::Escape($smindHome)) "status does not show the task's home '$smindHome' (non-ASCII path lost in the schtasks round trip?)"

    Step 'the daemon runs hidden (conhost --headless, no window) in the interactive session'
    $d = DaemonProc
    Check ($null -ne $d) 'no smind.exe serve process'
    $proc = Get-Process -Id $d.ProcessId
    Write-Host "daemon pid $($d.ProcessId), session $($proc.SessionId), MainWindowHandle $($proc.MainWindowHandle)"
    Check ($proc.MainWindowHandle -eq 0) 'the daemon has a window'
    $headless = Get-CimInstance Win32_Process -Filter "Name='conhost.exe'" | Where-Object { $_.CommandLine -match '--headless' -and $_.CommandLine -match [regex]::Escape($Exe) }
    Check ($null -ne $headless) 'the task did not launch through conhost --headless'
    Check ($proc.SessionId -ne 0) 'the daemon runs in session 0, not the user''s interactive session'

    Step 'install again changes nothing and leaves the daemon alone'
    $pidBefore = $d.ProcessId
    $r = Svc install
    Write-Host $r.Out
    Check ($r.Code -eq 0 -and $r.Out -match 'up to date' -and $r.Out -match 'already running') 'a second install must be a no-op'
    Check ((DaemonProc).ProcessId -eq $pidBefore) 'a second install restarted the daemon'

    Step 'logs'
    $r = Svc logs -n 20
    Check ($r.Code -eq 0 -and $r.Out -match 'smind listening') "daemon.log has no startup line: $($r.Out)"

    Step 'a graceful stop (smind stop) stays stopped'
    & $Exe stop
    WaitFor 'the daemon to exit' { -not (Health) } 30
    Start-Sleep 5
    Check (-not (Health)) 'the daemon came back after a graceful stop'
    Check (-not (SupervisorProcs)) 'the supervisor outlived a graceful stop'
    $r = Svc status
    Check ($r.Code -eq 3) "status after a stop exited $($r.Code), want 3"

    Step 'start'
    $r = Svc start
    Write-Host $r.Out
    Check ($r.Code -eq 0) "start exited $($r.Code)"
    WaitFor 'the daemon' { Health }

    Step 'a crash is restarted (3x, 1 minute apart)'
    $victim = DaemonProc
    Stop-Process -Id $victim.ProcessId -Force
    WaitFor 'the crashed daemon to be restarted' { $n = DaemonProc; $n -and $n.ProcessId -ne $victim.ProcessId -and (Health) } 150
    $log = (Svc logs -n 50).Out
    Check ($log -match 'restarting in') "daemon.log does not record the restart: $log"

    Step 'service stop is graceful (POST /api/shutdown) and leaves nothing behind'
    $r = Svc stop
    Write-Host $r.Out
    Check ($r.Code -eq 0) "stop exited $($r.Code)"
    Check (-not (Health)) 'the daemon is still answering'
    Check (-not (Procs)) 'smind.exe processes were left behind'
    Check ($r.Out -notmatch 'ending the task') 'a normal stop must not need the fallback'

    Step 'the stop fallback: a failed shutdown request ends the task and kills what is left'
    $r = Svc start
    Check ($r.Code -eq 0) "start exited $($r.Code)"
    WaitFor 'the daemon' { Health }
    Remove-Item (Join-Path $smindHome 'token') -Force  # the request can no longer authenticate
    $r = Svc stop
    Write-Host $r.Out
    Check ($r.Code -eq 0) "stop (fallback) exited $($r.Code)"
    Check ($r.Out -match 'could not ask the daemon') 'the fallback path was not taken'
    Check (-not (Health)) 'the daemon survived the fallback'
    Check (-not (Procs)) 'smind.exe processes were left behind (schtasks /End alone leaves the daemon)'

    Step 'restart'
    $r = Svc start
    Check ($r.Code -eq 0) "start exited $($r.Code)"
    WaitFor 'the daemon' { Health }
    $before = (DaemonProc).ProcessId
    $r = Svc restart
    Write-Host $r.Out
    Check ($r.Code -eq 0) "restart exited $($r.Code)"
    WaitFor 'the restarted daemon' { $n = DaemonProc; $n -and $n.ProcessId -ne $before -and (Health) }

    Step 'a password-protected daemon (P4) still runs under the task, with no secret on any command line'
    $secret = 'correct-horse-battery-staple'
    & $Exe config set server.listen "127.0.0.1:$Port"
    Check ($LASTEXITCODE -eq 0) 'smind config set server.listen failed'
    $secret | & $Exe set-password --password-stdin
    Check ($LASTEXITCODE -eq 0) 'smind set-password failed'
    $r = Svc restart
    Write-Host $r.Out
    Check ($r.Code -eq 0) "restart with a password exited $($r.Code)"
    WaitFor 'the password-protected daemon' { (Health).managedBy -eq 'taskscheduler' }
    Check ((ApiTokenStatus) -ne 200) '/api/token must be closed while a password is set'
    $q = (schtasks /Query /TN $taskName /XML 2>&1 | Out-String)
    Check ($q -notmatch [regex]::Escape($secret)) 'the password is in the task definition'
    $cmdlines = (Get-CimInstance Win32_Process | Where-Object { $_.Name -in 'smind.exe', 'conhost.exe' } | ForEach-Object { $_.CommandLine }) -join "`n"
    Check ($cmdlines -notmatch [regex]::Escape($secret)) 'the password is on a process command line'
    Check ($cmdlines -notmatch 'set-password|SMIND_PASSWORD') 'a secret-related flag is on a command line'
    $r = Svc stop
    Write-Host $r.Out
    Check ($r.Code -eq 0 -and $r.Out -notmatch 'ending the task') 'stop of a password-protected daemon must be graceful (token file + bearer)'
    Check (-not (Health)) 'the daemon is still answering after stop'
    $r = Svc start
    Check ($r.Code -eq 0) "start with a password exited $($r.Code)"
    WaitFor 'the daemon' { (Health).managedBy -eq 'taskscheduler' }

    Step 'an old machine-wide \smind task of this user is migrated to the per-user name'
    $xml = (schtasks /Query /TN $taskName /XML 2>&1 | Out-String)
    $xmlFile = Join-Path $work 'legacy-task.xml'
    [IO.File]::WriteAllText($xmlFile, $xml, [Text.UnicodeEncoding]::new($false, $true))  # UTF-16 LE with a BOM
    Svc stop | Out-Null
    schtasks /Delete /TN $taskName /F | Out-Null
    schtasks /Create /TN smind /XML $xmlFile /F | Out-Null
    Check ($LASTEXITCODE -eq 0) 'could not register the legacy task for the check'
    Check ((LegacyTaskExists) -and -not (TaskExists)) 'the legacy setup is wrong'
    $r = Svc status
    Write-Host $r.Out
    Check ($r.Out -match 'old machine-wide task') "status does not mention the legacy task: $($r.Out)"
    $r = Svc install --no-start
    Write-Host $r.Out
    Check ($r.Code -eq 0) "install (migration) exited $($r.Code)"
    Check ($r.Out -match 'moved the old machine-wide task') "install did not report the migration: $($r.Out)"
    Check ((TaskExists) -and -not (LegacyTaskExists)) 'migration left the legacy task behind or did not create the per-user one'
    $r = Svc start
    Check ($r.Code -eq 0) "start after migration exited $($r.Code)"
    WaitFor 'the daemon after migration' { (Health).managedBy -eq 'taskscheduler' }

    Step 'uninstall removes the task and keeps SMIND_HOME'
    $r = Svc uninstall
    Write-Host $r.Out
    Check ($r.Code -eq 0) "uninstall exited $($r.Code)"
    Check (-not (TaskExists)) 'the task is still registered'
    Check (-not (Health)) 'the daemon is still answering'
    Check (-not (Procs)) 'smind.exe processes were left behind'
    Check (Test-Path (Join-Path $smindHome 'config.yaml')) 'uninstall touched SMIND_HOME'
    Check ((Svc status).Code -eq 4) 'status after uninstall should exit 4'

    Step 'a daemon this task did not start is never started over, stopped or killed'
    $foreign = Start-Process -FilePath $Exe -ArgumentList 'serve' -PassThru -WindowStyle Hidden
    WaitFor 'the hand-started daemon' { Health }
    Check (-not (Health).managedBy) 'a hand-started daemon must not claim to be managed'
    $r = Svc install
    Write-Host $r.Out
    Check ($r.Code -eq 0 -and $r.Out -match 'NOT starting') 'install must register but not start next to a foreign daemon'
    Check (TaskExists) 'the task should still be registered'
    Check ((Svc start).Code -ne 0) 'start must refuse'
    Check ((Svc stop).Code -ne 0) 'stop must refuse a daemon it did not start'
    Check ((Health) -and -not $foreign.HasExited) 'the foreign daemon was touched'
    & $Exe stop
    WaitFor 'the foreign daemon to exit' { -not (Health) } 30
    $r = Svc start
    Check ($r.Code -eq 0) "start after the foreign daemon left exited $($r.Code)"
    WaitFor 'the daemon' { (Health).managedBy -eq 'taskscheduler' }
    Check ((Svc uninstall).Code -eq 0) 'final uninstall failed'
    Check (-not (Procs)) 'smind.exe processes were left behind'

    Step 'the decoy smind.exe was never touched'
    Check (-not $decoySup.HasExited) 'the decoy supervisor was killed by smind service'
    Check (DecoyHealth) 'the decoy daemon was stopped by smind service'
} finally {
    Cleanup
}
Write-Host ''
Write-Host 'service smoke test passed (Task Scheduler)'
exit 0
