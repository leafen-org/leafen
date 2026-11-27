//go:build windows && arm64

package main

import (
	"encoding/binary"
	"math"
	"sync"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// On arm64 Windows a double goes to a method in the first floating-point
// register, d0, which Go's calls to the system leave as they are
// (golang.org/issue/62583: they pass integer registers alone), so the page's
// size can't reach put_ZoomFactor that way. A bridge of two instructions,
// made once in memory the system lets run, takes the call instead: it moves
// the double's bits from the second argument's register into d0 and jumps
// on to the method, the controller staying the first argument and the
// method returning straight to Go's call.
var zoomBridge = sync.OnceValue(func() uintptr {
	code := []uint32{
		0x9e670020, // fmov d0, x1
		0xd61f0040, // br x2
	}
	address, err := windows.VirtualAlloc(0, uintptr(len(code)*4), windows.MEM_COMMIT|windows.MEM_RESERVE, windows.PAGE_READWRITE)
	if err != nil || address == 0 {
		return 0
	}
	bytes := make([]byte, len(code)*4)
	for index, instruction := range code {
		binary.LittleEndian.PutUint32(bytes[index*4:], instruction)
	}
	if windows.WriteProcessMemory(windows.CurrentProcess(), address, &bytes[0], uintptr(len(bytes)), nil) != nil {
		return 0
	}
	var before uint32
	if windows.VirtualProtect(address, uintptr(len(code)*4), windows.PAGE_EXECUTE_READ, &before) != nil {
		return 0
	}
	flush := windows.NewLazySystemDLL("kernel32.dll").NewProc("FlushInstructionCache")
	if flush.Find() == nil {
		flush.Call(uintptr(windows.CurrentProcess()), address, uintptr(len(code)*4))
	}
	return address
})

// putZoomFactor gives a WebView2 controller its page's size through the bridge.
func putZoomFactor(controller *comObject, zoom float64) bool {
	bridge := zoomBridge()
	if bridge == 0 {
		return false
	}
	syscall.SyscallN(bridge, uintptr(unsafe.Pointer(controller)), uintptr(math.Float64bits(zoom)), controller.method(controllerPutZoomFactor))
	return true
}
