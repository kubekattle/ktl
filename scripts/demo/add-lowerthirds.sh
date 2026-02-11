#!/usr/bin/env bash
set -euo pipefail

in="${1:-}"
if [[ -z "$in" ]]; then
  echo "usage: $0 /path/to/ktl-demo.mov [output.mp4]" >&2
  exit 2
fi

out="${2:-ktl-demo-lowerthirds.mp4}"
timeline="${KTL_LOWERTHIRDS_FILE:-$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/lowerthirds.txt}"

if ! command -v ffmpeg >/dev/null 2>&1; then
  echo "ffmpeg not found (install via Homebrew: brew install ffmpeg)" >&2
  exit 2
fi

if [[ ! -f "$timeline" ]]; then
  echo "lower thirds file not found: $timeline" >&2
  exit 2
fi

font="${KTL_LOWERTHIRDS_FONT:-/System/Library/Fonts/Supplemental/Arial.ttf}"
if [[ ! -f "$font" ]]; then
  # Fallback to any existing system font.
  font="$(ls -1 /System/Library/Fonts/Supplemental/*.ttf 2>/dev/null | head -n 1 || true)"
fi
if [[ -z "$font" || ! -f "$font" ]]; then
  echo "no usable .ttf font found; set KTL_LOWERTHIRDS_FONT to a .ttf path" >&2
  exit 2
fi

filter=""
while IFS='|' read -r start end text; do
  start="$(printf "%s" "$start" | xargs)"
  end="$(printf "%s" "$end" | xargs)"
  text="$(printf "%s" "$text" | sed 's/^[[:space:]]*//; s/[[:space:]]*$//')"
  [[ -z "$start" || -z "$end" || -z "$text" ]] && continue
  [[ "$start" =~ ^# ]] && continue

  # Escape for ffmpeg drawtext.
  text_esc="$(printf "%s" "$text" | sed 's/\\/\\\\/g; s/:/\\:/g; s/'\''/\\\x27/g')"
  filter="${filter}drawtext=fontfile=${font}:text='${text_esc}':"\
"x=(w-text_w)/2:y=h-(text_h+60):fontsize=42:fontcolor=white:"\
"box=1:boxcolor=0x000000AA:boxborderw=20:"\
"enable='between(t,${start},${end})',"
done <"$timeline"

filter="${filter%,}"

ffmpeg -y -i "$in" -vf "$filter" -c:v libx264 -crf 18 -preset slow -c:a aac -b:a 192k "$out"
echo "wrote $out"

