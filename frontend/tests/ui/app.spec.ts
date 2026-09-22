import { test, expect, type Page } from '@playwright/test';
import { readFileSync } from 'node:fs';
import { fileURLToPath } from 'node:url';
import type { FileInfo, Snapshot, Transcript } from '../../src/types.ts';

// The app decodes its poster frame in the webview, so the fixture is a real
// video rather than an image the native layer hands over.
const sampleVideo = readFileSync(fileURLToPath(new URL('../fixtures/sample.webm', import.meta.url)));

async function servePoster(page: Page) {
  await page.route('**/media?*', route => route.fulfill({ contentType: 'video/webm', body: sampleVideo }));
}

async function desktop(page: Page, scenario = 'normal') {
  await page.addInitScript(scenario => {
    const info: FileInfo = { name: 'Recording.mp4', path: 'C:\\Videos\\Recording.mp4', size: 10_485_760, durationMs: 125_000, height: 1080 };
    const transcript: Transcript = {
      id: 'saved-1', fileName: info.name, createdAt: '2026-09-21T12:00:00Z', language: 'en', durationMs: info.durationMs, wordCount: 12,
      text: 'Here is a clear thought. Keep the original recording for comparison.',
      segments: [
        { startMs: 0, endMs: 6000, text: 'Here is a clear thought.' },
        { startMs: 6000, endMs: 14000, text: 'Keep the original recording for comparison.' },
      ],
    };
    if (scenario === 'silent') { transcript.text = ''; transcript.wordCount = 0; transcript.segments = []; }
    if (scenario === 'unsafe-text') {
      transcript.fileName = '<img src=x onerror="window.attacked=true">.mp4';
      transcript.text = '<script>window.attacked=true</script>';
      transcript.segments[0].text = transcript.text;
    }
    if (scenario === 'long') {
      transcript.segments = Array.from({ length: 100 }, (_, index) => ({ startMs: index * 6000, endMs: index * 6000 + 5000, text: `Line ${index}: Keep the original recording for comparison.` }));
    }
    const snapshot: Snapshot = {
      ready: scenario !== 'setup-error' && scenario !== 'checking',
      setupError: scenario === 'setup-error' ? 'Bundled model not found. Extract the full portable folder.' : '',
      modelName: 'Whisper base multilingual', version: '0.1.0', job: null, history: [transcript],
      historyWarning: scenario === 'history-warning' ? 'Some older transcripts could not be read. Healthy transcripts are still available.' : '',
    };
    let statusInFlight = 0;
    let maximumStatusInFlight = 0;
    const calls: string[] = [];
    let drop: ((x: number, y: number, paths: string[]) => void) | undefined;
    const controls = {
      calls,
      complete: () => { if (snapshot.job) { snapshot.job.state = 'completed'; snapshot.job.progress = 100; snapshot.job.transcriptID = transcript.id; } },
      fail: () => { if (snapshot.job) { snapshot.job.state = 'failed'; snapshot.job.error = 'The audio decoder could not read this recording.'; } },
      finishSetup: () => { snapshot.ready = true; },
      connectionError: false,
      maximumStatus: () => maximumStatusInFlight,
      drop: (paths: string[]) => drop?.(0, 0, paths),
    };
    Object.assign(window, { __test: controls });
    window.runtime = { OnFileDrop: callback => { drop = callback; }, OnFileDropOff: () => { drop = undefined; } };
    window.go = { main: { App: {
      Status: async () => {
        if (controls.connectionError) throw new Error('Native bridge unavailable');
        statusInFlight++;
        maximumStatusInFlight = Math.max(maximumStatusInFlight, statusInFlight);
        await new Promise(resolve => setTimeout(resolve, scenario === 'slow' ? 1000 : 10));
        statusInFlight--;
        return structuredClone(snapshot);
      },
      ChooseFile: async () => scenario === 'cancel-choose' ? null : info,
      InspectFile: async path => {
        calls.push(`inspect:${path}`);
        if (scenario === 'inspect-error') throw new Error('This file does not contain an audio stream.');
        return info;
      },
      StartTranscription: async (path, language) => {
        calls.push(`start:${path}:${language}`);
        snapshot.job = { id: 'job-1', state: 'transcribing', progress: 32, message: 'Processing locally.', fileName: info.name, error: '', transcriptID: '' };
      },
      Cancel: async () => { calls.push('cancel'); if (snapshot.job) snapshot.job.state = 'cancelled'; },
      GetTranscript: async id => { calls.push(`get:${id}`); return structuredClone(transcript); },
      CopyTranscript: async id => { calls.push(`copy:${id}`); },
      ExportTranscript: async (id, format) => {
        calls.push(`export:${id}:${format}`);
        return scenario === 'cancel-export' ? '' : `C:\\Exports\\Recording.${format}`;
      },
    } } };
  }, scenario);
  await page.goto('/');
  if (scenario === 'setup-error') {
    await expect(page.getByRole('alert')).toContainText('Bundled model not found');
  } else if (scenario === 'checking') {
    await expect(page.locator('#history-count')).toHaveText('1');
    await expect(page.locator('#engine-label')).toHaveText('Checking engine');
  } else {
    await expect(page.getByRole('button', { name: 'Choose Video' })).toBeEnabled();
  }
}

async function calls(page: Page): Promise<string[]> {
  return page.evaluate(() => (window as unknown as { __test: { calls: string[] } }).__test.calls);
}

async function control(page: Page, action: 'complete' | 'fail' | 'finishSetup') {
  await page.evaluate(action => (window as unknown as { __test: Record<string, () => void> }).__test[action](), action);
}

async function drop(page: Page, paths: string[]) {
  await page.evaluate(paths => (window as unknown as { __test: { drop: (paths: string[]) => void } }).__test.drop(paths), paths);
}

async function startJob(page: Page) {  await page.getByRole('button', { name: 'Choose Video' }).click();
  await expect(page.getByText('Transcribing locally', { exact: true })).toBeVisible();
}

async function download(page: Page, item: string) {
  await page.getByRole('button', { name: 'Download' }).click();
  await page.getByRole('menuitem', { name: item, exact: true }).click();
}

test('ordinary browser has no simulated desktop functionality', async ({ page }) => {
  await page.goto('/');
  await expect(page.getByRole('heading', { name: 'Desktop app required' })).toBeVisible();
  await expect(page.getByRole('button', { name: 'Choose Video' })).not.toBeVisible();
  await expect(page.locator('script[src^="http"]')).toHaveCount(0);
});

test('choosing a video starts it immediately and every download format works', async ({ page }) => {
  await desktop(page);
  await servePoster(page);
  await page.getByLabel('Spoken language').selectOption('es');
  await page.getByRole('button', { name: 'Choose Video' }).click();

  await expect(page.locator('#reader-title')).toHaveText('Recording.mp4');
  await expect(page.locator('#reader-meta')).toHaveText('02:05 • 1080p • 10.0 MB');
  await expect(page.locator('#poster img')).toHaveAttribute('src', /^data:image\/jpeg/);
  await expect(page.getByText('Transcribing locally', { exact: true })).toBeVisible();
  await expect(page.getByRole('button', { name: 'Choose Video' })).toBeDisabled();
  await expect(page.getByRole('button', { name: /Recording.mp4/ })).toBeDisabled();

  await control(page, 'complete');
  await expect(page.getByText('Transcription complete', { exact: true })).toBeVisible();
  await expect(page.locator('#progress-line')).not.toBeVisible();
  await expect(page.locator('#segments li')).toHaveCount(2);
  await page.getByRole('button', { name: 'Find in transcript' }).click();
  await page.locator('#search').fill('original');
  await expect(page.locator('#segments li')).toHaveCount(1);
  await expect(page.locator('#segments .timecode')).toHaveText('00:06');

  await download(page, 'Copy text');
  await expect(page.locator('#toast')).toHaveText('Transcript copied to the clipboard.');
  for (const format of ['txt', 'srt', 'vtt']) {
    await download(page, `Save as ${format.toUpperCase()}`);
    await expect(page.locator('#toast')).toHaveText(`Saved to C:\\Exports\\Recording.${format}`);
  }

  const recorded = await calls(page);
  expect(recorded).toContain('start:C:\\Videos\\Recording.mp4:es');
  expect(recorded).toContain('copy:saved-1');
  for (const format of ['txt', 'srt', 'vtt']) expect(recorded).toContain(`export:saved-1:${format}`);
});

test('a file without a usable frame falls back to the placeholder art', async ({ page }) => {
  await desktop(page);
  await page.route('**/media?*', route => route.fulfill({ status: 404, body: '' }));
  await startJob(page);
  await expect(page.locator('#poster svg')).toBeVisible();
  await expect(page.locator('#poster img')).toHaveCount(0);
});

test('inspection errors are visible and never start a job', async ({ page }) => {
  await desktop(page, 'inspect-error');
  await drop(page, ['C:\\Videos\\silent.mp4']);
  await expect(page.getByRole('alert')).toHaveText('This file does not contain an audio stream.');
  await expect(page.locator('#reader-head')).not.toBeVisible();
  expect(await calls(page)).not.toContain('start:C:\\Videos\\silent.mp4:auto');
});

test('setup failure blocks new work but still allows saved transcript review', async ({ page }) => {
  await desktop(page, 'setup-error');
  await expect(page.getByRole('button', { name: 'Choose Video' })).toBeDisabled();
  await page.locator('.history-item').click();
  await expect(page.locator('#reader-title')).toHaveText('Recording.mp4');
  await expect(page.getByText('Transcription complete', { exact: true })).toBeVisible();
});

test('history corruption warning persists separately without blocking healthy transcripts', async ({ page }) => {
  await desktop(page, 'history-warning');
  const warning = page.locator('#history-warning');
  await expect(warning).toHaveText('Some older transcripts could not be read. Healthy transcripts are still available.');
  await page.locator('.history-item').click();
  await expect(page.locator('#reader-title')).toHaveText('Recording.mp4');
  await download(page, 'Copy text');
  await expect(page.locator('#toast')).toHaveText('Transcript copied to the clipboard.');
  await page.waitForTimeout(1700);
  await expect(warning).toBeVisible();
  await expect(page.getByRole('button', { name: 'Choose Video' })).toBeEnabled();
});

test('initial model verification is neutral and automatically becomes ready', async ({ page }) => {
  await desktop(page, 'checking');
  await expect(page.getByRole('button', { name: 'Choose Video' })).toBeDisabled();
  await expect(page.locator('#service-error')).not.toBeVisible();
  await control(page, 'finishSetup');
  await expect(page.getByRole('button', { name: 'Choose Video' })).toBeEnabled();
  await expect(page.locator('#engine-label')).toHaveText('Running offline');
});

test('all additional backend-supported language codes are selectable and remembered', async ({ page }) => {
  await desktop(page);
  const language = page.getByLabel('Spoken language');
  for (const code of ['vi', 'id', 'sv', 'da', 'no', 'fi', 'el', 'he', 'cs', 'ro', 'hu', 'th']) {
    await language.selectOption(code);
    await expect(language).toHaveValue(code);
  }
  await language.selectOption('vi');
  await page.reload();
  await expect(page.getByLabel('Spoken language')).toHaveValue('vi');
  await startJob(page);
  expect(await calls(page)).toContain('start:C:\\Videos\\Recording.mp4:vi');
});

test('cancel releases the workflow and failed jobs explain the problem', async ({ page }) => {
  await desktop(page);
  await startJob(page);
  await page.getByRole('button', { name: 'Cancel', exact: true }).click();
  await expect(page.getByText('Transcription cancelled', { exact: true })).toBeVisible();
  await expect(page.getByRole('button', { name: 'Choose Video' })).toBeEnabled();
  await page.reload();
  await startJob(page);
  await control(page, 'fail');
  await expect(page.getByText('Transcription failed', { exact: true })).toBeVisible();
  await expect(page.getByRole('alert')).toHaveText('The audio decoder could not read this recording.');
  await expect(page.getByRole('button', { name: 'Choose Video' })).toBeEnabled();
});

test('history handles literal HTML safely and preserves find focus during polling', async ({ page }) => {
  await desktop(page, 'unsafe-text');
  await page.locator('.history-item').click();
  await expect(page.locator('#reader-title')).toContainText('<img src=x');
  await expect(page.locator('#segments')).toContainText('<script>window.attacked=true</script>');
  await expect(page.locator('#reader-title img')).toHaveCount(0);
  expect(await page.evaluate(() => (window as unknown as { attacked?: boolean }).attacked)).toBeUndefined();
  await page.getByRole('button', { name: 'Find in transcript' }).click();
  await page.locator('#search').fill('script');
  await expect(page.locator('#search')).toBeFocused();
  await page.waitForTimeout(1700);
  await expect(page.locator('#search')).toHaveValue('script');
  await expect(page.locator('#search')).toBeFocused();
});

test('silent audio has a helpful state without empty export actions', async ({ page }) => {
  await desktop(page, 'silent');
  await page.locator('.history-item').click();
  await expect(page.locator('#transcript-empty')).toContainText('No speech was detected');
  await expect(page.getByRole('button', { name: 'Download' })).toBeDisabled();
  await page.getByRole('tab', { name: 'Preview' }).click();
  await expect(page.locator('#preview-text')).toContainText('No speech was detected');
});

test('no search matches can be cleared without losing the transcript', async ({ page }) => {
  await desktop(page);
  await page.locator('.history-item').click();
  await page.keyboard.press('Control+f');
  await expect(page.locator('#search')).toBeFocused();
  await page.locator('#search').fill('nonexistent phrase');
  await expect(page.getByText('No matching segments. Try another word or clear your search.')).toBeVisible();
  await page.keyboard.press('Escape');
  await expect(page.locator('#search')).toBeHidden();
  await expect(page.locator('#segments li')).toHaveCount(2);
});

test('native export cancellation is not claimed as a saved file', async ({ page }) => {
  await desktop(page, 'cancel-export');
  await page.locator('.history-item').click();
  await download(page, 'Save as TXT');
  await expect(page.locator('#toast')).toHaveText('Export cancelled. No file was saved.');
});

test('native chooser cancellation leaves the reader empty', async ({ page }) => {
  await desktop(page, 'cancel-choose');
  await page.getByRole('button', { name: 'Choose Video' }).click();
  await expect(page.locator('#reader-head')).not.toBeVisible();
  await expect(page.locator('#empty-state')).toBeVisible();
});

test('real runtime drop hook starts a single native path', async ({ page }) => {
  await desktop(page);
  await drop(page, ['C:\\Videos\\Recording.mp4']);
  await expect(page.locator('#reader-title')).toHaveText('Recording.mp4');
  await expect(page.getByText('Transcribing locally', { exact: true })).toBeVisible();
});

test('dropping more than one file is refused', async ({ page }) => {
  await desktop(page);
  await drop(page, ['a.mp4', 'b.mp4']);
  await expect(page.getByRole('alert')).toHaveText('Drop one video or audio file at a time.');
  await expect(page.locator('#reader-head')).not.toBeVisible();
});

test('status polling is serial even when the backend takes longer than 800ms', async ({ page }) => {
  await desktop(page, 'slow');
  await page.waitForTimeout(2900);
  const maximum = await page.evaluate(() => (window as unknown as { __test: { maximumStatus: () => number } }).__test.maximumStatus());
  expect(maximum).toBe(1);
});

test('lost native connection is visible and recovers without reloading', async ({ page }) => {
  await desktop(page);
  await page.evaluate(() => { (window as unknown as { __test: { connectionError: boolean } }).__test.connectionError = true; });
  await expect(page.getByRole('alert')).toContainText('Cannot reach the local engine');
  await expect(page.getByRole('button', { name: 'Choose Video' })).toBeDisabled();
  await page.evaluate(() => { (window as unknown as { __test: { connectionError: boolean } }).__test.connectionError = false; });
  await expect(page.getByRole('button', { name: 'Choose Video' })).toBeEnabled();
  await expect(page.locator('#service-error')).not.toBeVisible();
});

test('reader scroll is preserved, 800px fits, and theme choice persists', async ({ page }) => {
  await page.setViewportSize({ width: 800, height: 600 });
  await desktop(page, 'long');
  await page.locator('.history-item').click();
  await page.screenshot({ path: test.info().outputPath('desktop-800-light.png') });
  await page.locator('#reader').evaluate(element => { element.scrollTop = 400; });
  await page.waitForTimeout(1700);
  expect(await page.locator('#reader').evaluate(element => element.scrollTop)).toBe(400);
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
  await page.getByRole('button', { name: 'Switch to dark theme' }).click();
  await expect(page.locator('html')).toHaveAttribute('data-theme', 'dark');
  await page.reload();
  await expect(page.locator('html')).toHaveAttribute('data-theme', 'dark');
  await expect(page.getByRole('button', { name: 'Switch to light theme' })).toBeVisible();
  await page.locator('.history-item').click();
  await page.screenshot({ path: test.info().outputPath('desktop-800-dark.png') });
});
