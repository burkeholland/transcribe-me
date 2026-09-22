These 17 small regression fixtures contain an original generated 440 Hz tone.
video-aac.mp4 also contains a generated 32x32 solid black video track.
Each fixture is one second long, sampled at 48 kHz before encoding.
They contain no user recording or third-party creative work.

Generated with FFmpeg's lavfi sine source:
  -f lavfi -i sine=frequency=440:sample_rate=48000:duration=1
The audio encoder matches the filename: AAC, MP2, MP3 (libmp3lame),
PCM signed 16-bit little-endian, WMA v2, Vorbis (libvorbis), Opus
(libopus), or FLAC. Filename extensions select the container.
The video fixture uses the MPEG-4 Part 2 video encoder and AAC audio.

The fixture encoder is a development-only tool, not a shipped dependency.
test-native.ps1 exercises the shipped decoder, first-audio-track selection,
mono resampling to 16 kHz, and PCM WAV output on every fixture.
These fixtures are distributed under TranscribeMe's MIT license.
