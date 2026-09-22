#!/usr/bin/env bash
set -euo pipefail
export TMPDIR="$PWD/.configure-work"
mkdir -p "$TMPDIR"

# Run from the extracted, unmodified FFmpeg 8.1.2 source directory in Git Bash.
# Native MinGW-w64 GCC and mingw32-make must be on PATH.
./configure \
  --target-os=mingw32 --arch=x86_64 \
  --disable-autodetect --disable-network --disable-gpl --disable-nonfree \
  --disable-version3 --disable-shared --enable-static \
  --disable-doc --disable-debug --disable-x86asm \
  --disable-pthreads --enable-w32threads \
  --disable-everything --disable-avdevice --disable-swscale \
  --enable-ffmpeg --enable-ffprobe \
  --enable-protocol=file,pipe \
  --enable-demuxer=mov,matroska,avi,asf,mpegps,mpegts,mp3,wav,aac,flac,ogg,ac3,eac3,aiff,amr,ape,wv \
  --enable-decoder=aac,aac_fixed,aac_latm,ac3,eac3,mp3,mp3float,mp2,mp2float,flac,vorbis,opus,alac,wmav1,wmav2,wmapro,wmalossless,pcm_s16le,pcm_s16be,pcm_s24le,pcm_s24be,pcm_s32le,pcm_s32be,pcm_f32le,pcm_f32be,pcm_f64le,pcm_f64be,pcm_u8,pcm_s8,pcm_alaw,pcm_mulaw,pcm_bluray,pcm_dvd,adpcm_ima_wav,adpcm_ms,adpcm_ima_qt,amrnb,amrwb,ape,wavpack,dca,truehd,mlp \
  --enable-parser=aac,aac_latm,ac3,mpegaudio,flac,vorbis,opus,dca,mlp \
  --enable-encoder=pcm_s16le --enable-muxer=wav \
  --enable-filter=aresample,aformat,anull,atrim \
  --extra-cflags=-O2 --extra-ldflags=-static
