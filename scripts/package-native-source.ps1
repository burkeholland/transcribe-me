#Requires -Version 7.2
param(
    [ValidatePattern('^\d+\.\d+\.\d+([.-][A-Za-z0-9.-]+)?$')]
    [string]$Version = '0.2.0',
    [string]$OutputDirectory = (Join-Path (Split-Path $PSScriptRoot -Parent) 'build\source-package-validation')
)
. (Join-Path $PSScriptRoot 'common.ps1')
$root = Split-Path $PSScriptRoot -Parent
$lock = Get-Content "$PSScriptRoot\runtime-lock.json" -Raw | ConvertFrom-Json
$receiptPath = Join-Path $root 'build\native\output\build-receipt.json'
$receipt = Get-Content $receiptPath -Raw | ConvertFrom-Json
$inputs = @('runtime-lock.json', 'configure-ffmpeg.sh', 'whisper-utf8.manifest', 'build-native.ps1') |
    ForEach-Object { (Get-FileHash (Join-Path $PSScriptRoot $_) -Algorithm SHA256).Hash }
if ($receipt.inputKey -ne ($inputs -join ':')) { throw 'Native source recipe differs from the native build receipt. Rebuild native tools first.' }
foreach ($file in $receipt.files) {
    if ((Get-FileHash "$root\build\native\output\$($file.name)" -Algorithm SHA256).Hash.ToLowerInvariant() -ne $file.sha256) {
        throw "Native build output differs from its receipt: $($file.name)"
    }
}
$name = "TranscribeMe-$Version-native-source"
$source = Join-Path $OutputDirectory $name
if (Test-Path $source) { Remove-Item -LiteralPath $source -Recurse -Force }
New-Item -ItemType Directory -Force "$source\build\native\downloads", "$source\scripts" | Out-Null
foreach ($asset in @($lock.ffmpeg, $lock.whisper)) {
    $archive = Get-VerifiedAsset $asset "$root\build\native\downloads\$($asset.archive)"
    Copy-Item $archive "$source\build\native\downloads"
}
Copy-Item "$PSScriptRoot\common.ps1", "$PSScriptRoot\build-native.ps1",
    "$PSScriptRoot\configure-ffmpeg.sh", "$PSScriptRoot\whisper-utf8.manifest",
    "$PSScriptRoot\runtime-lock.json" "$source\scripts"
Copy-Item $receiptPath "$source\build-receipt.json"
Copy-Item "$root\notices" "$source\notices" -Recurse
Copy-Item "$root\THIRD-PARTY-NOTICES.md", "$root\LICENSE" $source
@(
    'TranscribeMe native corresponding source'
    ''
    'The exact unmodified FFmpeg 8.1.2 and whisper.cpp 1.8.3 archives are'
    'under build\native\downloads. Their hashes are in scripts\runtime-lock.json.'
    'The exact FFmpeg configure recipe is scripts\configure-ffmpeg.sh.'
    'Whisper embeds scripts\whisper-utf8.manifest using Windows SDK mt.exe.'
    'This selects UTF-8 for Windows narrow arguments and filesystem APIs.'
    'Both source trees are unmodified. Whisper uses static MSVC CRT, no OpenMP,'
    'and the optimized AVX2/FMA/F16C/BMI2 CPU baseline.'
    'Compiler versions and produced binary hashes are in build-receipt.json.'
    'SHA256SUMS.txt covers every accompanying source, recipe, and notice file.'
    ''
    'Windows x64 prerequisites: PowerShell 7.2+, Visual Studio Build Tools 2022'
    'with C++ x64 tools, CMake, Windows SDK mt.exe, Git Bash, MinGW-w64 GCC,'
    'and mingw32-make on PATH. Tested GCC: 13.2.0 x86_64-ucrt-posix-seh,'
    'with MinGW-w64 11.0.1 (Strawberry Perl). No optional FFmpeg libraries.'
    ''
    'From this extracted directory run: .\scripts\build-native.ps1 -Rebuild'
    'Output: build\native\output\ffmpeg.exe, ffprobe.exe, whisper-cli.exe.'
    '-Rebuild re-extracts each pinned source archive and rebuilds native code.'
    '-RebuildWhisper cleans only Whisper source/build files and reuses FFmpeg.'
    'To modify sources, extract them under build\native, edit, then run the'
    'script without -Rebuild. Do not retain an old build-receipt.json when'
    'testing modified source, because it enables verified binary cache reuse.'
    'Source can be modified under its included licenses. TranscribeMe itself'
    'is MIT licensed. Rebuild the app with an updated runtime integrity manifest'
    'when replacing its native binaries. Cross-toolchain identical bytes are'
    'not claimed. This source archive is not an app release or signing claim.'
) | Set-Content "$source\README.txt" -Encoding utf8NoBOM
$sourceFiles = @(Get-ChildItem $source -File -Recurse | Sort-Object FullName)
$checksums = @{}
@($sourceFiles | ForEach-Object {
    $relative = [IO.Path]::GetRelativePath($source, $_.FullName).Replace('\', '/')
    $hash = (Get-FileHash $_.FullName -Algorithm SHA256).Hash.ToLowerInvariant()
    $checksums[$relative] = $hash
    "$hash  $relative"
}) | Set-Content "$source\SHA256SUMS.txt" -Encoding ascii
$zip = "$source.zip"
if (Test-Path $zip) { Remove-Item -LiteralPath $zip -Force }
Compress-Archive -LiteralPath $source -DestinationPath $zip -CompressionLevel Optimal
$archive = [IO.Compression.ZipFile]::OpenRead($zip)
try {
    foreach ($entry in $archive.Entries) {
        $relative = $entry.FullName.Replace('\', '/').Substring($name.Length + 1)
        if (-not $checksums.ContainsKey($relative)) { continue }
        $stream = $entry.Open()
        $sha = [Security.Cryptography.SHA256]::Create()
        try { $actual = [Convert]::ToHexString($sha.ComputeHash($stream)).ToLowerInvariant() }
        finally { $stream.Dispose(); $sha.Dispose() }
        if ($actual -ne $checksums[$relative]) { throw "Source ZIP entry failed SHA-256 verification: $relative" }
        $checksums.Remove($relative)
    }
    if ($checksums.Count) { throw 'Source ZIP omitted files from its checksum manifest.' }
} finally { $archive.Dispose() }
Write-Host "Corresponding-source ZIP verified: $zip"
Get-Item -LiteralPath $zip
