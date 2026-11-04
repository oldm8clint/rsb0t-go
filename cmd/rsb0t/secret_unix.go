//go:build !windows

package main

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// readSecret prints prompt and reads one line with the terminal's echo off (stty, where there is a terminal).
func readSecret(prompt string) (string, error) {
	fmt.Fprint(os.Stderr, prompt)
	stty := func(arg string) bool {
		c := exec.Command("stty", arg)
		c.Stdin = os.Stdin
		return c.Run() == nil
	}
	quiet := stty("-echo")
	if !quiet {
		fmt.Fprintln(os.Stderr)
		return "", errNoTerminal
	}
	defer stty("echo")
	line, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if quiet {
		fmt.Fprintln(os.Stderr)
	}
	if err != nil && line == "" {
		return "", err
	}
	return strings.TrimRight(line, "\r\n"), nil
}
