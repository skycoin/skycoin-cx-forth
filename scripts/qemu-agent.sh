#!/bin/sh
# Run the chat agent on an ESP32 in Espressif's QEMU, against
# cxforth-server -mock. The board's datagrams leave on UART0, and
# cxforth-relay forwards them to the server as UDP.
#
#   scripts/qemu-agent.sh esp32|esp32c3 [message ...]
#
# Needs tinygo (TINYGO=...) and Espressif's qemu-system-xtensa or
# qemu-system-riscv32 on PATH, as scripts/qemu-esp32.sh does.
set -eu

chip=${1:?usage: $0 esp32|esp32c3 [message ...]}
shift
[ $# -gt 0 ] || set -- 'turn the led on' 'remember color blue' 'recall color' 'hello'
tinygo=${TINYGO:-tinygo}
root=$(cd "$(dirname "$0")/.." && pwd)
tcp=${TCP_PORT:-5555}
udp=${UDP_PORT:-7400}
out=$(mktemp -d)
pids=
trap 'kill $pids 2>/dev/null || true; rm -rf "$out"' EXIT

case $chip in
esp32)
	(cd "$root" && "$tinygo" build -size short -target=esp32-generic -o "$out/flash.img" ./cmd/cxforth-esp32)
	qemu="qemu-system-xtensa -machine esp32"
	;;
esp32c3)
	(cd "$root" && "$tinygo" build -size short -target=esp32c3-generic -serial=uart -o "$out/app.bin" ./cmd/cxforth-esp32)
	cp "$out/app.bin" "$out/flash.img"
	truncate -s 4M "$out/flash.img"
	qemu="qemu-system-riscv32 -machine esp32c3"
	;;
*)
	echo "unknown chip $chip" >&2
	exit 2
	;;
esac
(cd "$root" && go build -o "$out/server" ./cmd/cxforth-server && go build -o "$out/relay" ./cmd/cxforth-relay)
key=$("$out/server" -genkey)

$qemu -display none -monitor none -serial "tcp:127.0.0.1:$tcp,server=on,wait=on" \
	-drive "file=$out/flash.img,if=mtd,format=raw" &
pids="$pids $!"
"$out/relay" -line "127.0.0.1:$tcp" -server "127.0.0.1:$udp" -type "AGENT $key" >"$out/console" 2>&1 &
pids="$pids $!"

for m in "$@"; do
	set -- "$@" -say "$m"
	shift
done
status=0
CXFORTH_KEY=$key "$out/server" -listen "127.0.0.1:$udp" -mock -wait "${WAIT:-180s}" "$@" || status=$?
echo "--- board console"
tr -d '\r' <"$out/console" | sed "s/$key/<key>/"
exit $status
