#!/usr/bin/env bash
# Builds the ffmpeg that the Windows zip and the macOS app ship beside
# spytifyd, from the official sources of FFmpeg, LAME and zlib, pinned and
# checked, with only what Spytify runs it for: the four output formats, the
# filters internal/encode applies, and tags and covers copied as they are. A
# full build is over 130 MB; this one is a few.
#
#   scripts/build-ffmpeg.sh OUT [HOST]
#
# OUT gets ffmpeg (ffmpeg.exe for Windows), its license and a README that
# says where it comes from. HOST is a cross-compiler's prefix, such as
# x86_64-w64-mingw32 (Debian: apt install mingw-w64), or a clang target on
# a Mac, such as x86_64-apple-macos14.2: the processor and the oldest macOS
# it runs on. Without it, the build is for this machine, which tests the
# same components. Needs make, nasm, xz and a C compiler. The build is kept
# in the cache, by HOST and this script's checksum.
set -euo pipefail

cd "$(dirname "$0")/.."
# shellcheck source=scripts/fetch.sh
. scripts/fetch.sh
out="$1"
host="${2:-}"

ffmpeg_version=9.0.2
ffmpeg_url="https://ffmpeg.org/releases/ffmpeg-$ffmpeg_version.tar.xz"
ffmpeg_sha256=8c3850283eb25fa026482078a04051e0be17347b09ef81a0849bec15a96e002e
lame_version=3.100
lame_url="https://downloads.sourceforge.net/project/lame/lame/$lame_version/lame-$lame_version.tar.gz"
lame_sha256=ddfe36cab873794038ae2c1210557ad34857a4b6bdc515785d1da9e175b1da1e
zlib_version=1.3.2
zlib_url="https://github.com/madler/zlib/releases/download/v$zlib_version/zlib-$zlib_version.tar.gz"
zlib_sha256=bb329a0a2cd0274d05519d61c667c062e06990d72e125ee2dfa8de64f0119d16

# What Spytify runs ffmpeg for, and nothing else: an encoder, a filter or a
# format left out here is missing from the ffmpeg of the Windows zip and of
# the macOS app. Code that runs ffmpeg for something new adds it here, with
# a test that runs it, and checks the build:
#
#   scripts/build-ffmpeg.sh /tmp/ffmpeg && PATH=/tmp/ffmpeg:$PATH go test ./...
#
# CI's windows job runs the tests with the Windows build, and macos-build
# the encode and tag tests with the macOS one: a component missing fails
# them, but only where a test runs it. configure takes lists with commas.
# shellcheck disable=SC2054
components=(
  # internal/encode: a take's WAV, trimmed, its volume set, resampled and
  # written as MP3, AAC, FLAC or WAV.
  --enable-libmp3lame
  --enable-encoder=libmp3lame,aac,flac,pcm_s16le,pcm_f32le
  --enable-decoder=pcm_s16le,pcm_s24le,pcm_s32le,pcm_f32le
  --enable-demuxer=wav
  --enable-muxer=mp3,ipod,flac,wav,null
  --enable-filter=atrim,asetpts,volume,aresample,loudnorm,aformat,anull
  # internal/tag and internal/encode: tags and a JPEG or PNG cover, read and
  # rewritten without encoding again. The covers are decoded for their size,
  # which FLAC writes: PNG's decoder needs zlib.
  --enable-zlib
  --enable-decoder=mp3float,aac,flac,mjpeg,png
  --enable-demuxer=mp3,flac,mov,image2,image_jpeg_pipe,image_png_pipe
  --enable-muxer=ffmetadata,image2pipe
  --enable-parser=mpegaudio,aac,flac,mjpeg,png
  --enable-protocol=file,pipe
  # The tests: silence from lavfi, the loudness ebur128 measures, and the
  # delays and joins gate1's makes.
  --enable-indev=lavfi
  --enable-decoder=pcm_u8
  --enable-filter=anullsrc,ebur128,adelay,apad,concat
)

exe=ffmpeg
case "$host" in *mingw*) exe=ffmpeg.exe ;; esac
cache="${XDG_CACHE_HOME:-$HOME/.cache}/spytify/ffmpeg-$ffmpeg_version-${host:-native}-$(sha256 scripts/build-ffmpeg.sh | cut -c1-12)"
if [ ! -f "$cache/$exe" ]; then
  src="$(mktemp -d)"
  trap 'rm -rf "$src"' EXIT
  tar -xzf "$(fetch "$lame_url" "$lame_sha256")" -C "$src"
  tar -xzf "$(fetch "$zlib_url" "$zlib_sha256")" -C "$src"
  tar -xJf "$(fetch "$ffmpeg_url" "$ffmpeg_sha256")" -C "$src"
  jobs="$(getconf _NPROCESSORS_ONLN)"
  prefix="$src/prefix"
  lame=(--prefix="$prefix" --disable-shared --enable-static --disable-frontend --disable-decoder)
  zlib=()
  ffmpeg=(--prefix="$prefix" --pkg-config-flags=--static --extra-cflags="-I$prefix/include" --extra-ldflags="-L$prefix/lib")
  strip="strip"
  case "$host" in
    "") ;;
    *-apple-macos*)
      # Xcode's clang builds for either processor from either Mac. LAME's
      # configure knows arm64 as aarch64.
      cc="clang -target $host"
      arch="${host%%-*}"
      lame+=(--host="${arch/arm64/aarch64}-apple-darwin" CC="$cc")
      zlib=(CC="$cc")
      # configure links CoreFoundation, CoreMedia and CoreVideo whatever is
      # enabled; nothing here uses them, so only libSystem is loaded.
      ffmpeg+=(--enable-cross-compile --arch="$arch" --target-os=darwin --cc="$cc" "--extra-ldexeflags=-Wl,-dead_strip_dylibs")
      ;;
    *)
      lame+=(--host="$host")
      zlib=(CC="$host-gcc" AR="$host-ar" RANLIB="$host-ranlib")
      # -static: nothing but Windows' own DLLs to load.
      ffmpeg+=(--enable-cross-compile --cross-prefix="$host-" --arch="${host%%-*}" --target-os=mingw32 --extra-ldexeflags=-static)
      strip="$host-strip"
      ;;
  esac
  (cd "$src/lame-$lame_version" && ./configure "${lame[@]}" > /dev/null && make -j"$jobs" > /dev/null && make install > /dev/null)
  (cd "$src/zlib-$zlib_version" && env ${zlib[@]+"${zlib[@]}"} ./configure --prefix="$prefix" --static > /dev/null && make -j"$jobs" libz.a > /dev/null && make install > /dev/null)
  (
    cd "$src/ffmpeg-$ffmpeg_version"
    ./configure "${ffmpeg[@]}" \
      --disable-everything --disable-autodetect --disable-network --disable-debug --disable-doc \
      --disable-ffplay --disable-ffprobe --disable-swscale \
      "${components[@]}" > /dev/null
    make -j"$jobs" > /dev/null
  )
  mkdir -p "$cache.part"
  cp "$src/ffmpeg-$ffmpeg_version/$exe" "$cache.part/"
  "$strip" "$cache.part/$exe"
  cp "$src/ffmpeg-$ffmpeg_version/COPYING.LGPLv2.1" "$cache.part/LICENSE.txt"
  rm -rf "$cache"
  mv "$cache.part" "$cache"
fi

mkdir -p "$out"
cp "$cache/$exe" "$out/"
cp "$cache/LICENSE.txt" "$out/ffmpeg-LICENSE.txt"
cat > "$out/ffmpeg-README.txt" <<NOTE
$exe is FFmpeg $ffmpeg_version with LAME $lame_version and zlib $zlib_version, under the LGPL
(ffmpeg-LICENSE.txt; zlib's license is in its source), built by
scripts/build-ffmpeg.sh in https://github.com/spytify/spytify from their
sources:
  $ffmpeg_url
  $lame_url
  $zlib_url
with only what Spytify runs it for:
  ${components[*]}
Spytify runs it as a separate program to encode the recordings.
NOTE
