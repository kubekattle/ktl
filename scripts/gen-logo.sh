#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
OUT_DIR="$ROOT_DIR/docs/assets/logo"
mkdir -p "$OUT_DIR"

ICON="$OUT_DIR/ktl-logo-icon.png"
ICON_SMALL="$OUT_DIR/ktl-logo-icon-256.png"
LOCKUP="$OUT_DIR/ktl-logo-lockup.png"
LOCKUP_DARK="$OUT_DIR/ktl-logo-lockup-dark.png"

# Core palette from DESIGN.md.
ACCENT="#2563eb"
TEXT="#0f172a"
SURFACE="#ffffff"

# Cross-only mark (no borders, tiles, or other decoration).
magick -size 1024x1024 xc:none \
  -fill "$ACCENT" -stroke none \
  -draw "polygon 512,220 628,430 396,430" \
  -draw "polygon 804,512 594,396 594,628" \
  -draw "polygon 512,804 396,594 628,594" \
  -draw "polygon 220,512 430,628 430,396" \
  "$ICON"

magick "$ICON" -resize 256x256 "$ICON_SMALL"

# Light lockup: transparent canvas with centered dark cross.
magick -size 1900x640 xc:none \
  -fill "$TEXT" -stroke none \
  -draw "polygon 950,140 1040,300 860,300" \
  -draw "polygon 1130,320 970,230 970,410" \
  -draw "polygon 950,500 860,340 1040,340" \
  -draw "polygon 770,320 930,410 930,230" \
  "$LOCKUP"

# Dark lockup: transparent canvas with centered light cross.
magick -size 1900x640 xc:none \
  -fill "$SURFACE" -stroke none \
  -draw "polygon 950,140 1040,300 860,300" \
  -draw "polygon 1130,320 970,230 970,410" \
  -draw "polygon 950,500 860,340 1040,340" \
  -draw "polygon 770,320 930,410 930,230" \
  "$LOCKUP_DARK"

printf 'Generated:\n- %s\n- %s\n- %s\n- %s\n' "$ICON" "$ICON_SMALL" "$LOCKUP" "$LOCKUP_DARK"
