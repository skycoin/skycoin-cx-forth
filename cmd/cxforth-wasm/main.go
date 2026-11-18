//go:build js && wasm

// Command cxforth-wasm is the Forth REPL for a browser page.
//
//	GOOS=js GOARCH=wasm go build -o cxforth.wasm ./cmd/cxforth-wasm
//
// It sets cxforthEval(line) on the page, which runs one line in a VM that
// lives as long as the page and returns the output. CX-LIST lists the CX IR
// examples and CX-RUN <name> runs one.
package main

import (
	"bytes"
	"errors"
	"strings"
	"syscall/js"

	"github.com/skycoin/skycoin-cx-forth/cxir"
	"github.com/skycoin/skycoin-cx-forth/forth"
)

var (
	out bytes.Buffer
	vm  = forth.New(&out)
)

func main() {
	js.Global().Set("cxforthEval", js.FuncOf(func(_ js.Value, args []js.Value) any {
		if len(args) == 0 {
			return ""
		}
		return eval(args[0].String())
	}))
	js.Global().Set("cxforthBanner", banner())
	select {}
}

func banner() string {
	var b strings.Builder
	b.WriteString("skycoin-cx-forth\nexamples:")
	for _, ex := range cxir.Examples {
		b.WriteString(" " + ex.Name)
	}
	b.WriteString("\nrun one with: CX-RUN <name>, show them with: CX-LIST\n")
	return b.String()
}

func eval(src string) string {
	out.Reset()
	err := run(strings.TrimSpace(src))
	if err != nil {
		out.WriteString(" error: " + err.Error())
	} else {
		out.WriteString(" ok")
	}
	return out.String()
}

func run(src string) error {
	if src == "CX-LIST" {
		for _, ex := range cxir.Examples {
			out.WriteString("\n" + ex.Name + "\n" + ex.Source + "\n")
		}
		return nil
	}
	if name, ok := strings.CutPrefix(src, "CX-RUN "); ok {
		name = strings.TrimSpace(name)
		for i := range cxir.Examples {
			if cxir.Examples[i].Name != name {
				continue
			}
			// Load into the REPL VM and forget the example after, like the ESP32 REPL.
			m := vm.Mark()
			err := cxir.Load(vm, &cxir.Examples[i].Program)
			if err == nil {
				err = vm.Interpret(cxir.WordName("main"))
			}
			vm.Restore(m)
			return err
		}
		return errors.New("no example " + name + ", try CX-LIST")
	}
	return vm.Interpret(src)
}
