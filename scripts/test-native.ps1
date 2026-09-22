#Requires -Version 7.2
param([string]$RuntimePath = (Join-Path (Split-Path $PSScriptRoot -Parent) 'runtime'))
. (Join-Path $PSScriptRoot 'common.ps1')
$root = Split-Path $PSScriptRoot -Parent
$results = Join-Path $root 'build\native-validation'
$work = Join-Path $results 'Local data 日本'
New-Item -ItemType Directory -Force $work | Out-Null
$ffmpeg = Join-Path $RuntimePath 'ffmpeg\bin\ffmpeg.exe'
$ffprobe = Join-Path $RuntimePath 'ffmpeg\bin\ffprobe.exe'
$originalPath = $env:PATH
try {
    $env:PATH = "$env:SystemRoot\System32;$env:SystemRoot"
    $fixtures = Get-ChildItem (Join-Path $PSScriptRoot 'fixtures') -File | Where-Object Extension -ne '.txt'
    if ($fixtures.Count -lt 17) { throw 'The complete codec fixture set is required.' }
    foreach ($fixture in $fixtures) {
        $inputPath = Join-Path $work $fixture.Name
        $output = Join-Path $work "$($fixture.Name).decoded.wav"
        Copy-Item -LiteralPath $fixture.FullName -Destination $inputPath -Force
        Invoke-Checked $ffmpeg @('-hide_banner', '-loglevel', 'error', '-nostdin', '-y',
            '-i', $inputPath, '-map', '0:a:0', '-vn', '-sn', '-dn',
            '-ar', '16000', '-ac', '1', '-c:a', 'pcm_s16le', '-f', 'wav', $output)
        $json = & $ffprobe -v error -show_streams -show_format -of json $output
        if ($LASTEXITCODE) { throw "Probe failed: $($fixture.Name)" }
        $probe = $json | ConvertFrom-Json
        if ($probe.streams.Count -ne 1 -or $probe.streams[0].codec_name -ne 'pcm_s16le' -or
            $probe.streams[0].sample_rate -ne '16000' -or $probe.streams[0].channels -ne 1 -or
            [double]$probe.format.duration -lt 0.9 -or [double]$probe.format.duration -gt 1.2) {
            throw "Audio conversion regression: $($fixture.Name)"
        }
        Remove-Item -LiteralPath $output, $inputPath
    }
    $upstreamSample = Join-Path $root 'build\native\whisper.cpp-1.8.3\samples\jfk.wav'
    if (-not (Test-Path $upstreamSample)) { throw 'Whisper upstream sample missing. Run build-native.ps1.' }
    $sample = Join-Path $work 'jfk.wav'
    $whisper = Join-Path $work 'whisper-cli.exe'
    $model = Join-Path $work 'ggml-base.bin'
    $vad = Join-Path $work 'ggml-silero-v5.1.2.bin'
    Copy-Item $upstreamSample $sample -Force
    Copy-Item (Join-Path $RuntimePath 'whisper\whisper-cli.exe') $whisper -Force
    Copy-Item (Join-Path $RuntimePath 'models\ggml-base.bin') $model -Force
    Copy-Item (Join-Path $RuntimePath 'models\ggml-silero-v5.1.2.bin') $vad -Force
    $prefix = Join-Path $work 'whisper-jfk'
    $log = Join-Path $results 'whisper-jfk.log'
    & $whisper -m $model -f $sample -t 4 -otxt -oj -of $prefix --vad --vad-model $vad *> $log
    if ($LASTEXITCODE) { Get-Content $log -Tail 30; throw 'Whisper Unicode model/VAD/input/output test failed.' }
    $systemInfo = (Select-String -LiteralPath $log -Pattern '^system_info:' | Select-Object -First 1).Line
    if ($systemInfo -notmatch '\bAVX2 = 1\b' -or $systemInfo -notmatch '\bFMA = 1\b' -or
        $systemInfo -notmatch '\bF16C = 1\b' -or $systemInfo -notmatch '\bBMI2 = 1\b') {
        throw 'Whisper must retain the optimized AVX2/FMA/F16C/BMI2 CPU baseline.'
    }
    $transcript = Get-Content "$prefix.txt" -Raw
    if ($transcript -notmatch 'ask not what your country can do for you' -or
        $transcript -notmatch 'what you can do for your country') {
        throw 'Whisper produced an unexpected result for the upstream JFK sample.'
    }
    $null = Get-Content "$prefix.json" -Raw | ConvertFrom-Json
    Copy-Item "$prefix.txt", "$prefix.json" $results -Force

    $silence = Join-Path $work 'silence.wav'
    $writer = [IO.BinaryWriter]::new([IO.File]::Create($silence))
    try {
        $size = 16000 * 2 * 2
        $writer.Write([Text.Encoding]::ASCII.GetBytes('RIFF')); $writer.Write([uint32](36 + $size))
        $writer.Write([Text.Encoding]::ASCII.GetBytes('WAVEfmt ')); $writer.Write([uint32]16)
        $writer.Write([uint16]1); $writer.Write([uint16]1); $writer.Write([uint32]16000)
        $writer.Write([uint32]32000); $writer.Write([uint16]2); $writer.Write([uint16]16)
        $writer.Write([Text.Encoding]::ASCII.GetBytes('data')); $writer.Write([uint32]$size)
        $writer.Write([byte[]]::new($size))
    } finally { $writer.Dispose() }
    $silenceHash = (Get-FileHash $silence -Algorithm SHA256).Hash
    $silencePrefix = Join-Path $work 'silence-result'
    & $whisper -m $model -f $silence -t 4 -oj -of $silencePrefix *> (Join-Path $results 'whisper-unicode-silence.log')
    if ($LASTEXITCODE) { throw 'Whisper Unicode silence.wav regression test failed.' }
    $null = Get-Content "$silencePrefix.json" -Raw | ConvertFrom-Json
    if ((Get-FileHash $silence -Algorithm SHA256).Hash -ne $silenceHash) {
        throw 'Whisper modified the Unicode-path input file.'
    }
    Copy-Item "$silencePrefix.json" (Join-Path $results 'whisper-unicode-silence.json') -Force
    Write-Host "Passed $($fixtures.Count) Unicode-path format conversions, Whisper + VAD inference, and Unicode silence.wav input/output."
} finally {
    $env:PATH = $originalPath
    Remove-Item -LiteralPath $work -Recurse -Force
}
