//go:build esp32 && !esp32c3 && !esp32s3

package main

import (
	"runtime/volatile"
	"unsafe"
)

// rng reads the classic ESP32's random number register, WDEV_RND_REG in
// the ESP32 Technical Reference Manual, section 26 Random Number Generator.
// It is truly random only while the radio runs; QEMU emulates it.
type rng struct{}

func (rng) Read(b []byte) (int, error) {
	reg := (*volatile.Register32)(unsafe.Pointer(uintptr(0x3FF75144)))
	var v uint32
	for i := range b {
		if i%4 == 0 {
			v = reg.Get()
		}
		b[i] = byte(v >> (8 * (i % 4)))
	}
	return len(b), nil
}
