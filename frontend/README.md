# Transcribe Me frontend

Vanilla TypeScript, Vite 7.3.6, and the native Wails Go bridge. Production contains no sample transcripts, cloud API calls, or browser-only fallback. Open it through the desktop application to transcribe.

The complete portable bundle works offline, including its first run. Windows may need an internet connection to install WebView2 if that OS dependency is missing. The backend limits each media file to six hours to bound resource use; the app and landing page disclose this limit.

## Develop and verify

Use Node.js 22.12 or newer and run commands from this directory:

```powershell
npm ci
npm run dev
npm run typecheck
npm test
npm run test:e2e
npm run build
```

The UI regression suite uses installed Microsoft Edge through Playwright, on port 5179. Its bridge fixtures exist only under `tests\ui`. The suite covers native actions, cancellation, history, errors, safe rendering, serial polling, focus/scroll preservation, themes, and the minimum desktop size. Landing-page tests check mobile and desktop layouts, local download links, and absence of remote requests. It does not replace an end-to-end test of the actual Windows transcription engine.

`dist` is the production embed directory. A normal browser deliberately displays “Desktop app required.” The parent Wails application supplies `window.go.main.App` with the methods and data types in `src\types.ts`. `Job.progress` is a percentage from 0 to 100. `Status` polls serially, 800 ms after the previous request and associated rendering complete. A completed job needs `transcriptID` to open its result.

Optional `Snapshot.historyWarning` appears persistently beside recent transcripts, separately from action feedback. It does not block healthy history or new transcription. Saved transcripts can still be reviewed if the engine reports a setup error.

Native file selection and an explicit file-path form work without drag-and-drop. If the host exposes `window.runtime.OnFileDrop`, the frontend registers the documented `(x, y, paths)` callback and enables its drop hint. It does not read `File.path` or assume browser drop events provide native paths.

`docs\index.html` at the repository root is the independent static landing site. It references versioned ZIP and checksum files in its `downloads` subdirectory; packaging supplies those real files. The example transcript on that page is original illustrative copy, not user audio.
