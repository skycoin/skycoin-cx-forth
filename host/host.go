// Package host defines the words a cxforth program uses to reach the world
// outside the VM. The same words run on the desktop and on a
// microcontroller; only the Env behind them changes.
//
//	net.request  ( method path body -- status body )  status 0 on failure, body holds the error
//	time.now     ( -- ms )
//	sleep        ( ms -- )
//	kv.get       ( key -- value )   "" when unset
//	kv.set       ( key value -- )
//	gpio.set     ( pin level -- )
//	gpio.get     ( pin -- level )
//	json.get     ( doc path -- raw )   raw JSON text, "" when absent
//	json.str     ( doc path -- s )     a string decoded, anything else as raw text
//	json.int     ( doc path -- n )     0 when absent or not a number
//	json.len     ( doc path -- n )     elements of an array or object
//	json.set     ( doc path raw -- doc )
//	json.push    ( doc path raw -- doc )
//	json.quote   ( s -- raw )
//
// A failing json.set, a pin the Env refuses or a missing Net stops the
// program with an error.
package host

import (
	"strconv"

	"github.com/skycoin/skycoin-cx-forth/forth"
)

// Net carries requests to the gateway, see package wire.
type Net interface {
	Request(method, path, body string) (status int, resp string, err error)
}

// Pins drives GPIO. Set and Get return an error for a pin that may not be used.
type Pins interface {
	Set(pin, level int) error
	Get(pin int) (int, error)
}

// Env is what the host words act on.
type Env struct {
	Net   Net
	Pins  Pins
	KV    map[string]string
	Now   func() int64 // milliseconds
	Sleep func(ms int64)
}

// Install defines the host words in vm.
func Install(vm *forth.VM, e *Env) {
	if e.KV == nil {
		e.KV = map[string]string{}
	}
	vm.Define("net.request", func(vm *forth.VM) {
		body, path, method := vm.PopString(), vm.PopString(), vm.PopString()
		if e.Net == nil {
			vm.Fail("net.request: no network")
			return
		}
		st, resp, err := e.Net.Request(method, path, body)
		if err != nil {
			st, resp = 0, err.Error()
		}
		vm.Push(forth.Int(int64(st)))
		vm.PushString(resp)
	})
	vm.Define("time.now", func(vm *forth.VM) { vm.Push(forth.Int(e.Now())) })
	vm.Define("sleep", func(vm *forth.VM) { e.Sleep(vm.PopInt()) })
	vm.Define("kv.get", func(vm *forth.VM) { vm.PushString(e.KV[vm.PopString()]) })
	vm.Define("kv.set", func(vm *forth.VM) {
		v, k := vm.PopString(), vm.PopString()
		e.KV[k] = v
	})
	vm.Define("gpio.set", func(vm *forth.VM) {
		level, pin := vm.PopInt(), vm.PopInt()
		if err := e.Pins.Set(int(pin), int(level)); err != nil {
			vm.Fail("gpio.set: " + err.Error())
		}
	})
	vm.Define("gpio.get", func(vm *forth.VM) {
		v, err := e.Pins.Get(int(vm.PopInt()))
		if err != nil {
			vm.Fail("gpio.get: " + err.Error())
		}
		vm.Push(forth.Int(int64(v)))
	})
	vm.Define("json.get", func(vm *forth.VM) {
		path, doc := vm.PopString(), vm.PopString()
		vm.PushString(Get(doc, path))
	})
	vm.Define("json.str", func(vm *forth.VM) {
		path, doc := vm.PopString(), vm.PopString()
		s, _ := Unquote(Get(doc, path))
		vm.PushString(s)
	})
	vm.Define("json.int", func(vm *forth.VM) {
		path, doc := vm.PopString(), vm.PopString()
		raw := Get(doc, path)
		n, err := strconv.ParseInt(raw, 10, 64)
		if err != nil {
			f, _ := strconv.ParseFloat(raw, 64)
			n = int64(f)
		}
		vm.Push(forth.Int(n))
	})
	vm.Define("json.len", func(vm *forth.VM) {
		path, doc := vm.PopString(), vm.PopString()
		vm.Push(forth.Int(int64(Len(doc, path))))
	})
	vm.Define("json.set", func(vm *forth.VM) {
		raw, path, doc := vm.PopString(), vm.PopString(), vm.PopString()
		out, err := Set(doc, path, raw)
		if err != nil {
			vm.Fail(err.Error())
		}
		vm.PushString(out)
	})
	vm.Define("json.push", func(vm *forth.VM) {
		raw, path, doc := vm.PopString(), vm.PopString(), vm.PopString()
		out, err := Push(doc, path, raw)
		if err != nil {
			vm.Fail(err.Error())
		}
		vm.PushString(out)
	})
	vm.Define("json.quote", func(vm *forth.VM) { vm.PushString(Quote(vm.PopString())) })
}

// MemPins is a GPIO bank in memory, for the desktop and tests.
type MemPins map[int]int

// Set stores level.
func (p MemPins) Set(pin, level int) error {
	p[pin] = level
	return nil
}

// Get returns the stored level.
func (p MemPins) Get(pin int) (int, error) { return p[pin], nil }
