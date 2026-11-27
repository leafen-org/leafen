//go:build windows

package main

import (
	"strings"
	"syscall"
	"unsafe"

	"github.com/wailsapp/wails/v3/pkg/w32"
	"golang.org/x/sys/windows"
)

// The app's menu on Windows, drawn as Notion 7.44.1's Windows app's menu
// (Chromium's own menu, measured beside it 2026-11-24, dark): the system's
// menu, with its keys and its way through submenus, but each line drawn
// here — 28 high, its words 22 in, its keys and a thin chevron at its end,
// lit #363635 across the whole width under the pointer or the keys — on
// #1f1f1f, 12 above the first line and below the last, a 1px #5e5e5e line
// across in 17 between groups, the menu's corners rounded and no border.

// drawnItem is a line of the menu as it is drawn.
type drawnItem struct {
	label, keys string
	off, sub    bool
	line, edge  bool
}

const (
	drawnRow       = 28
	drawnLineRoom  = 17
	drawnEdge      = 12
	drawnTextStart = 22
	drawnEndRoom   = 18
	drawnKeysGap   = 32
	drawnArrowRoom = 26
)

// The menu's colors, dark and light (light as Chromium's menu, not measured
// beside Notion's).
type drawnColors struct{ ground, lit, text, keys, off, line rgb }

var (
	drawnDark  = drawnColors{ground: rgb{0x1f, 0x1f, 0x1f}, lit: rgb{0x36, 0x36, 0x35}, text: rgb{0xe3, 0xe3, 0xe3}, keys: rgb{0x9b, 0x9b, 0x9b}, off: rgb{0x6e, 0x6e, 0x6e}, line: rgb{0x5e, 0x5e, 0x5e}}
	drawnLight = drawnColors{ground: rgb{0xff, 0xff, 0xff}, lit: rgb{0xe8, 0xe8, 0xe8}, text: rgb{0x1f, 0x1f, 0x1f}, keys: rgb{0x6e, 0x6e, 0x6e}, off: rgb{0xa0, 0xa0, 0xa0}, line: rgb{0xe0, 0xe0, 0xe0}}
)

// drawnMenu is the menu open now: its lines, by the index each carries.
var drawnMenu struct {
	items  []drawnItem
	colors drawnColors
	font   w32.HFONT
}

var (
	user32Drawn          = windows.NewLazySystemDLL("user32.dll")
	procAppendMenuDrawn  = user32Drawn.NewProc("AppendMenuW")
	procSetHookDrawn     = user32Drawn.NewProc("SetWindowsHookExW")
	procUnhookDrawn      = user32Drawn.NewProc("UnhookWindowsHookEx")
	procCallNextDrawn    = user32Drawn.NewProc("CallNextHookEx")
	procSystemParamDrawn = user32Drawn.NewProc("SystemParametersInfoW")
	drawnHook            uintptr
	drawnHookProc        = syscall.NewCallback(drawnMenuHook)
)

// popupMenu shows entries as the app's menu and runs the one chosen.
func popupMenu(hwnd w32.HWND, entries []menuEntry, x, y int) {
	var actions []func()
	drawnMenu.items = drawnMenu.items[:0]
	drawnMenu.colors = drawnLight
	if desktopTabs.look.Dark {
		drawnMenu.colors = drawnDark
	}
	ground := w32.CreateSolidBrush(drawnMenu.colors.ground.ref())
	defer w32.DeleteObject(w32.HGDIOBJ(ground))
	drawnMenu.font = menuFont(hwnd)
	defer w32.DeleteObject(w32.HGDIOBJ(drawnMenu.font))
	add := func(menu w32.HMENU, flags uint32, id uintptr, item drawnItem) {
		drawnMenu.items = append(drawnMenu.items, item)
		procAppendMenuDrawn.Call(uintptr(menu), uintptr(flags|w32.MF_OWNERDRAW), id, uintptr(len(drawnMenu.items)-1))
	}
	var build func([]menuEntry) w32.HMENU
	build = func(entries []menuEntry) w32.HMENU {
		menu := w32.HMENU(w32.CreatePopupMenu())
		info := w32.MENUINFO{FMask: w32.MIM_BACKGROUND, HbrBack: ground}
		info.CbSize = uint32(unsafe.Sizeof(info))
		w32.SetMenuInfo(menu, &info)
		add(menu, w32.MF_SEPARATOR, 0, drawnItem{edge: true})
		for _, entry := range entries {
			flags := uint32(w32.MF_STRING)
			if entry.off {
				flags |= w32.MF_GRAYED
			}
			item := drawnItem{label: entry.label, keys: entry.keys, off: entry.off}
			switch {
			case entry.separator:
				add(menu, w32.MF_SEPARATOR, 0, drawnItem{line: true})
			case entry.sub != nil:
				item.sub = true
				add(menu, flags|w32.MF_POPUP, uintptr(build(entry.sub)), item)
			default:
				actions = append(actions, entry.do)
				add(menu, flags, uintptr(len(actions)), item)
			}
		}
		add(menu, w32.MF_SEPARATOR, 0, drawnItem{edge: true})
		return menu
	}
	root := build(entries)
	defer w32.DestroyMenu(root)
	// Its windows are made without the system's border, rounded by DWM.
	thread, _, _ := windows.NewLazySystemDLL("kernel32.dll").NewProc("GetCurrentThreadId").Call()
	drawnHook, _, _ = procSetHookDrawn.Call(uintptr(w32.WH_CBT), drawnHookProc, 0, thread)
	w32.SetForegroundWindow(hwnd)
	// The window's edge pixel is cut, so it opens a pixel up and left.
	chosen := w32.TrackPopupMenuCommand(root, w32.TPM_LEFTALIGN|w32.TPM_TOPALIGN|w32.TPM_RIGHTBUTTON, int32(x-1), int32(y-1), hwnd, nil)
	if drawnHook != 0 {
		procUnhookDrawn.Call(drawnHook)
		drawnHook = 0
	}
	w32.PostMessage(hwnd, w32.WM_NULL, 0, 0)
	if chosen > 0 && int(chosen) <= len(actions) && actions[chosen-1] != nil {
		actions[chosen-1]()
	}
}

// menuFont is the system's menu font at the window's size.
func menuFont(hwnd w32.HWND) w32.HFONT {
	var metrics struct {
		size                                  uint32
		border, scrollWidth, scrollHeight     int32
		captionWidth, captionHeight           int32
		captionFont                           w32.LOGFONT
		smallCaptionWidth, smallCaptionHeight int32
		smallCaptionFont                      w32.LOGFONT
		menuWidth, menuHeight                 int32
		menuFont, statusFont, messageFont     w32.LOGFONT
		paddedBorder                          int32
	}
	metrics.size = uint32(unsafe.Sizeof(metrics))
	const spiGetNonClientMetrics = 0x0029
	procSystemParamDrawn.Call(spiGetNonClientMetrics, uintptr(metrics.size), uintptr(unsafe.Pointer(&metrics)), 0)
	font := metrics.menuFont
	if font.Height == 0 {
		font.Height = -12
	}
	font.Height = int32(float64(font.Height) * dpiScale(hwnd))
	return w32.CreateFontIndirect(&font)
}

// drawnMenuHook takes the menu's windows as they are made: no border of
// the system's, round corners and no edge line from DWM.
func drawnMenuHook(code int, wparam, lparam uintptr) uintptr {
	const hcbtCreateWindow = 3
	if code == hcbtCreateWindow && w32.GetClassName(w32.HWND(wparam)) == "#32768" {
		type createStruct struct {
			params       uintptr
			instance     uintptr
			menu         uintptr
			parent       uintptr
			cy, cx, y, x int32
			style        int32
			name, class  uintptr
			exStyle      uint32
		}
		// lparam is CBT_CREATEWND, whose first field is the CREATESTRUCT.
		made := *(**struct{ create *createStruct })(unsafe.Pointer(&lparam))
		if made != nil && made.create != nil {
			made.create.style &^= w32.WS_BORDER | w32.WS_DLGFRAME
			made.create.exStyle &^= w32.WS_EX_DLGMODALFRAME | w32.WS_EX_WINDOWEDGE | w32.WS_EX_CLIENTEDGE
		}
		// Its frame goes too: the whole window is the menu's ground.
		window := w32.HWND(wparam)
		menuProcs[window] = w32.SetWindowLongPtr(window, w32.GWLP_WNDPROC, menuWindowProc)
	}
	result, _, _ := procCallNextDrawn.Call(drawnHook, uintptr(code), wparam, lparam)
	return result
}

// drawnMenuLine answers the system's questions of the menu's lines: their
// size, and drawing them.
func drawnMenuLine(hwnd w32.HWND, message uint32, pointer unsafe.Pointer) bool {
	if message == w32.WM_MEASUREITEM {
		return measureDrawnMenu(hwnd, pointer)
	}
	return drawDrawnMenu(hwnd, pointer)
}

// measureDrawnMenu answers the system's question of a line's size.
func measureDrawnMenu(hwnd w32.HWND, pointer unsafe.Pointer) bool {
	measure := (*w32.MEASUREITEMSTRUCT)(pointer)
	if measure.CtlType != w32.ODT_MENU || int(measure.ItemData) >= len(drawnMenu.items) {
		return false
	}
	item := drawnMenu.items[measure.ItemData]
	scale := dpiScale(hwnd)
	at := func(value int) uint32 { return uint32(float64(value)*scale + 0.5) }
	switch {
	case item.edge:
		// Its frame gone, the window's edge pixel is cut (drawnMenuWindow).
		measure.ItemHeight, measure.ItemWidth = at(drawnEdge)+1, 0
		return true
	case item.line:
		measure.ItemHeight, measure.ItemWidth = at(drawnLineRoom), 0
		return true
	}
	dc := w32.GetDC(hwnd)
	defer w32.ReleaseDC(hwnd, dc)
	old := w32.SelectObject(dc, w32.HGDIOBJ(drawnMenu.font))
	defer w32.SelectObject(dc, old)
	width := textWidth(dc, item.label) + int(at(drawnTextStart+drawnEndRoom)) + 2
	if item.keys != "" {
		width += int(at(drawnKeysGap)) + textWidth(dc, item.keys)
	}
	if item.sub {
		width += int(at(drawnArrowRoom))
	}
	// The system adds a check mark's room to what it is told.
	check := w32.GetSystemMetrics(w32.SM_CXMENUCHECK)
	measure.ItemWidth, measure.ItemHeight = uint32(max(0, width-check+1)), at(drawnRow)
	return true
}

func textWidth(dc w32.HDC, text string) int {
	words := windows.StringToUTF16(text)
	var size w32.SIZE
	w32.GetTextExtentPoint32(dc, &words[0], len(words)-1, &size)
	return int(size.CX)
}

// drawDrawnMenu draws a line of the menu.
func drawDrawnMenu(hwnd w32.HWND, pointer unsafe.Pointer) bool {
	draw := (*w32.DRAWITEMSTRUCT)(pointer)
	if draw.ControlType != w32.ODT_MENU || int(draw.ItemData) >= len(drawnMenu.items) {
		return false
	}
	item := drawnMenu.items[draw.ItemData]
	colors := drawnMenu.colors
	scale := dpiScale(hwnd)
	at := func(value int) int32 { return int32(float64(value)*scale + 0.5) }
	rect := draw.RcItem
	// A line takes the menu's whole width, its frame gone (drawnMenuWindow).
	if found, _, _ := procWindowFromDC.Call(uintptr(draw.HDC)); found != 0 {
		if client := w32.GetClientRect(w32.HWND(found)); client != nil {
			rect.Left, rect.Right = client.Left, client.Right
		}
	}
	lit := draw.ItemState&w32.ODS_SELECTED != 0 && !item.off && !item.line && !item.edge
	ground := colors.ground
	if lit {
		ground = colors.lit
	}
	fillRect(draw.HDC, rect, ground)
	if item.line {
		middle := rect.Top + (rect.Bottom-rect.Top)/2
		fillRect(draw.HDC, w32.RECT{Left: rect.Left, Top: middle, Right: rect.Right, Bottom: middle + max(1, at(1))}, colors.line)
	}
	if item.line || item.edge {
		return true
	}
	old := w32.SelectObject(draw.HDC, w32.HGDIOBJ(drawnMenu.font))
	defer w32.SelectObject(draw.HDC, old)
	w32.SetBkMode(draw.HDC, w32.TRANSPARENT)
	text := colors.text
	if item.off {
		text = colors.off
	}
	w32.SetTextColor(draw.HDC, text.ref())
	// The window's edge pixel is cut on each side (drawnMenuWindow).
	rect.Left, rect.Right = rect.Left+1, rect.Right-1
	label := rect
	label.Left += at(drawnTextStart)
	words := windows.StringToUTF16(strings.ReplaceAll(item.label, "&", "&&"))
	w32.DrawText(draw.HDC, words, -1, &label, w32.DT_LEFT|w32.DT_VCENTER|w32.DT_SINGLELINE|w32.DT_NOPREFIX)
	end := rect.Right - at(drawnEndRoom)
	if item.keys != "" {
		if !item.off {
			w32.SetTextColor(draw.HDC, colors.keys.ref())
		}
		keys := rect
		keys.Right = end
		w32.DrawText(draw.HDC, windows.StringToUTF16(item.keys), -1, &keys, w32.DT_RIGHT|w32.DT_VCENTER|w32.DT_SINGLELINE|w32.DT_NOPREFIX)
	}
	if item.sub {
		// A thin chevron at the end; the system's own arrow is kept out.
		middle := rect.Top + (rect.Bottom-rect.Top)/2
		tip := rect.Right - at(drawnArrowRoom) + at(10)
		for step := int32(0); step <= at(4); step++ {
			fillRect(draw.HDC, w32.RECT{Left: tip - at(4) + step, Top: middle - at(4) + step, Right: tip - at(4) + step + max(1, at(1)), Bottom: middle - at(4) + step + max(1, at(1))}, text)
			fillRect(draw.HDC, w32.RECT{Left: tip - at(4) + step, Top: middle + at(4) - step, Right: tip - at(4) + step + max(1, at(1)), Bottom: middle + at(4) - step + max(1, at(1))}, text)
		}
		w32.ExcludeClipRect(draw.HDC, rect.Left, rect.Top, rect.Right, rect.Bottom)
	}
	return true
}

var (
	procRoundRegion     = windows.NewLazySystemDLL("gdi32.dll").NewProc("CreateRoundRectRgn")
	procSetWindowRegion = user32Drawn.NewProc("SetWindowRgn")
	procWindowFromDC    = user32Drawn.NewProc("WindowFromDC")
	menuProcs           = map[w32.HWND]uintptr{}
	menuWindowProc      = syscall.NewCallback(drawnMenuWindow)
)

// drawnMenuWindow is the menu window's own procedure before the system's:
// no frame (its client is the whole window, its frame left unpainted), and
// its corners rounded by DWM with no edge line.
func drawnMenuWindow(hwnd w32.HWND, message uint32, wparam, lparam uintptr) uintptr {
	previous := menuProcs[hwnd]
	call := func() uintptr { return w32.CallWindowProc(previous, hwnd, message, wparam, lparam) }
	switch message {
	case w32.WM_NCCALCSIZE:
		if wparam != 0 {
			params := *(**ncCalcSizeParams)(unsafe.Pointer(&lparam))
			whole := params.rects[0]
			result := call()
			params.rects[0] = whole
			return result
		}
	case w32.WM_NCPAINT:
		return 0
	case w32.WM_CREATE:
		result := call()
		round, none := uint32(2), uint32(0xfffffffe)
		w32.DwmSetWindowAttribute(hwnd, 33, unsafe.Pointer(&round), 4)
		w32.DwmSetWindowAttribute(hwnd, 34, unsafe.Pointer(&none), 4)
		return result
	case w32.WM_WINDOWPOSCHANGED:
		// The system's flat menu still lines its edge: the window is cut to
		// a rounded box inside it, 8 round as Notion's.
		result := call()
		if box := w32.GetWindowRect(hwnd); box != nil {
			width, height := box.Right-box.Left, box.Bottom-box.Top
			diameter := uintptr(float64(16) * dpiScale(hwnd))
			region, _, _ := procRoundRegion.Call(1, 1, uintptr(width), uintptr(height), diameter, diameter)
			if region != 0 {
				procSetWindowRegion.Call(uintptr(hwnd), region, 1)
			}
		}
		return result
	case w32.WM_NCDESTROY:
		result := call()
		delete(menuProcs, hwnd)
		return result
	}
	return call()
}
