# Privacy

TranscribeMe processes media on your Windows PC. It does not upload your
recordings, extracted audio, or transcripts, and has no account, analytics,
telemetry, or cloud transcription service. Its Whisper speech model and Silero
voice activity model are downloaded only after you select **Download and
install** in the app.

## Files on your computer

Transcripts are saved under `%LOCALAPPDATA%\TranscribeMe\transcripts`. They
remain there until you delete them. Exporting or copying a transcript creates
another copy wherever you put it. Clipboard contents are managed by Windows,
including any clipboard history or synchronization you have enabled.

The app creates working audio files while processing. It removes those
working files when the job finishes or is canceled. A power failure, forced
termination, or operating system crash can leave temporary files behind.
On the next startup, the app removes its interrupted-job audio directories.
It does not remove unrelated directories or damaged saved transcripts.
The original media is not modified.

Saved transcripts are not encrypted by TranscribeMe. They rely on Windows
user-profile permissions and any disk encryption you have enabled. Other
software or administrators with access to your profile may read them.
Delete sensitive output when it is no longer needed.

## Internet and Windows components

The first-run engine installer downloads a versioned ZIP from this project's
GitHub release. That ZIP contains Whisper, FFmpeg, the Whisper base model, and
the VAD model. GitHub and its download infrastructure receive ordinary network
request information such as your IP address and user agent. The request does
not contain your recordings, transcripts, or filenames. The app verifies every
runtime file against SHA-256 hashes embedded in the executable, installs the
files under `%LOCALAPPDATA%\TranscribeMe\runtime`, and removes the downloaded
ZIP after installation.

Transcription itself works offline after the engine is installed. Microsoft
Edge WebView2 is the Windows component used to display the interface. Most
Windows 10 and Windows 11 systems already include it. On a system without it,
the embedded Microsoft bootstrapper needs an internet connection to install
WebView2. Its installation and servicing are governed by Microsoft's terms and
privacy policy, not this app's offline transcription behavior.

Build tools download source code, dependencies, and model files when a
developer builds the application. Those downloads do not contain user media.

## Responsible use

Only process recordings you are entitled to access and use. Speech recognition
can omit, mishear, or invent words. Review output before relying on it,
especially for medical, legal, safety, or other consequential decisions.
