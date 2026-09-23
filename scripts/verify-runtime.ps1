#Requires -Version 7.2
param(
    [string]$RuntimePath = (Join-Path (Split-Path $PSScriptRoot -Parent) 'runtime'),
    [string]$ManifestPath = (Join-Path (Split-Path $PSScriptRoot -Parent) 'assets\runtime-manifest.json'),
    [string]$ApplicationPath,
    [switch]$NoManifest,
    [switch]$ToolsOnly,
    [switch]$ModelsOnly
)
. (Join-Path $PSScriptRoot 'common.ps1')
if ($ToolsOnly -and $ModelsOnly) { throw 'Choose either -ToolsOnly or -ModelsOnly, not both.' }
if (-not ('TranscribeMe.PEImports' -as [type])) {
    Add-Type -TypeDefinition @'
using System;
using System.Collections.Generic;
using System.IO;
using System.Text;
namespace TranscribeMe {
    public static class PEImports {
        public static string[] Read(string path) {
            byte[] b = File.ReadAllBytes(path);
            if (b.Length < 128 || b[0] != 'M' || b[1] != 'Z') throw new Exception("Not a PE file: " + path);
            int pe = BitConverter.ToInt32(b, 0x3c);
            if (BitConverter.ToUInt32(b, pe) != 0x4550 || BitConverter.ToUInt16(b, pe + 4) != 0x8664)
                throw new Exception("Expected Windows x64 PE: " + path);
            int count = BitConverter.ToUInt16(b, pe + 6), opt = pe + 24;
            if (BitConverter.ToUInt16(b, opt) != 0x20b) throw new Exception("Expected PE32+: " + path);
            int sections = opt + BitConverter.ToUInt16(b, pe + 20);
            Func<uint, int> offset = rva => {
                for (int i = 0; i < count; i++) {
                    int s = sections + i * 40;
                    uint start = BitConverter.ToUInt32(b, s + 12);
                    uint size = Math.Max(BitConverter.ToUInt32(b, s + 8), BitConverter.ToUInt32(b, s + 16));
                    if (rva >= start && rva < start + size)
                        return checked((int)(rva - start + BitConverter.ToUInt32(b, s + 20)));
                }
                throw new Exception("Invalid PE RVA: " + path);
            };
            var result = new List<string>();
            Action<uint> add = rva => {
                int p = offset(rva), end = p;
                while (b[end] != 0) end++;
                result.Add(Encoding.ASCII.GetString(b, p, end - p));
            };
            uint imports = BitConverter.ToUInt32(b, opt + 120);
            if (imports != 0)
                for (int p = offset(imports); BitConverter.ToUInt32(b, p + 12) != 0; p += 20)
                    add(BitConverter.ToUInt32(b, p + 12));
            uint delay = BitConverter.ToUInt32(b, opt + 112 + 13 * 8);
            if (delay != 0)
                for (int p = offset(delay); BitConverter.ToUInt32(b, p + 4) != 0; p += 32) {
                    if ((BitConverter.ToUInt32(b, p) & 1) == 0) throw new Exception("Unsupported VA delay imports: " + path);
                    add(BitConverter.ToUInt32(b, p + 4));
                }
            return result.ToArray();
        }
    }
}
'@
}
$system = @(
    'ADVAPI32.dll', 'AVRT.dll', 'BCRYPT.dll', 'BCRYPTPRIMITIVES.dll', 'CABINET.dll',
    'CFGMGR32.dll', 'COMBASE.dll', 'COMCTL32.dll', 'COMDLG32.dll', 'CRYPT32.dll',
    'CRYPTBASE.dll', 'CRYPTSP.dll', 'D2D1.dll', 'D3D11.dll', 'D3D12.dll', 'DBGHELP.dll',
    'DCOMP.dll', 'DNSAPI.dll', 'DWMAPI.dll', 'DWRITE.dll', 'DXGI.dll', 'GDI32.dll',
    'HID.dll', 'IMM32.dll', 'IPHLPAPI.dll', 'KERNEL32.dll', 'KERNELBASE.dll',
    'MF.dll', 'MFPLAT.dll', 'MFREADWRITE.dll', 'MFUUID.dll', 'MSVCRT.dll',
    'MSWSOCK.dll', 'NCRYPT.dll', 'NETAPI32.dll', 'NORMALIZ.dll', 'NTDLL.dll',
    'OLE32.dll', 'OLEAUT32.dll', 'POWRPROF.dll', 'PROPSYS.dll', 'PSAPI.dll',
    'RPCRT4.dll', 'SECUR32.dll', 'SETUPAPI.dll', 'SHCORE.dll', 'SHELL32.dll',
    'SHLWAPI.dll', 'USER32.dll', 'USERENV.dll', 'USP10.dll', 'UXTHEME.dll',
    'UCRTBASE.dll', 'VERSION.dll', 'WINHTTP.dll', 'WININET.dll', 'WINMM.dll',
    'WINSPOOL.DRV', 'WINTRUST.dll', 'WS2_32.dll', 'WTSAPI32.dll'
)
$tools = @('whisper\whisper-cli.exe', 'ffmpeg\bin\ffmpeg.exe', 'ffmpeg\bin\ffprobe.exe')
$models = @('models\ggml-base.bin', 'models\ggml-silero-v5.1.2.bin')
$expected = if ($ToolsOnly) { $tools } elseif ($ModelsOnly) { $models } else { $tools + $models }
foreach ($name in $expected) {
    if (-not (Test-Path (Join-Path $RuntimePath $name) -PathType Leaf)) { throw "Missing runtime file: $name" }
}
if ($ToolsOnly -or $ModelsOnly) {
    $expectedSet = @{}
    foreach ($name in $expected) { $expectedSet[$name] = $true }
    foreach ($file in Get-ChildItem $RuntimePath -Recurse -File) {
        $relative = [IO.Path]::GetRelativePath($RuntimePath, $file.FullName)
        if (-not $expectedSet.ContainsKey($relative)) { throw "Unexpected runtime file: $relative" }
    }
}
$nativeFiles = @(Get-ChildItem $RuntimePath -Recurse -File | Where-Object Extension -in '.exe', '.dll')
if ($ApplicationPath) { $nativeFiles += Get-Item -LiteralPath $ApplicationPath }
foreach ($file in $nativeFiles) {
    foreach ($dependency in [TranscribeMe.PEImports]::Read($file.FullName)) {
        if ($dependency -match '^(MSVCP\d|VCRUNTIME\d|VCOMP\d|libgcc|libstdc\+\+|libwinpthread)') {
            throw "Forbidden separately installed native runtime: $($file.Name) imports $dependency"
        }
        if ($dependency -in $system -or $dependency -match '^(api-ms-win-|ext-ms-win-).+\.dll$') { continue }
        if (-not (Test-Path (Join-Path $file.DirectoryName $dependency) -PathType Leaf)) {
            throw "Unresolved app-local PE dependency: $($file.Name) imports $dependency"
        }
    }
}
$lock = Get-Content (Join-Path $PSScriptRoot 'runtime-lock.json') -Raw | ConvertFrom-Json
if (-not $ToolsOnly) {
    foreach ($model in @(@{ name = $models[0]; hash = $lock.model.sha256 }, @{ name = $models[1]; hash = $lock.vad.sha256 })) {
        if ((Get-FileHash (Join-Path $RuntimePath $model.name) -Algorithm SHA256).Hash.ToLowerInvariant() -ne $model.hash) {
            throw "Model integrity failure: $($model.name)"
        }
    }
}
if (-not $NoManifest) {
    $manifest = Get-Content $ManifestPath -Raw | ConvertFrom-Json
    $manifestFiles = @($manifest.files)
    if ($ToolsOnly -or $ModelsOnly) {
        $selected = @{}
        foreach ($name in $expected) { $selected["runtime/$($name.Replace('\', '/'))"] = $true }
        $manifestFiles = @($manifestFiles | Where-Object { $selected.ContainsKey($_.path) })
    }
    $seen = @{}
    foreach ($file in $manifestFiles) {
        if ($file.path -notmatch '^runtime/[a-zA-Z0-9._/-]+$' -or $file.path.Contains('..')) {
            throw "Unsafe manifest path: $($file.path)"
        }
        $relative = $file.path.Substring(8).Replace('/', '\')
        if ($seen.ContainsKey($relative)) { throw "Duplicate manifest path: $relative" }
        $seen[$relative] = $true
        $path = Join-Path $RuntimePath $relative
        if (-not (Test-Path $path) -or (Get-Item $path).Length -ne $file.size -or
            (Get-FileHash $path -Algorithm SHA256).Hash.ToLowerInvariant() -ne $file.sha256) {
            throw "Runtime integrity failure: $relative"
        }
    }
    foreach ($file in Get-ChildItem $RuntimePath -File -Recurse) {
        if (-not $seen.ContainsKey([IO.Path]::GetRelativePath($RuntimePath, $file.FullName))) {
            throw "Unmanifested runtime file: $($file.FullName)"
        }
    }
    if ($ApplicationPath) {
        $binary = [Text.Encoding]::Latin1.GetString([IO.File]::ReadAllBytes($ApplicationPath))
        $embedded = [Text.Encoding]::Latin1.GetString([IO.File]::ReadAllBytes($ManifestPath))
        if (-not $binary.Contains($embedded)) { throw 'Application embeds a different runtime manifest. Rebuild Wails before packaging.' }
    }
}
if (-not $ModelsOnly) {
    $ffmpeg = Join-Path $RuntimePath 'ffmpeg\bin\ffmpeg.exe'
    $probe = Join-Path $RuntimePath 'ffmpeg\bin\ffprobe.exe'
    $version = (& $ffmpeg -version 2>&1 | Out-String)
    if ($LASTEXITCODE -ne 0 -or $version -notmatch 'ffmpeg version 8\.1\.2' -or
        $version -notmatch '--disable-network' -or $version -match '--enable-(gpl|nonfree|version3|lib)') {
        throw 'FFmpeg version or redistribution configuration check failed.'
    }
    Invoke-Checked $probe @('-version')
    $protocols = (& $ffmpeg -hide_banner -protocols 2>&1 | Out-String)
    if ($LASTEXITCODE -ne 0 -or $protocols -match '(?m)^\s+(https?|tcp|tls|udp|ftp)$') {
        throw 'Network protocols must not be compiled into FFmpeg.'
    }
    $help = (& (Join-Path $RuntimePath 'whisper\whisper-cli.exe') --help 2>&1 | Out-String)
    if ($LASTEXITCODE -ne 0 -or $help -notmatch '--vad-model') { throw 'Whisper CLI did not pass its startup check.' }
}
Write-Host "Verified x64 PE dependency closure, model hashes, native startup, and offline FFmpeg: $RuntimePath"
