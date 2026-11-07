#!/usr/bin/env bash
# Packs each built grammar as the package `vale sync` installs: grammar-<name>.zip
# holding grammar-<name>/styles/config/grammars/<name>.wasm.
#
# Usage: grammars/pack.sh   (after build.sh; writes grammars/dist/pkg/*.zip)
set -euo pipefail
cd "$(dirname "$0")/dist"

rm -rf pkg && mkdir -p pkg
for wasm in *.wasm; do
  name=${wasm%.wasm}
  pkg="grammar-$name"
  rm -rf "$pkg"
  mkdir -p "$pkg/styles/config/grammars"
  cp "$wasm" "$pkg/styles/config/grammars/$wasm"
  (cd . && zip -X -q -r "pkg/$pkg.zip" "$pkg")
  rm -rf "$pkg"
  printf '%-26s %5.2f MB\n' "pkg/$pkg.zip" "$(awk "BEGIN{print $(wc -c <"pkg/$pkg.zip")/1e6}")"
done
