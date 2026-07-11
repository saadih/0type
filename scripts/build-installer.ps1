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
#   pwsh scripts/build-installer.ps1
$ErrorActionPreference = "Stop"
$root = Split-Path $PSScriptRoot -Parent
$bin = Join-Path $root "build\bin"

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

# Build the exe, and let Wails fill in wails_tools.nsh and fetch the WebView2
# bootstrapper. Its own makensis pass (admin scope) succeeds because the DLLs are
# already staged; the next step overrides it with a per-user installer.
$env:CGO_ENABLED = "1"
Write-Host "Building exe + processing NSIS templates (wails build -nsis)..."
Push-Location $root
wails build -tags parakeet -nsis
Pop-Location

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

Write-Host "Done: build\bin\0type-amd64-installer.exe (per-user, no admin)."
