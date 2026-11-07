#!/usr/bin/env bash
# Compiles every grammar Vale reads into a WebAssembly module: tree-sitter's
# core plus the grammar's parser and scanner, behind shim.c. The sources are
# the ones the go-tree-sitter bindings carry, so each module parses exactly
# what the CGo build did.
#
# Usage: grammars/build.sh [name ...]   (needs zig; writes grammars/dist/<name>.wasm)
set -euo pipefail
cd "$(dirname "$0")/.."

# The sources live in the module cache, which a fresh checkout has not filled
# yet: `go list -m` reports no directory for a module that is not there.
go mod download github.com/smacker/go-tree-sitter github.com/jdkato/go-tree-sitter-julia
TS=$(go list -m -f '{{.Dir}}' github.com/smacker/go-tree-sitter)
JULIA=$(go list -m -f '{{.Dir}}' github.com/jdkato/go-tree-sitter-julia)
OUT=grammars/dist
INC=$OUT/include

# name | symbol | source directory
TABLE='
bash|tree_sitter_bash|'"$TS"'/bash
c|tree_sitter_c|'"$TS"'/c
cpp|tree_sitter_cpp|'"$TS"'/cpp
csharp|tree_sitter_c_sharp|'"$TS"'/csharp
css|tree_sitter_css|'"$TS"'/css
elixir|tree_sitter_elixir|'"$TS"'/elixir
elm|tree_sitter_elm|'"$TS"'/elm
go|tree_sitter_go|'"$TS"'/golang
java|tree_sitter_java|'"$TS"'/java
javascript|tree_sitter_javascript|'"$TS"'/javascript
julia|tree_sitter_julia|'"$JULIA"'/julia
kotlin|tree_sitter_kotlin|'"$TS"'/kotlin
lua|tree_sitter_lua|'"$TS"'/lua
php|tree_sitter_php|'"$TS"'/php
proto|tree_sitter_proto|'"$TS"'/protobuf
python|tree_sitter_python|'"$TS"'/python
ruby|tree_sitter_ruby|'"$TS"'/ruby
rust|tree_sitter_rust|'"$TS"'/rust
toml|tree_sitter_toml|'"$TS"'/toml
tsx|tree_sitter_tsx|'"$TS"'/typescript/tsx
typescript|tree_sitter_typescript|'"$TS"'/typescript/typescript
yaml|tree_sitter_yaml|'"$TS"'/yaml
'

# The module cache is read-only, so the copies are refreshed from scratch.
rm -rf "$INC" "$OUT/core"
mkdir -p "$INC/tree_sitter" "$OUT/core"
cp "$TS/api.h" "$INC/tree_sitter/api.h"
cp "$TS/golang/parser.h" "$INC/tree_sitter/parser.h"
cp "$TS"/*.h "$OUT/core/"
for f in "$TS"/*.c; do [ "$(basename "$f")" = bindings.c ] || cp "$f" "$OUT/core/"; done
chmod -R u+w "$INC" "$OUT/core"

want=("$@")
wanted() {
  [ ${#want[@]} -eq 0 ] && return 0
  for w in "${want[@]}"; do [ "$w" = "$1" ] && return 0; done
  return 1
}

build() {
  local name=$1 symbol=$2 dir=$3
  local srcs=("$dir/parser.c")
  [ -f "$dir/scanner.c" ] && srcs+=("$dir/scanner.c")
  # A scanner written in C++ (YAML's) builds with the same driver, without
  # exceptions or RTTI, which wasm32-wasi has no runtime for.
  local cc="zig cc"
  [ -f "$dir/scanner.cc" ] && { srcs+=("$dir/scanner.cc"); cc="zig c++"; }
  $cc -target wasm32-wasi -O2 -mexec-model=reactor -fno-exceptions -fno-rtti \
    -Wno-implicit-function-declaration -Wno-unused-but-set-variable \
    -DTS_LANG="$symbol" -I "$INC" -I "$OUT/core" -I "$dir" \
    "$OUT"/core/*.c "${srcs[@]}" grammars/shim.c -o "$OUT/$name.wasm"
  printf '%-11s %5.2f MB\n' "$name" "$(awk "BEGIN{print $(wc -c <"$OUT/$name.wasm")/1e6}")"
}

while IFS='|' read -r name symbol dir; do
  [ -z "$name" ] && continue
  wanted "$name" || continue
  build "$name" "$symbol" "$dir" &
done <<<"$TABLE"
wait
