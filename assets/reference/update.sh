#!/usr/bin/env sh
set -eu

# Downloads are deliberately confined to this maintainer-only script. The app
# serves the checked-in snapshot and never fetches reference data at runtime.
root=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT

curl --fail --location --proto '=https' --tlsv1.2 \
  --max-time 60 --output "$work/m49.html" \
  'https://unstats.un.org/unsd/methodology/m49/overview/'
curl --fail --location --proto '=https' --tlsv1.2 \
  --max-time 60 --output "$work/ne_50m_admin_0_countries.zip" \
  'https://naciscdn.org/naturalearth/50m/cultural/ne_50m_admin_0_countries.zip'
expected='5fed433373581fa648920435f937d95f2d3c0200e067409c6478dcdf1b853139'
actual=$(shasum -a 256 "$work/ne_50m_admin_0_countries.zip" | awk '{print $1}')
[ "$actual" = "$expected" ] || { echo "unexpected Natural Earth archive checksum" >&2; exit 1; }
command -v ogr2ogr >/dev/null || { echo "ogr2ogr (GDAL) is required to update Natural Earth" >&2; exit 1; }
unzip -qq "$work/ne_50m_admin_0_countries.zip" -d "$work/ne"
ogr2ogr -f GeoJSON "$work/ne_50m_admin_0_countries.geojson" "$work/ne/ne_50m_admin_0_countries.shp"
node "$root/build.mjs" "$work/m49.html" "$work/ne_50m_admin_0_countries.geojson" "$root"
