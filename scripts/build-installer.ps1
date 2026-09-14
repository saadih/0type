# Build the single-file Windows installer: build\bin\0type-amd64-installer.exe.
#
# It bundles the Parakeet-enabled exe plus the sherpa DLLs, installs per-user to
# %LOCALAPPDATA%\Programs\0type (no admin / UAC), adds a Start Menu + desktop
# shortcut and an uninstaller, and checks for the WebView2 runtime.
#
# Prerequisites: the WinLibs C toolchain (gcc) and NSIS (makensis):
#   winget install BrechtSanders.WinLibs.POSIX.UCRT
#   winget install NSIS.NSIS
#
#   powershell -ExecutionPolicy Bypass -File scripts/build-installer.ps1
#
# Code signing is optional here but strongly recommended. An unsigned
# installer with no download history is what Defender objects to: v0.3.0 was
# flagged Trojan:Win32/Sabsik.FL.A!ml while v0.2.0, built the same way but with
# weeks of downloads behind it, passed. 0type also looks like a keylogger to a
# classifier -- global input hook, synthetic keystrokes, clipboard, microphone,
# and it downloads and runs llama-server -- so this recurs every release until
# the binaries are signed.
#
# Set ONE of these to sign the exe and the installer. With none set the build
# runs exactly as before and ends with a reminder.
#   $env:ZEROTYPE_SIGN_SHA1 = "<thumbprint>"   a cert in the current user store
#   $env:ZEROTYPE_SIGN_PFX  = "path\to.pfx"    a PFX, with ZEROTYPE_SIGN_PASS
#   $env:ZEROTYPE_SIGN_DLIB = "<dll path>"     Azure Trusted Signing, with
#   $env:ZEROTYPE_SIGN_META = "metadata.json"  its metadata file
# ZEROTYPE_SIGN_TS overrides the RFC 3161 timestamp server.
#
# SignPath's free tier for open-source projects signs in CI instead: its GitHub
# Action uploads the artifact and returns a signed one.
$ErrorActionPreference = "Stop"
$root = Split-Path $PSScriptRoot -Parent
$bin = Join-Path $root "build\bin"

$timestampUrl = $env:ZEROTYPE_SIGN_TS
if (-not $timestampUrl) { $timestampUrl = "http://timestamp.digicert.com" }

$signArgs = $null
if ($env:ZEROTYPE_SIGN_SHA1) {
    $signArgs = @("/sha1", $env:ZEROTYPE_SIGN_SHA1)
} elseif ($env:ZEROTYPE_SIGN_PFX) {
    $signArgs = @("/f", $env:ZEROTYPE_SIGN_PFX)
    if ($env:ZEROTYPE_SIGN_PASS) { $signArgs += @("/p", $env:ZEROTYPE_SIGN_PASS) }
} elseif ($env:ZEROTYPE_SIGN_DLIB) {
    if (-not $env:ZEROTYPE_SIGN_META) { Write-Error "ZEROTYPE_SIGN_DLIB needs ZEROTYPE_SIGN_META as well."; exit 1 }
    $signArgs = @("/dlib", $env:ZEROTYPE_SIGN_DLIB, "/dmdf", $env:ZEROTYPE_SIGN_META)
}
# Always timestamp: without it, signatures stop validating when the cert expires.
if ($signArgs) { $signArgs += @("/fd", "sha256", "/tr", $timestampUrl, "/td", "sha256") }

function Get-SignTool {
    $c = Get-Command signtool.exe -ErrorAction SilentlyContinue
    if ($c) { return $c.Source }
    $kits = "${env:ProgramFiles(x86)}\Windows Kits\10\bin"
    if (Test-Path $kits) {
        $hit = Get-ChildItem $kits -Recurse -Filter signtool.exe -ErrorAction SilentlyContinue |
            Where-Object { $_.FullName -match '\\x64\\' } |
            Sort-Object FullName -Descending | Select-Object -First 1
        if ($hit) { return $hit.FullName }
    }
    return $null
}

# Invoke-Sign signs one file, or does nothing when signing is not configured.
# The exe is signed before makensis bundles it and the installer afterwards, so
# whichever artifact a user takes -- installer or portable zip -- is signed.
function Invoke-Sign {
    param([Parameter(Mandatory = $true)][string]$Path)
    if (-not $signArgs) { return }
    $tool = Get-SignTool
    if (-not $tool) { Write-Error "signtool.exe not found. Install the Windows SDK signing tools."; exit 1 }
    Write-Host "Signing $(Split-Path $Path -Leaf)..."
    & $tool sign @signArgs $Path
    if ($LASTEXITCODE -ne 0) { Write-Error "signtool failed on $Path."; exit 1 }
}

if (-not (Get-Command gcc -ErrorAction SilentlyContinue)) {
    Write-Error "gcc not found on PATH. Install WinLibs (winget install BrechtSanders.WinLibs.POSIX.UCRT) and reopen the terminal."
    exit 1
}

# Stage the sherpa DLLs from the Go module cache so makensis can bundle them.
New-Item -ItemType Directory -Force -Path $bin | Out-Null
$k2 = Join-Path $env:USERPROFILE "go\pkg\mod\github.com\k2-fsa"
$modwin = Get-ChildItem $k2 -Directory -ErrorAction SilentlyContinue |
    Where-Object { $_.Name -like "sherpa-onnx-go-windows@*" } | Select-Object -First 1
if (-not $modwin) { Write-Error "sherpa-onnx-go-windows module not found in the Go cache."; exit 1 }
Get-ChildItem $modwin.FullName -Recurse -Filter *.dll |
    Where-Object { $_.FullName -match 'x86_64' } |
    ForEach-Object { Copy-Item $_.FullName $bin -Force }

# The bindings step compiles a throwaway binary and RUNS it, and with -tags
# parakeet that binary loads the sherpa DLLs. It runs from a temp directory, so
# the staged copies have to be reachable on PATH or it dies with 0xc0000135.
$env:PATH = "$bin;$env:PATH"

# Build the exe, and let Wails fill in wails_tools.nsh and fetch the WebView2
# bootstrapper. Its own makensis pass (admin scope) succeeds because the DLLs are
# already staged; the next step overrides it with a per-user installer.
$env:CGO_ENABLED = "1"
Write-Host "Building exe + processing NSIS templates (wails build -nsis)..."
$exe = Join-Path $bin "0type.exe"
if (Test-Path $exe) { Remove-Item $exe -Force }  # never package a stale build
Push-Location $root
wails build -tags parakeet -nsis
$buildFailed = ($LASTEXITCODE -ne 0)
Pop-Location
if ($buildFailed) { Write-Error "wails build failed (exit $LASTEXITCODE)."; exit 1 }
if (-not (Test-Path $exe)) { Write-Error "wails build produced no exe."; exit 1 }

# v0.3.0 shipped without local transcription because the build failed, the
# failure went unnoticed, and makensis packaged a leftover CGO-free exe. The
# Parakeet build load-time links sherpa, so its import table names the DLL;
# a stub build does not. Check rather than trust.
$ascii = [System.Text.Encoding]::ASCII.GetString([System.IO.File]::ReadAllBytes($exe))
if (-not $ascii.Contains("sherpa-onnx-c-api.dll")) {
    Write-Error "Built exe does not link sherpa-onnx: this is not a Parakeet build, so local transcription would be dead. Refusing to package it."
    exit 1
}
Write-Host "Verified: exe links sherpa-onnx (Parakeet build)."
# Sign before makensis bundles it, so the portable zip carries a signed exe too.
Invoke-Sign $exe

$makensis = "${env:ProgramFiles(x86)}\NSIS\makensis.exe"
if (-not (Test-Path $makensis)) {
    $c = Get-Command makensis -ErrorAction SilentlyContinue
    if ($c) { $makensis = $c.Source }
}
if (-not (Test-Path $makensis)) { Write-Error "makensis not found. Install NSIS (winget install NSIS.NSIS)."; exit 1 }

Write-Host "Building per-user installer..."
& $makensis `
    "/DARG_WAILS_AMD64_BINARY=$bin\0type.exe" `
    "/DREQUEST_EXECUTION_LEVEL=user" `
    "/DWAILS_INSTALL_SCOPE=user" `
    "$root\build\windows\installer\project.nsi"
if ($LASTEXITCODE -ne 0) { Write-Error "makensis failed."; exit 1 }
Invoke-Sign (Join-Path $bin "0type-amd64-installer.exe")

Write-Host "Done: build\bin\0type-amd64-installer.exe (per-user, no admin)."
if (-not $signArgs) {
    Write-Warning "Unsigned build: expect a SmartScreen warning, and Defender may flag the fresh installer until it has download history. See this script header for how to sign."
}
