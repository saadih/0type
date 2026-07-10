# Launches a local OpenAI-compatible cleanup server for 0type by reusing the
# llama.cpp server and Qwen model that OpenWhispr already installed. Override the
# params if your paths differ. Point ZEROTYPE_CLEANUP_URL at http://127.0.0.1:<Port>.
#
#   pwsh scripts/cleanup-server.ps1
#   $env:ZEROTYPE_CLEANUP_URL = "http://127.0.0.1:8719"; go run ./cmd/0type
#
# --jinja is required so Qwen's chat template honors enable_thinking:false;
# without it the model burns its whole budget on a <think> trace.
param(
    [string]$Server = "$env:APPDATA\open-whispr\bin\llama-server-vulkan.exe",
    [string]$Model  = "$env:USERPROFILE\.cache\openwhispr\models\Qwen_Qwen3.5-4B-Q4_K_M.gguf",
    [int]$Port = 8719,
    [int]$GpuLayers = 99
)
if (-not (Test-Path $Server)) { Write-Error "llama-server not found: $Server"; exit 1 }
if (-not (Test-Path $Model)) { Write-Error "model not found: $Model"; exit 1 }
Write-Host "Serving $Model"
Write-Host "  -> http://127.0.0.1:$Port   (set ZEROTYPE_CLEANUP_URL to this)"
& $Server -m $Model --host 127.0.0.1 --port $Port -c 4096 -ngl $GpuLayers --jinja
