package cxir

import (
	"strings"
	"testing"

	"github.com/skycoin/skycoin-cx-forth/forth"
)

func TestExamples(t *testing.T) {
	for _, ex := range Examples {
		t.Run(ex.Name, func(t *testing.T) {
			var b strings.Builder
			if err := Run(&ex.Program, &b); err != nil {
				src, _ := Translate(&ex.Program)
				t.Fatalf("%v\n%s", err, src)
			}
			if b.String() != ex.Output {
				t.Errorf("got %q want %q", b.String(), ex.Output)
			}
		})
	}
}

func TestTranslateErrors(t *testing.T) {
	main := func(e ...Expr) *Program {
		return &Program{Functions: []Function{{Name: "main", Locals: []Arg{Var("x", I32)}, Exprs: e}}}
	}
	cases := []struct {
		p    *Program
		want string
	}{
		{&Program{}, "no main"},
		{main(Print(Var("y", I32))), "undefined variable y"},
		{main(Goto(5)), "jump target out of range"},
		{main(Call("nope", nil)), "undefined function nope"},
		{main(Op1(OpAdd, Int32(1), Int32(1), Int32(1))), "assign to a literal"},
		{main(Host("a b", nil)), "bad host word"},
		{main(Op1(OpAdd, Var("x", I32), Var("x", I32), Int64(1))), "type error: Int32, Int64 into Int32"},
		{main(Op1(OpLt, Var("x", I32), Var("x", I32), Int32(1))), "want a Bool or Ternary result"},
		{main(Op1(OpSub, Var("x", I32), Str("a"), Str("b"))), "type error"},
		{main(Op1(OpBoolAnd, Var("x", I32), TriLit(true), Bool(true))), "want a Ternary result"},
		{main(Jmp(Var("x", I32), 0, 0)), "jmp on Int32"},
		{main(Print(Var("x", I64))), "x is Int32, used as Int64"},
		{&Program{Functions: []Function{
			{Name: "main", Exprs: []Expr{Call("f", nil, Int64(1))}},
			{Name: "f", Inputs: []Arg{Var("n", I32)}},
		}}, "f takes Int32 for n, got Int64"},
		{&Program{Functions: []Function{
			{Name: "main", Exprs: []Expr{Call("a", nil)}},
			{Name: "a", Exprs: []Expr{Call("b", nil)}},
			{Name: "b", Exprs: []Expr{Call("a", nil)}},
		}}, "mutual recursion"},
	}
	for _, c := range cases {
		_, err := Translate(c.p)
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("got %v want %q", err, c.want)
		}
	}
}

// A runtime error inside translated code surfaces as a VM error.
func TestRuntimeError(t *testing.T) {
	p := &Program{Functions: []Function{{
		Name:   "main",
		Locals: []Arg{Var("x", I32)},
		Exprs:  []Expr{Op1(OpDiv, Var("x", I32), Int32(1), Int32(0))},
	}}}
	err := Run(p, nil)
	if _, ok := err.(*forth.Error); !ok || !strings.Contains(err.Error(), "division by zero") {
		t.Fatalf("got %v", err)
	}
}

// Lines resolves labels, OpHost reaches a host word and string literals
// may hold quotes and newlines.
func TestHostLinesStrings(t *testing.T) {
	var l Lines
	n, s, c := Var("n", I32), Var("s", STR), Var("c", BOOL)
	l.Add(Op1(OpIdentity, s, Str("{\"a\":\n")))
	l.Label("loop")
	l.Add(Op1(OpLt, c, n, Int32(3)))
	l.If(c, "", "done")
	l.Add(Host("tick", []Arg{n}, n))
	l.Goto("loop")
	l.Label("done")
	l.Add(Print(s), Print(n))
	exprs, err := l.Exprs()
	if err != nil {
		t.Fatal(err)
	}
	p := &Program{Functions: []Function{{Name: "main", Locals: []Arg{n, s, c}, Exprs: exprs}}}
	var b strings.Builder
	vm := forth.New(&b)
	vm.Define("tick", func(vm *forth.VM) { vm.Push(forth.Int(vm.PopInt() + 1)) })
	if err := Load(vm, p); err != nil {
		t.Fatal(err)
	}
	if err := vm.Interpret(WordName("main")); err != nil {
		t.Fatal(err)
	}
	if want := "{\"a\":\n\n3\n"; b.String() != want {
		t.Fatalf("got %q want %q", b.String(), want)
	}

	var bad Lines
	bad.Goto("nowhere")
	if _, err := bad.Exprs(); err == nil {
		t.Fatal("undefined label accepted")
	}
}
