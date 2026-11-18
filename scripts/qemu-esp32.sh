#!/bin/sh
# Build cmd/cxforth-esp32 and boot it in Espressif's QEMU fork.
#
#   scripts/qemu-esp32.sh esp32|esp32c3            interactive, Ctrl-A x quits
#   scripts/qemu-esp32.sh esp32|esp32c3 check      type scripts/qemu-session.txt and check the replies
#
# Needs tinygo (TINYGO=...) and qemu-system-xtensa or qemu-system-riscv32 from
# https://github.com/espressif/qemu/releases on PATH.
set -eu

chip=${1:?usage: $0 esp32|esp32c3 [check]}
mode=${2:-}
tinygo=${TINYGO:-tinygo}
root=$(cd "$(dirname "$0")/.." && pwd)
out=$(mktemp -d)
trap 'rm -rf "$out"' EXIT

case $chip in
esp32)
	# A .img output is a full 4 MB flash image, app at 0x1000.
	(cd "$root" && "$tinygo" build -target=esp32-generic -o "$out/flash.img" ./cmd/cxforth-esp32)
	qemu="qemu-system-xtensa -machine esp32"
	;;
esp32c3)
	# The C3 ROM loads the image from flash offset 0, so pad the .bin to 4 MB.
	# QEMU has no USB-Serial-JTAG console, so use UART0.
	(cd "$root" && "$tinygo" build -target=esp32c3-generic -serial=uart -o "$out/app.bin" ./cmd/cxforth-esp32)
	cp "$out/app.bin" "$out/flash.img"
	truncate -s 4M "$out/flash.img"
	qemu="qemu-system-riscv32 -machine esp32c3"
	;;
*)
	echo "unknown chip $chip" >&2
	exit 2
	;;
esac

run="$qemu -nographic -drive file=$out/flash.img,if=mtd,format=raw"
if [ "$mode" != check ]; then
	exec $run
fi

{
	sleep 3
	while IFS= read -r line; do
		printf '%s\r' "$line"
		sleep 1
	done <"$root/scripts/qemu-session.txt"
	sleep 2
} | timeout 40 $run >"$out/log" 2>&1 || true
tr -d '\r' <"$out/log"

fail=0
for want in '2 3 + . 5  ok' '10 FACT . 3628800  ok' 'TRUE NULL AND . null  ok' \
	'MAYBE TNOT T. ? ok' 'error: division by zero' 'hello, world' '377' '5050' \
	'0.3333333333333333' 'secure' 'no such example' '9 SQ . 81  ok'; do
	if ! grep -qF -- "$want" "$out/log"; then
		echo "missing: $want" >&2
		fail=1
	fi
done
exit $fail
