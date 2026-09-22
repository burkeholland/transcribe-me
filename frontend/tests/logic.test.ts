import { test } from 'node:test';
import assert from 'node:assert/strict';
import { clock, errorText, fileSize, isActive, matchingSegments, progressPercent, resolution, timecode } from '../src/format.ts';
import type { Job, Transcript } from '../src/types.ts';

test('timecodes preserve hours and sanitize invalid durations', () => {
  assert.equal(timecode(3_723_456), '01:02:03');
  assert.equal(timecode(100 * 3_600_000), '100:00:00');
  for (const value of [-1, NaN, Infinity]) assert.equal(timecode(value), '00:00:00');
});

test('reading clocks drop the hour until a recording needs one', () => {
  assert.equal(clock(0), '00:00');
  assert.equal(clock(6_000), '00:06');
  assert.equal(clock(754_000), '12:34');
  assert.equal(clock(3_599_000), '59:59');
  assert.equal(clock(3_723_456), '1:02:03');
  assert.equal(clock(100 * 3_600_000), '100:00:00');
  for (const value of [-1, NaN, Infinity]) assert.equal(clock(value), '00:00');
});

test('resolutions read the way people describe video', () => {
  assert.equal(resolution(1080), '1080p');
  assert.equal(resolution(720), '720p');
  assert.equal(resolution(2160), '4K');
  for (const value of [0, -4, NaN, undefined]) assert.equal(resolution(value), '');
});

test('file sizes cover typical local recordings', () => {
  assert.equal(fileSize(0), '0 B');
  assert.equal(fileSize(1024), '1.0 KB');
  assert.equal(fileSize(1024 ** 2 * 4.5), '4.5 MB');
  assert.equal(fileSize(1024 ** 3 * 2), '2.0 GB');
  assert.equal(fileSize(NaN), '0 B');
});

test('only live job states lock the workflow', () => {
  assert.equal(isActive(null), false);
  for (const state of ['preparing', 'transcribing', 'completed', 'cancelled', 'failed'] as Job['state'][]) {
    assert.equal(isActive({ state } as Job), ['preparing', 'transcribing'].includes(state));
  }
});

test('progress is bounded and error strings retain useful native errors', () => {
  assert.equal(progressPercent(42.7), 43);
  assert.equal(progressPercent(-10), 0);
  assert.equal(progressPercent(400), 100);
  assert.equal(progressPercent(NaN), 0);
  assert.equal(errorText(new Error('File not found')), 'File not found');
  assert.equal(errorText('Permission denied'), 'Permission denied');
  assert.match(errorText(null), /Try again/);
});

const transcript = {
  text: 'Hello world. A second thought.',
  durationMs: 10_000,
  segments: [
    { startMs: 0, endMs: 2_000, text: 'Hello world.' },
    { startMs: 2_000, endMs: 10_000, text: 'A second thought.' },
  ],
} as Transcript;

test('search is case-insensitive, literal, and ignores surrounding whitespace', () => {
  assert.equal(matchingSegments(transcript, ' WORLD ')[0].text, 'Hello world.');
  assert.equal(matchingSegments(transcript, '').length, 2);
  assert.equal(matchingSegments(transcript, '.*').length, 0);
  assert.equal(matchingSegments(transcript, 'not present').length, 0);
});

test('missing timestamps retain text and silent recordings stay empty', () => {
  assert.deepEqual(matchingSegments({ ...transcript, segments: [] }, ''), [
    { startMs: 0, endMs: 10_000, text: transcript.text },
  ]);
  assert.deepEqual(matchingSegments({ ...transcript, segments: [], text: ' ' }, ''), []);
});
