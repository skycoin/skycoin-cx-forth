//go:build esp32c3 || esp32s3

package main

import "crypto/rand"

type rng struct{}

func (rng) Read(b []byte) (int, error) { return rand.Read(b) }
