# Installs the smind daemon/CLI on Windows from a GitHub Release.
#
#   irm https://raw.githubusercontent.com/spacingmind/smind/master/scripts/install.ps1 | iex
#
# (spacingmind.com/install.ps1 will serve the same script once the landing
# redirect exists; see README.md.)
#
# Environment overrides:
#   SMIND_VERSION      release to install, e.g. 0.11.1 (default: latest)
#   SMIND_INSTALL_DIR  target directory (default: %LOCALAPPDATA%\Programs\smind)
#   SMIND_SERVICE=1    same as -Service
#
# Options (when the script is run as a file or as a script block; `iex` cannot
# take parameters, so use SMIND_SERVICE there):
#   -Service           after installing, run `smind service install`: a per-user
#                      scheduled task that starts the daemon at logon
#
#   & ([scriptblock]::Create((irm <url>))) -Service
#   $env:SMIND_SERVICE = 1; irm <url> | iex
#
# Downloads smind_<version>_windows_<amd64|arm64>.zip plus checksums.txt from
# the Release, verifies the SHA-256 (mandatory), extracts only smind.exe,
# installs it to the target directory and adds that directory to the user
# PATH once. Re-running it installs the requested (or latest) release over
# the previous one; any failure leaves the previous install untouched.
#
# This script NEVER writes under %LOCALAPPDATA%\smind\: that is the desktop
# app's managed daemon directory (ADR-0013/0030) and the app owns it.
#
# It is meant to be piped to `iex`, so it never calls `exit` at top level (that
# would close the user's shell), keeps its settings inside functions, and is
# plain ASCII. Works on Windows PowerShell 5.1 and PowerShell 7.
#

function Resolve-SmindArch {
    # Maps the OS architecture to the release asset suffix. NativeMachine is
    # the IMAGE_FILE_MACHINE value from IsWow64Process2 (0xAA64 = ARM64,
    # 0x8664 = AMD64) and wins over the environment: an x64 PowerShell
    # running emulated on an ARM64 OS still reports PROCESSOR_ARCHITECTURE=AMD64.
    param(
        [int]$NativeMachine = 0,
        [string]$ProcessorArchitecture,
        [string]$ProcessorArchitectureW6432
    )
    if ($NativeMachine -ne 0) {
        switch ($NativeMachine) {
            0xAA64 { return 'arm64' }
            0x8664 { return 'amd64' }
            default { throw ('unsupported architecture (machine type 0x{0:X})' -f $NativeMachine) }
        }
    }
    $arch = $ProcessorArchitectureW6432
    if (-not $arch) { $arch = $ProcessorArchitecture }
    switch ($arch) {
        'AMD64' { return 'amd64' }
        'ARM64' { return 'arm64' }
        default { throw "unsupported architecture: $arch" }
    }
}

function Get-SmindNativeMachine {
    # 0 when it cannot be determined (Windows older than 10 1709, non-Windows).
    try {
        if (-not ('SmindInstall.Native' -as [type])) {
            Add-Type -Namespace SmindInstall -Name Native -MemberDefinition @'
[System.Runtime.InteropServices.DllImport("kernel32.dll", SetLastError = true)]
public static extern bool IsWow64Process2(System.IntPtr hProcess, out ushort processMachine, out ushort nativeMachine);
'@
        }
        $process = 0
        $native = 0
        $handle = [System.Diagnostics.Process]::GetCurrentProcess().Handle
        if ([SmindInstall.Native]::IsWow64Process2($handle, [ref]$process, [ref]$native)) {
            return [int]$native
        }
    } catch {
        Write-Verbose "IsWow64Process2 unavailable: $($_.Exception.Message)"
    }
    return 0
}

function Get-SmindArch {
    Resolve-SmindArch -NativeMachine (Get-SmindNativeMachine) `
        -ProcessorArchitecture $env:PROCESSOR_ARCHITECTURE `
        -ProcessorArchitectureW6432 $env:PROCESSOR_ARCHITEW6432
}

function Invoke-SmindDownload {
    param([string]$Url, [string]$OutFile)
    Invoke-WebRequest -Uri $Url -OutFile $OutFile -UseBasicParsing -TimeoutSec 600
}

function Get-SmindLatestVersion {
    # Resolve "latest" via the release redirect rather than the API, which is
    # rate-limited for anonymous callers.
    $response = Invoke-WebRequest -Uri 'https://github.com/spacingmind/smind/releases/latest' `
        -Method Head -UseBasicParsing -MaximumRedirection 5 -TimeoutSec 60
    $base = $response.BaseResponse
    if ($base -is [System.Net.HttpWebResponse]) {
        $final = $base.ResponseUri.AbsoluteUri
    } else {
        $final = $base.RequestMessage.RequestUri.AbsoluteUri
    }
    return $final.TrimEnd('/').Split('/')[-1]
}

function Get-SmindUserPath {
    # Raw (unexpanded) user PATH, or '' when unset.
    $key = [Microsoft.Win32.Registry]::CurrentUser.OpenSubKey('Environment')
    if (-not $key) { return '' }
    try {
        $value = $key.GetValue('Path', '', [Microsoft.Win32.RegistryValueOptions]::DoNotExpandEnvironmentNames)
        return [string]$value
    } finally {
        $key.Close()
    }
}

function Set-SmindUserPath {
    param([string]$Value)
    # Written as REG_EXPAND_SZ so existing %VAR% entries keep expanding;
    # [Environment]::SetEnvironmentVariable would flatten them.
    $key = [Microsoft.Win32.Registry]::CurrentUser.CreateSubKey('Environment')
    try {
        $key.SetValue('Path', $Value, [Microsoft.Win32.RegistryValueKind]::ExpandString)
    } finally {
        $key.Close()
    }
    # Tell Explorer (and so newly opened terminals) the environment changed.
    try {
        if (-not ('SmindInstall.Broadcast' -as [type])) {
            Add-Type -Namespace SmindInstall -Name Broadcast -MemberDefinition @'
[System.Runtime.InteropServices.DllImport("user32.dll", CharSet = System.Runtime.InteropServices.CharSet.Auto)]
public static extern System.IntPtr SendMessageTimeout(System.IntPtr hWnd, uint Msg, System.UIntPtr wParam, string lParam, uint fuFlags, uint uTimeout, out System.UIntPtr lpdwResult);
'@
        }
        $result = [UIntPtr]::Zero
        [void][SmindInstall.Broadcast]::SendMessageTimeout([IntPtr]0xffff, 0x1A, [UIntPtr]::Zero, 'Environment', 2, 5000, [ref]$result)
    } catch {
        Write-Verbose "could not broadcast the environment change: $($_.Exception.Message)"
    }
}

function ConvertTo-SmindPathKey {
    param([string]$Path)
    $expanded = [Environment]::ExpandEnvironmentVariables($Path.Trim())
    return $expanded.TrimEnd('\', '/')
}

function Test-SmindPathEntry {
    param([string]$PathList, [string]$Dir)
    $want = ConvertTo-SmindPathKey $Dir
    foreach ($entry in $PathList.Split(';')) {
        if (-not $entry.Trim()) { continue }
        if ((ConvertTo-SmindPathKey $entry) -ieq $want) { return $true }
    }
    return $false
}

function Test-SmindUnderDir {
    param([string]$Path, [string]$Parent)
    $full = [IO.Path]::GetFullPath($Path).TrimEnd('\', '/')
    $root = [IO.Path]::GetFullPath($Parent).TrimEnd('\', '/')
    if ($full -ieq $root) { return $true }
    $sep = [IO.Path]::DirectorySeparatorChar
    return $full.StartsWith($root + $sep, [StringComparison]::OrdinalIgnoreCase)
}

function Get-SmindLocalAppData {
    $dir = $env:LOCALAPPDATA
    if (-not $dir) { $dir = [Environment]::GetFolderPath('LocalApplicationData') }
    if (-not $dir) { throw 'could not determine %LOCALAPPDATA%' }
    return $dir
}

function Get-SmindChecksum {
    # Returns the lower-case SHA-256 listed for $Name in a checksums.txt file.
    param([string]$ChecksumsFile, [string]$Name)
    foreach ($line in Get-Content -LiteralPath $ChecksumsFile) {
        if ($line -match '^\s*([0-9A-Fa-f]{64})\s+\*?(.+?)\s*$' -and $Matches[2] -ceq $Name) {
            return $Matches[1].ToLowerInvariant()
        }
    }
    return $null
}

function Expand-SmindExe {
    # Extracts only smind.exe from the zip to $Destination. The archive is
    # already checksum-verified; refusing path traversal is defence in depth.
    param([string]$ZipFile, [string]$Destination)
    Add-Type -AssemblyName System.IO.Compression.FileSystem
    $zip = [System.IO.Compression.ZipFile]::OpenRead($ZipFile)
    try {
        $found = $null
        foreach ($entry in $zip.Entries) {
            $name = $entry.FullName.Replace('\', '/')
            if ($name.StartsWith('/') -or $name -match '^[A-Za-z]:' -or (($name -split '/') -contains '..')) {
                throw "refusing archive with unsafe entry path: $($entry.FullName)"
            }
            if ($name -ieq 'smind.exe') { $found = $entry }
        }
        if (-not $found) { throw 'smind.exe not found at the root of the archive' }
        $in = $found.Open()
        try {
            $out = [IO.File]::Create($Destination)
            try { $in.CopyTo($out) } finally { $out.Dispose() }
        } finally {
            $in.Dispose()
        }
    } finally {
        $zip.Dispose()
    }
}

function Test-SmindGitInstalled {
    if (Get-Command git -ErrorAction SilentlyContinue) { return $true }
    $programFiles = [Environment]::GetFolderPath('ProgramFiles')
    if (-not $programFiles) { return $false }
    return (Test-Path -LiteralPath (Join-Path $programFiles 'Git\cmd\git.exe'))
}

function Install-SmindExe {
    # Swaps $Source into $Dir\smind.exe. A running smind.exe cannot be
    # overwritten on Windows but can be renamed, so the old one is moved
    # aside first and restored if anything fails.
    param([string]$Source, [string]$Dir)
    $final = Join-Path $Dir 'smind.exe'
    $stage = "$final.new"
    $old = $null
    $done = $false
    try {
        Copy-Item -LiteralPath $Source -Destination $stage -Force
        if (Test-Path -LiteralPath $final) {
            $old = "$final.old-" + [guid]::NewGuid().ToString('N').Substring(0, 8)
            Move-Item -LiteralPath $final -Destination $old -Force
        }
        Move-Item -LiteralPath $stage -Destination $final -Force
        $done = $true
    } finally {
        # finally (not catch) so Ctrl+C between the two moves also restores.
        if (-not $done) {
            if ($old -and -not (Test-Path -LiteralPath $final) -and (Test-Path -LiteralPath $old)) {
                Move-Item -LiteralPath $old -Destination $final -Force
            }
            Remove-Item -LiteralPath $stage -Force -ErrorAction SilentlyContinue
        }
    }
    # Best effort: stale copies from earlier updates (may still be running).
    Get-ChildItem -LiteralPath $Dir -Filter 'smind.exe.old-*' -ErrorAction SilentlyContinue |
        ForEach-Object { Remove-Item -LiteralPath $_.FullName -Force -ErrorAction SilentlyContinue }
}

function Invoke-SmindServiceInstall {
    # Runs `<exe> service install` (output goes straight to the console) and
    # reports whether it succeeded. A function so tests can stand in for the exe.
    param([string]$Exe)
    # Out-Host: the exe's output must reach the console, not the function's
    # output stream (where it would become part of the return value and make a
    # failure look like success: `-not @('...', $false)` is false).
    & $Exe service install | Out-Host
    return [bool]($LASTEXITCODE -eq 0)
}

function Install-Smind {
    param([string]$Version, [string]$InstallDir, [switch]$Service)
    $ErrorActionPreference = 'Stop'
    $ProgressPreference = 'SilentlyContinue' # Windows PowerShell 5.1 downloads crawl with progress on
    Set-StrictMode -Version Latest
    $repo = 'spacingmind/smind'

    if ($PSVersionTable.PSVersion.Major -lt 6) {
        # Windows PowerShell 5.1 may default to TLS 1.0/1.1, which GitHub refuses.
        [Net.ServicePointManager]::SecurityProtocol = [Net.ServicePointManager]::SecurityProtocol -bor [Net.SecurityProtocolType]::Tls12
    }

    $arch = Get-SmindArch

    $managedDir = Join-Path (Get-SmindLocalAppData) 'smind'
    if (-not $InstallDir) { $InstallDir = Join-Path (Join-Path (Get-SmindLocalAppData) 'Programs') 'smind' }
    # Expand %VARS% and resolve a relative path against the PowerShell location
    # (GetFullPath would use the process directory, which Set-Location does not change).
    $InstallDir = [Environment]::ExpandEnvironmentVariables($InstallDir)
    $InstallDir = [IO.Path]::GetFullPath($ExecutionContext.SessionState.Path.GetUnresolvedProviderPathFromPSPath($InstallDir))
    if (Test-SmindUnderDir -Path $InstallDir -Parent $managedDir) {
        throw "refusing to install into ${managedDir}: that is the sMind desktop app's managed directory; choose another SMIND_INSTALL_DIR"
    }

    if (-not $Version) { $Version = Get-SmindLatestVersion }
    $Version = $Version.Trim()
    if ($Version.StartsWith('v')) { $Version = $Version.Substring(1) }
    if ($Version -notmatch '^[0-9A-Za-z.+-]+$') {
        throw "could not determine a valid version (got '$Version')"
    }

    $name = "smind_${Version}_windows_${arch}.zip"
    $base = "https://github.com/$repo/releases/download/v$Version"
    $tmp = Join-Path ([IO.Path]::GetTempPath()) ('smind-install-' + [guid]::NewGuid().ToString('N'))
    New-Item -ItemType Directory -Path $tmp -Force | Out-Null
    try {
        Write-Host "Downloading smind v$Version (windows/$arch)..."
        try { Invoke-SmindDownload -Url "$base/$name" -OutFile (Join-Path $tmp $name) }
        catch { throw "download failed: $base/$name ($($_.Exception.Message))" }
        try { Invoke-SmindDownload -Url "$base/checksums.txt" -OutFile (Join-Path $tmp 'checksums.txt') }
        catch { throw "download failed: $base/checksums.txt ($($_.Exception.Message))" }

        $expected = Get-SmindChecksum -ChecksumsFile (Join-Path $tmp 'checksums.txt') -Name $name
        if (-not $expected) { throw "no checksum for $name in checksums.txt" }
        $actual = (Get-FileHash -LiteralPath (Join-Path $tmp $name) -Algorithm SHA256).Hash.ToLowerInvariant()
        if ($actual -ne $expected) { throw "checksum mismatch for $name" }

        $extracted = Join-Path $tmp 'smind.exe'
        Expand-SmindExe -ZipFile (Join-Path $tmp $name) -Destination $extracted

        New-Item -ItemType Directory -Path $InstallDir -Force | Out-Null
        $running = Get-Process -Name smind -ErrorAction SilentlyContinue
        Install-SmindExe -Source $extracted -Dir $InstallDir
    } finally {
        Remove-Item -LiteralPath $tmp -Recurse -Force -ErrorAction SilentlyContinue
    }

    $exe = Join-Path $InstallDir 'smind.exe'
    $installed = $null
    $savedExit = Get-Variable -Scope Global -Name LASTEXITCODE -ValueOnly -ErrorAction SilentlyContinue # a native call here must not change the caller's $LASTEXITCODE
    try { $installed = (& $exe --version 2>$null | Select-Object -First 1) } catch { $installed = $null }
    if ($null -ne $savedExit) { $global:LASTEXITCODE = $savedExit }
    if (-not $installed) { $installed = "smind v$Version" }
    Write-Host "Installed $installed to $exe"
    if ($running) {
        Write-Host 'A smind daemon is running; restart it to use the new version.'
    }

    # The binary is in place by now; a PATH problem must not look like a failed install.
    try {
        $userPath = Get-SmindUserPath
        if (-not (Test-SmindPathEntry -PathList $userPath -Dir $InstallDir)) {
            $newPath = if ($userPath) { $userPath.TrimEnd(';') + ';' + $InstallDir } else { $InstallDir }
            Set-SmindUserPath -Value $newPath
            Write-Host "Added $InstallDir to your user PATH; open a new terminal to pick it up."
        }
    } catch {
        Write-Warning "could not add $InstallDir to your user PATH ($($_.Exception.Message)); add it yourself or run $exe directly"
    }
    if (-not (Test-SmindPathEntry -PathList $env:Path -Dir $InstallDir)) {
        $env:Path = ([string]$env:Path).TrimEnd(';') + ';' + $InstallDir
    }

    if (-not (Test-SmindGitInstalled)) {
        Write-Warning 'Git for Windows was not found. smind needs it for workspaces; install it from https://gitforwindows.org'
    }

    if ($Service) {
        Write-Host ''
        Write-Host 'Installing the smind service (a scheduled task that starts the daemon at logon)...'
        $savedExit = Get-Variable -Scope Global -Name LASTEXITCODE -ValueOnly -ErrorAction SilentlyContinue
        $ok = Invoke-SmindServiceInstall -Exe $exe
        if ($null -ne $savedExit) { $global:LASTEXITCODE = $savedExit }
        if (-not $ok) {
            throw "smind is installed at $exe, but 'smind service install' failed (see above)"
        }
        Write-Host ''
        Write-Host 'Next:'
        Write-Host '  smind service status    # then open http://localhost:4648'
        Write-Host '  smind service logs -f'
        Write-Host 'The task starts at logon, not at boot.'
    } else {
        Write-Host ''
        Write-Host 'Next:'
        Write-Host '  smind serve    # then open http://localhost:4648'
        Write-Host '  (or run it at every logon: smind service install)'
    }
}

function Invoke-SmindInstaller {
    param([switch]$Service)
    try {
        # Platform only exists on PowerShell 6+; Windows PowerShell 5.1 is always Windows.
        if ($PSVersionTable['Platform'] -and $PSVersionTable['Platform'] -ne 'Win32NT') {
            throw 'this script is for Windows; on Linux and macOS run: curl -fsSL https://spacingmind.com/install.sh | sh'
        }
        Install-Smind -Version $env:SMIND_VERSION -InstallDir $env:SMIND_INSTALL_DIR -Service:$Service
    } catch {
        $global:LASTEXITCODE = 1
        # Under `iex` there is no script file and `exit` would close the shell:
        # throw instead, which also makes `powershell -Command "irm ... | iex"` fail.
        if (-not $PSCommandPath) { throw "smind-install: $($_.Exception.Message)" }
        Write-Host "smind-install: $($_.Exception.Message)" -ForegroundColor Red
        exit 1
    }
}

# Tests set $SmindInstallNoRun before dot-sourcing this file to get the functions only.
if (-not (Get-Variable -Name SmindInstallNoRun -ErrorAction SilentlyContinue)) {
    # -Service from the command line / a script block, or SMIND_SERVICE=1 (the iex form).
    $wantService = ($args -contains '-Service') -or ($env:SMIND_SERVICE -in '1', 'true')
    Invoke-SmindInstaller -Service:$wantService
}
