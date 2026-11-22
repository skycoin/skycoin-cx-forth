//go:build esp32c3

package main

import "machine"

// The C3 UART driver rejects the zero TX and RX pins, so name UART0's pins.
var uartConfig = machine.UARTConfig{BaudRate: 115200, TX: machine.GPIO21, RX: machine.GPIO20}

// GPIO 20 and 21 are UART0.
var gpioAllowed = []int{2, 3, 4, 5, 6, 7, 8, 9, 10}
