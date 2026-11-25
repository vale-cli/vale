#!/usr/bin/env bash
# Builds typst.wasm, the Typst reader Vale embeds: typst2vast, compiled to
# WebAssembly. The module is checked in, so this is only needed to move to a
# new typst2vast.
#
# Usage: internal/lint/typst/build.sh   (needs cargo with the wasm32-unknown-unknown target)
set -euo pipefail
cd "$(dirname "$0")"

cargo build --manifest-path module/Cargo.toml --release --locked --target wasm32-unknown-unknown
cp module/target/wasm32-unknown-unknown/release/vale_typst.wasm typst.wasm
printf 'typst.wasm %5.2f MB\n' "$(awk "BEGIN{print $(wc -c <typst.wasm)/1e6}")"
