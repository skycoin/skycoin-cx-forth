package forth

import (
	"slices"
	"strings"
	"testing"
)

const structABD = "StructDefineStart AtomicTypeInt32 StructDefineFieldAdd AtomicTypeInt32 StructDefineFieldAdd " +
	"AtomicTypeInt32 StructDefineFieldAdd StructDefineEnd CONSTANT abd "

func TestStructDefine(t *testing.T) {
	var b strings.Builder
	vm := New(&b)
	if err := vm.Interpret(structABD + "abd . StructTypeCount . StructTypeIDStructOrdinal ."); err != nil {
		t.Fatal(err)
	}
	if b.String() != "3 4 2 " {
		t.Fatalf("got %q", b.String())
	}
	want := []AtomicType{AtomicTypeInt32, AtomicTypeInt32, AtomicTypeInt32}
	if got := vm.StructTypeOrdinalTypes(3); !slices.Equal(got, want) {
		t.Fatalf("abd ordinals %v", got)
	}
}

// The built-in structs describe the struct model, including themselves.
func TestStructSelfDescribing(t *testing.T) {
	vm := New(nil)
	ref := []AtomicType{AtomicTypeInt64, AtomicTypeInt64}
	link := []AtomicType{AtomicTypeInt64, AtomicTypeStructRef}
	for id, want := range [][]AtomicType{ref, link, link} {
		if got := vm.StructTypeOrdinalTypes(int64(id)); !slices.Equal(got, want) {
			t.Errorf("built-in %d: %v", id, got)
		}
	}
	// Walk abd's ordinal chain from its descriptor, reading only struct cells.
	if err := vm.Interpret(structABD + "abd StructTypeDescriptorRef"); err != nil {
		t.Fatal(err)
	}
	d := vm.Pop()
	if structRefType(d) != StructTypeIDStructTypeDescriptor {
		t.Fatalf("descriptor ref %v", d)
	}
	count := vm.structCells[structRefIndex(d)].V
	o := vm.structCells[structRefIndex(d)+1]
	var types []AtomicType
	for !structRefNil(o) {
		if structRefType(o) != StructTypeIDStructOrdinal {
			t.Fatalf("ordinal ref %v", o)
		}
		types = append(types, AtomicType(vm.structCells[structRefIndex(o)].V))
		o = vm.structCells[structRefIndex(o)+1]
	}
	if count != 3 || len(types) != 3 || types[2] != AtomicTypeInt32 {
		t.Fatalf("count %d types %v", count, types)
	}
}

func TestStructInstanceNew(t *testing.T) {
	src := "StructDefineStart AtomicTypeString StructDefineFieldAdd AtomicTypeStructRef StructDefineFieldAdd " +
		"AtomicTypeFloat32 StructDefineFieldAdd StructDefineEnd StructInstanceNew DUP . StructRefTypeID ."
	if got := run(t, src); got != "StructRef(3,27) 3 " {
		t.Fatalf("got %q", got)
	}
}

func TestStructMarkRestore(t *testing.T) {
	vm := New(nil)
	m := vm.Mark()
	if err := vm.Interpret(structABD + "abd StructInstanceNew DROP"); err != nil {
		t.Fatal(err)
	}
	vm.Restore(m)
	if vm.StructTypeCount() != 3 || vm.structTop != m.structTop {
		t.Fatalf("types %d top %d", vm.StructTypeCount(), vm.structTop)
	}
}

func TestStructErrors(t *testing.T) {
	cases := []struct{ src, want string }{
		{"StructDefineStart StructDefineStart", "already being defined"},
		{"AtomicTypeInt8 StructDefineFieldAdd", "without StructDefineStart"},
		{"StructDefineEnd", "without StructDefineStart"},
		{"StructDefineStart StructDefineEnd", "struct with no fields"},
		{"StructDefineStart 99 StructDefineFieldAdd", "bad atomic type"},
		{"7 StructInstanceNew", "bad struct type id 7"},
		{"1 StructRefTypeID", "StructRefTypeID wants StructRef, got Int64"},
		{": T 200 0 DO 2 StructInstanceNew DROP LOOP ; T", "struct space full"},
	}
	for _, c := range cases {
		_, err := Run(c.src)
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: got %v want %q", c.src, err, c.want)
		}
	}
	// A failed definition does not leave the next one open.
	vm := New(nil)
	_ = vm.Interpret("StructDefineStart 99 StructDefineFieldAdd")
	if err := vm.Interpret(structABD); err != nil {
		t.Fatal(err)
	}
}
