//go:build windows

package fleet

import (
	"fmt"
	"net"
	"os"
	"syscall"
	"testing"
)

// A login whose proxy cuts the connection during the final read is retried, not retired, on Windows too.
func TestAWindowsConnectionResetDuringLoginIsRetried(t *testing.T) {
	for _, code := range []syscall.Errno{10054, 10053} {
		err := fmt.Errorf("login: reading final response: %w", &net.OpError{Op: "read", Net: "tcp", Err: os.NewSyscallError("wsarecv", code)})
		if !retryableLoginError(err) {
			t.Errorf("%v is retired as terminal", err)
		}
	}
}
