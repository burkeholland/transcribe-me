import './style.css';
import type { FileInfo, Snapshot, Summary, Transcript } from './types.ts';
import { clock, errorText, fileSize, isActive, matchingSegments, progressPercent, resolution } from './format.ts';

const logoIcon = `<svg viewBox="0 0 32 32" fill="none" aria-hidden="true"><g stroke="currentColor" stroke-width="2.6" stroke-linecap="round"><path d="M2.6 13v6"/><path d="M8.2 9.5v13"/><path d="M13.8 4v24"/><path d="M19.4 8v16"/><path d="M25 5.5v21"/><path d="M29.4 13v6"/></g></svg>`;
const chipIcon = `<svg viewBox="0 0 24 24" fill="none" aria-hidden="true"><rect x="7" y="7" width="10" height="10" rx="1.6" stroke="currentColor" stroke-width="1.6"/><path d="M9.5 7V4.4M14.5 7V4.4M9.5 19.6V17M14.5 19.6V17M7 9.5H4.4M7 14.5H4.4M19.6 9.5H17M19.6 14.5H17" stroke="currentColor" stroke-width="1.6" stroke-linecap="round"/></svg>`;
const folderIcon = `<svg viewBox="0 0 24 24" fill="none" aria-hidden="true"><path d="M3.4 6.6A1.6 1.6 0 0 1 5 5h3.9a1.6 1.6 0 0 1 1.13.47L11.2 6.6H19a1.6 1.6 0 0 1 1.6 1.6v8.2A1.6 1.6 0 0 1 19 18H5a1.6 1.6 0 0 1-1.6-1.6Z" stroke="currentColor" stroke-width="1.65" stroke-linejoin="round"/></svg>`;
const mediaIcon = `<svg viewBox="0 0 24 24" fill="none" aria-hidden="true"><path d="M6 3.3h7.6l4.6 4.6v12.8a1 1 0 0 1-1 1H6a1 1 0 0 1-1-1V4.3a1 1 0 0 1 1-1Z" stroke="currentColor" stroke-width="1.5" stroke-linejoin="round" vector-effect="non-scaling-stroke"/><path d="M13.4 3.4v3.6a1 1 0 0 0 1 1H18" stroke="currentColor" stroke-width="1.5" stroke-linejoin="round" vector-effect="non-scaling-stroke"/><path d="M10.1 11.6v4.9a.55.55 0 0 0 .85.46l3.8-2.45a.55.55 0 0 0 0-.92l-3.8-2.45a.55.55 0 0 0-.85.46Z" fill="currentColor"/></svg>`;
const downloadIcon = `<svg viewBox="0 0 24 24" fill="none" aria-hidden="true"><path d="M12 3.6v11.2m0 0 3.9-3.9M12 14.8l-3.9-3.9" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round"/><path d="M4.6 16.3v2.1A1.6 1.6 0 0 0 6.2 20h11.6a1.6 1.6 0 0 0 1.6-1.6v-2.1" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round"/></svg>`;
const checkIcon = `<svg viewBox="0 0 20 20" fill="none" aria-hidden="true"><circle cx="10" cy="10" r="8.4" fill="currentColor"/><path d="M6.2 10.2 8.8 12.8 13.8 7.4" stroke="#fff" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round"/></svg>`;
const spinnerIcon = `<svg viewBox="0 0 20 20" fill="none" aria-hidden="true" class="spin"><circle cx="10" cy="10" r="7.6" stroke="currentColor" stroke-opacity=".25" stroke-width="2.2"/><path d="M17.6 10A7.6 7.6 0 0 0 10 2.4" stroke="currentColor" stroke-width="2.2" stroke-linecap="round"/></svg>`;
const alertIcon = `<svg viewBox="0 0 20 20" fill="none" aria-hidden="true"><circle cx="10" cy="10" r="8.4" fill="currentColor"/><path d="M10 5.8v5M10 13.6v.2" stroke="#fff" stroke-width="1.9" stroke-linecap="round"/></svg>`;
const searchIcon = `<svg viewBox="0 0 24 24" fill="none" aria-hidden="true"><circle cx="10.8" cy="10.8" r="6.1" stroke="currentColor" stroke-width="1.7"/><path d="m15.4 15.4 3.9 3.9" stroke="currentColor" stroke-width="1.7" stroke-linecap="round"/></svg>`;
const sunIcon = `<svg viewBox="0 0 24 24" fill="none" aria-hidden="true"><circle cx="12" cy="12" r="4.4" stroke="currentColor" stroke-width="1.6"/><path d="M12 3.4v2.1M12 18.5v2.1M20.6 12h-2.1M5.5 12H3.4m14.1-6.1-1.5 1.5M8 16.1l-1.5 1.5m11 0-1.5-1.5M8 8 6.5 6.5" stroke="currentColor" stroke-width="1.6" stroke-linecap="round"/></svg>`;
const moonIcon = `<svg viewBox="0 0 24 24" fill="none" aria-hidden="true"><path d="M20.3 14.6A8.6 8.6 0 0 1 9.4 3.7a8.6 8.6 0 1 0 10.9 10.9Z" stroke="currentColor" stroke-width="1.6" stroke-linejoin="round"/></svg>`;
const minIcon = `<svg viewBox="0 0 12 12" aria-hidden="true"><path d="M1.5 6h9" stroke="currentColor" stroke-width="1.1" stroke-linecap="square"/></svg>`;
const maxIcon = `<svg viewBox="0 0 12 12" fill="none" aria-hidden="true"><rect x="1.6" y="1.6" width="8.8" height="8.8" rx="1" stroke="currentColor" stroke-width="1.1"/></svg>`;
const closeIcon = `<svg viewBox="0 0 12 12" aria-hidden="true"><path d="m1.9 1.9 8.2 8.2M10.1 1.9l-8.2 8.2" stroke="currentColor" stroke-width="1.1" stroke-linecap="square"/></svg>`;

const languageOptions = [
  ['auto', 'Detect automatically'], ['en', 'English'], ['es', 'Spanish'], ['fr', 'French'], ['de', 'German'],
  ['it', 'Italian'], ['pt', 'Portuguese'], ['ja', 'Japanese'], ['zh', 'Chinese'], ['ko', 'Korean'], ['hi', 'Hindi'],
  ['ar', 'Arabic'], ['ru', 'Russian'], ['nl', 'Dutch'], ['uk', 'Ukrainian'], ['pl', 'Polish'], ['tr', 'Turkish'],
  ['vi', 'Vietnamese'], ['id', 'Indonesian'], ['sv', 'Swedish'], ['da', 'Danish'], ['no', 'Norwegian'],
  ['fi', 'Finnish'], ['el', 'Greek'], ['he', 'Hebrew'], ['cs', 'Czech'], ['ro', 'Romanian'], ['hu', 'Hungarian'],
  ['th', 'Thai'],
].map(([value, label]) => `<option value="${value}">${label}</option>`).join('');

const app = document.querySelector<HTMLDivElement>('#app')!;
app.innerHTML = `
  <div class="window" id="window">
    <header class="titlebar">
      <div class="titlebar-grab">
        <span class="logo">${logoIcon}</span>
        <span class="wordmark">Transcribe Me</span>
      </div>
      <div class="titlebar-actions">
        <button id="theme" class="icon-button" type="button" aria-label="Switch to dark theme" title="Switch to dark theme">${moonIcon}</button>
      </div>
      <div class="window-buttons" id="window-buttons" hidden>
        <button id="win-minimise" class="win-button" type="button" aria-label="Minimise">${minIcon}</button>
        <button id="win-maximise" class="win-button" type="button" aria-label="Maximise">${maxIcon}</button>
        <button id="win-close" class="win-button win-button-close" type="button" aria-label="Close">${closeIcon}</button>
      </div>
    </header>

    <div class="body">
      <nav class="sidebar" aria-label="Recent transcripts">
        <section id="recent" class="recent">
          <p class="recent-head">Recent<span id="history-count" class="recent-count">0</span></p>
          <p id="history-warning" class="warning" role="status" aria-live="polite" hidden></p>
          <p id="recent-empty" class="recent-empty">Transcripts you finish are saved here.</p>
          <ul id="history-list"></ul>
        </section>
        <div class="engine">${chipIcon}<span class="engine-text"><strong>Local Whisper</strong><span id="engine-label" role="status" aria-live="polite">Checking engine</span></span></div>
        <p class="engine-meta"><span id="model">Whisper base multilingual</span> · <span id="version">v0.2.0</span></p>
      </nav>

      <main class="picker" id="picker">
        <h1>Transcribe Me</h1>
        <p class="lede">Turn your videos into text using a local Whisper model.</p>
        <section id="runtime-setup" class="runtime-setup" aria-labelledby="runtime-setup-title" hidden>
          <span class="runtime-setup-icon">${chipIcon}</span>
          <h2 id="runtime-setup-title">Install the local engine</h2>
          <p>Transcribe Me needs Whisper, FFmpeg, and two speech models. They are downloaded once and kept on this computer.</p>
          <p id="runtime-size" class="runtime-size"></p>
          <button id="install-runtime" class="button-primary" type="button">${downloadIcon}<span id="install-runtime-label">Download and install</span></button>
          <div id="runtime-progress-line" class="runtime-progress-line" hidden>
            <progress id="runtime-progress" max="100" aria-label="Engine download progress"></progress>
            <span id="runtime-progress-label"></span>
          </div>
          <p id="runtime-error" class="runtime-error" role="alert" hidden></p>
        </section>
        <div class="dropzone" id="dropzone">
          <span class="dropzone-icon">${mediaIcon}</span>
          <p class="dropzone-title">Drop a video here</p>
          <p class="dropzone-hint">or click to browse</p>
          <button id="choose-file" class="button-primary" type="button" disabled>${folderIcon}<span>Choose Video</span></button>
        </div>
        <p id="supports" class="supports">Supports: MP4, MOV, MKV, AVI, WEBM, M4V</p>
        <div id="language-row" class="language-row">
          <label for="language">Spoken language</label>
          <select id="language">${languageOptions}</select>
        </div>
      </main>

      <section class="reader" id="reader" aria-label="Transcript">
        <div id="service-error" class="banner" role="alert" hidden></div>

        <div id="reader-head" class="reader-head" hidden>
          <span id="poster" class="poster">${mediaIcon}</span>
          <div class="reader-head-text">
            <h2 id="reader-title"></h2>
            <p id="reader-meta" class="reader-meta"></p>
            <p id="reader-status" class="reader-status"><span id="reader-status-icon" class="reader-status-icon"></span><span id="reader-status-text"></span></p>
          </div>
          <div class="reader-head-actions">
            <button id="cancel" class="button-secondary" type="button" hidden>Cancel</button>
            <div class="menu-anchor">
              <button id="export" class="button-secondary" type="button" aria-haspopup="menu" aria-expanded="false" hidden>${downloadIcon}<span>Download</span></button>
              <div id="export-menu" class="menu" role="menu" hidden>
                <button role="menuitem" type="button" data-export="copy">Copy text</button>
                <hr>
                <button role="menuitem" type="button" data-export="txt">Save as TXT</button>
                <button role="menuitem" type="button" data-export="srt">Save as SRT</button>
                <button role="menuitem" type="button" data-export="vtt">Save as VTT</button>
              </div>
            </div>
          </div>
        </div>

        <div id="progress-line" class="progress-line" hidden>
          <progress id="progress" max="100" value="0" aria-label="Transcription progress"></progress>
          <span id="progress-label"></span>
        </div>
        <p id="job-error" class="job-error" role="alert" hidden></p>

        <div id="tabs" class="tabs" hidden>
          <div class="tab-list" role="tablist">
            <button id="tab-transcript" class="tab active" role="tab" type="button" aria-selected="true">Transcript</button>
            <button id="tab-preview" class="tab" role="tab" type="button" aria-selected="false">Preview</button>
          </div>
          <div class="search">
            <button id="search-toggle" class="search-toggle" type="button" aria-label="Find in transcript" aria-expanded="false">${searchIcon}</button>
            <input id="search" type="search" placeholder="Find in transcript" aria-label="Find in transcript" autocomplete="off" hidden>
            <span id="search-count" class="sr-only" role="status" aria-live="polite"></span>
          </div>
        </div>

        <div id="panel-transcript" class="panel" hidden>
          <p id="transcript-empty" class="transcript-empty" hidden></p>
          <ol id="segments" class="segments" aria-label="Timestamped transcript"></ol>
        </div>
        <div id="panel-preview" class="panel" hidden><p id="preview-text" class="preview-text"></p></div>

        <div id="empty-state" class="empty">
          <span class="empty-icon">${mediaIcon}</span>
          <p class="empty-title">Your transcript appears here</p>
          <p class="empty-hint">Choose a video and Transcribe Me starts reading it straight away. Timestamps included.</p>
        </div>
      </section>
    </div>
  </div>

  <div id="toast" class="toast" role="status" aria-live="polite" hidden></div>

  <div id="browser-notice" class="browser-notice" hidden>
    <span class="logo">${logoIcon}</span>
    <h1>Desktop app required</h1>
    <p>Transcribe Me uses a local Whisper engine installed by the Windows app. This browser page cannot open or transcribe your files.</p>
    <p>Open <strong>TranscribeMe.exe</strong>. The app will offer to download its verified local engine before the first transcript.</p>
  </div>`;

function el<T extends HTMLElement = HTMLElement>(id: string): T {
  return document.getElementById(id) as T;
}

function text(id: string, value: string): void {
  const element = el(id);
  if (element.textContent !== value) element.textContent = value;
}

const bridge = window.go?.main?.App;

type Head = { name: string; path: string; durationMs: number; size: number; height: number };

let snapshot: Snapshot | null = null;
let transcript: Transcript | null = null;
let head: Head | null = null;
let headFromJob = false;
let pending = false;
let disconnected = false;
let awaitingStart = false;
let startPreviousJobID = '';
let loadedCompletion = '';
let historySignature = '';
let timer: ReturnType<typeof setTimeout> | undefined;
let stopped = false;
let cancelRequested = false;
let toastTimer: ReturnType<typeof setTimeout> | undefined;
const posters = new Map<string, string>();

/* ---------- chrome ---------- */

el('win-minimise').addEventListener('click', () => void bridge?.MinimiseWindow?.());
el('win-maximise').addEventListener('click', () => void bridge?.ToggleMaximiseWindow?.());
el('win-close').addEventListener('click', () => void bridge?.CloseWindow?.());

function showTab(name: 'transcript' | 'preview'): void {
  const isTranscript = name === 'transcript';
  el('tab-transcript').classList.toggle('active', isTranscript);
  el('tab-transcript').setAttribute('aria-selected', String(isTranscript));
  el('tab-preview').classList.toggle('active', !isTranscript);
  el('tab-preview').setAttribute('aria-selected', String(!isTranscript));
  el('panel-transcript').hidden = !isTranscript;
  el('panel-preview').hidden = isTranscript;
}
el('tab-transcript').addEventListener('click', () => showTab('transcript'));
el('tab-preview').addEventListener('click', () => showTab('preview'));

function toast(message: string, error = false): void {
  const element = el('toast');
  clearTimeout(toastTimer);
  element.hidden = !message;
  element.classList.toggle('toast-error', error);
  element.setAttribute('role', error ? 'alert' : 'status');
  text('toast', message);
  if (message) toastTimer = setTimeout(() => { el('toast').hidden = true; }, 6000);
}

/* ---------- state ---------- */

function busy(): boolean {
  return pending || awaitingStart || isActive(snapshot?.job);
}

function exportable(): boolean {
  return Boolean(transcript && transcript.text.trim());
}

// While a start is in flight the snapshot still carries the previous job, so
// the head must ignore it rather than label the new file with an old outcome.
function jobForHead(): Snapshot['job'] {
  if (!headFromJob || awaitingStart) return null;
  return snapshot?.job ?? null;
}

function updateControls(): void {
  const locked = busy();
  const unavailable = !bridge || !snapshot?.ready || disconnected;
  el<HTMLButtonElement>('choose-file').disabled = locked || unavailable;
  el('dropzone').classList.toggle('disabled', locked || unavailable);
  const running = awaitingStart || isActive(snapshot?.job);
  el('cancel').hidden = !running;
  el<HTMLButtonElement>('cancel').disabled = pending || cancelRequested || disconnected;
  el<HTMLButtonElement>('export').disabled = locked || disconnected || !exportable();
  for (const button of el('history-list').querySelectorAll('button')) button.disabled = locked || disconnected;
  const installing = snapshot?.runtimeState === 'downloading' || snapshot?.runtimeState === 'installing';
  el<HTMLButtonElement>('install-runtime').disabled = pending || installing || disconnected;
  text('cancel', cancelRequested ? 'Cancelling…' : 'Cancel');
}

async function action(work: () => Promise<void>): Promise<void> {
  if (pending) return;
  pending = true;
  updateControls();
  try {
    await work();
  } catch (error) {
    toast(errorText(error), true);
  } finally {
    pending = false;
    updateControls();
  }
}

/* ---------- reader ---------- */

// The local ffmpeg build is audio-only, so the webview decodes the poster
// frame itself from the file the user chose. Anything it cannot decode (MKV,
// AVI, audio-only) simply keeps the placeholder art.
function grabFrame(path: string, durationMs: number): Promise<string> {
  return new Promise(resolve => {
    const video = document.createElement('video');
    let settled = false;
    const finish = (data: string) => {
      if (settled) return;
      settled = true;
      clearTimeout(timer);
      video.removeAttribute('src');
      video.load();
      resolve(data);
    };
    const draw = () => {
      if (settled) return;
      if (!video.videoWidth) return finish('');
      const width = 320;
      const canvas = document.createElement('canvas');
      canvas.width = width;
      canvas.height = Math.max(1, Math.round((video.videoHeight / video.videoWidth) * width));
      const context = canvas.getContext('2d');
      if (!context) return finish('');
      try {
        context.drawImage(video, 0, 0, canvas.width, canvas.height);
        finish(canvas.toDataURL('image/jpeg', 0.82));
      } catch {
        finish('');
      }
    };
    const timer = setTimeout(draw, 8000);
    video.muted = true;
    video.preload = 'auto';
    video.addEventListener('error', () => finish(''));
    video.addEventListener('loadeddata', () => {
      // A tenth of the way in usually clears title cards and fades to black.
      const target = Math.min(Math.max(durationMs, 0) / 1000 * 0.1, 10);
      if (target > 0.05 && Number.isFinite(video.duration) && video.duration > target + 0.05) {
        video.addEventListener('seeked', draw, { once: true });
        video.currentTime = target;
        return;
      }
      draw();
    }, { once: true });
    video.src = `/media?path=${encodeURIComponent(path)}`;
  });
}

function setPoster(path: string, durationMs: number): void {
  const element = el('poster');
  const cached = path ? posters.get(path) : undefined;
  if (cached) {
    element.replaceChildren(Object.assign(new Image(), { src: cached, alt: '' }));
    return;
  }
  element.innerHTML = mediaIcon;
  if (!path || posters.has(path)) return;
  posters.set(path, '');
  void grabFrame(path, durationMs).then(data => {
    if (!data) return;
    posters.set(path, data);
    if (head?.path === path) setPoster(path, durationMs);
  });
}

function setHead(next: Head, fromJob: boolean): void {
  head = next;
  headFromJob = fromJob;
  el('reader-head').hidden = false;
  el('empty-state').hidden = true;
  text('reader-title', next.name);
  el('reader-title').title = next.path;
  const parts = [clock(next.durationMs), resolution(next.height), next.size > 0 ? fileSize(next.size) : ''].filter(Boolean);
  text('reader-meta', parts.join(' • '));
  setPoster(next.path, next.durationMs);
  renderStatus();
}

function renderStatus(): void {
  const job = jobForHead();
  const state = job ? job.state : headFromJob ? 'preparing' : transcript ? 'completed' : '';
  const label: Record<string, string> = {
    preparing: 'Preparing audio', transcribing: 'Transcribing locally', completed: 'Transcription complete',
    cancelled: 'Transcription cancelled', failed: 'Transcription failed',
  };
  const tone: Record<string, string> = { preparing: 'busy', transcribing: 'busy', completed: 'ok', cancelled: 'warn', failed: 'bad' };
  const icon: Record<string, string> = { preparing: spinnerIcon, transcribing: spinnerIcon, completed: checkIcon, cancelled: alertIcon, failed: alertIcon };
  el('reader-status').hidden = !state;
  if (!state) return;
  el('reader-status').className = `reader-status tone-${tone[state]}`;
  el('reader-status-icon').innerHTML = icon[state];
  text('reader-status-text', label[state]);
}

function renderSegments(): void {
  if (!transcript) return;
  const query = el<HTMLInputElement>('search').value;
  const segments = matchingSegments(transcript, query);
  const fragment = document.createDocumentFragment();
  for (const segment of segments) {
    const li = document.createElement('li');
    const stamp = document.createElement('span');
    stamp.className = 'timecode';
    stamp.textContent = clock(segment.startMs);
    stamp.setAttribute('aria-label', `From ${clock(segment.startMs)} to ${clock(segment.endMs)}`);
    const paragraph = document.createElement('p');
    paragraph.textContent = segment.text.trim();
    li.append(stamp, paragraph);
    fragment.append(li);
  }
  el('segments').replaceChildren(fragment);
  text('search-count', query.trim() ? `${segments.length} matching ${segments.length === 1 ? 'segment' : 'segments'}` : `${segments.length} ${segments.length === 1 ? 'segment' : 'segments'}`);
  const silent = 'No speech was detected. Check that the recording contains audible speech, then try another file or choose its spoken language.';
  el('transcript-empty').hidden = segments.length > 0;
  text('transcript-empty', transcript.text.trim() ? 'No matching segments. Try another word or clear your search.' : silent);
  text('preview-text', transcript.text.trim() || silent);
}

function headFromSummary(summary: Summary): Head {
  return {
    name: summary.fileName, path: summary.sourcePath || '',
    durationMs: summary.durationMs, size: summary.size || 0, height: summary.height || 0,
  };
}

async function loadTranscript(id: string, fromHistory = false): Promise<void> {
  if (!bridge) return;
  const result = await bridge.GetTranscript(id);
  transcript = result;
  if (fromHistory || !head) setHead(headFromSummary(result), fromHistory ? false : headFromJob);
  el('empty-state').hidden = true;
  el('tabs').hidden = false;
  el('export').hidden = false;
  el<HTMLInputElement>('search').value = '';
  closeSearch();
  showTab('transcript');
  renderSegments();
  renderStatus();
  renderHistory();
  updateControls();
  if (fromHistory) {
    el('reader-title').tabIndex = -1;
    el('reader-title').focus();
    el('reader').scrollTop = 0;
  }
}

function clearResult(): void {
  transcript = null;
  el('tabs').hidden = true;
  el('panel-transcript').hidden = true;
  el('panel-preview').hidden = true;
  el('export').hidden = true;
  closeMenu();
}

/* ---------- file intake ---------- */

async function begin(file: FileInfo): Promise<void> {
  if (!bridge) return;
  clearResult();
  setHead({ name: file.name, path: file.path, durationMs: file.durationMs, size: file.size, height: file.height || 0 }, true);
  awaitingStart = true;
  startPreviousJobID = snapshot?.job?.id || '';
  cancelRequested = false;
  loadedCompletion = '';
  updateControls();
  try {
    await bridge.StartTranscription(file.path, el<HTMLSelectElement>('language').value);
  } catch (error) {
    awaitingStart = false;
    throw error;
  }
}

async function openPath(path: string): Promise<void> {
  if (busy() || !bridge || !snapshot?.ready || disconnected) return;
  const cleaned = path.trim();
  if (!cleaned) return;
  await action(async () => begin(await bridge.InspectFile(cleaned)));
}

el('choose-file').addEventListener('click', () => {
  if (busy() || !bridge) return;
  void action(async () => {
    const file = await bridge.ChooseFile();
    if (file) await begin(file);
  });
});
el('dropzone').addEventListener('click', event => {
  if (event.target === el('choose-file') || (event.target as HTMLElement).closest('#choose-file')) return;
  el('choose-file').click();
});

el('install-runtime').addEventListener('click', () => {
  if (!bridge || busy() || disconnected) return;
  void action(async () => {
    await bridge.InstallRuntime();
    toast('The local transcription engine is ready.');
  });
});

el('cancel').addEventListener('click', () => {
  if (!bridge || !isActive(snapshot?.job) || cancelRequested || pending) return;
  void action(async () => {
    await bridge.Cancel();
    cancelRequested = true;
  });
});

/* ---------- download menu ---------- */

function closeMenu(): void {
  el('export-menu').hidden = true;
  el('export').setAttribute('aria-expanded', 'false');
}

el('export').addEventListener('click', event => {
  event.stopPropagation();
  if (!exportable() || busy()) return;
  const open = el('export-menu').hidden;
  el('export-menu').hidden = !open;
  el('export').setAttribute('aria-expanded', String(open));
});

for (const item of el('export-menu').querySelectorAll<HTMLButtonElement>('[data-export]')) {
  item.addEventListener('click', () => {
    closeMenu();
    if (!bridge || !transcript || busy()) return;
    const id = transcript.id;
    const kind = item.dataset.export!;
    void action(async () => {
      if (kind === 'copy') {
        await bridge.CopyTranscript(id);
        toast('Transcript copied to the clipboard.');
        return;
      }
      const path = await bridge.ExportTranscript(id, kind);
      toast(path ? `Saved to ${path}` : 'Export cancelled. No file was saved.');
    });
  });
}

document.addEventListener('click', closeMenu);
document.addEventListener('keydown', event => {
  if (event.key === 'Escape') { closeMenu(); closeSearch(); }
  if (event.key === 'f' && (event.ctrlKey || event.metaKey) && !el('tabs').hidden) {
    event.preventDefault();
    openSearch();
  }
});

/* ---------- find in transcript ---------- */

function openSearch(): void {
  const input = el<HTMLInputElement>('search');
  input.hidden = false;
  el('search-toggle').setAttribute('aria-expanded', 'true');
  input.focus();
}

function closeSearch(): void {
  const input = el<HTMLInputElement>('search');
  if (input.hidden) return;
  input.hidden = true;
  input.value = '';
  el('search-toggle').setAttribute('aria-expanded', 'false');
  if (transcript) renderSegments();
}

el('search-toggle').addEventListener('click', () => {
  if (el('search').hidden) openSearch();
  else closeSearch();
});
el('search').addEventListener('input', renderSegments);

/* ---------- history ---------- */

function renderHistory(): void {
  const history = snapshot?.history || [];
  const warning = snapshot?.historyWarning || '';
  el('history-warning').hidden = !warning;
  text('history-warning', warning);
  const signature = JSON.stringify(history);
  if (signature !== historySignature) {
    historySignature = signature;
    const fragment = document.createDocumentFragment();
    for (const item of history) {
      const li = document.createElement('li');
      const button = document.createElement('button');
      button.type = 'button';
      button.className = 'history-item';
      button.dataset.id = item.id;
      const name = document.createElement('strong');
      name.textContent = item.fileName;
      const detail = document.createElement('span');
      const date = new Date(item.createdAt);
      detail.textContent = `${Number.isNaN(date.getTime()) ? 'Saved' : date.toLocaleDateString(undefined, { month: 'short', day: 'numeric' })} • ${item.wordCount.toLocaleString()} words`;
      button.append(name, detail);
      button.addEventListener('click', () => {
        if (busy()) return;
        void action(() => loadTranscript(item.id, true));
      });
      li.append(button);
      fragment.append(li);
    }
    el('history-list').replaceChildren(fragment);
    el('recent-empty').hidden = history.length > 0;
    el('history-count').hidden = history.length === 0;
    text('history-count', String(history.length));
  }
  for (const button of el('history-list').querySelectorAll('button')) {
    button.classList.toggle('current', button.dataset.id === transcript?.id);
  }
}

/* ---------- polling ---------- */

function renderJob(): void {
  const job = jobForHead();
  const working = headFromJob && (awaitingStart || isActive(job));
  el('progress-line').hidden = !working;
  el('job-error').hidden = !job?.error;
  text('job-error', job?.error || '');
  if (working) {
    const progress = el<HTMLProgressElement>('progress');
    if (!job || job.state === 'preparing') {
      progress.removeAttribute('value');
      text('progress-label', 'Preparing');
    } else {
      const percentage = progressPercent(job.progress);
      progress.value = percentage;
      text('progress-label', `${percentage}%`);
    }
  }
  if (cancelRequested && snapshot?.job && !isActive(snapshot.job)) cancelRequested = false;
  renderStatus();
  if (!head) el('empty-state').hidden = Boolean(transcript);
}

function renderRuntime(): void {
  if (!snapshot) return;
  const state = snapshot.runtimeState || (snapshot.ready ? 'ready' : 'checking');
  const active = state === 'downloading' || state === 'installing';
  const showSetup = !snapshot.ready && !snapshot.setupError && state !== 'checking';
  el('runtime-setup').hidden = !showSetup;
  el('dropzone').hidden = showSetup;
  el('supports').hidden = showSetup;
  el('language-row').hidden = showSetup;
  el('install-runtime').hidden = active;
  text('install-runtime-label', state === 'failed' ? 'Retry download' : 'Download and install');
  const installedSize = snapshot.runtimeTotalBytes > 0 ? fileSize(snapshot.runtimeTotalBytes) : '';
  text('runtime-size', installedSize ? `Uses about ${installedSize} after installation.` : '');
  const progressLine = el('runtime-progress-line');
  progressLine.hidden = !active;
  if (active) {
    const progress = el<HTMLProgressElement>('runtime-progress');
    if (state === 'installing' || snapshot.runtimeProgress <= 0) {
      progress.removeAttribute('value');
    } else {
      progress.value = progressPercent(snapshot.runtimeProgress);
    }
    const downloaded = snapshot.runtimeDownloadedBytes > 0 ? fileSize(snapshot.runtimeDownloadedBytes) : '';
    const total = snapshot.runtimeDownloadTotalBytes > 0 ? fileSize(snapshot.runtimeDownloadTotalBytes) : '';
    text('runtime-progress-label', state === 'installing' ? 'Installing' : downloaded && total ? `${downloaded} of ${total}` : 'Downloading');
  }
  el('runtime-error').hidden = !snapshot.runtimeError;
  text('runtime-error', snapshot.runtimeError || '');
}

async function poll(): Promise<void> {
  if (stopped || !bridge) return;
  try {
    const next = await bridge.Status();
    if (stopped) return;
    disconnected = false;
    snapshot = next;
    if (awaitingStart && next.job && next.job.id !== startPreviousJobID) awaitingStart = false;
    const engineLabel: Record<string, string> = {
      checking: 'Checking engine',
      required: 'Download required',
      downloading: `Downloading ${progressPercent(next.runtimeProgress || 0)}%`,
      installing: 'Installing engine',
      failed: 'Setup failed',
      ready: 'Running offline',
    };
    text('engine-label', next.setupError ? 'Engine unavailable' : engineLabel[next.runtimeState] || 'Checking engine');
    text('version', `v${next.version || '0.2.0'}`);
    text('model', next.modelName || 'Whisper base multilingual');
    const serviceError = next.setupError || (next.ready ? next.runtimeError : '');
    el('service-error').hidden = !serviceError;
    text('service-error', serviceError);
    renderRuntime();
    renderJob();
    renderHistory();
    updateControls();
    const job = next.job;
    if (job?.state === 'completed' && job.transcriptID && loadedCompletion !== job.id && !awaitingStart && !pending) {
      pending = true;
      updateControls();
      try {
        await loadTranscript(job.transcriptID);
        loadedCompletion = job.id;
      } catch (error) {
        toast(`Could not open the completed transcript: ${errorText(error)} Open it from Recent to retry.`, true);
        loadedCompletion = job.id;
      } finally {
        pending = false;
        updateControls();
      }
    }
  } catch (error) {
    disconnected = true;
    el('service-error').hidden = false;
    text('engine-label', 'Connection interrupted');
    text('service-error', `Cannot reach the local engine. Retrying automatically. ${errorText(error)}`);
    updateControls();
  } finally {
    if (!stopped) timer = setTimeout(() => void poll(), 800);
  }
}

/* ---------- preferences ---------- */

function readStored(key: string): string {
  try { return localStorage.getItem(key) || ''; } catch { return ''; }
}
function writeStored(key: string, value: string): void {
  try { localStorage.setItem(key, value); } catch { /* Preferences are optional. */ }
}

function updateTheme(): void {
  const dark = document.documentElement.dataset.theme === 'dark';
  const label = `Switch to ${dark ? 'light' : 'dark'} theme`;
  const button = el('theme');
  button.setAttribute('aria-label', label);
  button.setAttribute('title', label);
  button.innerHTML = dark ? sunIcon : moonIcon;
}
const storedTheme = readStored('transcribe-me-theme');
if (storedTheme === 'light' || storedTheme === 'dark') document.documentElement.dataset.theme = storedTheme;
updateTheme();
el('theme').addEventListener('click', () => {
  const next = document.documentElement.dataset.theme === 'dark' ? 'light' : 'dark';
  document.documentElement.dataset.theme = next;
  writeStored('transcribe-me-theme', next);
  updateTheme();
});

const storedLanguage = readStored('transcribe-me-language');
const language = el<HTMLSelectElement>('language');
if (storedLanguage && [...language.options].some(option => option.value === storedLanguage)) language.value = storedLanguage;
language.addEventListener('change', () => writeStored('transcribe-me-language', language.value));

/* ---------- boot ---------- */

if (!bridge) {
  el('window').hidden = true;
  el('browser-notice').hidden = false;
} else {
  el('window-buttons').hidden = false;
  window.runtime?.OnFileDrop?.((_x, _y, paths) => {
    if (busy()) return;
    if (paths.length !== 1) {
      toast('Drop one video or audio file at a time.', true);
      return;
    }
    void openPath(paths[0]);
  }, false);  updateControls();
  void poll();
}

window.addEventListener('beforeunload', () => {
  stopped = true;
  clearTimeout(timer);
  window.runtime?.OnFileDropOff?.();
});
