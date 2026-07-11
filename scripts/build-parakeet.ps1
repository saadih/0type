# Build 0type with fully-local Parakeet transcription (CGO + sherpa-onnx) and
# ship the required native DLLs next to the exe.
#
# One-time prerequisite: a C toolchain on PATH:
#   winget install BrechtSanders.WinLibs.POSIX.UCRT
# then reopen the terminal so gcc is on PATH.
#
#   pwsh scripts/build-parakeet.ps1   ->   build\bin\0type.exe (Parakeet enabled)
$ErrorActionPreference = "Stop"
$root = Split-Path $PSScriptRoot -Parent

if (-not (Get-Command gcc -ErrorAction SilentlyContinue)) {
    Write-Error "gcc not found on PATH. Install WinLibs (winget install BrechtSanders.WinLibs.POSIX.UCRT) and reopen the terminal."
    exit 1
}

$env:CGO_ENABLED = "1"
Write-Host "Building 0type with -tags parakeet (CGO)..."
Push-Location $root
wails build -tags parakeet
Pop-Location

$bin = Join-Path $root "build\bin"
$k2 = Join-Path $env:USERPROFILE "go\pkg\mod\github.com\k2-fsa"
$modwin = Get-ChildItem $k2 -Directory -ErrorAction SilentlyContinue |
    Where-Object { $_.Name -like "sherpa-onnx-go-windows@*" } | Select-Object -First 1
if (-not $modwin) {
    Write-Error "sherpa-onnx-go-windows module not found in the Go cache."
    exit 1
}
$dlls = Get-ChildItem $modwin.FullName -Recurse -Filter *.dll |
    Where-Object { $_.FullName -match 'x86_64' }
$dlls | ForEach-Object { Copy-Item $_.FullName $bin -Force }
Write-Host "Done: build\bin\0type.exe + $($dlls.Count) sherpa DLLs. Parakeet is enabled."
