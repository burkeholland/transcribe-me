# Privacy

TranscribeMe processes media on your Windows PC. It does not upload your
recordings, extracted audio, or transcripts, and has no account, analytics,
telemetry, or cloud transcription service. Its Whisper speech model and Silero
voice activity model are downloaded only after you select **Download models**
in the app.

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

Whisper CLI and the offline FFmpeg tools are included in the application ZIP.
On first run, the app downloads the Whisper base and VAD model files from
immutable revisions of their upstream Hugging Face repositories. Hugging Face
and its CDN receive ordinary network request information such as your IP
address and user agent. The requests do not contain your recordings,
transcripts, or filenames. The app verifies each model against its expected
size and SHA-256 hash, then installs it under
`%LOCALAPPDATA%\TranscribeMe\runtime\models`.
Incomplete model staging directories are removed on the next startup after a
power failure, forced termination, or operating system crash.

Transcription itself works offline after the models are installed. Microsoft
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
