# Proves `smind service install` needs no elevation: creates a STANDARD local
# user (Medium integrity, not in Administrators), runs the task registration,
# status and uninstall as that user, and checks the results. CI only
# (windows-latest); it creates and deletes a local account.
#
#   pwsh -NoProfile -File scripts/service_unelevated_check.ps1 -Exe C:\path\to\smind.exe
[Diagnostics.CodeAnalysis.SuppressMessageAttribute('PSAvoidUsingConvertToSecureStringWithPlainText', '', Justification = 'password of a throwaway local CI account')]
param([Parameter(Mandatory)][string]$Exe)
$ErrorActionPreference = 'Stop'
$PSStyle.OutputRendering = 'PlainText'
$userName = 'smindstd'
$password = 'Sm1nd-Std-Pw!'  # 13 chars: net user warns (and prompts) above 14
$work = 'C:\Users\Public\smind-unelevated'
New-Item -ItemType Directory -Force $work | Out-Null
icacls $work /grant 'Everyone:(OI)(CI)F' | Out-Null
Copy-Item $Exe "$work\smind.exe" -Force
$smindHome = "$work\home"
New-Item -ItemType Directory -Force $smindHome | Out-Null

net user $userName $password /add /y | Out-Null
try {
    @"
`$ErrorActionPreference = 'Continue'
# Start-Process -Credential hands the child the PARENT's environment: point the
# profile-bound variables at this user's own, as a real logon would.
`$env:USERPROFILE = "C:\Users\$userName"
`$env:LOCALAPPDATA = "`$env:USERPROFILE\AppData\Local"
`$env:APPDATA = "`$env:USERPROFILE\AppData\Roaming"
`$env:TEMP = '$work\tmp'
`$env:TMP = '$work\tmp'
New-Item -ItemType Directory -Force `$env:TEMP | Out-Null
`$exe = '$work\smind.exe'
"whoami: `$(whoami)"
"integrity/groups:"
whoami /groups | Select-String 'Mandatory Label|Administrators'
net session 2>&1 | Out-Null
"net session exit: `$LASTEXITCODE (non-zero = not elevated)"
`$out = & `$exe service install --no-start --home '$smindHome' 2>&1 | Out-String
"install exit: `$LASTEXITCODE"
`$out
`$tn = 'smind-' + [Security.Principal.WindowsIdentity]::GetCurrent().User.Value
`$q = schtasks /Query /TN `$tn /XML 2>&1 | Out-String
"task has logon trigger: `$(`$q -match 'LogonTrigger')"
"task asks for elevation: `$(`$q -match 'HighestAvailable')"
`$out = & `$exe service status 2>&1 | Out-String
"status exit: `$LASTEXITCODE (3 = installed, not running)"
`$out
`$out = & `$exe service uninstall 2>&1 | Out-String
"uninstall exit: `$LASTEXITCODE"
`$out
schtasks /Query /TN `$tn 2>&1 | Out-Null
"task gone after uninstall: `$(`$LASTEXITCODE -ne 0)"
"@ | Set-Content -Encoding UTF8 "$work\inner.ps1"

    $cred = New-Object System.Management.Automation.PSCredential("$env:COMPUTERNAME\$userName", (ConvertTo-SecureString $password -AsPlainText -Force))
    $p = Start-Process -FilePath powershell.exe -ArgumentList '-NoProfile', '-ExecutionPolicy', 'Bypass', '-File', "$work\inner.ps1" `
        -Credential $cred -WorkingDirectory $work -LoadUserProfile -Wait -PassThru `
        -RedirectStandardOutput "$work\out.txt" -RedirectStandardError "$work\err.txt"
    $out = Get-Content "$work\out.txt" -Raw
    Write-Host $out
    Write-Host (Get-Content "$work\err.txt" -Raw)
    if ($p.ExitCode -ne 0) { throw "the unelevated run exited $($p.ExitCode)" }
    if ($out -notmatch 'net session exit: [1-9]') { throw 'the check did not run unelevated (net session succeeded)' }
    if ($out -notmatch 'install exit: 0') { throw 'registering the task needed elevation (or failed)' }
    if ($out -notmatch 'task has logon trigger: True') { throw 'the registered task has no logon trigger' }
    if ($out -notmatch 'task asks for elevation: False') { throw 'the task asks for elevation' }
    if ($out -notmatch 'status exit: 3') { throw 'status of an installed, stopped service should exit 3' }
    if ($out -notmatch 'uninstall exit: 0') { throw 'uninstall failed' }
    if ($out -notmatch 'task gone after uninstall: True') { throw 'the task is still registered' }
    Write-Host ''
    Write-Host 'unelevated check passed: a standard user registered, queried and removed the logon task'
} finally {
    net user $userName /delete 2>&1 | Out-Null
}
exit 0
