#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
OUT_DIR="$ROOT_DIR/docs/assets/logo"
mkdir -p "$OUT_DIR"

ICON="$OUT_DIR/ktl-logo-icon.png"
ICON_SMALL="$OUT_DIR/ktl-logo-icon-256.png"
LOCKUP="$OUT_DIR/ktl-logo-lockup.png"
LOCKUP_DARK="$OUT_DIR/ktl-logo-lockup-dark.png"
MARK_PNG="$OUT_DIR/.ktl-logo-mark-render.png"

BLUE="#326CE5"
BLUE_SOFT="#6B9EFF"
SURFACE="#ffffff"
SURFACE_SOFT="#fcfdff"
BORDER="#d9e6ff"
DARK="#0f172a"

# Draw mark directly to avoid SVG raster inconsistencies.
magick -size 920x920 xc:none \
  -stroke "$BLUE" -strokewidth 20 -fill none -draw "circle 460,460 460,76" \
  -stroke "rgba(107,158,255,0.55)" -strokewidth 3 -draw "circle 460,460 460,104" \
  -stroke "$BLUE" -strokewidth 20 -draw "line 460,120 460,800" -draw "line 120,460 800,460" \
  -strokewidth 13 -draw "line 460,460 700,220" -draw "line 460,460 220,700" \
  -strokewidth 8 -stroke "rgba(50,108,229,0.78)" -draw "line 460,460 700,700" -draw "line 460,460 220,220" \
  -stroke none -fill "$BLUE_SOFT" -draw "circle 460,460 460,434" \
  -fill "$SURFACE" -draw "circle 460,460 460,451" \
  "$MARK_PNG"

# App icon
magick -size 1024x1024 xc:none \
  -fill "$SURFACE_SOFT" -stroke none -draw "roundrectangle 64,64 960,960 210,210" \
  -stroke "$BORDER" -strokewidth 5 -fill none -draw "roundrectangle 64,64 960,960 210,210" \
  \( "$MARK_PNG" -resize 704x704 \) -gravity center -compose Over -composite \
  "$ICON"

magick "$ICON" -resize 256x256 "$ICON_SMALL"

# Light lockup
magick -size 1900x640 xc:none \
  -fill "$SURFACE" -stroke none -draw "roundrectangle 120,84 1780,556 80,80" \
  -stroke "$BORDER" -strokewidth 3 -fill none -draw "roundrectangle 120,84 1780,556 80,80" \
  \( "$MARK_PNG" -resize 396x396 \) -gravity center -compose Over -composite \
  "$LOCKUP"

# Dark lockup
magick -size 1900x640 xc:none \
  -fill "$DARK" -stroke none -draw "roundrectangle 120,84 1780,556 80,80" \
  -stroke "#274163" -strokewidth 3 -fill none -draw "roundrectangle 120,84 1780,556 80,80" \
  \( "$MARK_PNG" -resize 396x396 \) -gravity center -compose Over -composite \
  "$LOCKUP_DARK"

rm -f "$MARK_PNG"

printf 'Generated:\n- %s\n- %s\n- %s\n- %s\n' "$ICON" "$ICON_SMALL" "$LOCKUP" "$LOCKUP_DARK"
