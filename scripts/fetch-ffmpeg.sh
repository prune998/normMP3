#!/usr/bin/env bash
# Télécharge des builds statiques de ffmpeg et les compresse en xz pour
# l'incorporation au binaire final (internal/ffmpeg/bin/*.xz).
#
#   scripts/fetch-ffmpeg.sh all    quatre cibles (défaut)
#   scripts/fetch-ffmpeg.sh host   seulement la machine courante
#
# Cibles embarquées :
#   ffmpeg-linux-amd64.xz    johnvansickle.com (statique)
#   ffmpeg-linux-arm64.xz    johnvansickle.com (statique)
#   ffmpeg-windows-amd64.xz  gyan.dev essentials
#   ffmpeg-darwin-universal.xz  osxexperts.net (arm64 + intel, lipo)
# Windows ARM64 et les autres combinaisons n'ont pas de build statique
# disponible : l'application utilisera alors le ffmpeg du PATH.

set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
BIN="$ROOT/internal/ffmpeg/bin"
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT
mkdir -p "$BIN"

want="${1:-all}"
host="$(go env GOOS)/$(go env GOARCH)"
case "$want" in
  host)
    case "$host" in
      darwin/*) want=darwin ;;
      linux/*) want=linux ;;
      windows/*) want=windows ;;
      *) echo "machine $host non gérée, utilisez 'all'" >&2; exit 1 ;;
    esac
    ;;
  all) ;;
  *) echo "usage: $0 [all|host]" >&2; exit 1 ;;
esac

xzify() { # fichier -> $BIN/<nom>.xz
  local src="$1" dst="$2"
  if [ -f "$BIN/$dst.xz" ]; then
    echo "  $dst.xz déjà présent"
    return 0
  fi
  if command -v xz >/dev/null; then
    xz -T0 -9 -c "$src" > "$BIN/$dst.xz"
  else
    python3 -c 'import lzma, sys; open(sys.argv[2], "wb").write(lzma.compress(open(sys.argv[1], "rb").read(), preset=9))' "$src" "$BIN/$dst.xz"
  fi
  echo "  → $BIN/$dst.xz ($(du -h "$BIN/$dst.xz" | cut -f1))"
}

fetch_linux() {
  local arch="$1"
  local f="ffmpeg-release-$arch-static.tar.xz"
  echo "ffmpeg linux $arch (johnvansickle)…"
  curl -sL --retry 3 -o "$TMP/linux-$arch.tar.xz" "https://www.johnvansickle.com/ffmpeg/releases/$f"
  mkdir -p "$TMP/linux-$arch"
  tar -xJf "$TMP/linux-$arch.tar.xz" -C "$TMP/linux-$arch"
  xzify "$TMP/linux-$arch"/ffmpeg-*/ffmpeg "ffmpeg-linux-$arch"
}

fetch_windows() {
  echo "ffmpeg windows amd64 (gyan.dev essentials)…"
  curl -sL --retry 3 -o "$TMP/win.zip" "https://www.gyan.dev/ffmpeg/builds/ffmpeg-release-essentials.zip"
  unzip -o -q "$TMP/win.zip" -d "$TMP/win"
  local exe
  exe="$(find "$TMP/win" -type f -name ffmpeg.exe | head -1)"
  xzify "$exe" "ffmpeg-windows-amd64"
}

fetch_darwin() {
  echo "ffmpeg darwin universel (osxexperts, lipo arm64+intel)…"
  local index="$TMP/index.html"
  curl -sL --retry 3 -A "Mozilla/5.0" -o "$index" "https://www.osxexperts.net/"
  local arm intel
  arm="$(grep -io 'href="[^"]*ffmpeg[0-9]*arm\.zip"' "$index" | head -1 | sed 's/href="//;s/"//')"
  intel="$(grep -io 'href="[^"]*ffmpeg[0-9]*intel\.zip"' "$index" | head -1 | sed 's/href="//;s/"//')"
  if [ -z "$arm" ] || [ -z "$intel" ]; then
    echo "impossible de trouver les liens ffmpeg sur osxexperts.net" >&2
    exit 1
  fi
  curl -sL --retry 3 -o "$TMP/arm.zip" "$arm"
  curl -sL --retry 3 -o "$TMP/intel.zip" "$intel"
  unzip -o -q "$TMP/arm.zip" -d "$TMP/arm"
  unzip -o -q "$TMP/intel.zip" -d "$TMP/intel"
  lipo -create -output "$TMP/ffmpeg-universal" \
    "$(find "$TMP/arm" -type f -name ffmpeg | head -1)" \
    "$(find "$TMP/intel" -type f -name ffmpeg | head -1)"
  xzify "$TMP/ffmpeg-universal" "ffmpeg-darwin-universal"
}

case "$want" in
  linux)   fetch_linux amd64; fetch_linux arm64 ;;
  windows) fetch_windows ;;
  darwin)  fetch_darwin ;;
  all)     fetch_linux amd64; fetch_linux arm64; fetch_windows; fetch_darwin ;;
esac

echo "terminé : $(ls "$BIN" | tr '\n' ' ')"
