# TranscribeMe

Private video and audio transcription for Windows. Drop in a recording and
get a local transcript. No account, API key, subscription, or cloud upload is
needed. On first run, the app offers to download its pinned local engine and
speech models from the matching GitHub release.

## Install and run

1. Download `TranscribeMe-0.2.0-windows-x64.zip` from the project's release
   page.
2. Optionally compare the download's SHA-256 with `SHA256SUMS.txt`:
   `Get-FileHash .\TranscribeMe-0.2.0-windows-x64.zip -Algorithm SHA256`.
3. Use **Extract All**, then open `TranscribeMe.exe`.
4. Select **Download and install** when prompted. The app downloads the matching
   Whisper CLI, FFmpeg tools, Whisper base model, and VAD model into
   `%LOCALAPPDATA%\TranscribeMe\runtime`. The download is verified against
   hashes embedded in the app before it is activated.
5. Choose your recording and start transcription.

**Requirements:** Windows 10 22H2 or Windows 11, x64 processor with AVX2,
FMA (FMA3), F16C, and BMI2 support, and at least 4 GB RAM.
8 GB RAM is recommended. Leave free
disk space for extracted audio as well as the application. A six-hour mono
16 kHz working WAV uses about 700 MB. Inference uses the CPU.

Microsoft Edge WebView2 is normally already installed. If it is missing, the
embedded Microsoft installer needs internet access for that one-time setup.
The local transcription engine also needs a one-time internet download. You do
not need Python, a separate FFmpeg installation, or a Visual C++ runtime.
After the engine and WebView2 are available, transcription works offline.

### Unsigned build

The current build is **unsigned**. An unsigned download may show Windows
publisher or SmartScreen warnings. This documentation does not recommend
bypassing those protections. A signed distribution requires the owner to sign
and timestamp the executable and verify the resulting archive. Published
releases include matching binary, runtime, source, and checksum downloads.
A checksum detects changes; it does not authenticate an unsigned publisher.

## What to expect

- Common inputs: MP4, MOV, MKV, WebM, AVI, WMV, MPEG, TS, MP3, WAV, M4A,
  AAC, FLAC, OGG, OPUS, and WMA. Encrypted media and unsupported codecs inside
  these containers may not work.
- The first audio track is transcribed. Additional tracks are ignored.
- Files may be up to six hours long. Processing time depends on the recording
  and your CPU. Very long recordings can take substantial time.
- The downloaded multilingual Whisper base model handles speech, not perfect
  dictation. Review the result. Noise, accents, overlapping voices, and music
  can reduce accuracy. There are no speaker labels.
- Transcripts are stored in `%LOCALAPPDATA%\TranscribeMe\transcripts`.
  They persist until deleted and are not encrypted by the app.
  Damaged history files are preserved and reported without blocking new work.
  Healthy saved transcripts remain available even if the engine is damaged.
- Working audio files are removed on completion or cancellation. Original
  media is left untouched.

See [PRIVACY.md](PRIVACY.md) for storage, clipboard, crash cleanup, and privacy
details. See [THIRD-PARTY-NOTICES.md](THIRD-PARTY-NOTICES.md) for component
licenses and corresponding source.

## Build from source

Use PowerShell 7.2 or later on Windows x64 with the patched Go version required
by `go.mod` (currently Go 1.26.8, including any newer `toolchain` directive), Node.js with npm,
Wails CLI 2.15.0, Visual Studio Build Tools 2022 with the C++ x64 workload,
CMake, the Windows SDK manifest tool (`mt.exe`), Git for Windows Bash, and
MinGW-w64 GCC with `mingw32-make`.
The tested MinGW toolchain is Strawberry Perl's GCC 13.2.0
(x86_64-ucrt-posix-seh, MinGW-w64 11.0.1). No global software is installed
by the build scripts.
Go's normal automatic toolchain selection can download the required version.
If automatic selection is disabled, install a compatible patched toolchain
yourself. Packaging checks both the selected compiler and the Go version
recorded inside `TranscribeMe.exe`; an older prebuilt binary is rejected.
Packaging always builds current frontend and Go sources. There is no
package-only shortcut.

```powershell
.\scripts\build.ps1
```

The build verifies SHA-pinned native source and models, builds static native
tools, generates the runtime integrity manifest and icon, runs `npm ci`,
`npm test`, the frontend production build, `npm run test:e2e`,
`go test ./...`, and `go vet ./...`, then runs
`wails build -clean -platform windows/amd64 -webview2 embed -s`.
The `-s` flag prevents Wails from repeating the already completed frontend
build. The UI tests use the installed Microsoft Edge browser.
It also checks native imports, converts 17 real codec/container fixtures, and
runs Whisper with VAD on the upstream JFK sample. Any failed command stops
packaging. There is no default test bypass.
The Whisper executable embeds a UTF-8 Windows application manifest so media,
model, and transcript paths with non-ASCII characters work without short
filenames. Native tests include a `Local data 日本` directory.
Whisper is built from unmodified pinned source with the optimized
AVX2/FMA/F16C/BMI2 instruction set, static MSVC runtime, and OpenMP disabled.
Source downloads and build intermediates live under `build\native`.

The final output is:

```text
dist\TranscribeMe-0.2.0-windows-x64\
dist\TranscribeMe-0.2.0-windows-x64.zip
dist\TranscribeMe-0.2.0-runtime-windows-x64.zip
dist\TranscribeMe-0.2.0-native-source.zip
dist\SHA256SUMS.txt
```

The app ZIP intentionally excludes the local engine. The runtime ZIP contains
the exact files the app downloads on first run, and the embedded manifest
verifies every extracted file before activation. The archives and checksums are
also copied to `docs\downloads` for static hosting. Publish the runtime and
native-source ZIPs alongside the app ZIP. The source archive contains the exact
upstream archives, build recipes, and hashes required for the native tools.
To validate the corresponding-source archive without building or launching
the app, run `.\scripts\package-native-source.ps1`. Its default output stays
under `build\source-package-validation`, not in the public downloads folder.

Close the running app before rebuilding runtime files.
`.\scripts\fetch-runtime.ps1 -RebuildNative` forces a clean source
rebuild; ordinary builds reuse only a hash-verified native build receipt.
`.\scripts\fetch-runtime.ps1 -RebuildWhisper` cleans and rebuilds only Whisper,
without recompiling an unchanged FFmpeg build.
Use `.\scripts\fetch-runtime.ps1 -StageOnly` to validate a replacement under
`build\runtime-staging` without changing the active runtime or embedded
manifest. Running without `-StageOnly` promotes it and regenerates the
manifest, so rebuild the application immediately afterward.

Reproducible here means pinned inputs and explicit configuration. MSVC,
MinGW, Go, Node, and Wails versions affect binary bytes; cross-toolchain
bit-for-bit reproducibility is not claimed.

## Test with a real recording

Use a short video containing clear speech. These opt-in tests run the actual
native engine, not mocks. They verify source-file preservation, Unicode paths,
TXT/SRT/VTT output, silence, cancellation, temporary-file cleanup, and all
17 codec/container fixtures.

```powershell
$env:TRANSCRIBEME_TEST_VIDEO = 'C:\Videos\recording.mp4'
go test .\internal\transcribe -run '^TestReal' -v -count=1 -timeout 15m
```

To exercise a specific runtime rather than the default development runtime,
set this variable to the folder containing its `runtime` directory:

```powershell
$env:TRANSCRIBEME_TEST_RUNTIME_DIR = (Resolve-Path '.').Path
go test .\internal\transcribe -run '^TestReal' -v -count=1 -timeout 15m
```

The tests keep their history in temporary profiles. They do not upload the
recording or change the normal application's transcript history.

## License

TranscribeMe is MIT licensed, Copyright 2026 Burke Holland. Third-party
components and models keep their original licenses.
