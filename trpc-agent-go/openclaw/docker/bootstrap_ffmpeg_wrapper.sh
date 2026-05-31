#!/usr/bin/env bash
set -euo pipefail

FFMPEG_BIN="$(
  python3 -c 'import imageio_ffmpeg; print(imageio_ffmpeg.get_ffmpeg_exe())'
)"

exec "$FFMPEG_BIN" "$@"
