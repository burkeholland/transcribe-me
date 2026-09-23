#Requires -Version 7.2
param(
    [ValidatePattern('^\d+\.\d+\.\d+([.-][A-Za-z0-9.-]+)?$')]
    [string]$Version = '0.2.0',
    [string]$OutputDirectory = (Join-Path (Split-Path $PSScriptRoot -Parent) 'dist')
)
. (Join-Path $PSScriptRoot 'common.ps1')
$root = Split-Path $PSScriptRoot -Parent
$manifestPath = Join-Path $root 'assets\runtime-manifest.json'
& "$PSScriptRoot\verify-runtime.ps1" | Out-Host
$packageRoot = Join-Path $root 'build\runtime-package'
if (Test-Path $packageRoot) { Remove-Item -LiteralPath $packageRoot -Recurse -Force }
try {
    New-Item -ItemType Directory -Force $packageRoot, $OutputDirectory | Out-Null
    Copy-Item -LiteralPath (Join-Path $root 'runtime') -Destination $packageRoot -Recurse
    $zip = Join-Path $OutputDirectory "TranscribeMe-$Version-runtime-windows-x64.zip"
    if (Test-Path $zip) { Remove-Item -LiteralPath $zip -Force }
    [IO.Compression.ZipFile]::CreateFromDirectory(
        $packageRoot, $zip, [IO.Compression.CompressionLevel]::Optimal, $false
    )

    $expected = @{}
    $manifest = Get-Content $manifestPath -Raw | ConvertFrom-Json
    foreach ($file in $manifest.files) { $expected[$file.path] = $file }
    $archive = [IO.Compression.ZipFile]::OpenRead($zip)
    try {
        foreach ($entry in $archive.Entries) {
            if (-not $entry.Name) { continue }
            $name = $entry.FullName.Replace('\', '/')
            if (-not $expected.ContainsKey($name)) { throw "Runtime ZIP contains an unexpected file: $name" }
            $file = $expected[$name]
            if ($entry.Length -ne $file.size) { throw "Runtime ZIP entry has the wrong size: $name" }
            $stream = $entry.Open()
            $sha = [Security.Cryptography.SHA256]::Create()
            try { $actual = [Convert]::ToHexString($sha.ComputeHash($stream)).ToLowerInvariant() }
            finally { $stream.Dispose(); $sha.Dispose() }
            if ($actual -ne $file.sha256) { throw "Runtime ZIP entry failed SHA-256 verification: $name" }
            $expected.Remove($name)
        }
        if ($expected.Count) { throw "Runtime ZIP omitted files: $($expected.Keys -join ', ')" }
    } finally {
        $archive.Dispose()
    }
    Write-Host "Downloadable runtime ZIP verified: $zip"
    Get-Item -LiteralPath $zip
} finally {
    if (Test-Path $packageRoot) { Remove-Item -LiteralPath $packageRoot -Recurse -Force }
}
