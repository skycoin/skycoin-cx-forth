package cxir

import (
	"errors"
	"io"
	"strings"

	"github.com/skycoin/skycoin-cx-forth/forth"
)

// Load translates p and compiles it into vm one function at a time, so
// the Forth source of the whole program never sits in memory at once.
func Load(vm *forth.VM, p *Program) error {
	order, err := callOrder(p)
	if err != nil {
		return err
	}
	for _, f := range order {
		if err := LoadFunc(vm, f); err != nil {
			return err
		}
	}
	return nil
}

// LoadFunc translates one function and compiles it into vm. The functions
// it calls must be loaded already. A caller short of memory can build and
// load a program a function at a time with it.
func LoadFunc(vm *forth.VM, f *Function) error {
	var b strings.Builder
	if err := translateFunc(&b, f); err != nil {
		return errors.New(f.Name + ": " + err.Error())
	}
	return vm.Interpret(b.String())
}

// Run translates p, compiles it into a fresh VM and runs main.
func Run(p *Program, out io.Writer) error {
	vm := forth.New(out)
	if err := Load(vm, p); err != nil {
		return err
	}
	return vm.Interpret(WordName("main"))
}
