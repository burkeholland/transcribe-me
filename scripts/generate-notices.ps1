#Requires -Version 7.2
param()
. (Join-Path $PSScriptRoot 'common.ps1')
$root = Split-Path $PSScriptRoot -Parent
$target = Join-Path $root 'notices'
$go = Find-Tool 'go.exe' @('C:\Program Files\Go\bin\go.exe')
New-Item -ItemType Directory -Force $target, "$target\go-modules" | Out-Null
Copy-Item "$root\build\native\whisper.cpp-1.8.3\LICENSE" "$target\Whisper.cpp-LICENSE.txt" -Force
$whisperSource = Join-Path $root 'build\native\whisper.cpp-1.8.3'
foreach ($header in @('miniaudio.h', 'stb_vorbis.c')) {
    $text = Get-Content (Join-Path $whisperSource "examples\$header") -Raw
    $start = $text.LastIndexOf('/*')
    if ($start -lt 0 -or $text.Substring($start) -notmatch 'Permission is hereby granted') {
        throw "Embedded license extraction failed: $header"
    }
    $text.Substring($start) | Set-Content "$target\Whisper-$header-LICENSE.txt" -Encoding utf8NoBOM
}
Get-Content (Join-Path $whisperSource 'ggml\src\ggml-cpu\llamafile\sgemm.cpp') -TotalCount 21 |
    Set-Content "$target\GGML-llamafile-LICENSE.txt" -Encoding utf8NoBOM
@(
    'GGML CPU operations include MIT-licensed code.'
    'Copyright (c) 2023 Jeffrey Quesnelle and Bowen Peng.'
    ''
    (Get-Content (Join-Path $whisperSource 'LICENSE') -Raw)
) | Set-Content "$target\GGML-CPU-NOTICE.txt" -Encoding utf8NoBOM
Copy-Item "$root\build\native\ffmpeg-8.1.2\COPYING.LGPLv2.1" "$target\FFmpeg-LGPL-2.1.txt" -Force
Copy-Item "$root\build\native\ffmpeg-8.1.2\LICENSE.md" "$target\FFmpeg-LICENSE.md" -Force
$goRoot = & $go env GOROOT
if ($LASTEXITCODE) { throw 'Cannot locate Go runtime license.' }
Copy-Item (Join-Path $goRoot 'LICENSE') "$target\Go-LICENSE.txt" -Force
$lines = & $go list -tags desktop,production -deps -f '{{if .Module}}{{.Module.Path}}|{{.Module.Version}}|{{.Module.Dir}}{{end}}' .
if ($LASTEXITCODE) { throw 'Cannot enumerate Go module licenses. Run go mod download first.' }
$lines = $lines | Where-Object { $_ } | Sort-Object -Unique
$index = [Collections.Generic.List[string]]::new()
$index.Add('Go module notices for Windows desktop production imports in TranscribeMe.')
$index.Add('Go runtime has its own notice. Development-only dependencies are not distributed.')
$webviewLoaderLicense = $null
foreach ($line in $lines) {
    $parts = $line.Split('|')
    if ($parts[0] -eq 'transcribeme') { continue }
    if ($parts.Count -ne 3 -or -not $parts[2]) { throw "Module source missing: $line. Run go mod download." }
    $name = ($parts[0] + '@' + $parts[1]) -replace '[^a-zA-Z0-9._@-]', '_'
    $licenses = @(Get-ChildItem $parts[2] -File | Where-Object { $_.Name -match '^(LICENSE|LICENCE|COPYING|NOTICE|PATENTS)([._-]|$)' })
    if ($licenses.Count -eq 0) { throw "No root license found for module $($parts[0]); investigate before distribution." }
    $moduleTarget = Join-Path "$target\go-modules" $name
    New-Item -ItemType Directory -Force $moduleTarget | Out-Null
    $licenses | Copy-Item -Destination $moduleTarget -Force
    if ($parts[0] -eq 'github.com/wailsapp/wails/v2') {
        foreach ($nested in @('internal\frontend\desktop\windows\winc\LICENSE', 'internal\go-common-file-dialog\LICENSE')) {
            Copy-Item (Join-Path $parts[2] $nested) (Join-Path $moduleTarget ($nested.Replace('\', '_') + '.txt')) -Force
        }
    }
    if ($parts[0] -eq 'github.com/wailsapp/go-webview2') {
        $webviewLoaderLicense = Join-Path $moduleTarget 'webviewloader-LICENSE.txt'
        Copy-Item (Join-Path $parts[2] 'webviewloader\LICENSE') $webviewLoaderLicense -Force
    }
    $index.Add("$($parts[0]) $($parts[1]) -> go-modules\$name")
}
if (-not $webviewLoaderLicense -or -not (Test-Path $webviewLoaderLicense -PathType Leaf) -or
    (Get-Item $webviewLoaderLicense).Length -eq 0) {
    throw 'The go-webview2 webviewloader license must be present before distribution.'
}
$index | Set-Content "$target\GO-MODULES.txt" -Encoding utf8NoBOM
foreach ($required in @('OpenAI-Whisper-LICENSE.txt', 'Silero-VAD-LICENSE.txt', 'MinGW-w64-COPYING.txt', 'MinGW-w64-CRT-COPYING.txt', 'MinGW-w64-AUTHORS.txt', 'GCC-RUNTIME-EXCEPTION.txt', 'GCC-GPL-3.txt')) {
    if (-not (Test-Path (Join-Path $target $required))) { throw "Required redistribution notice missing: $required" }
}
Write-Host "Collected notices for $($index.Count - 2) Go modules."
