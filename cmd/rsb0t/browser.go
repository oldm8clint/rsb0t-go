package main

import (
	"net"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

// openWhenUp opens url in the default browser once the panel's port accepts connections (for at most
// half a minute), so the browser never lands on a refused connection.
func openWhenUp(url string) {
	host := strings.TrimSuffix(strings.TrimPrefix(url, "http://"), "/")
	for i := 0; i < 60; i++ {
		if c, err := net.DialTimeout("tcp", host, 300*time.Millisecond); err == nil {
			c.Close()
			openBrowser(url)
			return
		}
		time.Sleep(500 * time.Millisecond)
	}
}

func openBrowser(url string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	case "darwin":
		cmd = exec.Command("open", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	_ = cmd.Start() // best effort: the address is printed either way
}
