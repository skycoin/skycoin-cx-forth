//go:build esp32 || esp32c3 || esp32s3

// Command cxforth-esp32 is the Forth REPL on an ESP32 UART.
//
//	tinygo flash -target=esp32 ./cmd/cxforth-esp32
//
// Type at 115200 baud. Backspace edits the line, Enter runs it.
// CX-RUN <name> runs one of the compiled in CX IR examples in a fresh VM.
// AGENT <key> runs the chat agent through cxforth-relay, SEAL-BENCH times
// the datagram cipher.
package main

import (
	"machine"
	"time"

	"github.com/skycoin/skycoin-cx-forth/cxir"
	"github.com/skycoin/skycoin-cx-forth/forth"
)

const lineMax = 256

// serial is the part of machine.Serial the REPL uses. On classic ESP32
// that is a UART, on some newer chips it is USB CDC.
type serial interface {
	Write([]byte) (int, error)
	WriteByte(byte) error
	Buffered() int
	ReadByte() (byte, error)
}

func main() {
	var uart serial = machine.Serial
	err := machine.Serial.Configure(uartConfig)
	// The agent needs no data memory and about 1000 instructions of code, and
	// the heap left over is what it runs in.
	vm := forth.NewSized(uart, forth.Sizes{Mem: 256, Locals: 256, Code: 2048})
	if err != nil {
		uart.Write([]byte("\r\nserial configure: " + err.Error()))
	}

	uart.Write([]byte("\r\nskycoin-cx-forth\r\nexamples: "))
	for _, ex := range cxir.Examples {
		uart.Write([]byte(ex.Name + " "))
	}
	uart.Write([]byte("\r\nrun one with: CX-RUN <name>\r\n"))

	var line [lineMax]byte
	n := 0
	for {
		if uart.Buffered() == 0 {
			time.Sleep(5 * time.Millisecond)
			continue
		}
		c, err := uart.ReadByte()
		if err != nil {
			continue
		}
		switch {
		case c == '\r' || c == '\n':
			uart.Write([]byte(" "))
			run(vm, uart, string(line[:n]))
			n = 0
		case c == 8 || c == 127:
			if n > 0 {
				n--
				uart.Write([]byte("\b \b"))
			}
		case n < lineMax:
			line[n] = c
			n++
			uart.WriteByte(c)
		}
	}
}

func run(vm *forth.VM, uart serial, src string) {
	if len(src) > 6 && src[:6] == "AGENT " {
		report(uart, runAgent(vm, uart, src[6:]))
		return
	}
	if src == "SEAL-BENCH" {
		sealBench(uart)
		return
	}
	if len(src) > 7 && src[:7] == "CX-RUN " {
		name := src[7:]
		for i := range cxir.Examples {
			if cxir.Examples[i].Name != name {
				continue
			}
			p := &cxir.Examples[i].Program
			// Load into the REPL VM and forget it after, one VM fits in DRAM.
			m := vm.Mark()
			err := cxir.Load(vm, p)
			if err == nil {
				err = vm.Interpret(cxir.WordName("main"))
			}
			vm.Restore(m)
			report(uart, err)
			return
		}
		uart.Write([]byte("no such example\r\n"))
		return
	}
	report(uart, vm.Interpret(src))
}

func report(uart serial, err error) {
	if err != nil {
		uart.Write([]byte("error: " + err.Error() + "\r\n"))
		return
	}
	uart.Write([]byte(" ok\r\n"))
}
