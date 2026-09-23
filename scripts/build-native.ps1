#Requires -Version 7.2
param([switch]$Rebuild, [switch]$RebuildWhisper)
. (Join-Path $PSScriptRoot 'common.ps1')
$root = Split-Path $PSScriptRoot -Parent
$native = Join-Path $root 'build\native'
$output = Join-Path $native 'output'
$lockPath = Join-Path $PSScriptRoot 'runtime-lock.json'
$lock = Get-Content $lockPath -Raw | ConvertFrom-Json
New-Item -ItemType Directory -Force $native, $output | Out-Null
$inputs = @('runtime-lock.json', 'configure-ffmpeg.sh', 'whisper-utf8.manifest', 'build-native.ps1') |
    ForEach-Object { (Get-FileHash (Join-Path $PSScriptRoot $_) -Algorithm SHA256).Hash }
$key = $inputs -join ':'
$receipt = Join-Path $output 'build-receipt.json'
if (-not $Rebuild -and -not $RebuildWhisper -and (Test-Path $receipt)) {
    $saved = Get-Content $receipt -Raw | ConvertFrom-Json
    $valid = $saved.inputKey -eq $key
    foreach ($file in $saved.files) {
        $path = Join-Path $output $file.name
        $valid = $valid -and (Test-Path $path) -and
            ((Get-FileHash $path -Algorithm SHA256).Hash.ToLowerInvariant() -eq $file.sha256)
    }
    if ($valid) { Write-Host 'Verified cached native build.'; return }
}
$cmake = Find-Tool 'cmake.exe' @('C:\Strawberry\c\bin\cmake.exe')
$bash = Find-Tool 'bash.exe' @('C:\Program Files\Git\bin\bash.exe')
$make = Find-Tool 'mingw32-make.exe' @('C:\Strawberry\c\bin\mingw32-make.exe')
$gcc = Find-Tool 'gcc.exe' @('C:\Strawberry\c\bin\gcc.exe')
$cmakeOutput = & $cmake --version
if ($LASTEXITCODE -ne 0) { throw 'Cannot read CMake version.' }
$cmakeVersion = $cmakeOutput | Select-Object -First 1
$gccOutput = & $gcc --version
if ($LASTEXITCODE -ne 0) { throw 'Cannot read GCC version.' }
$gccVersion = $gccOutput | Select-Object -First 1
$vswhere = Join-Path ${env:ProgramFiles(x86)} 'Microsoft Visual Studio\Installer\vswhere.exe'
if (-not (Test-Path $vswhere)) { throw 'Visual Studio Build Tools 2022 C++ workload is required.' }
$vs = & $vswhere -latest -products '*' -requires Microsoft.VisualStudio.Component.VC.Tools.x86.x64 -property installationPath
if ($LASTEXITCODE -ne 0 -or -not $vs) { throw 'MSVC x64 build tools were not found.' }
$vsVersion = & $vswhere -latest -products '*' -requires Microsoft.VisualStudio.Component.VC.Tools.x86.x64 -property installationVersion
if ($LASTEXITCODE -ne 0 -or -not $vsVersion) { throw 'Cannot read Visual Studio version.' }
$sdkBin = Join-Path ${env:ProgramFiles(x86)} 'Windows Kits\10\bin'
$manifestTools = @(Get-ChildItem "$sdkBin\*\x64\mt.exe" -ErrorAction SilentlyContinue |
    Sort-Object { [version]$_.Directory.Parent.Name } -Descending | ForEach-Object FullName)
$mt = Find-Tool 'mt.exe' $manifestTools
foreach ($asset in @($lock.whisper, $lock.ffmpeg)) {
    $archive = Get-VerifiedAsset $asset (Join-Path $native "downloads\$($asset.archive)")
    $source = Join-Path $native $asset.directory
    $cleanSource = $Rebuild -or ($RebuildWhisper -and $asset.directory -eq $lock.whisper.directory)
    if ($cleanSource -and (Test-Path $source)) { Remove-Item -LiteralPath $source -Recurse -Force }
    if (-not (Test-Path $source)) { Invoke-Checked 'tar.exe' @('-xf', $archive, '-C', $native) }
}
$whisperBuild = Join-Path $native 'whisper-build'
if (($Rebuild -or $RebuildWhisper) -and (Test-Path $whisperBuild)) { Remove-Item -LiteralPath $whisperBuild -Recurse -Force }
Invoke-Checked $cmake @('-S', (Join-Path $native $lock.whisper.directory), '-B', $whisperBuild,
    '-G', 'Visual Studio 17 2022', '-A', 'x64',
    '-DCMAKE_POLICY_DEFAULT_CMP0091=NEW', '-DCMAKE_MSVC_RUNTIME_LIBRARY=MultiThreaded', '-DBUILD_SHARED_LIBS=OFF',
    '-DGGML_OPENMP=OFF', '-DWHISPER_SDL2=OFF', '-DGGML_NATIVE=OFF',
    '-DGGML_AVX=ON', '-DGGML_AVX2=ON', '-DGGML_FMA=ON', '-DGGML_F16C=ON', '-DGGML_BMI2=ON',
    '-DGGML_AVX512=OFF', '-DWHISPER_BUILD_TESTS=OFF', '-DWHISPER_BUILD_EXAMPLES=ON',
    '-DWHISPER_CURL=OFF')
Invoke-Checked $cmake @('--build', $whisperBuild, '--config', 'Release', '--target', 'whisper-cli', '--parallel', '8')
$oldPath = $env:PATH
try {
    $gitUtilities = Join-Path (Split-Path (Split-Path $bash)) 'usr\bin'
    $env:PATH = "$(Split-Path $gcc);$(Split-Path $make);$gitUtilities;$env:PATH"
    Push-Location (Join-Path $native $lock.ffmpeg.directory)
    try {
        $configureKey = ((Get-FileHash (Join-Path $PSScriptRoot 'configure-ffmpeg.sh') -Algorithm SHA256).Hash) +
            ':' + $lock.ffmpeg.sha256 + ':' + $gccVersion
        $configureReceipt = Join-Path $native 'ffmpeg-configure-key.txt'
        if ($Rebuild -or -not (Test-Path 'ffbuild\config.mak') -or -not (Test-Path $configureReceipt) -or
            (Get-Content $configureReceipt -Raw).Trim() -ne $configureKey) {
            Invoke-Checked $bash @((Join-Path $PSScriptRoot 'configure-ffmpeg.sh'))
            $configureKey | Set-Content $configureReceipt -Encoding utf8NoBOM
        }
        # GNU make accepts the forward-slash shell path, including its spaces.
        $sh = (Join-Path (Split-Path $bash) 'sh.exe').Replace('\', '/')
        Invoke-Checked $make @('-j8', "SHELL=$sh")
    } finally { Pop-Location }
} finally { $env:PATH = $oldPath }
Copy-Item (Join-Path $whisperBuild 'bin\Release\whisper-cli.exe') $output -Force
$utf8Manifest = Join-Path $PSScriptRoot 'whisper-utf8.manifest'
Invoke-Checked $mt @('-nologo', '-manifest', $utf8Manifest, '-validate_manifest')
Invoke-Checked $mt @('-nologo', '-manifest', $utf8Manifest,
    "-outputresource:$(Join-Path $output 'whisper-cli.exe');#1")
Copy-Item (Join-Path $native "$($lock.ffmpeg.directory)\ffmpeg.exe") $output -Force
Copy-Item (Join-Path $native "$($lock.ffmpeg.directory)\ffprobe.exe") $output -Force
$files = Get-ChildItem $output -Filter '*.exe' | Sort-Object Name | ForEach-Object {
    @{ name = $_.Name; sha256 = (Get-FileHash $_.FullName -Algorithm SHA256).Hash.ToLowerInvariant() }
}
@{
    inputKey = $key; files = @($files)
    tools = @{
        cmake = $cmakeVersion
        gcc = $gccVersion
        visualStudio = $vs
        visualStudioVersion = $vsVersion
        manifestTool = (Get-Item $mt).VersionInfo.FileVersion
    }
} | ConvertTo-Json -Depth 5 | Set-Content $receipt -Encoding utf8NoBOM
Write-Host "Native binaries staged at $output. Active runtime was not modified."
