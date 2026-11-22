//go:build esp32 || esp32c3 || esp32s3

package main

import (
	"encoding/hex"
	"errors"
	"machine"
	"runtime"
	"strconv"
	"time"

	"golang.org/x/crypto/chacha20poly1305"

	"github.com/skycoin/skycoin-cx-forth/agent"
	"github.com/skycoin/skycoin-cx-forth/cxir"
	"github.com/skycoin/skycoin-cx-forth/forth"
	"github.com/skycoin/skycoin-cx-forth/host"
	"github.com/skycoin/skycoin-cx-forth/wire"
)

// runAgent runs the chat agent in vm. Its datagrams go out framed on the
// UART to cxforth-relay, which forwards them to cxforth-server.
func runAgent(vm *forth.VM, uart serial, keyHex string) error {
	key, err := hex.DecodeString(keyHex)
	if err != nil || len(key) != wire.KeySize {
		return errors.New("usage: AGENT <64 hex digit key>")
	}
	conn := &wire.SerialConn{R: uart, W: uart, Idle: time.Millisecond}
	client, err := wire.NewClient(key, conn, rng{})
	if err != nil {
		return err
	}
	client.Part = 512
	m := vm.Mark()
	defer vm.Restore(m)
	err = agent.Load(vm, &host.Env{
		Net:   client,
		Pins:  pins{},
		Now:   func() int64 { return time.Now().UnixMilli() },
		Sleep: func(ms int64) { time.Sleep(time.Duration(ms) * time.Millisecond) },
	})
	if err != nil {
		return err
	}
	u := vm.Usage()
	var ms runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&ms)
	uart.Write([]byte("agent: code " + strconv.Itoa(u.Code) + "/" + strconv.Itoa(u.CodeMax) +
		", words " + strconv.Itoa(u.Words) + ", strings " + strconv.Itoa(u.Strings) +
		", heap in use " + strconv.Itoa(int(ms.HeapInuse)) + " of " + strconv.Itoa(int(ms.HeapSys)) + "\r\n"))
	sealBench(uart)
	return vm.Interpret(cxir.WordName("main"))
}

// sealBench times sealing and opening one full datagram.
func sealBench(uart serial) {
	aead, _ := chacha20poly1305.NewX(make([]byte, wire.KeySize))
	nonce := make([]byte, chacha20poly1305.NonceSizeX)
	msg := make([]byte, 512)
	const n = 20
	start := time.Now()
	var out []byte
	for i := 0; i < n; i++ {
		out = aead.Seal(out[:0], nonce, msg, nil)
	}
	seal := time.Since(start) / n
	start = time.Now()
	for i := 0; i < n; i++ {
		_, _ = aead.Open(nil, nonce, out, nil)
	}
	open := time.Since(start) / n
	uart.Write([]byte("xchacha20-poly1305, 512 bytes: seal " + strconv.Itoa(int(seal.Microseconds())) +
		" us, open " + strconv.Itoa(int(open.Microseconds())) + " us\r\n"))
}

// pins drives the GPIOs in gpioAllowed and refuses the rest, which include
// the flash pins on a classic ESP32.
type pins struct{}

func allowed(pin int) bool {
	for _, p := range gpioAllowed {
		if p == pin {
			return true
		}
	}
	return false
}

func (pins) Set(pin, level int) error {
	if !allowed(pin) {
		return errors.New("pin " + strconv.Itoa(pin) + " not allowed")
	}
	p := machine.Pin(pin)
	p.Configure(machine.PinConfig{Mode: machine.PinOutput})
	p.Set(level != 0)
	return nil
}

func (pins) Get(pin int) (int, error) {
	if !allowed(pin) {
		return 0, errors.New("pin " + strconv.Itoa(pin) + " not allowed")
	}
	if machine.Pin(pin).Get() {
		return 1, nil
	}
	return 0, nil
}
