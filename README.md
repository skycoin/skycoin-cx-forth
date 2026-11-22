# skycoin-cx-forth

A small Forth VM in Go that runs a base subset of CX IR, with three-valued
(ternary) logic. It builds with Go for the host and with TinyGo for
microcontrollers.

- `forth/` is the VM: fixed-size stacks and memory, colon definitions,
  IF/ELSE/THEN, loops, variables, and tagged cells (int, f64, f32, ternary,
  string).
- `cxir/` holds the CX IR subset and its translator to Forth words.
- `cmd/cxforth` is the host REPL. `cmd/cxforth-esp32` is a UART REPL for ESP32.
- `host/` holds the words a program uses to reach the world outside the VM:
  `net.request`, `kv.get`, `gpio.set`, `json.get` and the rest.
- `agent/` is a small chat agent written as CX IR, after
  [zclaw](https://github.com/tnm/zclaw).
- `wire/` is the encrypted UDP protocol the agent uses to reach
  `cmd/cxforth-server`, which talks to Telegram and the LLM and holds their
  credentials.

## Ternary logic

Cells can be TRUE, FALSE or NULL (unknown), with Kleene AND, OR, NOT and
IMPLIES. Comparisons on NULL give NULL, and arithmetic on a ternary value is
an error. `TRI-IF ... ELSE ... NULL-ELSE ... THEN` branches three ways.

The words MAYBE, TNOT, TAND, TOR, TEQUIV, TIMPLY, TXOR and T. follow
PETREMANN's eForth/ESP32forth
[ternary.fs](https://github.com/MPETREMANN11/eForth-Windows/blob/main/INFERENCE/ternary.fs),
and accept both its integer encoding (TRUE=-1, FALSE=0, MAYBE=1) and the
tagged cells. See `forth/doc.go`.

## Usage

```
go run ./cmd/cxforth -list
go run ./cmd/cxforth -cx fib
go run ./cmd/cxforth -e 'TRUE NULL AND .'
go test ./...
tinygo build -target=esp32c3-generic ./cmd/cxforth-esp32
```

Try the REPL in the browser at https://skycoin.github.io/skycoin-cx-forth/
(`scripts/build-pages.sh` builds it from `cmd/cxforth-wasm`).

## Chat agent

The agent polls Telegram, asks an LLM with a few tools (gpio_write,
gpio_read, memory_set, memory_get, get_time), runs the tool calls and sends
the reply. It never touches HTTPS. Each request goes to cxforth-server in UDP
datagrams sealed with XChaCha20-Poly1305 under a pre-shared key, and the
server makes the real call. See `wire/wire.go` for the datagram format.

Try it against fake Telegram and LLM endpoints:

```
export CXFORTH_KEY=$(go run ./cmd/cxforth-server -genkey)
go run ./cmd/cxforth -agent &
go run ./cmd/cxforth-server -mock -say 'turn the led on' -say 'remember color blue'
```

For real services, run the server with `TG_TOKEN`, `LLM_URL` and `LLM_KEY` set.

## ESP32 in QEMU

`scripts/qemu-esp32.sh esp32` and `scripts/qemu-esp32.sh esp32c3` build the
REPL and boot it in [Espressif's QEMU](https://github.com/espressif/qemu/releases)
with UART0 on the terminal. Add `check` to type `scripts/qemu-session.txt`
and check the replies. The classic ESP32 needs a TinyGo whose LLVM includes
the Xtensa backend with the esp32 CPU.

Both chips pass that session in QEMU 9.2.2 (esp_develop_9.2.2_20260417) with
TinyGo 0.45.0-dev on LLVM 22. It covers arithmetic, colon definitions,
recursion, the ternary words, an error, and all four CX IR examples, with
the same output as the host REPL.

Not tested on real hardware yet. The C3 under QEMU uses UART0 (`-serial=uart`)
because QEMU has no USB-Serial-JTAG console, so the default C3 build that
talks over USB has not run anywhere.

## License

See LICENSE. `forth/testdata/ternary.fs` is a verbatim copy of PETREMANN's
file, kept as test data under its own GNU GPL header. It is not built into
any binary.
