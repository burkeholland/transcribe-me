#Requires -Version 7.2
param([switch]$RebuildNative, [switch]$RebuildWhisper, [switch]$StageOnly)
. (Join-Path $PSScriptRoot 'common.ps1')
$root = Split-Path $PSScriptRoot -Parent
$lock = Get-Content (Join-Path $PSScriptRoot 'runtime-lock.json') -Raw | ConvertFrom-Json
& (Join-Path $PSScriptRoot 'build-native.ps1') -Rebuild:$RebuildNative -RebuildWhisper:$RebuildWhisper
$stage = Join-Path $root 'build\runtime-staging'
if (Test-Path $stage) { Remove-Item $stage -Recurse -Force }
New-Item -ItemType Directory -Force "$stage\whisper", "$stage\ffmpeg\bin", "$stage\models" | Out-Null
Copy-Item "$root\build\native\output\whisper-cli.exe" "$stage\whisper"
Copy-Item "$root\build\native\output\ffmpeg.exe", "$root\build\native\output\ffprobe.exe" "$stage\ffmpeg\bin"
foreach ($model in @(
    @{ asset = $lock.model; name = 'ggml-base.bin' },
    @{ asset = $lock.vad; name = 'ggml-silero-v5.1.2.bin' }
)) {
    $path = Get-VerifiedAsset $model.asset (Join-Path $root ".cache\$($model.name)")
    Copy-Item -LiteralPath $path -Destination (Join-Path $stage "models\$($model.name)")
}
& (Join-Path $PSScriptRoot 'verify-runtime.ps1') -RuntimePath $stage -NoManifest
if ($StageOnly) {
    Write-Host "Verified replacement runtime staged at $stage. Active runtime and manifest were not changed."
    return
}
$runtime = Join-Path $root 'runtime'
# Stop the application before running this script. Never mix old DLLs with a new static build.
$backup = Join-Path $root 'build\runtime-previous'
if (Test-Path $backup) { Remove-Item $backup -Recurse -Force }
if (Test-Path $runtime) { Move-Item -LiteralPath $runtime -Destination $backup }
try {
    Move-Item -LiteralPath $stage -Destination $runtime
} catch {
    if (Test-Path $backup) { Move-Item -LiteralPath $backup -Destination $runtime }
    throw
}
$modelSources = @{
    'runtime/models/ggml-base.bin' = $lock.model.url
    'runtime/models/ggml-silero-v5.1.2.bin' = $lock.vad.url
}
$manifest = @{ files = @(Get-ChildItem $runtime -File -Recurse | Sort-Object FullName | ForEach-Object {
    $path = [IO.Path]::GetRelativePath($root, $_.FullName).Replace('\', '/')
    $entry = [ordered]@{
        path = $path
        sha256 = (Get-FileHash $_.FullName -Algorithm SHA256).Hash.ToLowerInvariant()
        size = $_.Length
    }
    if ($modelSources.ContainsKey($path)) { $entry.url = $modelSources[$path] }
    $entry
}) }
$manifest | ConvertTo-Json -Depth 4 | Set-Content -Encoding utf8NoBOM (Join-Path $root 'assets\runtime-manifest.json')
& (Join-Path $PSScriptRoot 'verify-runtime.ps1')
Write-Host "Runtime ready: $($manifest.files.Count) verified files."
