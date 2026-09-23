#Requires -Version 7.2
[CmdletBinding()]
param(
    [switch]$RebuildNative,
    [ValidatePattern('^\d+\.\d+\.\d+([.-][A-Za-z0-9.-]+)?$')]
    [string]$Version = '0.3.0'
)
. (Join-Path $PSScriptRoot 'common.ps1')
$root = Split-Path $PSScriptRoot -Parent
$go = Find-Tool 'go.exe' @('C:\Program Files\Go\bin\go.exe')
$npm = Find-Tool 'npm.cmd'
$wails = Find-Tool 'wails.exe' @((Join-Path $HOME 'go\bin\wails.exe'))
$exe = Join-Path $root 'build\bin\TranscribeMe.exe'
$configuredVersion = (Get-Content (Join-Path $root 'wails.json') -Raw | ConvertFrom-Json).info.productVersion
if ($Version -ne $configuredVersion) { throw "Package version $Version differs from wails.json product version $configuredVersion." }
Push-Location $root
try {
    $goCheck = & "$PSScriptRoot\verify-go-toolchain.ps1" -GoCommand $go
    & "$PSScriptRoot\fetch-runtime.ps1" -RebuildNative:$RebuildNative
    & "$PSScriptRoot\generate-icon.ps1"
    & "$PSScriptRoot\verify-runtime.ps1"
    & "$PSScriptRoot\test-native.ps1"
    Push-Location (Join-Path $root 'frontend')
    try {
        Invoke-Checked $npm @('ci', '--no-audit', '--no-fund')
        Invoke-Checked $npm @('test')
        Invoke-Checked $npm @('run', 'build')
        Invoke-Checked $npm @('run', 'test:e2e')
    } finally { Pop-Location }
    Invoke-Checked $go @('test', './...')
    Invoke-Checked $go @('vet', './...')
    & "$PSScriptRoot\generate-notices.ps1"
    Invoke-Checked $wails @('build', '-clean', '-platform', 'windows/amd64', '-webview2', 'embed', '-s')
    if (-not (Test-Path $exe)) { throw 'Application executable is missing. Build Wails first.' }
    $goCheck = & "$PSScriptRoot\verify-go-toolchain.ps1" -GoCommand $go -ApplicationPath $exe
    & "$PSScriptRoot\verify-runtime.ps1" -ApplicationPath $exe

    $dist = Join-Path $root 'dist'
    $name = "TranscribeMe-$Version-windows-x64"
    $package = Join-Path $dist $name
    if (Test-Path $package) { Remove-Item -LiteralPath $package -Recurse -Force }
    New-Item -ItemType Directory -Force $package | Out-Null
    Copy-Item -LiteralPath $exe -Destination $package
    New-Item -ItemType Directory -Force "$package\runtime\whisper", "$package\runtime\ffmpeg\bin" | Out-Null
    Copy-Item "$root\runtime\whisper\whisper-cli.exe" "$package\runtime\whisper"
    Copy-Item "$root\runtime\ffmpeg\bin\ffmpeg.exe", "$root\runtime\ffmpeg\bin\ffprobe.exe" "$package\runtime\ffmpeg\bin"
    & "$PSScriptRoot\verify-runtime.ps1" -RuntimePath "$package\runtime" -ToolsOnly
    Copy-Item "$root\README.md", "$root\PRIVACY.md", "$root\THIRD-PARTY-NOTICES.md" $package
    New-Item -ItemType Directory -Force "$package\licenses" | Out-Null
    Copy-Item "$root\LICENSE" "$package\licenses"
    Copy-Item "$root\notices" "$package\licenses" -Recurse
    $signature = Get-AuthenticodeSignature "$package\TranscribeMe.exe"
    $goVersionText = (& $go version | Out-String).Trim()
    if ($LASTEXITCODE) { throw 'Could not record Go toolchain version.' }
    $wailsVersionText = (& $wails version | Out-String).Trim()
    if ($LASTEXITCODE) { throw 'Could not record Wails version.' }
    @(
        "TranscribeMe $Version Windows x64"
        "Authenticode status: $($signature.Status)"
        "Go: $goVersionText"
        "Executable Go version: $($goCheck.Binary); go.mod minimum: $($goCheck.Required)"
        "Wails: $wailsVersionText"
        'Native configuration and input hashes: companion native-source archive.'
        'Whisper CLI and the offline FFmpeg tools are bundled. The app downloads only the two pinned speech models.'
        'Tests: go test ./...; go vet ./...; npm ci; npm test; npm run build; npm run test:e2e; verify-runtime.ps1; test-native.ps1.'
        'A passing local build is not a signed or published production release.'
    ) | Set-Content "$package\BUILD-INFO.txt" -Encoding utf8NoBOM
    $sourceArchive = & "$PSScriptRoot\package-native-source.ps1" -Version $Version -OutputDirectory $dist
    $zip = "$package.zip"
    if (Test-Path $zip) { Remove-Item -LiteralPath $zip -Force }
    Compress-Archive -LiteralPath $package -DestinationPath $zip -CompressionLevel Optimal
    $archives = @((Get-Item $zip), $sourceArchive)
    @($archives | ForEach-Object {
        "$((Get-FileHash $_.FullName -Algorithm SHA256).Hash.ToLowerInvariant())  $($_.Name)"
    }) | Set-Content "$dist\SHA256SUMS.txt" -Encoding ascii
    $downloads = Join-Path $root 'docs\downloads'
    New-Item -ItemType Directory -Force $downloads | Out-Null
    $archives | Copy-Item -Destination $downloads -Force
    Copy-Item "$dist\SHA256SUMS.txt" $downloads -Force
    Write-Host "Application with bundled native tools and corresponding source verified: $dist"
    Write-Host "Authenticode: $($signature.Status). No public release was published."
} finally { Pop-Location }
