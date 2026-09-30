#!/bin/sh
# Regenerates web/static icons and the social image from the sources here.
# Needs rsvg-convert, ImageMagick and the Chromium used by the e2e suite.
set -eu
cd "$(dirname "$0")"
out=../static
cp icon.svg "$out/favicon.svg"
rsvg-convert -w 16 -h 16 icon-16.svg -o "$out/favicon-16.png"
rsvg-convert -w 32 -h 32 icon.svg -o "$out/favicon-32.png"
rsvg-convert -w 192 -h 192 icon.svg -o "$out/favicon-192.png"
rsvg-convert -w 512 -h 512 icon.svg -o "$out/favicon-512.png"
rsvg-convert -w 180 -h 180 icon-fullbleed.svg -o "$out/apple-touch-icon.png"   # iOS rounds the corners itself
rsvg-convert -w 512 -h 512 icon-fullbleed.svg -o "$out/maskable-512.png"        # Android masks: art stays in the safe zone
magick "$out/favicon-16.png" "$out/favicon-32.png" "$out/favicon.ico"
node render.mjs "$out/og-image.png"
