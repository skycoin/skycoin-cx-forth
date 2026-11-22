package cxir

import (
	"strings"
	"testing"

	"github.com/skycoin/skycoin-cx-forth/forth"
)

var structPoint = StructDefinition{Name: "Point", Fields: []Arg{
	Var("x", I32), Var("y", I32), Var("visible", BOOL), Var("label", STR),
}}

// main sets the fields of a Point, passes it to len2 and prints the
// result. loop calls a function with a Point local many times, which only
// fits in the struct space because each call frees its instance.
func structProgram() *Program {
	p, r, i, c := VarStruct("p", "Point"), Var("r", I32), Var("i", I32), Var("c", BOOL)
	return &Program{
		Structs: []StructDefinition{structPoint},
		Functions: []Function{
			{
				Name:   "main",
				Locals: []Arg{p, r, i, c},
				Exprs: []Expr{
					Op1(OpIdentity, VarField("p", "x", I32), Int32(3)),
					Op1(OpIdentity, VarField("p", "y", 0), Int32(4)),
					Op1(OpIdentity, VarField("p", "visible", BOOL), Bool(true)),
					Op1(OpIdentity, VarField("p", "label", STR), Str("corner")),
					Call("len2", []Arg{r}, p),
					Print(r),
					Print(VarField("p", "label", STR)),
					Jmp(VarField("p", "visible", BOOL), 0, 1),
					Print(Str("visible")),
					// for i = 0; i < 500; i++ { tmp() }
					Op1(OpLt, c, i, Int32(500)),
					Jmp(c, 0, 3),
					Call("tmp", nil),
					Op1(OpAdd, i, i, Int32(1)),
					Goto(-5),
					Print(i),
				},
			},
			{
				Name:    "len2",
				Inputs:  []Arg{VarStruct("q", "Point")},
				Outputs: []Arg{Var("n", I32)},
				Locals:  []Arg{Var("t", I32)},
				Exprs: []Expr{
					Op1(OpMul, Var("n", I32), VarField("q", "x", I32), VarField("q", "x", I32)),
					Op1(OpMul, Var("t", I32), VarField("q", "y", I32), VarField("q", "y", I32)),
					Op1(OpAdd, Var("n", I32), Var("n", I32), Var("t", I32)),
				},
			},
			{
				Name:   "tmp",
				Locals: []Arg{VarStruct("s", "Point")},
				Exprs:  []Expr{Op1(OpIdentity, VarField("s", "x", I32), Int32(1))},
			},
		},
	}
}

func TestStruct(t *testing.T) {
	var b strings.Builder
	if err := Run(structProgram(), &b); err != nil {
		src, _ := Translate(structProgram())
		t.Fatalf("%v\n%s", err, src)
	}
	if want := "25\ncorner\nvisible\n500\n"; b.String() != want {
		t.Fatalf("got %q want %q", b.String(), want)
	}
}

// The fields keep their CX types in the VM struct space.
func TestStructFieldTypes(t *testing.T) {
	vm := forth.New(nil)
	if err := Load(vm, structProgram()); err != nil {
		t.Fatal(err)
	}
	if err := vm.Interpret(StructWordName("Point")); err != nil {
		t.Fatal(err)
	}
	got := vm.StructTypeOrdinalTypes(vm.Pop().V)
	want := []forth.AtomicType{forth.AtomicTypeInt32, forth.AtomicTypeInt32, forth.AtomicTypeBool, forth.AtomicTypeString}
	if len(got) != len(want) {
		t.Fatalf("got %v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v want %v", got, want)
		}
	}
}

func TestStructErrors(t *testing.T) {
	withMain := func(locals []Arg, e ...Expr) *Program {
		return &Program{Structs: []StructDefinition{structPoint}, Functions: []Function{{Name: "main", Locals: locals, Exprs: e}}}
	}
	p := []Arg{VarStruct("p", "Point"), Var("n", I32)}
	cases := []struct {
		p    *Program
		want string
	}{
		{withMain([]Arg{VarStruct("p", "Nope")}), `undefined struct "Nope"`},
		{withMain(p, Print(VarField("p", "z", I32))), "struct Point has no field z"},
		{withMain(p, Print(VarField("n", "x", I32))), "n is not a struct"},
		{withMain(p, Op1(OpIdentity, VarField("p", "x", I32), Int64(1))), "type error: Int64 into Int32"},
		{withMain(p, Print(VarField("p", "x", I64))), "p.x is Int32, used as Int64"},
		{withMain(p, Op1(OpIdentity, Var("n", I32), Var("p", TypeStruct))), "operators take no whole struct"},
		{&Program{Structs: []StructDefinition{{Name: "E"}}, Functions: []Function{{Name: "main"}}}, "struct E has no fields"},
		{&Program{Structs: []StructDefinition{{Name: "N", Fields: []Arg{VarStruct("a", "Point")}}}, Functions: []Function{{Name: "main"}}},
			"field a is a struct"},
		{&Program{Structs: []StructDefinition{structPoint, {Name: "Other", Fields: []Arg{Var("a", I32)}}}, Functions: []Function{
			{Name: "main", Locals: []Arg{VarStruct("o", "Other")}, Exprs: []Expr{Call("f", nil, VarStruct("o", "Other"))}},
			{Name: "f", Inputs: []Arg{VarStruct("p", "Point")}},
		}}, "f takes StructRef for p"},
	}
	for _, c := range cases {
		_, err := Translate(c.p)
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("got %v want %q", err, c.want)
		}
	}
}
