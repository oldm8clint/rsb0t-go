//go:build windows

package fleet

import "syscall"

// platformTransientLoginErrors are the socket errors that only Windows reports. A proxy that cuts a connection mid-login reaches
// Go as WSAECONNRESET or WSAECONNABORTED (10054, 10053), and errors.Is never matches those against syscall.ECONNRESET, which is
// an invented errno on Windows: on 19 Oct 2026 a ScarabSwarm account whose Psiphon tunnel reset during the login read ("wsarecv:
// An existing connection was forcibly closed by the remote host") was retired with "login failure requires attention" and sat
// out of game for more than seven hours.
var platformTransientLoginErrors = []error{syscall.WSAECONNRESET, syscall.WSAECONNABORTED}
