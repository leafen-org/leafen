//go:build windows

package main

import (
	"errors"
	"fmt"
	"runtime"
	"sync"
	"syscall"
	"time"
	"unsafe"
)

// The Microsoft Store's app is the Windows program in an MSIX package
// (scripts/package-desktop.mjs --msix, desktop/build/windows/AppxManifest.xml),
// and runs with the package's identity: Windows keeps its files read-only and
// the store updates it (updater.go); it keeps its data where the program
// unpackaged does, %APPDATA%\Leafen, which the manifest leaves outside the
// package's own storage, so removing the app leaves the data; and it opens at
// login by the startup task the manifest declares, which Windows starts with
// "--background", in place of the Run key (background_windows.go). The task
// is read and switched through WinRT's Windows.ApplicationModel.StartupTask,
// with the COM helpers of the owner check (device_owner_windows.go).

// startupTaskID is the manifest's startup task.
const startupTaskID = "LeafenAtLogin"

const (
	errorInsufficientBuffer = 122
	startupTaskWait         = 10 * time.Second
)

// The startup task's states (Windows.ApplicationModel.StartupTaskState).
const (
	startupTaskDisabled = iota
	startupTaskDisabledByUser
	startupTaskEnabled
	startupTaskDisabledByPolicy
	startupTaskEnabledByPolicy
)

var (
	procGetCurrentPackageFullName = kernel32.NewProc("GetCurrentPackageFullName")
	iidStartupTaskStatics         = mustGUID("{EE5B60BD-A148-41A7-B26E-E8B88A1E62F8}")
)

// packagedApp says whether the app runs from its package: only a packaged
// process has a package name to give.
var packagedApp = sync.OnceValue(func() bool {
	var length uint32
	result, _, _ := procGetCurrentPackageFullName.Call(uintptr(unsafe.Pointer(&length)), 0)
	return result == errorInsufficientBuffer
})

// startupTaskState is the login item's state for the window: "needs-approval"
// when the person turned Leafen off in Windows' startup apps, which only they
// can turn back on there.
func startupTaskState() string {
	state := -1
	err := withStartupTask(func(task unsafe.Pointer) error {
		var value int32
		if hr, _, _ := syscall.SyscallN(comMethod(task, 8), uintptr(task), uintptr(unsafe.Pointer(&value))); failed(hr) {
			return hresultError(hr)
		}
		state = int(value)
		return nil
	})
	switch {
	case err != nil || state == startupTaskDisabledByPolicy:
		return "unavailable"
	case state == startupTaskEnabled || state == startupTaskEnabledByPolicy:
		return "on"
	case state == startupTaskDisabledByUser:
		return "needs-approval"
	}
	return "off"
}

// setStartupTask turns the startup task on or off.
func setStartupTask(on bool) error {
	return withStartupTask(func(task unsafe.Pointer) error {
		if !on {
			if hr, _, _ := syscall.SyscallN(comMethod(task, 7), uintptr(task)); failed(hr) {
				return hresultError(hr)
			}
			return nil
		}
		var operation unsafe.Pointer
		if hr, _, _ := syscall.SyscallN(comMethod(task, 6), uintptr(task), uintptr(unsafe.Pointer(&operation))); failed(hr) {
			return hresultError(hr)
		}
		defer comRelease(operation)
		if err := awaitAsync(operation); err != nil {
			return err
		}
		var state int32
		if hr, _, _ := syscall.SyscallN(comMethod(operation, 8), uintptr(operation), uintptr(unsafe.Pointer(&state))); failed(hr) {
			return hresultError(hr)
		}
		if state != startupTaskEnabled && state != startupTaskEnabledByPolicy {
			return errors.New("windows kept Leafen from opening at login")
		}
		return nil
	})
}

// withStartupTask hands the manifest's startup task to use, on a thread of
// its own with WinRT started there.
func withStartupTask(use func(task unsafe.Pointer) error) error {
	done := make(chan error, 1)
	go func() {
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		done <- func() error {
			// A thread that already took another apartment still serves.
			if hr, _, _ := procRoInitialize.Call(roInitMultithreaded); !failed(hr) {
				defer func() { _, _, _ = procRoUninitialize.Call() }()
			} else if uint32(hr) != rpcChangedMode {
				return hresultError(hr)
			}
			class, freeClass, err := hstring("Windows.ApplicationModel.StartupTask")
			if err != nil {
				return err
			}
			defer freeClass()
			var statics unsafe.Pointer
			if hr, _, _ := procRoGetActivationFactory.Call(class, uintptr(unsafe.Pointer(&iidStartupTaskStatics)), uintptr(unsafe.Pointer(&statics))); failed(hr) {
				return hresultError(hr)
			}
			defer comRelease(statics)
			id, freeID, err := hstring(startupTaskID)
			if err != nil {
				return err
			}
			defer freeID()
			var operation unsafe.Pointer
			if hr, _, _ := syscall.SyscallN(comMethod(statics, 7), uintptr(statics), id, uintptr(unsafe.Pointer(&operation))); failed(hr) {
				return hresultError(hr)
			}
			defer comRelease(operation)
			if err := awaitAsync(operation); err != nil {
				return err
			}
			var task unsafe.Pointer
			if hr, _, _ := syscall.SyscallN(comMethod(operation, 8), uintptr(operation), uintptr(unsafe.Pointer(&task))); failed(hr) {
				return hresultError(hr)
			}
			if task == nil {
				return errors.New("the package declares no startup task")
			}
			defer comRelease(task)
			return use(task)
		}()
	}()
	return <-done
}

// awaitAsync waits for a WinRT operation to finish, asking its IAsyncInfo,
// for at most startupTaskWait.
func awaitAsync(operation unsafe.Pointer) error {
	var info unsafe.Pointer
	if hr, _, _ := syscall.SyscallN(comMethod(operation, 0), uintptr(operation), uintptr(unsafe.Pointer(&iidAsyncInfo)), uintptr(unsafe.Pointer(&info))); failed(hr) {
		return hresultError(hr)
	}
	defer comRelease(info)
	deadline := time.Now().Add(startupTaskWait)
	for {
		var status int32
		if hr, _, _ := syscall.SyscallN(comMethod(info, 7), uintptr(info), uintptr(unsafe.Pointer(&status))); failed(hr) {
			return hresultError(hr)
		}
		if status == asyncCompleted {
			return nil
		}
		if status != asyncStarted {
			var code int32
			syscall.SyscallN(comMethod(info, 8), uintptr(info), uintptr(unsafe.Pointer(&code)))
			return fmt.Errorf("the startup task's operation ended without a result (%d, 0x%08x)", status, uint32(code))
		}
		if time.Now().After(deadline) {
			return errors.New("windows didn't answer about the startup task")
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// comMethod is the method at index in a COM object's table, called here
// with syscall.SyscallN directly, so the addresses passed stay valid.
func comMethod(object unsafe.Pointer, index uintptr) uintptr {
	table := *(*unsafe.Pointer)(object)
	return *(*uintptr)(unsafe.Add(table, index*unsafe.Sizeof(uintptr(0))))
}

func failed(hr uintptr) bool { return int32(uint32(hr)) < 0 }

func hresultError(hr uintptr) error { return fmt.Errorf("windows error 0x%08x", uint32(hr)) }
