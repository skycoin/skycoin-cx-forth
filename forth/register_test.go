package forth

import (
	"strings"
	"testing"
)

func TestAtomicType(t *testing.T) {
	cases := []struct{ src, want string }{
		{"AtomicTypeInt64 . AtomicTypeStructRef .", "0 13 "},
		{"2147483647 AtomicTypeInt32 AtomicTypeConvert 1 + .", "-2147483648 "},
		{"127 AtomicTypeInt8 AtomicTypeConvert DUP + . 200 AtomicTypeUint8 AtomicTypeConvert 100 + .", "-2 44 "},
		{"0 AtomicTypeUint64 AtomicTypeConvert 1 - DUP . 1 > .", "18446744073709551615 true "},
		{"-7 AtomicTypeUint32 AtomicTypeConvert 2 / .", "2147483644 "},
		{"3.9 AtomicTypeInt16 AtomicTypeConvert DUP . AtomicTypeOf AtomicTypeNameOf TYPE", "3 Int16"},
		{"1 AtomicTypeBool AtomicTypeConvert . 0 AtomicTypeBool AtomicTypeConvert .", "true false "},
		{"5 AtomicTypeString AtomicTypeConvert S\" 1\" + TYPE", "51"},
		{"NULL AtomicTypeInt32 AtomicTypeConvert .", "null "},
		{"S\" x\" AtomicTypeOf . 1.5 AtomicTypeOf . TRUE AtomicTypeOf .", "4 1 3 "},
	}
	for _, c := range cases {
		if got := run(t, c.src); got != c.want {
			t.Errorf("%s: got %q want %q", c.src, got, c.want)
		}
	}
}

func TestRegister(t *testing.T) {
	cases := []struct{ src, want string }{
		{"RegisterFocusPushToStack . RegisterTypeGet .", "0 0 "},
		{"7 RegisterFocusPopFromStack RegisterFocusMiddle 9 RegisterFocusPopFromStack " +
			"RegisterFocusLeft RegisterFocusPushToStack . RegisterFocusMiddle RegisterFocusPushToStack .", "7 9 "},
		{"AtomicTypeInt32 RegisterTypeSet 2147483647 AtomicTypeInt32 AtomicTypeConvert RegisterFocusPopFromStack " +
			"1 AtomicTypeInt32 AtomicTypeConvert RegisterFocusAdd RegisterFocusPushToStack DUP . AtomicTypeOf AtomicTypeNameOf TYPE",
			"-2147483648 Int32"},
		{"AtomicTypeFloat64 RegisterTypeSet 1.5 RegisterFocusPopFromStack 2.0 RegisterFocusMultiply " +
			"RegisterFocusNegate RegisterFocusPushToStack .", "-3 "},
		{"AtomicTypeUint8 RegisterTypeSet 1 AtomicTypeUint8 AtomicTypeConvert RegisterFocusSubtract RegisterFocusPushToStack .", "255 "},
		{"AtomicTypeString RegisterTypeSet S\" ab\" RegisterFocusPopFromStack S\" cd\" RegisterFocusAdd RegisterFocusPushToStack TYPE", "abcd"},
		{"42 RegisterFocusPopFromStack RegisterClear RegisterFocusPushToStack . RegisterTypeGet .", "0 0 "},
		{"AtomicTypeTernary RegisterTypeSet RegisterFocusPushToStack . TRUE RegisterFocusPopFromStack RegisterFocusPushToStack .", "null true "},
		{"RegisterFocusRight 17 5 RegisterFocusPopFromStack 3 RegisterFocusModulo RegisterFocusPushToStack . .", "2 17 "},
	}
	for _, c := range cases {
		if got := run(t, c.src); got != c.want {
			t.Errorf("%s: got %q want %q", c.src, got, c.want)
		}
	}
}

func TestRegisterStringSurvivesCollect(t *testing.T) {
	var b strings.Builder
	vm := New(&b)
	src := `AtomicTypeString RegisterTypeSet S" kept" RegisterFocusPopFromStack ` +
		`: T 2000 0 DO I >STR DROP LOOP ; T RegisterFocusPushToStack TYPE`
	if err := vm.Interpret(src); err != nil {
		t.Fatal(err)
	}
	if vm.Strings() > 1000 {
		t.Fatalf("no compaction ran, %d strings", vm.Strings())
	}
	if b.String() != "kept" {
		t.Fatalf("got %q", b.String())
	}
}

func TestTypeErrors(t *testing.T) {
	cases := []struct{ src, want string }{
		{"AtomicTypeInt32 RegisterTypeSet 1 RegisterFocusPopFromStack", "RegisterFocusPopFromStack wants Int32, got Int64"},
		{"AtomicTypeInt32 RegisterTypeSet 1 RegisterFocusAdd", "RegisterFocusAdd wants Int32, got Int64"},
		{"AtomicTypeBool RegisterTypeSet TRUE AtomicTypeBool AtomicTypeConvert RegisterFocusAdd", "RegisterFocusAdd on Bool"},
		{"AtomicTypeString RegisterTypeSet RegisterFocusNegate", "RegisterFocusNegate on String"},
		{"1 AtomicTypeInt8 AtomicTypeConvert 1 AtomicTypeInt16 AtomicTypeConvert +", "Int8 and Int16 in arithmetic"},
		{"1 AtomicTypeInt8 AtomicTypeConvert 0 /", "division by zero"},
		{`S" x" AtomicTypeInt32 AtomicTypeConvert`, "cannot convert String to Int32"},
		{"1 99 AtomicTypeConvert", "bad atomic type"},
		{"99 RegisterTypeSet", "bad atomic type"},
		{"TRUE AtomicTypeBool AtomicTypeConvert 1 +", "type error in arithmetic"},
		{"TRUE AtomicTypeBool AtomicTypeConvert TRUE =", "type error in comparison"},
	}
	for _, c := range cases {
		_, err := Run(c.src)
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: got %v want %q", c.src, err, c.want)
		}
	}
}

func TestWordNamesKeepCase(t *testing.T) {
	out := run(t, "WORDS")
	if !strings.Contains(out, "RegisterFocusLeft") || !strings.Contains(out, "DUP") {
		t.Fatalf("WORDS: %q", out)
	}
	if run(t, "registerfocuspushtostack .") != "0 " {
		t.Fatal("lookup is not case insensitive")
	}
}
