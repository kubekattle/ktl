#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
OUT_DIR="$ROOT_DIR/docs/assets/logo"
mkdir -p "$OUT_DIR"

ICON="$OUT_DIR/ktl-logo-icon.png"
ICON_SMALL="$OUT_DIR/ktl-logo-icon-256.png"
LOCKUP="$OUT_DIR/ktl-logo-lockup.png"
LOCKUP_DARK="$OUT_DIR/ktl-logo-lockup-dark.png"

# Kubernetes official digital colors.
K8S_BLUE="#326CE5"
SURFACE="#ffffff"

# Icon: white eight-point cross on a Kubernetes blue field.
magick -size 1024x1024 xc:none \
  -fill "$K8S_BLUE" -stroke none -draw "roundrectangle 64,64 960,960 210,210" \
  -fill "$SURFACE" -stroke none \
  -draw "polygon 512,120 708,470 316,470" \
  -draw "polygon 904,512 512,316 512,708" \
  -draw "polygon 512,904 316,512 708,512" \
  -draw "polygon 120,512 512,708 512,316" \
  "$ICON"

magick "$ICON" -resize 256x256 "$ICON_SMALL"

# Light lockup: clean Kubernetes blue field with larger centered white cross.
magick -size 1900x640 xc:none \
  -fill "$K8S_BLUE" -stroke none -draw "roundrectangle 120,84 1780,556 80,80" \
  -fill "$SURFACE" -stroke none \
  -draw "polygon 950,88 1078,318 822,318" \
  -draw "polygon 1210,320 950,190 950,450" \
  -draw "polygon 950,552 822,320 1078,320" \
  -draw "polygon 690,320 950,450 950,190" \
  "$LOCKUP"

# Dark lockup: same official-color mark.
magick -size 1900x640 xc:none \
  -fill "$K8S_BLUE" -stroke none -draw "roundrectangle 120,84 1780,556 80,80" \
  -fill "$SURFACE" -stroke none \
  -draw "polygon 950,88 1078,318 822,318" \
  -draw "polygon 1210,320 950,190 950,450" \
  -draw "polygon 950,552 822,320 1078,320" \
  -draw "polygon 690,320 950,450 950,190" \
  "$LOCKUP_DARK"

printf 'Generated:\n- %s\n- %s\n- %s\n- %s\n' "$ICON" "$ICON_SMALL" "$LOCKUP" "$LOCKUP_DARK"
