#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
OUT_DIR="$ROOT_DIR/docs/assets/logo"
mkdir -p "$OUT_DIR"

ICON="$OUT_DIR/ktl-logo-icon.png"
ICON_SMALL="$OUT_DIR/ktl-logo-icon-256.png"
LOCKUP="$OUT_DIR/ktl-logo-lockup.png"
LOCKUP_DARK="$OUT_DIR/ktl-logo-lockup-dark.png"

# Core palette (based on DESIGN.md with a luxury metallic accent).
ACCENT="#2563eb"
TEXT="#0f172a"
SURFACE="#ffffff"
SURFACE_SOFT="#f8fafc"
METAL="#d4af37"
METAL_SOFT="#f2dfa2"

# Icon: premium seal with a Maltese-style 8-point cross (no letters).
magick -size 1024x1024 xc:none \
  \( -size 1024x1024 gradient:"#1b2c56-$TEXT" -rotate 90 \) \
  \( -size 1024x1024 xc:black -fill white -draw "roundrectangle 64,64 960,960 210,210" \) \
  -compose CopyOpacity -composite \
  -stroke "$METAL_SOFT" -strokewidth 8 -fill none -draw "roundrectangle 86,86 938,938 186,186" \
  -stroke "$METAL" -strokewidth 4 -fill none -draw "roundrectangle 106,106 918,918 166,166" \
  -fill "$ACCENT" -stroke none -draw "circle 512,512 512,240" \
  -fill "$SURFACE" \
  -draw "polygon 512,278 600,430 424,430" \
  -draw "polygon 746,512 594,424 594,600" \
  -draw "polygon 512,746 424,594 600,594" \
  -draw "polygon 278,512 430,600 430,424" \
  -fill "$SURFACE_SOFT" -draw "circle 512,512 512,486" \
  "$ICON"

magick "$ICON" -resize 256x256 "$ICON_SMALL"

# Light plate: centered emblem, no typography.
magick -size 1900x640 xc:none \
  -fill "$SURFACE" -stroke none -draw "roundrectangle 120,84 1780,556 80,80" \
  -stroke "#e2e8f0" -strokewidth 4 -fill none -draw "roundrectangle 120,84 1780,556 80,80" \
  \( "$ICON" -resize 420x420 \) -gravity center -compose Over -composite \
  "$LOCKUP"

# Dark plate: centered emblem, luxury dark variant.
magick -size 1900x640 xc:none \
  \( -size 1900x640 gradient:"#0f172a-#111827" -rotate 90 \) -compose DstOver -composite \
  -stroke "$METAL" -strokewidth 4 -fill none -draw "roundrectangle 120,84 1780,556 80,80" \
  \( "$ICON" -resize 420x420 \) -gravity center -compose Over -composite \
  "$LOCKUP_DARK"

printf 'Generated:\n- %s\n- %s\n- %s\n- %s\n' "$ICON" "$ICON_SMALL" "$LOCKUP" "$LOCKUP_DARK"
