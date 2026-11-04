//go:build !windows

package fleet

// platformTransientLoginErrors is empty off Windows: there a reset socket is syscall.ECONNRESET, which retryableLoginError
// already lists.
var platformTransientLoginErrors []error
