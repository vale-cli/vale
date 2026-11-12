#!/usr/bin/env bash
# Puts binaryen's wasm-opt on a GitHub Actions runner's PATH for build.sh:
# the release tarball for the runner's platform, unpacked under its home.
#
# Usage: grammars/setup-binaryen.sh   (needs curl and tar)
set -euo pipefail

VERSION=version_133

case "$(uname -s)-$(uname -m)" in
  Linux-x86_64)   platform=x86_64-linux ;;
  Linux-aarch64)  platform=aarch64-linux ;;
  Darwin-arm64)   platform=arm64-macos ;;
  Darwin-x86_64)  platform=x86_64-macos ;;
  MINGW*-x86_64|MSYS*-x86_64) platform=x86_64-windows ;;
  *) echo "setup-binaryen.sh: no binaryen release for $(uname -s)-$(uname -m)" >&2; exit 1 ;;
esac

dir=$HOME/binaryen
mkdir -p "$dir"
curl -fsSL "https://github.com/WebAssembly/binaryen/releases/download/$VERSION/binaryen-$VERSION-$platform.tar.gz" |
  tar -xz -C "$dir" --strip-components=1

bin=$dir/bin
# The runner reads PATH entries as native paths; Git bash's are POSIX.
command -v cygpath >/dev/null && bin=$(cygpath -w "$bin")
echo "$bin" >> "$GITHUB_PATH"
