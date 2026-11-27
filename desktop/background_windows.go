//go:build windows

package main

import (
	"errors"
	"os"
	"runtime"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

// Windows opens the app at sign-in in the background: the program
// unpackaged from the user's Run key, the store's package by its startup task
// (packaged_windows.go).
const runKey = `Software\Microsoft\Windows\CurrentVersion\Run`

func loginItemState() string {
	if packagedApp() {
		return startupTaskState()
	}
	key, err := registry.OpenKey(registry.CURRENT_USER, runKey, registry.QUERY_VALUE)
	if err != nil {
		return "off"
	}
	defer key.Close()
	if _, _, err := key.GetStringValue("Leafen"); err == nil {
		return "on"
	}
	return "off"
}

func setLoginItem(on bool) error {
	if packagedApp() {
		return setStartupTask(on)
	}
	key, err := registry.OpenKey(registry.CURRENT_USER, runKey, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer key.Close()
	if !on {
		if err := key.DeleteValue("Leafen"); err != nil && !errors.Is(err, registry.ErrNotExist) {
			return err
		}
		return nil
	}
	executable, err := os.Executable()
	if err != nil {
		return err
	}
	return key.SetStringValue("Leafen", `"`+executable+`" --background`)
}

var (
	kernel32                  = windows.NewLazySystemDLL("kernel32.dll")
	procSetThreadExecution    = kernel32.NewProc("SetThreadExecutionState")
	procGetSystemPowerStatus  = kernel32.NewProc("GetSystemPowerStatus")
	esContinuous, esSystemReq = uintptr(0x80000000), uintptr(0x00000001)
)

func onBatteryPower() bool {
	var status struct {
		ACLineStatus, BatteryFlag, BatteryLifePercent, SystemStatusFlag byte
		BatteryLifeTime, BatteryFullLifeTime                            uint32
	}
	if result, _, _ := procGetSystemPowerStatus.Call(uintptr(unsafe.Pointer(&status))); result == 0 {
		return false
	}
	return status.ACLineStatus == 0
}

// keepAwake asks Windows to keep the system awake from a thread of its own,
// which holds the request until it is released; the display still sleeps.
type keepAwake struct{ stop chan struct{} }

func (awake *keepAwake) hold(string) error {
	if awake.stop != nil {
		return nil
	}
	awake.stop = make(chan struct{})
	started := make(chan error, 1)
	go func(stop chan struct{}) {
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		if result, _, err := procSetThreadExecution.Call(esContinuous | esSystemReq); result == 0 {
			started <- err
			return
		}
		started <- nil
		<-stop
		procSetThreadExecution.Call(esContinuous)
	}(awake.stop)
	if err := <-started; err != nil {
		awake.stop = nil
		return err
	}
	return nil
}

func (awake *keepAwake) release() {
	if awake.stop != nil {
		close(awake.stop)
		awake.stop = nil
	}
}

// setMenuBarIcon is a Mac's (tray_darwin.go): Windows draws the tray's icon
// at the system's small icon size, as it does Notion's.
func setMenuBarIcon([]byte) bool { return false }
