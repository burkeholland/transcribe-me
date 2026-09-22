import type { Job, Transcript } from './types.ts';

export function timecode(milliseconds: number): string {
  const seconds = Math.floor(Math.max(0, Number.isFinite(milliseconds) ? milliseconds : 0) / 1000);
  const hours = Math.floor(seconds / 3600);
  const minutes = Math.floor((seconds % 3600) / 60);
  return [hours, minutes, seconds % 60].map(value => String(value).padStart(2, '0')).join(':');
}

// Reading format: hours only appear when the recording is actually that long.
export function clock(milliseconds: number): string {
  const [hours, ...rest] = timecode(milliseconds).split(':');
  return hours === '00' ? rest.join(':') : `${Number(hours)}:${rest.join(':')}`;
}

export function resolution(height: number | undefined): string {
  if (!height || !Number.isFinite(height) || height <= 0) return '';
  if (height >= 2000) return '4K';
  return `${Math.round(height)}p`;
}

export function fileSize(bytes: number): string {
  const safe = Number.isFinite(bytes) ? Math.max(0, bytes) : 0;
  if (safe < 1024) return `${safe} B`;
  if (safe < 1024 ** 2) return `${(safe / 1024).toFixed(1)} KB`;
  if (safe < 1024 ** 3) return `${(safe / 1024 ** 2).toFixed(1)} MB`;
  return `${(safe / 1024 ** 3).toFixed(1)} GB`;
}

export function isActive(job: Job | null | undefined): boolean {
  return job?.state === 'preparing' || job?.state === 'transcribing';
}

export function progressPercent(value: number): number {
  return Number.isFinite(value) ? Math.max(0, Math.min(100, Math.round(value))) : 0;
}

export function errorText(error: unknown): string {
  if (error instanceof Error) return error.message;
  if (typeof error === 'string') return error;
  return 'Something went wrong. Try again, or restart the app.';
}

export function matchingSegments(transcript: Transcript, query: string): Transcript['segments'] {
  const segments = transcript.segments?.length
    ? transcript.segments
    : transcript.text.trim() ? [{ startMs: 0, endMs: transcript.durationMs, text: transcript.text }] : [];
  const normalized = query.trim().toLocaleLowerCase();
  return normalized ? segments.filter(segment => segment.text.toLocaleLowerCase().includes(normalized)) : segments;
}
