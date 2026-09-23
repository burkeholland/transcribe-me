import { readFile } from 'node:fs/promises';
import { test, expect } from '@playwright/test';

const landing = new URL('../../../docs/index.html', import.meta.url);

for (const viewport of [{ width: 1280, height: 900 }, { width: 390, height: 844 }]) {
  test(`landing works at ${viewport.width}px without remote resources`, async ({ page }) => {
    await page.setViewportSize(viewport);
    const requests: string[] = [];
    page.on('request', request => requests.push(request.url()));
    await page.setContent(await readFile(landing, 'utf8'));
    await expect(page.getByRole('heading', { level: 1 })).toContainText('Transcribe video');
    await expect(page.getByRole('heading', { level: 1 })).toContainText('audio.');
    await expect(page.getByRole('link', { name: 'GitHub', exact: true })).toHaveAttribute('href', 'https://github.com/burkeholland/transcribe-me');
    await expect(page.getByRole('link', { name: 'Download for Windows' })).toHaveAttribute('href', 'https://github.com/burkeholland/transcribe-me/releases/download/v0.1.0/TranscribeMe-0.1.0-windows-x64.zip');
    await expect(page.getByRole('link', { name: 'SHA-256 checksums' })).toHaveAttribute('href', 'https://github.com/burkeholland/transcribe-me/releases/download/v0.1.0/SHA256SUMS.txt');
    await expect(page.locator('footer a')).toHaveCount(0);
    await expect(page.getByText('Transcription complete', { exact: true })).toBeVisible();
    await expect(page.getByText('Spoken language', { exact: true })).toBeVisible();
    await expect(page.getByText(/This build is unsigned/)).toBeVisible();
    await page.getByRole('button', { name: 'Switch to dark theme' }).click();
    await expect(page.locator('html')).toHaveAttribute('data-theme', 'dark');
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
    expect(requests).toEqual([]);
  });
}
