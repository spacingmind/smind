# Proves two standard users of one machine can each have their own `smind
# service`: the task name is per user (smind-<SID>), so the second user is not
# refused because the first one registered a task. It also proves another
# user's old machine-wide `\smind` task neither blocks nor is touched by the
# second user, and that the first user's own old task is migrated.
#
# Creates and deletes two local standard accounts; CI only (windows-latest).
#
#   pwsh -NoProfile -File scripts/service_two_users_check.ps1 -Exe C:\path\to\smind.exe
[Diagnostics.CodeAnalysis.SuppressMessageAttribute('PSAvoidUsingConvertToSecureStringWithPlainText', '', Justification = 'password of throwaway local CI accounts')]
param([Parameter(Mandatory)][string]$Exe)
$ErrorActionPreference = 'Stop'
$PSStyle.OutputRendering = 'PlainText'
$password = 'Sm1nd-Std-Pw!'  # 13 chars: net user warns (and prompts) above 14
$work = 'C:\Users\Public\smind-two-users'
New-Item -ItemType Directory -Force $work | Out-Null
icacls $work /grant 'Everyone:(OI)(CI)F' | Out-Null
Copy-Item $Exe "$work\smind.exe" -Force
New-Item -ItemType Directory -Force "$work\tmp" | Out-Null

function Fail($m) { throw "FAIL: $m" }
function Check($cond, $m) { if (-not $cond) { Fail $m } }
function Sid($u) { (New-Object Security.Principal.NTAccount("$env:COMPUTERNAME\$u")).Translate([Security.Principal.SecurityIdentifier]).Value }

# Runs a PowerShell script as the local user (a standard user: not elevated), as
# a real logon would: the profile-bound variables point at that user's own.
function Invoke-AsUser($u, [string]$body) {
    $home_ = "$work\home-$u"
    New-Item -ItemType Directory -Force $home_ | Out-Null
    @"
`$ErrorActionPreference = 'Continue'
`$env:USERPROFILE = "C:\Users\$u"
`$env:LOCALAPPDATA = "`$env:USERPROFILE\AppData\Local"
`$env:APPDATA = "`$env:USERPROFILE\AppData\Roaming"
`$env:TEMP = '$work\tmp'
`$env:TMP = '$work\tmp'
`$exe = '$work\smind.exe'
`$smindHome = '$home_'
`$tn = 'smind-' + [Security.Principal.WindowsIdentity]::GetCurrent().User.Value
net session 2>&1 | Out-Null
"net session exit: `$LASTEXITCODE"
$body
"@ | Set-Content -Encoding UTF8 "$work\inner-$u.ps1"
    $cred = New-Object System.Management.Automation.PSCredential("$env:COMPUTERNAME\$u", (ConvertTo-SecureString $password -AsPlainText -Force))
    $p = Start-Process -FilePath powershell.exe -ArgumentList '-NoProfile', '-ExecutionPolicy', 'Bypass', '-File', "$work\inner-$u.ps1" `
        -Credential $cred -WorkingDirectory $work -LoadUserProfile -Wait -PassThru `
        -RedirectStandardOutput "$work\out-$u.txt" -RedirectStandardError "$work\err-$u.txt"
    $out = Get-Content "$work\out-$u.txt" -Raw
    Write-Host "---- $u ----"
    Write-Host $out
    Write-Host (Get-Content "$work\err-$u.txt" -Raw)
    Check ($p.ExitCode -eq 0) "the run as $u exited $($p.ExitCode)"
    Check ($out -match 'net session exit: [1-9]') "the run as $u was elevated"
    return $out
}

$users = 'smindua', 'smindub'
foreach ($u in $users) { net user $u $password /add /y | Out-Null }
try {
    $sidA = Sid $users[0]
    $sidB = Sid $users[1]
    Check ($sidA -ne $sidB) 'the two accounts share a SID'

    # User A installs, then turns its task into the OLD machine-wide one, as an
    # earlier smind version would have registered it.
    $outA = Invoke-AsUser $users[0] @'
$o = & $exe service install --no-start --home $smindHome 2>&1 | Out-String
"A install exit: $LASTEXITCODE"
$o
$xml = schtasks /Query /TN $tn /XML 2>&1 | Out-String
[IO.File]::WriteAllText("$smindHome\legacy.xml", $xml, [Text.UnicodeEncoding]::new($false, $true))
schtasks /Delete /TN $tn /F 2>&1 | Out-Null
schtasks /Create /TN smind /XML "$smindHome\legacy.xml" /F 2>&1 | Out-Null
"A legacy create exit: $LASTEXITCODE"
'@
    Check ($outA -match 'A install exit: 0') 'user A could not install'
    Check ($outA -match 'A legacy create exit: 0') 'could not register the legacy task as user A'

    # User B installs while A's \smind exists: it used to be refused.
    $outB = Invoke-AsUser $users[1] @'
$o = & $exe service install --no-start --home $smindHome 2>&1 | Out-String
"B install exit: $LASTEXITCODE"
$o
$null = schtasks /Query /TN $tn 2>&1
"B own task exists: $($LASTEXITCODE -eq 0)"
$s = & $exe service status 2>&1 | Out-String
"B status exit: $LASTEXITCODE"
$s
'@
    Check ($outB -match 'B install exit: 0') "user B was refused while user A's task exists"
    Check ($outB -match 'B own task exists: True') "user B's task was not registered"
    Check ($outB -match 'B status exit: 3') "user B's status should be 'installed, not running' (3)"
    Check ($outB -notmatch 'another user') "user B was told the task belongs to another user"

    $names = (schtasks /Query /FO CSV /NH 2>&1 | Out-String)
    Check ($names -match [regex]::Escape("\smind-$sidB")) "user B's task is not in the task list"
    Check ($names -match '"\\smind"') "user A's old \smind task was removed by user B"

    # User A runs install again: its own old task is migrated; B's stays.
    $outA2 = Invoke-AsUser $users[0] @'
$o = & $exe service install --no-start --home $smindHome 2>&1 | Out-String
"A install exit: $LASTEXITCODE"
$o
$null = schtasks /Query /TN $tn 2>&1
"A own task exists: $($LASTEXITCODE -eq 0)"
$null = schtasks /Query /TN smind 2>&1
"A legacy gone: $($LASTEXITCODE -ne 0)"
'@
    Check ($outA2 -match 'A install exit: 0') 'user A could not migrate'
    Check ($outA2 -match 'moved the old machine-wide task') "A's install did not report the migration"
    Check ($outA2 -match 'A own task exists: True' -and $outA2 -match 'A legacy gone: True') 'the migration left the legacy task or lost the new one'
    $names = (schtasks /Query /FO CSV /NH 2>&1 | Out-String)
    Check ($names -match [regex]::Escape("\smind-$sidA") -and $names -match [regex]::Escape("\smind-$sidB")) 'one of the two tasks is missing'

    # Uninstalling one leaves the other.
    $null = Invoke-AsUser $users[0] '& $exe service uninstall 2>&1 | Out-String; "A uninstall exit: $LASTEXITCODE"'
    $names = (schtasks /Query /FO CSV /NH 2>&1 | Out-String)
    Check ($names -notmatch [regex]::Escape("\smind-$sidA")) "A's task is still registered after A's uninstall"
    Check ($names -match [regex]::Escape("\smind-$sidB")) "A's uninstall removed B's task"
    $outB2 = Invoke-AsUser $users[1] '& $exe service uninstall 2>&1 | Out-String; "B uninstall exit: $LASTEXITCODE"'
    Check ($outB2 -match 'B uninstall exit: 0') 'user B could not uninstall'
    $names = (schtasks /Query /FO CSV /NH 2>&1 | Out-String)
    Check ($names -notmatch '\\smind') 'a smind task is left registered'

    Write-Host ''
    Write-Host 'two-user check passed: two standard users each registered, queried, migrated and removed their own task'
} finally {
    foreach ($u in $users) {
        $sid = try { Sid $u } catch { $null }
        if ($sid) { schtasks /Delete /TN "smind-$sid" /F 2>&1 | Out-Null }
        net user $u /delete 2>&1 | Out-Null
    }
    schtasks /Delete /TN smind /F 2>&1 | Out-Null
}
exit 0
