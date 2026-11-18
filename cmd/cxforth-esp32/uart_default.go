//go:build (esp32 || esp32s3) && !esp32c3

package main

import "machine"

var uartConfig = machine.UARTConfig{BaudRate: 115200}
