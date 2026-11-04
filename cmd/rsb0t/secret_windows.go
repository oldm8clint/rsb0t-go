//go:build windows

package main

import (
	"bufio"
	"fmt"
	"os"
	"strings"
	"syscall"
	"unsafe"
)

// readSecret prints prompt and reads one line with the console's echo off.
func readSecret(prompt string) (string, error) {
	k32 := syscall.NewLazyDLL("kernel32.dll")
	get, set := k32.NewProc("GetConsoleMode"), k32.NewProc("SetConsoleMode")
	h := os.Stdin.Fd()
	var mode uint32
	isConsole := false
	if r, _, _ := get.Call(h, uintptr(unsafe.Pointer(&mode))); r != 0 {
		isConsole = true
		const enableEcho = 0x0004
		set.Call(h, uintptr(mode&^enableEcho))
		defer set.Call(h, uintptr(mode))
	}
	if !isConsole {
		return "", errNoTerminal
	}
	fmt.Fprint(os.Stderr, prompt)
	line, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if isConsole {
		fmt.Fprintln(os.Stderr)
	}
	if err != nil && line == "" {
		return "", err
	}
	return strings.TrimRight(line, "\r\n"), nil
}
