//go:build windows

package main

import (
	"strings"
	"unsafe"

	"github.com/wailsapp/wails/v3/pkg/w32"
	"golang.org/x/sys/windows"
)

// The frame's buttons name themselves when the pointer rests on one, as the
// buttons of Notion 7.44.1's Windows app (Electron's title bar overlay) do:
// the system's own tip — the program's manifest asks for the common
// controls 6, whose tip it is (build/windows/winres.json) — "최소화",
// "최대화" or "복원", "닫기", half a second after the pointer comes, 10 right
// and 15 down from it (measured beside Notion's), gone as it leaves or
// presses. The buttons are the title bar's, so their pointer is followed
// here rather than by the tip's own.

const (
	captionTipTimer = 0x4c54 // "LT"
	captionTipDelay = 500
)

// toolInfo is TTTOOLINFOW.
type toolInfo struct {
	size     uint32
	flags    uint32
	hwnd     w32.HWND
	id       uintptr
	rect     w32.RECT
	instance uintptr
	text     *uint16
	param    uintptr
	reserved uintptr
}

const (
	toolTrack    = 0x0020 // TTF_TRACK
	toolAbsolute = 0x0080 // TTF_ABSOLUTE
)

// captionTips are each frame's tip, made the first time it shows.
var captionTips = map[w32.HWND]w32.HWND{}

// pointerOverCaption follows the pointer over the frame's title bar: the
// button under it lights, and its tip comes once it rests there.
func (chrome *frameChrome) pointerOverCaption(hwnd w32.HWND, hit uintptr) {
	hover := uintptr(0)
	if isButtonHit(hit) {
		hover = hit
	}
	if hover != chrome.hover {
		chrome.hover = hover
		w32.InvalidateRect(hwnd, captionRect(hwnd), false)
		hideCaptionTip(hwnd)
		if hover != 0 && chrome.pressed == 0 {
			w32.SetTimer(hwnd, captionTipTimer, captionTipDelay, 0)
		}
	}
	if !chrome.tracking {
		chrome.tracking = w32.TrackMouseEvent(&w32.TRACKMOUSEEVENT{CbSize: uint32(unsafe.Sizeof(w32.TRACKMOUSEEVENT{})),
			DwFlags: w32.TME_LEAVE | w32.TME_NONCLIENT, HwndTrack: hwnd})
	}
}

// pointerLeftCaption puts the buttons and the tip back as the pointer leaves.
func (chrome *frameChrome) pointerLeftCaption(hwnd w32.HWND) {
	chrome.tracking = false
	hideCaptionTip(hwnd)
	if chrome.hover != 0 || chrome.pressed != 0 {
		chrome.hover, chrome.pressed = 0, 0
		w32.InvalidateRect(hwnd, captionRect(hwnd), false)
	}
}

// showCaptionTip shows the tip of the button the pointer rests on.
func (chrome *frameChrome) showCaptionTip(hwnd w32.HWND) {
	w32.KillTimer(hwnd, captionTipTimer)
	if chrome.hover == 0 || chrome.pressed != 0 {
		return
	}
	x, y, ok := w32.GetCursorPos()
	if !ok {
		return
	}
	tip := captionTips[hwnd]
	if tip == 0 {
		controls := w32.INITCOMMONCONTROLSEX{DwICC: w32.ICC_WIN95_CLASSES}
		controls.DwSize = uint32(unsafe.Sizeof(controls))
		w32.InitCommonControlsEx(&controls)
		tip = w32.CreateWindowEx(w32.WS_EX_TOPMOST, windows.StringToUTF16Ptr("tooltips_class32"), nil,
			w32.WS_POPUP|w32.TTS_NOPREFIX|w32.TTS_ALWAYSTIP, 0, 0, 0, 0, hwnd, 0, 0, nil)
		if tip == 0 {
			return
		}
		captionTips[hwnd] = tip
		info := captionToolInfo(hwnd, "")
		w32.SendMessage(tip, w32.TTM_ADDTOOL, 0, uintptr(unsafe.Pointer(&info)))
	}
	info := captionToolInfo(hwnd, captionTipText(hwnd, chrome.hover))
	w32.SendMessage(tip, w32.TTM_UPDATETIPTEXT, 0, uintptr(unsafe.Pointer(&info)))
	scale := dpiScale(hwnd)
	at := uintptr(uint16(int32(x)+int32(10*scale))) | uintptr(uint16(int32(y)+int32(15*scale)))<<16
	w32.SendMessage(tip, w32.TTM_TRACKPOSITION, 0, at)
	w32.SendMessage(tip, w32.TTM_TRACKACTIVATE, 1, uintptr(unsafe.Pointer(&info)))
}

// hideCaptionTip takes the tip away, and the one coming.
func hideCaptionTip(hwnd w32.HWND) {
	w32.KillTimer(hwnd, captionTipTimer)
	if tip := captionTips[hwnd]; tip != 0 {
		info := captionToolInfo(hwnd, "")
		w32.SendMessage(tip, w32.TTM_TRACKACTIVATE, 0, uintptr(unsafe.Pointer(&info)))
	}
}

// forgetCaptionTip ends the frame's tip with it.
func forgetCaptionTip(hwnd w32.HWND) {
	if tip := captionTips[hwnd]; tip != 0 {
		w32.DestroyWindow(tip)
		delete(captionTips, hwnd)
	}
}

func captionToolInfo(hwnd w32.HWND, text string) toolInfo {
	info := toolInfo{flags: toolTrack | toolAbsolute, hwnd: hwnd, id: 1}
	info.size = uint32(unsafe.Sizeof(info))
	if text != "" {
		info.text = windows.StringToUTF16Ptr(text)
	}
	return info
}

// captionTipText is a button's name in the window's language, as
// Chromium's title bar buttons say it.
func captionTipText(hwnd w32.HWND, hit uintptr) string {
	desktopBackground.mu.Lock()
	korean := strings.HasPrefix(desktopBackground.language, "ko")
	desktopBackground.mu.Unlock()
	say := func(english, hangul string) string {
		if korean {
			return hangul
		}
		return english
	}
	switch hit {
	case w32.HTMINBUTTON:
		return say("Minimize", "최소화")
	case w32.HTMAXBUTTON:
		if w32.IsZoomed(hwnd) {
			return say("Restore", "복원")
		}
		return say("Maximize", "최대화")
	}
	return say("Close", "닫기")
}
