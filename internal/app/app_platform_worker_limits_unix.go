//go:build !leafen_noapps && unix

package app

import (
	"runtime"
	"syscall"
)

// limitAppWorkerProcess narrows the worker process itself: few open files,
// a lower priority than the server and, where the system holds to it, a
// ceiling on its address space.
func limitAppWorkerProcess() {
	_ = syscall.Setrlimit(syscall.RLIMIT_NOFILE, &syscall.Rlimit{Cur: 64, Max: 64})
	_ = syscall.Setpriority(syscall.PRIO_PROCESS, 0, 10)
	if runtime.GOOS == "linux" {
		_ = syscall.Setrlimit(syscall.RLIMIT_AS, &syscall.Rlimit{Cur: 4 << 30, Max: 4 << 30})
	}
}
