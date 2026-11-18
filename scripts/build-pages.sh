#!/bin/sh
# Build the browser REPL into a static site directory, _site by default.
set -eu
cd "$(dirname "$0")/.."
out=${1:-_site}
mkdir -p "$out"
GOOS=js GOARCH=wasm go build -trimpath -ldflags=-s -o "$out/cxforth.wasm" ./cmd/cxforth-wasm
root=$(go env GOROOT)
if [ -f "$root/lib/wasm/wasm_exec.js" ]; then
	cp "$root/lib/wasm/wasm_exec.js" "$out/"
else
	cp "$root/misc/wasm/wasm_exec.js" "$out/"
fi
cp web/index.html "$out/"
