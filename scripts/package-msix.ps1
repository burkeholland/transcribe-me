#Requires -Version 7.2
# Packages a verified application folder as an MSIX: the one build.ps1 has just built, or one
# extracted from a published release ZIP after checking that ZIP against SHA256SUMS.txt. The
# companion native-source archive and SHA256SUMS.txt must sit next to that folder. After changing
# sources, run build.ps1 -Msix or build.ps1 -StoreIdentity instead of calling this directly.
[CmdletBinding()]
param(
    [ValidatePattern('^\d+\.\d+\.\d+$')]
    [string]$Version = '0.3.0',
    [string]$AppDirectory,
    [string]$NativeSourceArchive,
    [string]$StoreIdentity,
    [string]$OutputDirectory
)
. (Join-Path $PSScriptRoot 'common.ps1')
Add-Type -AssemblyName System.Drawing
Add-Type -AssemblyName System.IO.Compression.FileSystem
$root = Split-Path $PSScriptRoot -Parent
$configuredVersion = (Get-Content (Join-Path $root 'wails.json') -Raw | ConvertFrom-Json).info.productVersion
if ($Version -ne $configuredVersion) { throw "Package version $Version differs from wails.json product version $configuredVersion." }
$parts = @($Version.Split('.') | ForEach-Object { [int]$_ })
if ($parts[0] -ge 65535 -or $parts[1] -gt 65535 -or $parts[2] -gt 65535) { throw "Version $Version cannot be mapped to an MSIX version." }
# MSIX needs a nonzero first number and the Store reserves the fourth, so 0.3.0 becomes 1.3.0.0.
$packageVersion = '{0}.{1}.{2}.0' -f ($parts[0] + 1), $parts[1], $parts[2]
if ($StoreIdentity) { $StoreIdentity = (Resolve-Path -LiteralPath $StoreIdentity).Path }
$identity = Get-MsixIdentity $StoreIdentity
if (-not $AppDirectory) { $AppDirectory = Join-Path $root "dist\TranscribeMe-$Version-windows-x64" }
if (-not $OutputDirectory) { $OutputDirectory = Join-Path $root 'dist' }
$AppDirectory = (Resolve-Path -LiteralPath $AppDirectory).Path
$OutputDirectory = [IO.Path]::GetFullPath($OutputDirectory)

$exe = Join-Path $AppDirectory 'TranscribeMe.exe'
$info = (Get-Item -LiteralPath $exe).VersionInfo
$exeVersion = '{0}.{1}.{2}' -f $info.FileMajorPart, $info.FileMinorPart, $info.FileBuildPart
if ($exeVersion -ne $Version) { throw "TranscribeMe.exe version $exeVersion differs from $Version." }
$null = & "$PSScriptRoot\verify-go-toolchain.ps1" -ApplicationPath $exe
& "$PSScriptRoot\verify-runtime.ps1" -RuntimePath (Join-Path $AppDirectory 'runtime') -ToolsOnly -ApplicationPath $exe

# The package carries the FFmpeg and whisper.cpp source itself, so prove the archive is the published
# one and that it recorded exactly these native binaries.
$sourceName = "TranscribeMe-$Version-native-source"
if (-not $NativeSourceArchive) { $NativeSourceArchive = Join-Path (Split-Path $AppDirectory -Parent) "$sourceName.zip" }
$NativeSourceArchive = (Resolve-Path -LiteralPath $NativeSourceArchive).Path
if ((Split-Path $NativeSourceArchive -Leaf) -cne "$sourceName.zip") { throw "The native source archive must be named $sourceName.zip." }
$checksums = Join-Path (Split-Path $NativeSourceArchive -Parent) 'SHA256SUMS.txt'
$publishedSourceSha256 = $null
foreach ($line in Get-Content -LiteralPath $checksums) {
    if ($line -match '^([0-9a-f]{64}) [ *](.+)$' -and $Matches[2] -ceq "$sourceName.zip") { $publishedSourceSha256 = $Matches[1] }
}
$sourceSha256 = (Get-FileHash -LiteralPath $NativeSourceArchive -Algorithm SHA256).Hash.ToLowerInvariant()
if ($sourceSha256 -ne $publishedSourceSha256) { throw "$sourceName.zip does not match its entry in $checksums." }
$zip = [IO.Compression.ZipFile]::OpenRead($NativeSourceArchive)
try {
    $receiptEntry = @($zip.Entries | Where-Object { $_.FullName.Replace('\', '/') -ceq "$sourceName/build-receipt.json" })
    if ($receiptEntry.Count -ne 1) { throw "$sourceName.zip has no build-receipt.json." }
    $reader = [IO.StreamReader]::new($receiptEntry[0].Open())
    try { $sourceReceipt = $reader.ReadToEnd() | ConvertFrom-Json } finally { $reader.Dispose() }
} finally { $zip.Dispose() }
foreach ($tool in 'runtime\whisper\whisper-cli.exe', 'runtime\ffmpeg\bin\ffmpeg.exe', 'runtime\ffmpeg\bin\ffprobe.exe') {
    $recorded = @($sourceReceipt.files | Where-Object { $_.name -ceq (Split-Path $tool -Leaf) })
    $toolSha256 = (Get-FileHash -LiteralPath (Join-Path $AppDirectory $tool) -Algorithm SHA256).Hash.ToLowerInvariant()
    if ($recorded.Count -ne 1 -or $recorded[0].sha256 -ne $toolSha256) { throw "$tool does not match the hash recorded in $sourceName.zip." }
}

function Find-SdkTool([string]$Name) {
    $sdkBin = Join-Path ${env:ProgramFiles(x86)} 'Windows Kits\10\bin'
    $candidates = @(Get-ChildItem "$sdkBin\*\x64\$Name" -ErrorAction SilentlyContinue |
        Sort-Object { [version]$_.Directory.Parent.Name } -Descending | ForEach-Object FullName)
    Find-Tool $Name $candidates
}
function Invoke-Quiet([string]$Command, [string[]]$Arguments) {
    $output = & $Command @Arguments 2>&1 | Out-String
    if ($LASTEXITCODE -ne 0) { throw "$Command failed with exit code $LASTEXITCODE.`n$output" }
}
$makeAppx = Find-SdkTool 'makeappx.exe'
$makePri = Find-SdkTool 'makepri.exe'

$flavor = if ($identity.Store) { 'store' } else { 'development' }
$stage = Join-Path $root "build\msix\$flavor"
$layout = Join-Path $stage 'layout'
$priRoot = Join-Path $stage 'pri'
$verification = Join-Path $stage 'verification'
if (Test-Path -LiteralPath $stage) { Remove-Item -LiteralPath $stage -Recurse -Force }
New-Item -ItemType Directory -Force $layout, "$priRoot\Assets", $verification, $OutputDirectory | Out-Null

foreach ($file in @(
    'TranscribeMe.exe',
    'runtime\whisper\whisper-cli.exe',
    'runtime\ffmpeg\bin\ffmpeg.exe',
    'runtime\ffmpeg\bin\ffprobe.exe',
    'licenses\LICENSE'
)) {
    $destination = Join-Path $layout $file
    New-Item -ItemType Directory -Force (Split-Path $destination -Parent) | Out-Null
    Copy-Item -LiteralPath (Join-Path $AppDirectory $file) -Destination $destination
}
Copy-Item -LiteralPath (Join-Path $AppDirectory 'licenses\notices') -Destination (Join-Path $layout 'licenses') -Recurse
# Licenses stay with the verified binaries. The privacy policy and notices come from this checkout
# (same version, checked above) because they describe how the MSIX stores data and carries source.
Copy-Item -LiteralPath (Join-Path $root 'PRIVACY.md'), (Join-Path $root 'THIRD-PARTY-NOTICES.md') -Destination $layout
New-Item -ItemType Directory -Force (Join-Path $layout 'source') | Out-Null
Copy-Item -LiteralPath $NativeSourceArchive -Destination (Join-Path $layout "source\$sourceName.zip")
$repository = 'https://github.com/burkeholland/transcribe-me'
@(
    "TranscribeMe $Version (MSIX package version $packageVersion)"
    ''
    "TranscribeMe is MIT licensed. Application source: $repository/tree/v$Version"
    ''
    'runtime\whisper\whisper-cli.exe is built from unmodified whisper.cpp 1.8.3 (MIT).'
    'runtime\ffmpeg\bin\ffmpeg.exe and ffprobe.exe are built from unmodified FFmpeg 8.1.2'
    '(LGPL 2.1 or later) without network protocols or optional external libraries.'
    'Their corresponding source archives, build recipes, and hashes are included in this'
    "package as source\$sourceName.zip (SHA-256 $sourceSha256)."
    "The same archive is published at $repository/releases/tag/v$Version"
    ''
    'The speech models are not part of this package. The app downloads them from'
    'Hugging Face after you confirm. See THIRD-PARTY-NOTICES.md and the licenses folder.'
) | Set-Content -LiteralPath (Join-Path $layout 'SOURCE.txt') -Encoding utf8NoBOM

$icon = Join-Path $root 'build\appicon.png'
if (-not (Test-Path -LiteralPath $icon)) { & "$PSScriptRoot\generate-icon.ps1" }
$assets = [ordered]@{}
foreach ($logo in @(@{ Name = 'StoreLogo'; Size = 50 }, @{ Name = 'Square44x44Logo'; Size = 44 }, @{ Name = 'Square150x150Logo'; Size = 150 })) {
    foreach ($scale in 100, 125, 150, 200, 400) {
        $assets["$($logo.Name).scale-$scale.png"] = [int][Math]::Round($logo.Size * $scale / 100, [MidpointRounding]::AwayFromZero)
    }
}
foreach ($size in 16, 24, 32, 48, 256) {
    $assets["Square44x44Logo.targetsize-$size.png"] = $size
    $assets["Square44x44Logo.targetsize-${size}_altform-unplated.png"] = $size
}
$source = [Drawing.Image]::FromFile($icon)
$attributes = [Drawing.Imaging.ImageAttributes]::new()
try {
    # Mirrored edge sampling keeps anything that touches the icon's edges from fading when scaled down.
    $attributes.SetWrapMode([Drawing.Drawing2D.WrapMode]::TileFlipXY)
    foreach ($name in $assets.Keys) {
        $size = $assets[$name]
        $bitmap = [Drawing.Bitmap]::new($size, $size, [Drawing.Imaging.PixelFormat]::Format32bppArgb)
        $graphics = [Drawing.Graphics]::FromImage($bitmap)
        try {
            $graphics.CompositingMode = [Drawing.Drawing2D.CompositingMode]::SourceCopy
            $graphics.InterpolationMode = [Drawing.Drawing2D.InterpolationMode]::HighQualityBicubic
            $graphics.PixelOffsetMode = [Drawing.Drawing2D.PixelOffsetMode]::HighQuality
            $graphics.DrawImage($source, [Drawing.Rectangle]::new(0, 0, $size, $size),
                0, 0, $source.Width, $source.Height, [Drawing.GraphicsUnit]::Pixel, $attributes)
            $bitmap.Save((Join-Path $priRoot "Assets\$name"), [Drawing.Imaging.ImageFormat]::Png)
        } finally { $graphics.Dispose(); $bitmap.Dispose() }
    }
} finally { $attributes.Dispose(); $source.Dispose() }
Copy-Item -LiteralPath "$priRoot\Assets" -Destination $layout -Recurse

$manifest = (Get-Content -LiteralPath (Join-Path $root 'packaging\msix\AppxManifest.xml.in') -Raw).
    Replace('{{IDENTITY_NAME}}', [Security.SecurityElement]::Escape($identity.IdentityName)).
    Replace('{{PUBLISHER}}', [Security.SecurityElement]::Escape($identity.Publisher)).
    Replace('{{PUBLISHER_DISPLAY_NAME}}', [Security.SecurityElement]::Escape($identity.PublisherDisplayName)).
    Replace('{{PRODUCT_DISPLAY_NAME}}', [Security.SecurityElement]::Escape($identity.DisplayName)).
    Replace('{{VERSION}}', $packageVersion)
if ($manifest.Contains('{{')) { throw 'The MSIX manifest template has an unknown placeholder.' }
$manifestPath = Join-Path $layout 'AppxManifest.xml'
Set-Content -LiteralPath $manifestPath -Value $manifest -Encoding utf8NoBOM -NoNewline

# Index only the icons so Windows can choose the right scale and taskbar size variants.
$priConfig = Join-Path $stage 'priconfig.xml'
@'
<?xml version="1.0" encoding="utf-8"?>
<resources targetOsVersion="10.0.0" majorVersion="1">
  <index root="\" startIndexAt="\">
    <default>
      <qualifier name="Language" value="en-US" />
      <qualifier name="Contrast" value="standard" />
      <qualifier name="Scale" value="100" />
      <qualifier name="HomeRegion" value="001" />
      <qualifier name="TargetSize" value="256" />
      <qualifier name="LayoutDirection" value="LTR" />
      <qualifier name="Theme" value="dark" />
      <qualifier name="AlternateForm" value="" />
      <qualifier name="DXFeatureLevel" value="DX9" />
      <qualifier name="Configuration" value="" />
      <qualifier name="DeviceFamily" value="Universal" />
      <qualifier name="Custom" value="" />
    </default>
    <indexer-config type="folder" foldernameAsQualifier="true" filenameAsQualifier="true" qualifierDelimiter="." />
  </index>
</resources>
'@ | Set-Content -LiteralPath $priConfig -Encoding utf8NoBOM
Invoke-Quiet $makePri @('new', '/pr', $priRoot, '/cf', $priConfig, '/mn', $manifestPath, '/of', (Join-Path $layout 'resources.pri'), '/o')
$priDump = Join-Path $stage 'resources.pri.xml'
Invoke-Quiet $makePri @('dump', '/if', (Join-Path $layout 'resources.pri'), '/of', $priDump, '/o')
[xml]$priIndex = Get-Content -LiteralPath $priDump -Raw
foreach ($logo in 'StoreLogo', 'Square44x44Logo', 'Square150x150Logo') {
    $indexed = @($assets.Keys | Where-Object { $_.StartsWith("$logo.") }).Count
    $candidates = @($priIndex.SelectNodes("//NamedResource[@name='$logo.png']/Candidate")).Count
    if ($candidates -ne $indexed) { throw "resources.pri indexes $candidates of $indexed $logo images." }
}

$package = Join-Path $OutputDirectory "TranscribeMe-$Version-windows-x64-$flavor.msix"
Invoke-Quiet $makeAppx @('pack', '/d', $layout, '/p', $package, '/o')
Invoke-Quiet $makeAppx @('unpack', '/p', $package, '/d', $verification, '/o')

$packed = @{}
foreach ($file in Get-ChildItem -LiteralPath $verification -Recurse -File) {
    $packed[[IO.Path]::GetRelativePath($verification, $file.FullName)] = $file.FullName
}
$staged = @(Get-ChildItem -LiteralPath $layout -Recurse -File)
foreach ($file in $staged) {
    $relative = [IO.Path]::GetRelativePath($layout, $file.FullName)
    if (-not $packed.ContainsKey($relative) -or
        (Get-FileHash -LiteralPath $packed[$relative] -Algorithm SHA256).Hash -ne (Get-FileHash -LiteralPath $file.FullName -Algorithm SHA256).Hash) {
        throw "MSIX content differs from the staged layout: $relative"
    }
    $packed.Remove($relative)
}
$extra = @($packed.Keys | Where-Object { $_ -notin 'AppxBlockMap.xml', '[Content_Types].xml' })
if ($extra.Count) { throw "Unexpected files in the MSIX: $($extra -join ', ')" }

[xml]$document = Get-Content -LiteralPath (Join-Path $verification 'AppxManifest.xml') -Raw
$ns = [Xml.XmlNamespaceManager]::new($document.NameTable)
$ns.AddNamespace('f', 'http://schemas.microsoft.com/appx/manifest/foundation/windows10')
$ns.AddNamespace('uap', 'http://schemas.microsoft.com/appx/manifest/uap/windows10')
$packageIdentity = $document.SelectSingleNode('/f:Package/f:Identity', $ns)
$application = $document.SelectSingleNode('/f:Package/f:Applications/f:Application', $ns)
$actual = [ordered]@{
    'identity name' = $packageIdentity.GetAttribute('Name')
    'publisher' = $packageIdentity.GetAttribute('Publisher')
    'version' = $packageIdentity.GetAttribute('Version')
    'architecture' = $packageIdentity.GetAttribute('ProcessorArchitecture')
    'package display name' = $document.SelectSingleNode('/f:Package/f:Properties/f:DisplayName', $ns).InnerText
    'publisher display name' = $document.SelectSingleNode('/f:Package/f:Properties/f:PublisherDisplayName', $ns).InnerText
    'application display name' = $application.SelectSingleNode('uap:VisualElements', $ns).GetAttribute('DisplayName')
    'executable' = $application.GetAttribute('Executable')
}
$expected = [ordered]@{
    'identity name' = $identity.IdentityName
    'publisher' = $identity.Publisher
    'version' = $packageVersion
    'architecture' = 'x64'
    'package display name' = $identity.DisplayName
    'publisher display name' = $identity.PublisherDisplayName
    'application display name' = $identity.DisplayName
    'executable' = 'TranscribeMe.exe'
}
foreach ($key in $expected.Keys) {
    if ($actual[$key] -cne $expected[$key]) { throw "Packaged manifest $key is '$($actual[$key])', expected '$($expected[$key])'." }
}
$capabilities = @($document.SelectNodes('/f:Package/f:Capabilities/*', $ns) | ForEach-Object { $_.GetAttribute('Name') })
if (($capabilities -join ',') -ne 'runFullTrust') { throw "Unexpected MSIX capabilities: $($capabilities -join ', ')" }

$receipt = [ordered]@{
    applicationVersion = $Version
    packageVersion = $packageVersion
    storeIdentity = $identity.Store
    identityName = $identity.IdentityName
    publisher = $identity.Publisher
    publisherDisplayName = $identity.PublisherDisplayName
    displayName = $identity.DisplayName
    packageFamilyName = $identity.PackageFamilyName
    architecture = 'x64'
    minimumWindowsVersion = $document.SelectSingleNode('//f:TargetDeviceFamily', $ns).GetAttribute('MinVersion')
    capabilities = $capabilities
    signed = $false
    package = $package
    packageSha256 = (Get-FileHash -LiteralPath $package -Algorithm SHA256).Hash.ToLowerInvariant()
    executableSha256 = (Get-FileHash -LiteralPath $exe -Algorithm SHA256).Hash.ToLowerInvariant()
    nativeSourceArchive = "source\$sourceName.zip"
    nativeSourceSha256 = $sourceSha256
    fileCount = $staged.Count
    verifiedByUnpack = $true
}
$receipt | ConvertTo-Json | Set-Content -LiteralPath "$package.json" -Encoding utf8NoBOM
Write-Host "MSIX package verified by unpacking: $package"
Write-Host "MSIX version $packageVersion, identity $($identity.IdentityName), unsigned, SHA-256 $($receipt.packageSha256)"
if (-not $identity.Store) { Write-Warning 'This package uses the development identity. Pass -StoreIdentity for a Microsoft Store upload.' }
[pscustomobject]$receipt
