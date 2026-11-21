package forth

import (
	"strings"
	"testing"
)

func TestDefine(t *testing.T) {
	var b strings.Builder
	vm := New(&b)
	vm.Define("greet", func(vm *VM) {
		n := vm.PopInt()
		s := vm.PopString()
		vm.PushString(strings.Repeat(s, int(n)))
		vm.Push(Int(n * 2))
	})
	src := `S" ab" 2 greet . . : T S" x" 3 GREET ; T . . ' greet EXECUTE`
	if err := vm.Interpret(src); err == nil || !strings.Contains(err.Error(), "underflow") {
		t.Fatalf("err = %v, want underflow from the last EXECUTE", err)
	}
	if got, want := b.String(), "4 abab 6 xxx "; got != want {
		t.Fatalf("output %q, want %q", got, want)
	}
	b.Reset()
	if err := vm.Interpret(`1 2 greet`); err == nil || !strings.Contains(err.Error(), "want a string") {
		t.Fatalf("err = %v, want type error", err)
	}
}

func TestStringWords(t *testing.T) {
	cases := []struct{ src, want string }{
		{`S\" a\"b\\c\td" TYPE`, "a\"b\\c\td"},
		{`: T S\" x\ny" ; T TYPE`, "x\ny"},
		{`42 >STR S" !" + TYPE 1.5 >STR TYPE TRUE >STR TYPE`, "42!1.5true"},
		{`S" a" S" a" = . S" a" S" b" = .`, "true false "},
	}
	for _, c := range cases {
		if got := run(t, c.src); got != c.want {
			t.Errorf("%s: got %q, want %q", c.src, got, c.want)
		}
	}
	if _, err := Run(`S\" \q"`); err == nil {
		t.Error("bad escape accepted")
	}
}

// A loop that makes a string per pass keeps the pool bounded, and strings
// held on the stack, in locals and in variables survive compaction.
func TestStringCollect(t *testing.T) {
	var b strings.Builder
	vm := New(&b)
	src := `VARIABLE V S" var" V !
: T 1 FRAME S" local" 0 L! S" stack" 0 BEGIN DUP >STR DROP 1+ DUP 5000 = UNTIL DROP 0 L@ UNFRAME ;
T TYPE TYPE V @ TYPE S" lit" TYPE`
	if err := vm.Interpret(src); err != nil {
		t.Fatal(err)
	}
	if got, want := b.String(), "localstackvarlit"; got != want {
		t.Fatalf("output %q, want %q", got, want)
	}
	if n := vm.Strings(); n > 2*StringsGC {
		t.Fatalf("string pool holds %d strings after the loop", n)
	}
}

func TestStringCollectMark(t *testing.T) {
	var b strings.Builder
	vm := New(&b)
	if err := vm.Interpret(`: KEEP S" kept" ;`); err != nil {
		t.Fatal(err)
	}
	m := vm.Mark()
	if err := vm.Interpret(`: T 0 BEGIN 1+ DUP >STR DROP DUP 3000 = UNTIL DROP ; T`); err != nil {
		t.Fatal(err)
	}
	vm.Restore(m)
	if err := vm.Interpret(`KEEP TYPE`); err != nil {
		t.Fatal(err)
	}
	if b.String() != "kept" {
		t.Fatalf("output %q", b.String())
	}
}
