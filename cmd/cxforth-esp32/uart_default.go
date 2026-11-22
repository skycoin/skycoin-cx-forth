//go:build (esp32 || esp32s3) && !esp32c3

package main

import "machine"

var uartConfig = machine.UARTConfig{BaudRate: 115200}

var gpioAllowed = []int{2, 4, 5, 12, 13, 14, 15, 16, 17, 18, 19, 21, 22, 23, 25, 26, 27, 32, 33}
