// Package forth is a small Forth with tagged cells and three valued logic.
//
// It is written for TinyGo on microcontrollers as well as for the host. All
// stacks, data memory and code space are fixed size arrays, there is no
// reflection, and output is formatted with strconv rather than fmt.
//
// # Cells
//
// A cell carries a tag: integer, float64, float32, ternary or string. The
// arithmetic words are polymorphic. Integer with integer stays integer, any
// float makes the result a float. >I32 and >F32 narrow a value the way CX
// i32 and f32 arithmetic does.
//
// # Ternary logic
//
// TRUE, FALSE and NULL push ternary cells. They are stored in balanced form,
// FALSE=-1 NULL=0 TRUE=1, which makes Kleene's strong logic arithmetic:
//
//	AND      min(a, b)
//	OR       max(a, b)
//	NOT      -a
//	IMPLIES  max(-a, b)          Kleene
//	LIMPLIES min(1, 1 - a + b)   Lukasiewicz, NULL LIMPLIES NULL is TRUE
//	XOR      -(a * b)
//	EQV      a * b
//
// AND, OR and XOR on two integers stay bitwise, as in standard Forth. When
// either operand is ternary, integers are read as Forth flags, zero false and
// anything else true. Comparisons always return a ternary cell, and any NULL
// operand makes the result NULL. Arithmetic on any ternary cell, NULL
// included, is an error rather than a value. A comment in the thread cited
// below points out that + on ternary values is meaningless, and an unknown
// reading can still be compared, which is where NULL propagation matters.
// >I32 >INT >F32 >F64 are conversions, not arithmetic, and pass NULL
// through. KNOWN? NULL? TRUE? and FALSE? test a value and never return NULL.
//
// # The eForth vocabulary
//
// MAYBE TNOT TAND TOR TEQUIV TIMPLY TXOR and T. follow ternary.fs by Marc
// Petremann (ESP32forth), from the r/Forth thread "TERNARY LOGIC revisited"
// https://www.reddit.com/r/Forth/comments/1xy20la/ternary_logic_revisited/
// and its code
// https://raw.githubusercontent.com/MPETREMANN11/eForth-Windows/main/INFERENCE/ternary.fs
// which is adapted from https://rosettacode.org/wiki/Ternary_logic#Forth .
//
// That file encodes TRUE=-1 FALSE=0 MAYBE=1 in plain integers, so its AND
// and OR are bitwise and still give Kleene results. This VM's tagged cells
// use FALSE=-1 NULL=0 TRUE=1 instead. MAYBE here pushes the tagged NULL.
// The T words work on both. With two integer operands they compute the
// eForth definitions bit for bit and return an integer, so ESP32forth code
// ports unchanged. With a tagged operand they read integers in the eForth
// encoding, -1 true, 0 false, anything else maybe, and return a tagged cell.
// T. prints T, F or ? for either form. AND OR NOT read integers as Forth
// flags, the T words read them in the eForth encoding.
//
// testdata/ternary.fs is a verbatim copy that the tests load and run.
//
// # Conditionals on NULL
//
// IF runs its true branch only for TRUE. FALSE and NULL both take the ELSE
// branch, the same rule SQL uses for WHERE. UNTIL and WHILE follow the same
// rule, so a loop ends only on TRUE.
//
// TRI-IF branches three ways.
//
//	cond TRI-IF  true-part  ELSE  false-part  NULL-ELSE  null-part  THEN
//
// ELSE and NULL-ELSE are both optional. Without NULL-ELSE, NULL takes the
// ELSE branch exactly as IF does.
//
// # Words for translated code
//
// n FRAME opens n zeroed local slots, i L@ and i L! read and write them and
// UNFRAME closes the frame. LABEL n, GOTO n and JMP3 t f n give numbered
// labels inside one definition, which is what jump based IR needs.
//
// # Host words
//
// VM.Define adds a word that Go code implements, with PushString, PopString
// and PopInt for its arguments. This is how a program reaches the network,
// clocks or pins, none of which the VM touches itself.
//
// # Strings
//
// S\" takes the escapes \" \\ \n \r and \t. + joins two strings, = and the
// other comparisons compare their text, and >STR formats any cell. Strings
// live in a pool that is compacted between instructions once it grows, so a
// loop that makes strings forever runs in bounded memory.
package forth
