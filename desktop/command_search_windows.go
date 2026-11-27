//go:build windows

package main

import (
	"log"
	"runtime"
	"strconv"
	"sync"
	"unsafe"

	"golang.org/x/sys/windows"
)

// The Windows app's command search key, Ctrl+Shift+K as Notion's Windows
// app's or the one chosen: RegisterHotKey on a thread of its own, whose
// message loop hears the key while another app is in front; a change of key
// is posted to that thread, which alone may give the key back
// (command_search_desktop.go). The app's search window centers on the
// screen under the pointer, as Notion's.

var (
	user32                 = windows.NewLazySystemDLL("user32.dll")
	procRegisterHotKey     = user32.NewProc("RegisterHotKey")
	procUnregisterHotKey   = user32.NewProc("UnregisterHotKey")
	procGetMessageW        = user32.NewProc("GetMessageW")
	procPeekMessageW       = user32.NewProc("PeekMessageW")
	procPostThreadMessageW = user32.NewProc("PostThreadMessageW")
	procGetCursorPos       = user32.NewProc("GetCursorPos")
	procMonitorFromPoint   = user32.NewProc("MonitorFromPoint")
	procGetMonitorInfoW    = user32.NewProc("GetMonitorInfoW")
	procGetWindowRect      = user32.NewProc("GetWindowRect")
	procSetWindowPos       = user32.NewProc("SetWindowPos")
)

const (
	wmHotKey           = 0x0312
	wmCommandSearchKey = 0x8000 + 0x4C // WM_APP + 'L'
	modAlt             = 0x0001
	modControl         = 0x0002
	modShift           = 0x0004
	modNoRepeat        = 0x4000
	commandSearchID    = 0x4C66 // 'Lf'
)

// hotKeyCodes are the keys' virtual-key codes, by the names Notion's setting
// writes them with.
var hotKeyCodes = map[string]int{
	"space": 0x20, "return": 0x0D, "backspace": 0x08, "delete": 0x2E,
	"left": 0x25, "up": 0x26, "right": 0x27, "down": 0x28,
	";": 0xBA, "=": 0xBB, ",": 0xBC, "-": 0xBD, ".": 0xBE, "/": 0xBF, "`": 0xC0,
	"[": 0xDB, "\\": 0xDC, "]": 0xDD, "'": 0xDE,
}

func init() {
	for c := 'a'; c <= 'z'; c++ {
		hotKeyCodes[string(c)] = int('A' + (c - 'a'))
	}
	for c := '0'; c <= '9'; c++ {
		hotKeyCodes[string(c)] = int(c)
	}
	for n := 1; n <= 12; n++ {
		hotKeyCodes["f"+strconv.Itoa(n)] = 0x6F + n
	}
}

// commandSearchThread is the thread holding the key, started with the first
// key taken.
var commandSearchThread struct {
	once    sync.Once
	id      uint32
	ready   chan struct{}
	mu      sync.Mutex
	want    hotKey
	on      bool
	answers chan bool
}

type winMsg struct {
	hwnd    uintptr
	message uint32
	wParam  uintptr
	lParam  uintptr
	time    uint32
	x, y    int32
	private uint32
}

// setCommandSearchKey takes the key for the app, or gives it back; false
// when the system refused it (another app holds it).
func setCommandSearchKey(key hotKey, on bool) bool {
	thread := &commandSearchThread
	thread.once.Do(func() {
		thread.ready = make(chan struct{})
		thread.answers = make(chan bool, 1)
		go holdCommandSearchKey()
		<-thread.ready
	})
	thread.mu.Lock()
	defer thread.mu.Unlock()
	thread.want, thread.on = key, on
	if ok, _, _ := procPostThreadMessageW.Call(uintptr(thread.id), wmCommandSearchKey, 0, 0); ok == 0 {
		return false
	}
	taken := <-thread.answers
	if !taken {
		log.Printf("command search: the system refused %s", key.key)
	}
	return taken
}

// holdCommandSearchKey runs the key's thread: it takes and gives back the
// key as asked and hears it pressed.
func holdCommandSearchKey() {
	runtime.LockOSThread()
	thread := &commandSearchThread
	thread.id = windows.GetCurrentThreadId()
	var msg winMsg
	// The thread's message queue is made by its first look at it.
	procPeekMessageW.Call(uintptr(unsafe.Pointer(&msg)), 0, 0, 0, 0)
	close(thread.ready)
	var held hotKey
	holding := false
	register := func(key hotKey) bool {
		modifiers := uintptr(modNoRepeat)
		if key.control {
			modifiers |= modControl
		}
		if key.option {
			modifiers |= modAlt
		}
		if key.shift {
			modifiers |= modShift
		}
		ok, _, _ := procRegisterHotKey.Call(0, commandSearchID, modifiers, uintptr(hotKeyCodes[key.key]))
		return ok != 0
	}
	for {
		got, _, _ := procGetMessageW.Call(uintptr(unsafe.Pointer(&msg)), 0, 0, 0)
		if int32(got) <= 0 {
			return
		}
		switch msg.message {
		case wmHotKey:
			if msg.wParam == commandSearchID {
				go commandSearchPressed(false)
			}
		case wmCommandSearchKey:
			want, on := thread.want, thread.on
			if holding {
				procUnregisterHotKey.Call(0, commandSearchID)
				holding = false
			}
			taken := true
			if on {
				if register(want) {
					held, holding = want, true
				} else {
					taken = false
					if held.key != "" && register(held) {
						holding = true
					}
				}
			}
			thread.answers <- taken
		}
	}
}

// centerOnPointerScreen puts a window in the middle of the work area of the
// screen under the pointer.
func centerOnPointerScreen(native unsafe.Pointer) {
	if native == nil {
		return
	}
	var pointer [2]int32
	if ok, _, _ := procGetCursorPos.Call(uintptr(unsafe.Pointer(&pointer))); ok == 0 {
		return
	}
	monitor, _, _ := procMonitorFromPoint.Call(uintptr(uint32(pointer[0]))|uintptr(uint32(pointer[1]))<<32, 2)
	info := struct {
		size          uint32
		monitor, work [4]int32
		flags         uint32
	}{size: 40}
	if ok, _, _ := procGetMonitorInfoW.Call(monitor, uintptr(unsafe.Pointer(&info))); ok == 0 {
		return
	}
	var frame [4]int32
	if ok, _, _ := procGetWindowRect.Call(uintptr(native), uintptr(unsafe.Pointer(&frame))); ok == 0 {
		return
	}
	width, height := frame[2]-frame[0], frame[3]-frame[1]
	x := info.work[0] + (info.work[2]-info.work[0]-width)/2
	y := info.work[1] + (info.work[3]-info.work[1]-height)/2
	// SWP_NOSIZE | SWP_NOZORDER | SWP_NOACTIVATE
	procSetWindowPos.Call(uintptr(native), 0, uintptr(x), uintptr(y), 0, 0, 0x0001|0x0004|0x0010)
}

// bringAppForward: a window's Focus brings it forward on Windows.
func bringAppForward() {}
