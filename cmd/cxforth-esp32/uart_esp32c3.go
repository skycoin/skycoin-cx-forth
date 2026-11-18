//go:build esp32c3

package main

import "machine"

// The C3 UART driver rejects the zero TX and RX pins, so name UART0's pins.
var uartConfig = machine.UARTConfig{BaudRate: 115200, TX: machine.GPIO21, RX: machine.GPIO20}
