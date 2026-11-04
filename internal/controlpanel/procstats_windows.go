//go:build windows

package controlpanel

import (
	"sync"
	"syscall"
	"time"
	"unsafe"
)

// procMemoryCounters mirrors Windows' own PROCESS_MEMORY_COUNTERS (psapi.h) closely enough
// for GetProcessMemoryInfo to fill in WorkingSetSize, the same "current resident memory"
// figure Task Manager's own default Memory column shows — distinct from its Peak working
// set column, and distinct from runtime.MemStats.Sys (the Go runtime's own view of memory
// it has obtained from the OS, not the OS's own account of this process). Live-found
// 2026-09-16: the account owner saw Task Manager report ~1GB against this dashboard's own
// much smaller Go-heap-only numbers and reasonably asked why — the 1GB was Task Manager's
// PEAK column from a transient login-storm spike, not the current cost, and this dashboard
// had no current-OS-memory or CPU% figure at all to show the actual, current, comparable
// number. Field layout matches the real Win32 struct exactly (uintptr-sized fields on the
// fields that are SIZE_T in the real struct); only the two used fields are named, the rest
// are padding to keep the struct the right total size for the syscall to write into safely.
type procMemoryCounters struct {
	cb                         uint32
	_                          uint32 // padding to align the uintptr fields below on amd64
	pageFaultCount             uint32
	_                          uint32
	peakWorkingSetSize         uintptr
	workingSetSize             uintptr
	quotaPeakPagedPoolUsage    uintptr
	quotaPagedPoolUsage        uintptr
	quotaPeakNonPagedPoolUsage uintptr
	quotaNonPagedPoolUsage     uintptr
	pagefileUsage              uintptr
	peakPagefileUsage          uintptr
}

var (
	psapi                   = syscall.NewLazyDLL("psapi.dll")
	kernel32                = syscall.NewLazyDLL("kernel32.dll")
	procGetProcessMemInfo   = psapi.NewProc("GetProcessMemoryInfo")
	procGetCurrentProcess   = kernel32.NewProc("GetCurrentProcess")
	procGetProcessTimes     = kernel32.NewProc("GetProcessTimes")
	procGetSystemInfo       = kernel32.NewProc("GetSystemInfo")
	currentProcessHandle, _ = getCurrentProcessHandle()
)

func getCurrentProcessHandle() (uintptr, error) {
	r, _, _ := procGetCurrentProcess.Call()
	return r, nil
}

// numCPU is read once via GetSystemInfo (dwNumberOfProcessors, the same field
// runtime.NumCPU() itself reads on Windows) so cpuPercent can normalize the OS's own
// per-core-summed CPU time into the familiar 0-100%-of-one-core-equivalent... actually
// normalized to 0-100% of TOTAL machine capacity (see cpuPercent's own doc comment for
// which convention this dashboard uses and why).
type systemInfo struct {
	_                           [4]byte // wProcessorArchitecture + wReserved, packed as one DWORD-sized field
	dwPageSize                  uint32
	lpMinimumApplicationAddress uintptr
	lpMaximumApplicationAddress uintptr
	dwActiveProcessorMask       uintptr
	dwNumberOfProcessors        uint32
	_                           uint32 // dwProcessorType
	dwAllocationGranularity     uint32
	_                           uint16 // wProcessorLevel
	_                           uint16 // wProcessorRevision
}

func numCPU() int {
	var si systemInfo
	procGetSystemInfo.Call(uintptr(unsafe.Pointer(&si)))
	if si.dwNumberOfProcessors == 0 {
		return 1
	}
	return int(si.dwNumberOfProcessors)
}

var cpuCores = numCPU()

// workingSetBytes returns this process's current resident memory (Task Manager's default
// "Memory" column, current not peak) via GetProcessMemoryInfo. 0 if the call fails for any
// reason (e.g. psapi.dll missing on a stripped-down Windows build) — callers treat 0 as
// "unavailable" rather than a real reading.
func workingSetBytes() uint64 {
	var pmc procMemoryCounters
	pmc.cb = uint32(unsafe.Sizeof(pmc))
	ret, _, _ := procGetProcessMemInfo.Call(currentProcessHandle, uintptr(unsafe.Pointer(&pmc)), uintptr(pmc.cb))
	if ret == 0 {
		return 0
	}
	return uint64(pmc.workingSetSize)
}

// filetimeToNs converts a Windows FILETIME (100ns ticks since 1601) to a plain int64 tick
// count — the epoch and unit don't matter here since only DIFFERENCES between two samples
// are ever used (see cpuTracker.sample).
func filetimeTicks(ft *syscall.Filetime) int64 {
	return int64(ft.HighDateTime)<<32 | int64(ft.LowDateTime)
}

// processCPUTicks returns this process's total kernel+user CPU time so far, in 100ns ticks,
// via GetProcessTimes. ok is false if the call fails.
func processCPUTicks() (ticks int64, ok bool) {
	var creation, exit, kernelTime, userTime syscall.Filetime
	ret, _, _ := procGetProcessTimes.Call(
		currentProcessHandle,
		uintptr(unsafe.Pointer(&creation)),
		uintptr(unsafe.Pointer(&exit)),
		uintptr(unsafe.Pointer(&kernelTime)),
		uintptr(unsafe.Pointer(&userTime)),
	)
	if ret == 0 {
		return 0, false
	}
	return filetimeTicks(&kernelTime) + filetimeTicks(&userTime), true
}

// cpuTracker turns processCPUTicks' cumulative counter into a percentage-of-machine
// figure, sampled at whatever cadence the caller polls at (the control panel's own memory
// tick, currently every second — see hub.go). A package-level singleton, not per-request
// state, since "percent CPU used since the LAST sample" only means something against a
// consistent sampling interval.
type cpuTracker struct {
	mu       sync.Mutex
	lastTime time.Time
	lastCPU  int64
	lastPct  float64
}

var globalCPUTracker cpuTracker

// percent returns CPU used since the previous call as a percentage of TOTAL machine
// capacity (i.e. divided across all cores — 100% means every core saturated, matching Task
// Manager's own Processes-tab convention, not the "can exceed 100% on multi-core" convention
// top/htop use) — chosen because that's the number a reader comparing against Task Manager
// itself (this whole feature's own motivating question) will actually recognize. Returns the
// previous reading, unchanged, on the very first call (nothing to diff against yet) or if
// either syscall fails.
func (c *cpuTracker) percent() float64 {
	ticks, ok := processCPUTicks()
	if !ok {
		return 0
	}
	now := time.Now()
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.lastTime.IsZero() {
		c.lastTime, c.lastCPU = now, ticks
		return 0
	}
	elapsed := now.Sub(c.lastTime)
	if elapsed <= 0 {
		return c.lastPct
	}
	cpuDeltaNs := float64(ticks-c.lastCPU) * 100 // FILETIME ticks are 100ns each
	pct := cpuDeltaNs / float64(elapsed.Nanoseconds()) / float64(cpuCores) * 100
	if pct < 0 {
		pct = 0
	}
	c.lastTime, c.lastCPU, c.lastPct = now, ticks, pct
	return pct
}

// realProcessStats is RuntimeMemStats' OS-level counterpart (source.go) — see MemStats'
// own doc comment for why both exist side by side.
func realProcessStats() (rssBytes uint64, cpuPercent float64) {
	return workingSetBytes(), globalCPUTracker.percent()
}
