//go:build !leafen_noapps && windows

package app

import (
	"unsafe"

	"golang.org/x/sys/windows"
)

// appWorkerJob keeps the worker's job object open for its life; when the
// worker ends, so does the job.
var appWorkerJob windows.Handle

// limitAppWorkerProcess puts the worker process in a job object of its own:
// one process (it starts no others), at most 1 GB of committed memory, and
// killed with the job; it also runs below the server's priority.
func limitAppWorkerProcess() {
	_ = windows.SetPriorityClass(windows.CurrentProcess(), windows.BELOW_NORMAL_PRIORITY_CLASS)
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return
	}
	limits := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{}
	limits.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_ACTIVE_PROCESS | windows.JOB_OBJECT_LIMIT_PROCESS_MEMORY |
		windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE | windows.JOB_OBJECT_LIMIT_DIE_ON_UNHANDLED_EXCEPTION
	limits.BasicLimitInformation.ActiveProcessLimit = 1
	limits.ProcessMemoryLimit = 1 << 30
	if _, err := windows.SetInformationJobObject(job, windows.JobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(&limits)), uint32(unsafe.Sizeof(limits))); err != nil {
		_ = windows.CloseHandle(job)
		return
	}
	if err := windows.AssignProcessToJobObject(job, windows.CurrentProcess()); err != nil {
		_ = windows.CloseHandle(job)
		return
	}
	appWorkerJob = job
}
