//go:build windows

package main

import (
	"strconv"
	"strings"
	"syscall"
	"unsafe"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/w32"
	"golang.org/x/sys/windows"
)

// A frame's title bar on Windows, as Notion's Windows app has it (7.44.1:
// titleBarStyle "hidden" with a titleBarOverlay the tab bar's height): the
// strip of tabs is the title bar, and the window's three buttons — minimize,
// maximize or restore, close — stand at its end, drawn by the app as
// Electron's overlay draws them (45px each and 1px between them, Notion's
// 137px — measured in Notion 7.44.1's Windows app; the overlay's color over
// the tab bar's end, the page's color, its
// symbols in the pages' secondary icon color, a hover and a press darker or
// lighter and close red). The frame keeps its resizing edges; the strip's top edge and the
// buttons' top resize it too, and the maximize button is the system's, so
// Windows 11 offers its snap layouts there. The strip's web view takes the
// title bar but the buttons' room, each tab's page the rest
// (window_tabs_windows.go).

const (
	captionButtonWidth = 45
	captionButtonGap   = 1
	captionWidth       = 3*captionButtonWidth + 2*captionButtonGap
	// The buttons' glyphs (Segoe Fluent Icons, Segoe MDL2 Assets before
	// Windows 11), as Windows draws its own.
	glyphMinimize = ''
	glyphMaximize = ''
	glyphRestore  = ''
	glyphClose    = ''
)

// frameChrome is a frame's title bar state: the window procedure it was
// given and the button the pointer is over or pressed.
type frameChrome struct {
	window         *application.WebviewWindow
	previous       uintptr
	hover, pressed uintptr
	tracking       bool
	maximized      bool
	maximizeOnShow bool
}

var (
	frameChromes  = map[w32.HWND]*frameChrome{}
	frameProc     uintptr
	glyphFonts    = map[int32]w32.HFONT{}
	glyphFontFace = "Segoe Fluent Icons"
)

func init() {
	// Made here, not as the variable's value: the procedure reaches the
	// app's menu, whose windows reach the procedure.
	frameProc = syscall.NewCallback(frameWindowProc)
	if windows.RtlGetVersion().BuildNumber < 22000 {
		glyphFontFace = "Segoe MDL2 Assets"
	}
}

// takeTitleBar gives the frame's title bar to the strip and the buttons,
// once, by a window procedure of its own before Wails' window's.
func takeTitleBar(window *application.WebviewWindow) {
	hwnd := hwndOf(window)
	if hwnd == 0 || frameChromes[hwnd] != nil {
		return
	}
	chrome := &frameChrome{window: window}
	frameChromes[hwnd] = chrome
	chrome.previous = w32.SetWindowLongPtr(hwnd, w32.GWLP_WNDPROC, frameProc)
	w32.SetWindowPos(hwnd, 0, 0, 0, 0, 0, w32.SWP_FRAMECHANGED|w32.SWP_NOMOVE|w32.SWP_NOSIZE|w32.SWP_NOZORDER|w32.SWP_NOACTIVATE)
	placeFrameViews(hwnd)
}

// maximizeOnShow maximizes a frame kept maximized once it first shows.
func maximizeOnShow(hwnd w32.HWND) {
	if chrome := frameChromes[hwnd]; chrome != nil {
		chrome.maximizeOnShow = true
	}
}

// paintFrameChrome draws the frame's buttons again, as the pages' colors
// change.
func paintFrameChrome(window *application.WebviewWindow) {
	if hwnd := hwndOf(window); hwnd != 0 && frameChromes[hwnd] != nil {
		w32.InvalidateRect(hwnd, captionRect(hwnd), false)
	}
}

// framed says the frame has its title bar (not full screen).
func framed(hwnd w32.HWND) bool {
	return uint32(w32.GetWindowLong(hwnd, w32.GWL_STYLE))&w32.WS_CAPTION == w32.WS_CAPTION
}

// resizeBorder is how thick the system's resizing edge is.
func resizeBorder(hwnd w32.HWND) int32 {
	dpi := w32.GetDpiForWindow(hwnd)
	return int32(w32.GetSystemMetricsForDpi(w32.SM_CYSIZEFRAME, dpi) + w32.GetSystemMetricsForDpi(w32.SM_CXPADDEDBORDER, dpi))
}

func captionRect(hwnd w32.HWND) *w32.RECT {
	client := w32.GetClientRect(hwnd)
	if client == nil {
		return nil
	}
	return &w32.RECT{Left: client.Right - scaled(hwnd, captionWidth), Top: 0, Right: client.Right, Bottom: scaled(hwnd, stripHeight())}
}

// buttonRects are the minimize, maximize and close buttons' places, from
// the window's end, a gap's pixel apart (no button's, the title bar's).
func buttonRects(hwnd w32.HWND) [3]w32.RECT {
	area := captionRect(hwnd)
	if area == nil {
		return [3]w32.RECT{}
	}
	var rects [3]w32.RECT
	right := area.Right
	for index := 2; index >= 0; index-- {
		left := right - scaled(hwnd, captionButtonWidth)
		rects[index] = w32.RECT{Left: left, Top: 0, Right: right, Bottom: area.Bottom}
		right = left - scaled(hwnd, captionButtonGap)
	}
	return rects
}

var buttonHits = [3]uintptr{w32.HTMINBUTTON, w32.HTMAXBUTTON, w32.HTCLOSE}

func isButtonHit(hit uintptr) bool {
	return hit == w32.HTMINBUTTON || hit == w32.HTMAXBUTTON || hit == w32.HTCLOSE
}

// hitTest says what a point of the title bar the strip leaves is: the top
// resizing edge, or one of the buttons.
func hitTest(hwnd w32.HWND, lparam uintptr) uintptr {
	x, y, ok := w32.ScreenToClient(hwnd, int(int16(lparam&0xffff)), int(int16((lparam>>16)&0xffff)))
	if !ok || y < 0 || int32(y) >= scaled(hwnd, stripHeight()) {
		return 0
	}
	if !w32.IsZoomed(hwnd) && framed(hwnd) && int32(y) < resizeBorder(hwnd)/2 {
		if area := captionRect(hwnd); area != nil && int32(x) >= area.Right-resizeBorder(hwnd) {
			return w32.HTTOPRIGHT
		}
		return w32.HTTOP
	}
	if !framed(hwnd) {
		return 0
	}
	for index, rect := range buttonRects(hwnd) {
		if int32(x) >= rect.Left && int32(x) < rect.Right {
			return buttonHits[index]
		}
	}
	// The gaps between the buttons are the title bar's, to drag by.
	if area := captionRect(hwnd); area != nil && int32(x) >= area.Left {
		return w32.HTCAPTION
	}
	return 0
}

// ncCalcSizeParams is NCCALCSIZE_PARAMS.
type ncCalcSizeParams struct {
	rects    [3]w32.RECT
	position uintptr
}

// frameWindowProc is a frame's window procedure before Wails' own; lparam
// is a pointer for WM_NCCALCSIZE, a number for the rest.
func frameWindowProc(hwnd w32.HWND, message uint32, wparam uintptr, pointer unsafe.Pointer) uintptr {
	lparam := uintptr(pointer)
	chrome := frameChromes[hwnd]
	if chrome == nil {
		return w32.DefWindowProc(hwnd, message, wparam, lparam)
	}
	next := func() uintptr { return w32.CallWindowProc(chrome.previous, hwnd, message, wparam, lparam) }
	switch message {
	case w32.WM_NCCALCSIZE:
		// The client takes the title bar too; the edges stay the system's.
		if wparam == 0 || !framed(hwnd) {
			return next()
		}
		params := (*ncCalcSizeParams)(pointer)
		top := params.rects[0].Top
		result := next()
		params.rects[0].Top = top
		if w32.IsZoomed(hwnd) {
			params.rects[0].Top += resizeBorder(hwnd)
		}
		return result
	case w32.WM_NCHITTEST:
		result := next()
		if result == w32.HTCLIENT {
			if hit := hitTest(hwnd, lparam); hit != 0 {
				return hit
			}
		}
		return result
	case w32.WM_NCMOUSEMOVE:
		// The buttons light and name themselves (window_tabs_windows_tips.go).
		chrome.pointerOverCaption(hwnd, wparam)
		return next()
	case w32.WM_NCMOUSELEAVE:
		chrome.pointerLeftCaption(hwnd)
		return next()
	case w32.WM_TIMER:
		if wparam == captionTipTimer {
			chrome.showCaptionTip(hwnd)
			return 0
		}
	case w32.WM_NCLBUTTONDOWN, w32.WM_NCLBUTTONDBLCLK:
		if isButtonHit(wparam) {
			hideCaptionTip(hwnd)
			chrome.pressed = wparam
			w32.InvalidateRect(hwnd, captionRect(hwnd), false)
			return 0
		}
	case w32.WM_NCLBUTTONUP:
		if isButtonHit(wparam) {
			if chrome.pressed == wparam {
				pressButton(hwnd, wparam)
			}
			chrome.pressed = 0
			w32.InvalidateRect(hwnd, captionRect(hwnd), false)
			return 0
		}
	case w32.WM_PAINT:
		paintCaption(hwnd, chrome)
		return 0
	case w32.WM_MEASUREITEM, w32.WM_DRAWITEM:
		// The app's menu's lines (app_menu_windows_drawn.go).
		if drawnMenuLine(hwnd, message, pointer) {
			return 1
		}
	case w32.WM_SIZE, w32.WM_DPICHANGED:
		result := next()
		placeFrameViews(hwnd)
		w32.InvalidateRect(hwnd, captionRect(hwnd), false)
		// The strip resizes by its top edge only while not maximized.
		if maximized := w32.IsZoomed(hwnd); maximized != chrome.maximized {
			chrome.maximized = maximized
			for _, frame := range desktopTabs.frames {
				if hwndOf(frame.window) == hwnd {
					desktopTabs.drawStrip(frame)
				}
			}
		}
		return result
	case w32.WM_MOVE:
		result := next()
		for _, tab := range hostedTabs(hwnd) {
			if controller := controllerOf(tab.window); controller != nil {
				controller.call(controllerNotifyParentChanged)
			}
		}
		return result
	case w32.WM_SYSCOMMAND:
		if appMenuByKey(hwnd, wparam, lparam) {
			return 0
		}
	case w32.WM_ACTIVATE:
		result := next()
		w32.InvalidateRect(hwnd, captionRect(hwnd), false)
		return result
	case w32.WM_SHOWWINDOW:
		result := next()
		if wparam != 0 && chrome.maximizeOnShow {
			chrome.maximizeOnShow = false
			w32.PostMessage(hwnd, w32.WM_SYSCOMMAND, w32.SC_MAXIMIZE, 0)
		}
		return result
	case w32.WM_NCDESTROY:
		forgetCaptionTip(hwnd)
		result := next()
		delete(frameChromes, hwnd)
		return result
	}
	return next()
}

// pressButton does what a button pressed and let go over it asks.
func pressButton(hwnd w32.HWND, hit uintptr) {
	command := uintptr(w32.SC_CLOSE)
	switch hit {
	case w32.HTMINBUTTON:
		command = w32.SC_MINIMIZE
	case w32.HTMAXBUTTON:
		command = w32.SC_MAXIMIZE
		if w32.IsZoomed(hwnd) {
			command = w32.SC_RESTORE
		}
	}
	w32.PostMessage(hwnd, w32.WM_SYSCOMMAND, command, 0)
}

// hostedTabs are the tabs whose pages are in the frame.
func hostedTabs(hwnd w32.HWND) []*appTab {
	var tabs []*appTab
	for _, tab := range desktopTabs.allTabs() {
		if tab.host != nil && tab.window != nil && hwndOf(tab.host.window) == hwnd {
			tabs = append(tabs, tab)
		}
	}
	return tabs
}

// placeFrameViews sets the strip's web view in the title bar but the
// buttons' room, and the tabs' pages under it, after Wails has given the
// strip the whole window.
func placeFrameViews(hwnd w32.HWND) {
	chrome := frameChromes[hwnd]
	client := w32.GetClientRect(hwnd)
	if chrome == nil || client == nil || client.Right <= 0 || client.Bottom <= 0 {
		return
	}
	if strip := controllerOf(chrome.window); strip != nil {
		putBounds(strip, stripBounds(hwnd, client))
	}
	for _, tab := range hostedTabs(hwnd) {
		if controller := controllerOf(tab.window); controller != nil {
			putBounds(controller, tabViewRect(hwnd))
		}
	}
}

// paintCaption draws the buttons as Electron's titleBarOverlay does (its
// WinCaptionButton): over the strip's color the overlay's (white at 10% in
// light), a hover 0x1A and a press 0x33 of black in light or white in dark
// (1.3 times over a colored overlay), close #E81123 with a white symbol.
func paintCaption(hwnd w32.HWND, chrome *frameChrome) {
	var paint w32.PAINTSTRUCT
	hdc := w32.BeginPaint(hwnd, &paint)
	defer w32.EndPaint(hwnd, &paint)
	area := captionRect(hwnd)
	if area == nil {
		return
	}
	look := desktopTabs.look
	// Under the buttons is the tab bar's end: the page's color with its line.
	page := cssColor(look.Colors["page"], rgb{0xff, 0xff, 0xff})
	symbol := cssColor(look.Colors["icon"], rgb{0x91, 0x91, 0x8e})
	base, hovered, pressedAlpha := rgb{}, 0x22, 0x42
	if look.Dark {
		page = cssColor(look.Colors["page"], rgb{0x19, 0x19, 0x19})
		base, hovered, pressedAlpha = rgb{0xff, 0xff, 0xff}, 0x1a, 0x33
	}
	border := cssColorOn(look.Colors["line"], page, page.over(rgb{0x2a, 0x1c, 0x00}, 0x12))
	bar := page
	if !look.Dark {
		bar, border = page.over(rgb{0xff, 0xff, 0xff}, 0x1a), border.over(rgb{0xff, 0xff, 0xff}, 0x1a)
	}
	bar, border = shadedCaption(hwnd, bar, border)
	fillRect(hdc, *area, bar)
	// The tab bar's bottom line runs on under the buttons, as Notion's tab
	// bar runs under its overlay.
	line := *area
	line.Top = line.Bottom - 1
	fillRect(hdc, line, border)
	if !framed(hwnd) {
		return
	}
	glyphs := [3]rune{glyphMinimize, glyphMaximize, glyphClose}
	if w32.IsZoomed(hwnd) {
		glyphs[1] = glyphRestore
	}
	font := glyphFont(scaled(hwnd, 10))
	old := w32.SelectObject(hdc, w32.HGDIOBJ(font))
	defer w32.SelectObject(hdc, old)
	w32.SetBkMode(hdc, w32.TRANSPARENT)
	for index, rect := range buttonRects(hwnd) {
		hit := buttonHits[index]
		fill, color := bar, symbol
		switch {
		case hit == w32.HTCLOSE && chrome.pressed == hit:
			fill, color = bar.over(rgb{0xe8, 0x11, 0x23}, 0x98), rgb{0xff, 0xff, 0xff}
		case hit == w32.HTCLOSE && chrome.hover == hit:
			fill, color = rgb{0xe8, 0x11, 0x23}, rgb{0xff, 0xff, 0xff}
		case chrome.pressed == hit:
			fill = bar.over(base, pressedAlpha)
		case chrome.hover == hit:
			fill = bar.over(base, hovered)
		}
		// A hover or press lies over the line; close's red covers it.
		under := rect
		if hit != w32.HTCLOSE || (chrome.pressed != hit && chrome.hover != hit) {
			under.Bottom--
		}
		fillRect(hdc, under, fill)
		if hit == w32.HTMINBUTTON {
			// Minimize is a 10px line through the middle of the symbols' 10px
			// box, as the overlay draws it (the font's sits a pixel higher).
			width := scaled(hwnd, 10)
			left, top := (rect.Left+rect.Right)/2-width/2, (rect.Top+rect.Bottom)/2
			fillRect(hdc, w32.RECT{Left: left, Top: top, Right: left + width, Bottom: top + max(1, scaled(hwnd, 1))}, color)
			continue
		}
		w32.SetTextColor(hdc, color.ref())
		text := windows.StringToUTF16(string(glyphs[index]))
		w32.DrawText(hdc, text, -1, &rect, w32.DT_CENTER|w32.DT_VCENTER|w32.DT_SINGLELINE|w32.DT_NOPREFIX)
	}
}

func fillRect(hdc w32.HDC, rect w32.RECT, color rgb) {
	brush := w32.CreateSolidBrush(color.ref())
	w32.FillRect(hdc, &rect, brush)
	w32.DeleteObject(w32.HGDIOBJ(brush))
}

// glyphFont is the buttons' symbol font at a pixel size, made once.
func glyphFont(size int32) w32.HFONT {
	if font, ok := glyphFonts[size]; ok {
		return font
	}
	// Smoothed in grays, as the overlay's symbols are, without ClearType's
	// colored edges.
	logFont := w32.LOGFONT{Height: -size, Weight: 400, CharSet: 1, Quality: w32.ANTIALIASED_QUALITY}
	copy(logFont.FaceName[:len(logFont.FaceName)-1], windows.StringToUTF16(glyphFontFace))
	font := w32.CreateFontIndirect(&logFont)
	glyphFonts[size] = font
	return font
}

// rgb is a color the app draws with.
type rgb struct{ r, g, b uint8 }

func (color rgb) ref() w32.COLORREF {
	return w32.COLORREF(uint32(color.r) | uint32(color.g)<<8 | uint32(color.b)<<16)
}

// over is the color with another laid over it at an alpha of 255.
func (color rgb) over(top rgb, alpha int) rgb {
	mix := func(under, over uint8) uint8 { return uint8((int(under)*(255-alpha) + int(over)*alpha + 127) / 255) }
	return rgb{mix(color.r, top.r), mix(color.g, top.g), mix(color.b, top.b)}
}

// cssColor reads a page's color — #rgb, #rrggbb, rgb() or rgba() — or gives
// the fallback; a see-through one is laid over white.
func cssColor(text string, fallback rgb) rgb {
	return cssColorOn(text, rgb{0xff, 0xff, 0xff}, fallback)
}

// cssColorOn reads a page's color as cssColor does, a see-through one laid
// over the color under it.
func cssColorOn(text string, under, fallback rgb) rgb {
	text = strings.TrimSpace(strings.ToLower(text))
	if hex, ok := strings.CutPrefix(text, "#"); ok {
		// #rgba and #rrggbbaa (as a built page's styles write a see-through
		// color) keep their alpha.
		if len(hex) == 3 || len(hex) == 4 {
			long := []byte{hex[0], hex[0], hex[1], hex[1], hex[2], hex[2]}
			if len(hex) == 4 {
				long = append(long, hex[3], hex[3])
			}
			hex = string(long)
		}
		value, err := strconv.ParseUint(hex, 16, 32)
		if err != nil || (len(hex) != 6 && len(hex) != 8) {
			return fallback
		}
		if len(hex) == 8 {
			return under.over(rgb{uint8(value >> 24), uint8(value >> 16), uint8(value >> 8)}, int(value&0xff))
		}
		return rgb{uint8(value >> 16), uint8(value >> 8), uint8(value)}
	}
	open, close := strings.IndexByte(text, '('), strings.LastIndexByte(text, ')')
	if !strings.HasPrefix(text, "rgb") || open < 0 || close <= open {
		return fallback
	}
	parts := strings.FieldsFunc(text[open+1:close], func(r rune) bool { return r == ',' || r == ' ' || r == '/' })
	if len(parts) < 3 {
		return fallback
	}
	var channels [3]uint8
	for index := range channels {
		value, err := strconv.ParseFloat(parts[index], 64)
		if err != nil {
			return fallback
		}
		channels[index] = uint8(min(255, max(0, value)))
	}
	color := rgb{channels[0], channels[1], channels[2]}
	if len(parts) > 3 {
		alpha, err := strconv.ParseFloat(strings.TrimSuffix(parts[3], "%"), 64)
		if err == nil && strings.HasSuffix(parts[3], "%") {
			alpha /= 100
		}
		if err == nil && alpha < 1 {
			return under.over(color, int(alpha*255))
		}
	}
	return color
}
