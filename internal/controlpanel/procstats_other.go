//go:build !windows

package controlpanel

// realProcessStats has no portable implementation here — this project targets Windows (see
// procstats_windows.go's own doc comment for why: matching what Task Manager shows was the
// whole point). 0, 0 is treated by callers as "unavailable," the same as a failed syscall on
// Windows itself, so a non-Windows build (e.g. CI) degrades to the Go-heap-only numbers this
// dashboard already showed before this feature existed rather than failing to build.
func realProcessStats() (rssBytes uint64, cpuPercent float64) {
	return 0, 0
}
