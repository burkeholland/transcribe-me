# Third-party software and models

TranscribeMe's own code is MIT licensed. The components below retain their
original licenses. The app package includes the full notices in
`licenses\notices`; the native tools and models are delivered separately in
the matching runtime ZIP. In the source tree those notices are in `notices`.

| Component | Version | License and use |
| --- | --- | --- |
| whisper.cpp and ggml | 1.8.3 | MIT; statically linked CPU transcription executable |
| Whisper CLI embedded helpers | Included with whisper.cpp 1.8.3 | miniaudio (MIT No Attribution), stb_vorbis (MIT), and ggml CPU/llamafile MIT notices |
| OpenAI Whisper base model | SHA-256 pinned in `scripts\runtime-lock.json` | MIT; multilingual model converted to GGML format |
| Silero VAD model | 5.1.2 | MIT; speech activity detection |
| FFmpeg and ffprobe | 8.1.2 | LGPL 2.1 or later; separate audio-processing executables |
| MinGW-w64 runtime | 11.0.1 toolchain headers/runtime | Permissive and public-domain components; full component notice included |
| GCC runtime | 13.2.0 | GPL 3 with GCC Runtime Library Exception 3.1 |
| Go standard library and runtime | Build toolchain version recorded with the package | BSD 3-Clause |
| Wails and transitive Go modules | Versions in `go.mod`, `go.sum`, and `notices\GO-MODULES.txt` | Individual upstream licenses copied into `notices\go-modules` |

The application uses the Windows-provided WebView2 runtime. It is not included
in the archive. Wails embeds the Microsoft WebView2 installation bootstrapper;
Microsoft's component terms apply to that installer and the WebView2 runtime.
The Go WebView2 binding license is included with the Go module notices.

## FFmpeg source and your rights

This distribution does **not** use the BtbN FFmpeg binary bundle. It builds
unmodified FFmpeg 8.1.2 source with no optional external libraries, GPL
components, nonfree components, or network protocols. Only media input,
audio decoding, resampling, WAV output, and probing components are enabled.
The exact configure command is in `scripts\configure-ffmpeg.sh`.
The binaries communicate with TranscribeMe through files and command-line
arguments, not through a linked proprietary library.

Every generated release has a companion
`TranscribeMe-0.2.0-native-source.zip` containing the exact FFmpeg source
archive, Whisper source archive, build scripts, input hashes, and toolchain
build receipt. Both native source trees are unmodified. The source bundle
includes the UTF-8 Windows application manifest embedded into the built
Whisper executable and the exact optimized CPU build configuration.
Distributors must host that source archive next to the portable
binary archive with equal access, retain both checksums in `SHA256SUMS.txt`, and
retain these license notices. This is actual corresponding source provision,
not a promise to locate source later.

You may modify, rebuild, replace, and redistribute FFmpeg under its LGPL
terms. Nothing in TranscribeMe's license restricts reverse engineering for
debugging modifications to LGPL components. Since the app embeds integrity
hashes, rebuild the app after replacing a runtime binary. The provided build
scripts regenerate the manifest before building the app.

Compiler system libraries are supplied by their respective toolchains. The
FFmpeg source is built with MinGW-w64 GCC and its static runtime under the GCC
Runtime Library Exception. Whisper uses the Microsoft C++ static runtime
under the Visual Studio redistribution terms. No DLL is copied from System32,
and no separately installed Visual C++ redistributable is required.

## Upstream projects

- https://github.com/ggml-org/whisper.cpp
- https://github.com/openai/whisper
- https://huggingface.co/ggerganov/whisper.cpp
- https://github.com/snakers4/silero-vad
- https://huggingface.co/ggml-org/whisper-vad
- https://ffmpeg.org/
- https://www.mingw-w64.org/
- https://gcc.gnu.org/onlinedocs/libstdc++/manual/license.html
- https://go.dev/
- https://wails.io/

No affiliation with or endorsement by these projects is implied.
