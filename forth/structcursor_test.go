package forth

import (
	"strings"
	"testing"
)

func TestStructCursor(t *testing.T) {
	i32 := func(n string) string { return n + " AtomicTypeInt32 AtomicTypeConvert " }
	pre := structABD + "abd StructInstanceNew CONSTANT p p StructCursorOpen "
	cases := []struct{ src, want string }{
		{"StructCursorOrdinal . StructCursorOrdinalType AtomicTypeNameOf TYPE StructCursorFieldGet .", "0 Int320 "},
		{i32("5") + "StructCursorFieldSet StructCursorNext . " + i32("6") + "StructCursorFieldSet " +
			"StructCursorNext . " + i32("7") + "StructCursorFieldSet StructCursorOrdinal .", "true true 2 "},
		// At the last ordinal Next stays put and pushes FALSE, Previous at 0 likewise.
		{"StructCursorLast StructCursorNext . StructCursorOrdinal . StructCursorFirst StructCursorPrevious . StructCursorOrdinal .",
			"false 2 false 0 "},
		{"1 StructCursorOrdinalSet " + i32("9") + "StructCursorFieldSet p StructUnpack . . .", "0 9 0 "},
		// Iterate every ordinal.
		{": SUM 0 p StructCursorOpen BEGIN StructCursorFieldGet + StructCursorNext NOT UNTIL ; " +
			i32("4") + "StructCursorFieldSet StructCursorLast " + i32("3") + "StructCursorFieldSet SUM .", "7 "},
		// The struct table can be walked with the cursor too.
		{"abd StructTypeDescriptorRef StructCursorOpen StructCursorFieldGet . StructCursorNext DROP " +
			"StructCursorFieldGet StructCursorOpen StructCursorFieldGet AtomicTypeNameOf TYPE", "3 Int32"},
	}
	for _, c := range cases {
		if got := run(t, pre+c.src); got != c.want {
			t.Errorf("%s: got %q want %q", c.src, got, c.want)
		}
	}
}

func TestStructSpaceRelease(t *testing.T) {
	src := structABD + ": T StructSpaceTop abd StructInstanceNew DROP StructSpaceRelease ; " +
		": LOTS 1000 0 DO T LOOP ; LOTS StructSpaceTop ."
	if got := run(t, src); got != "27 " {
		t.Fatalf("got %q", got)
	}
}

func TestStructCursorErrors(t *testing.T) {
	cases := []struct{ src, want string }{
		{"StructCursorNext", "StructCursorNext: no struct open"},
		{"1 StructCursorOpen", "StructCursorOpen wants StructRef, got Int64"},
		{"0 AtomicTypeStructRef AtomicTypeConvert", "cannot convert Int64 to StructRef"},
		{"AtomicTypeStructRef RegisterTypeSet RegisterFocusPushToStack StructCursorOpen", "StructCursorOpen: nil struct ref"},
		{structABD + "abd StructInstanceNew StructCursorOpen 1 StructCursorFieldSet", "StructCursorFieldSet wants Int32, got Int64"},
		{structABD + "abd StructInstanceNew StructCursorOpen 3 StructCursorOrdinalSet", "no such ordinal"},
		{"0 StructSpaceRelease", "bad struct space index"},
		{structABD + "abd StructInstanceNew StructCursorOpen 27 StructSpaceRelease StructCursorFieldGet", "no struct open"},
		{"5 StructUnpack", "StructUnpack wants StructRef"},
	}
	for _, c := range cases {
		_, err := Run(c.src)
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: got %v want %q", c.src, err, c.want)
		}
	}
}
