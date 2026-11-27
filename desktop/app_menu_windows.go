//go:build windows

package main

import (
	"strconv"
	"strings"
	"unsafe"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/w32"
	"golang.org/x/sys/windows"

	"leafen.org/leafen/internal/app"
)

// The Windows app has no menu bar: as Notion's Windows app (7.42.3), the
// strip's app menu button opens the app's menu — 파일, 편집, 보기, 기록, 창,
// 도움말, Notion's items in its order for Windows — under the strip, and the
// menu's keys answer while it is closed (tabKeyBindings). Keys a page answers
// itself — ⌃L, ⌃⌥L, ⌃\ and ⌃[ ⌃], as in a browser — stay the page's, and
// ⌃= ⌃- reach the app from the page (WindowTabs.Zoom), as Wails' keys can't
// name them.

// whatsNewWindowsAddress is where "Windows용 리펜 업데이트 사항 확인" goes.
const whatsNewWindowsAddress = whatsNewAddress

// showMenu: Windows has no menu bar to put in the window's language; its app
// menu is built in it each time it opens (appMenuEntries).
func (*Background) showMenu(string) {}

// showUpdateResult shows what the menu's "업데이트 확인" found in a message
// box captioned with the app's name, the words in its body, where a long
// caption would be cut.
func (background *Background) showUpdateResult(title, detail string) {
	caption := appCaption()
	application.InvokeAsync(func() {
		background.app.Dialog.Info().SetTitle(caption).SetMessage(title + "\n\n" + detail).Show()
	})
}

// updateFound says a newer version is out, which the zip doesn't install by
// itself (updater_windows.go), and offers its download page: Windows' message
// box answers only with its own Yes and No, in the system's language.
func (background *Background) updateFound(korean bool, current string, release *app.DesktopRelease) {
	say := func(english, hangul string) string {
		if korean {
			return hangul
		}
		return english
	}
	message := say("A new version of Leafen is available. Update it from the Microsoft Store, or download the new zip.",
		"새 버전의 리펜을 사용할 수 있습니다. Microsoft Store에서 업데이트하거나 새 zip을 다운로드하세요.") +
		"\n\n" + current + " → " + release.Version + "\n\n" + say("Open the download page?", "다운로드 페이지를 열까요?")
	caption := appCaption()
	application.InvokeAsync(func() {
		dialog := background.app.Dialog.Question().SetTitle(caption).SetMessage(message)
		dialog.AddButton("Yes").SetAsDefault().OnClick(func() { _ = runningUpdater.OpenDownload() })
		dialog.AddButton("No").SetAsCancel()
		dialog.Show()
	})
}

// appCaption is the app's name in the window's language, as its message
// boxes are captioned.
func appCaption() string {
	desktopBackground.mu.Lock()
	defer desktopBackground.mu.Unlock()
	if strings.HasPrefix(desktopBackground.language, "ko") {
		return "리펜"
	}
	return "Leafen"
}

// tabKeyBindings are the app menu's keys and the tab keys Notion's Windows
// app has without an item: ⌃1–⌃8 show the tab at that place and ⌃9 the
// last, ⌃⇧W closes the window, as Alt+F4 does a tab's page's window; ⌃M
// minimizes it, as 창 › 최소화 says (Notion 7.44.1's Windows app).
func tabKeyBindings() map[string]func(application.Window) {
	in := func(fn func(t *windowTabs, frame *tabFrame, tab *appTab)) func(application.Window) {
		return func(window application.Window) { tabsAskIn(window, fn) }
	}
	step := func(by int) func(application.Window) {
		return in(func(t *windowTabs, frame *tabFrame, _ *appTab) { t.step(frame, by) })
	}
	closeWindow := in(func(t *windowTabs, frame *tabFrame, _ *appTab) { t.closeAsked(frame) })
	bindings := map[string]func(application.Window){
		"ctrl+t":         in(func(t *windowTabs, frame *tabFrame, _ *appTab) { t.newTabSearch(frame) }),
		"ctrl+shift+n":   func(application.Window) { openNewAppWindow(nil) },
		"ctrl+shift+t":   func(application.Window) { tabsReopen() },
		"ctrl+w":         in(func(t *windowTabs, frame *tabFrame, tab *appTab) { t.closeTabs(frame, []*appTab{tab}) }),
		"ctrl+shift+w":   closeWindow,
		"ctrl+m":         in(func(_ *windowTabs, frame *tabFrame, _ *appTab) { frame.window.Minimise() }),
		"alt+f4":         closeWindow,
		"ctrl+shift+q":   func(application.Window) { tabsQuitWithoutSaving() },
		"ctrl+tab":       step(1),
		"ctrl+shift+tab": step(-1),
		"ctrl+r":         func(application.Window) { tabsReload(false) },
		"ctrl+shift+r":   func(application.Window) { tabsReload(true) },
		"ctrl+shift+i":   func(application.Window) { tabsInspect() },
		"ctrl+0":         func(application.Window) { tabsZoom(0) },
		"f11":            in(func(_ *windowTabs, frame *tabFrame, _ *appTab) { frame.window.ToggleFullscreen() }),
	}
	for number := 1; number <= 9; number++ {
		bindings["ctrl+"+strconv.Itoa(number)] = in(func(t *windowTabs, frame *tabFrame, _ *appTab) { t.selectNumber(frame, number) })
	}
	return bindings
}

// tabsAskIn runs fn with the frame a key was pressed in — its strip or a
// tab's page — and its shown tab; else the frame in front.
func tabsAskIn(window application.Window, fn func(t *windowTabs, frame *tabFrame, tab *appTab)) {
	onMain(func() {
		frame, _ := desktopTabs.frameOf(window)
		if frame == nil {
			frame, _ = desktopTabs.front()
		}
		if frame != nil && frame.active != nil {
			fn(desktopTabs, frame, frame.active)
		}
	})
}

// stripSaidHere takes what only a Windows strip says: its app menu button
// was pressed, its empty place pressed twice, or the title bar's menu asked
// for at a point.
func (t *windowTabs) stripSaidHere(frame *tabFrame, said stripMessage) {
	hwnd := hwndOf(frame.window)
	if hwnd == 0 {
		return
	}
	switch said.Do {
	case "appmenu":
		showAppMenu(frame, hwnd)
	case "maximize":
		// A double press on the title bar, as Windows' own.
		frame.window.ToggleMaximise()
	case "systemmenu":
		scale := dpiScale(hwnd)
		x, y := w32.ClientToScreen(hwnd, int(float64(said.X)*scale), int(float64(said.Y)*scale))
		menu := w32.GetSystemMenu(hwnd, false)
		// What a double press on the title bar does stands bold, as in
		// Notion's Windows app (Windows' own makes 닫기 bold).
		defaultItem := uintptr(w32.SC_MAXIMIZE)
		if w32.IsZoomed(hwnd) {
			defaultItem = w32.SC_RESTORE
		}
		procSetMenuDefaultItem.Call(uintptr(menu), defaultItem, 0)
		if command := w32.TrackPopupMenuCommand(menu, w32.TPM_LEFTALIGN|w32.TPM_TOPALIGN|w32.TPM_RIGHTBUTTON, int32(x), int32(y), hwnd, nil); command != 0 {
			w32.PostMessage(hwnd, w32.WM_SYSCOMMAND, command, 0)
		}
	}
}

// appMenuTop is where the app's menu's top stands in the window: on the
// strip's bottom line, as Notion 7.44.1's Windows app opens its menu (39,
// measured beside it; its 40px tab bar kept the old bar's 36 + 3).
const appMenuTop = 39

// showAppMenu opens the app's menu under the strip's app menu button, where
// Notion's opens: 12px in, its top at appMenuTop (in a right-to-left
// window, from its other end past the window's buttons).
func showAppMenu(frame *tabFrame, hwnd w32.HWND) {
	desktopBackground.mu.Lock()
	korean := strings.HasPrefix(desktopBackground.language, "ko")
	desktopBackground.mu.Unlock()
	x := scaled(hwnd, 12)
	if desktopTabs.look.RTL {
		if client := w32.GetClientRect(hwnd); client != nil {
			x = client.Right - scaled(hwnd, captionWidth+28)
		}
	}
	screenX, screenY := w32.ClientToScreen(hwnd, int(x), int(scaled(hwnd, appMenuTop)))
	popupMenu(hwnd, appMenuEntries(frame, korean), screenX, screenY)
}

// appMenuByKey opens the app's menu for Alt pressed and let go by itself,
// as Notion's Windows app does, in place of Windows' hidden menu mode, which
// took the next key; Alt with a key (Alt+Space) stays Windows'. It says
// whether it took the window's command.
func appMenuByKey(hwnd w32.HWND, command, key uintptr) bool {
	if command&0xfff0 != w32.SC_KEYMENU || key != 0 {
		return false
	}
	for _, frame := range desktopTabs.frames {
		if hwndOf(frame.window) == hwnd {
			showAppMenu(frame, hwnd)
			return true
		}
	}
	return false
}

// appMenuEntries are the app menu's items, as Notion's Windows app has them
// (7.42.3's menu source and its ko-KR words), but for the developer tools
// outside development builds and Notion's own Chromium diagnostics.
func appMenuEntries(frame *tabFrame, korean bool) []menuEntry {
	say := func(english, hangul string) string {
		if korean {
			return hangul
		}
		return english
	}
	t := desktopTabs
	shown := func(fn func(tab *appTab)) func() {
		return func() {
			if tab := frame.active; tab != nil && tab.window != nil && containsFrame(t.frames, frame) {
				fn(tab)
			}
		}
	}
	emit := func(event string) func() {
		return shown(func(tab *appTab) { tab.window.EmitEvent(event, tab.window.Name()) })
	}
	keys := func(chord ...uint16) func() {
		return shown(func(tab *appTab) {
			focusTabView(frame, tab)
			sendChord(chord...)
		})
	}
	file := []menuEntry{
		{label: say("New Tab", "새로운 탭"), keys: "Ctrl+T", do: func() { t.newTabSearch(frame) }},
		{label: say("New Window", "새 창"), keys: "Ctrl+Shift+N", do: func() { openNewAppWindow(nil) }},
		{label: say("Reopen Last Closed Tab", "마지막으로 닫은 탭 다시 열기"), keys: "Ctrl+Shift+T", do: t.reopen},
		{separator: true},
		{label: say("Close Tab", "탭 닫기"), keys: "Ctrl+W", do: shown(func(tab *appTab) { t.closeTabs(frame, []*appTab{tab}) })},
		{separator: true},
		// The shown page's export window, as the Mac app's (app_menu_bar_darwin.go).
		{label: say("Print…", "인쇄"), do: emit(exportPageEvent)},
		{separator: true},
	}
	// The zip only says a newer version is out, so its line is always
	// "업데이트 확인" (updater_windows.go); the store's app has none.
	if update, _ := updateLine(); update == "check" {
		file = append(file, menuEntry{label: say("Check for Updates…", "업데이트 확인"),
			do: func() { go desktopBackground.checkForUpdates(korean) }}, menuEntry{separator: true})
	}
	file = append(file,
		menuEntry{label: say("Exit Without Saving Tabs", "탭 저장 없이 종료"), keys: "Ctrl+Shift+Q", do: tabsQuitWithoutSaving},
		menuEntry{label: say("Exit", "나가기"), do: func() { go application.Get().Quit() }})
	edit := []menuEntry{
		{label: say("Undo", "실행 취소"), keys: "Ctrl+Z", do: keys(w32.VK_CONTROL, 'Z')},
		{label: say("Redo", "다시 실행"), keys: "Ctrl+Shift+Z", do: keys(w32.VK_CONTROL, w32.VK_SHIFT, 'Z')},
		{separator: true},
		{label: say("Cut", "잘라내기"), keys: "Ctrl+X", do: keys(w32.VK_CONTROL, 'X')},
		{label: say("Copy", "복사"), keys: "Ctrl+C", do: keys(w32.VK_CONTROL, 'C')},
		{label: say("Copy Link to Current Page", "현재 페이지 링크 복사"), keys: "Ctrl+L", do: emit(copyPageLinkEvent)},
		{label: say("Copy Name of Current Page", "현재 페이지의 이름 복사"), keys: "Ctrl+Alt+L", do: emit(copyPageNameEvent)},
		{label: say("Paste", "붙여넣기"), keys: "Ctrl+V", do: keys(w32.VK_CONTROL, 'V')},
		{separator: true},
		{label: say("Select All", "모두 선택"), keys: "Ctrl+A", do: keys(w32.VK_CONTROL, 'A')},
	}
	view := []menuEntry{
		{label: say("Reload", "다시 불러오기"), keys: "Ctrl+R", do: func() { tabsReload(false) }},
		{label: say("Force Reload", "강제 새로고침"), keys: "Ctrl+Shift+R", do: func() { tabsReload(true) }},
		{label: say("Toggle Developer Tools", "개발자 도구 토글"), keys: "Ctrl+Shift+I", do: tabsInspect},
		{separator: true},
		{label: say("Show/Hide Sidebar", "사이드바 표시/숨기기"), keys: "Ctrl+\\", do: emit(toggleSidebarEvent)},
		{separator: true},
		{label: say("Zoom In", "확대"), keys: "Ctrl+Plus", do: func() { tabsZoom(1) }},
		{label: say("Zoom Out", "축소"), keys: "Ctrl+-", do: func() { tabsZoom(-1) }},
		{label: say("Actual Size", "실제 크기"), keys: "Ctrl+0", do: func() { tabsZoom(0) }},
		{label: say("Toggle Full Screen", "전체 화면 토글"), keys: "F11", do: func() { frame.window.ToggleFullscreen() }},
	}
	history := []menuEntry{
		{label: say("Back", "뒤로"), keys: "Ctrl+[", do: shown(func(tab *appTab) { goTab(tab, false) })},
		{label: say("Forward", "앞으로"), keys: "Ctrl+]", do: shown(func(tab *appTab) { goTab(tab, true) })},
	}
	window := []menuEntry{
		{label: say("Minimize", "최소화"), keys: "Ctrl+M", do: func() { frame.window.Minimise() }},
		{label: say("Show Previous Tab", "이전 탭 표시"), keys: "Ctrl+Shift+Tab", do: func() { t.step(frame, -1) }},
		{label: say("Show Next Tab", "다음 탭 표시"), keys: "Ctrl+Tab", do: func() { t.step(frame, 1) }},
		{label: say("Maximize", "최대화"), do: func() { frame.window.ToggleMaximise() }},
	}
	var troubleshooting []menuEntry
	for _, item := range troubleshootingItems(korean) {
		troubleshooting = append(troubleshooting, menuEntry{label: item.label, do: item.do})
	}
	help := []menuEntry{
		{label: say("About Leafen", "리펜 소개"), do: func() {
			application.Get().Dialog.Info().SetTitle(say("About Leafen", "리펜 소개")).SetMessage("Leafen " + app.ReleaseVersion()).Show()
		}},
		{separator: true},
		{label: say("Troubleshooting", "트러블슈팅"), sub: troubleshooting},
		{separator: true},
		{label: say("Open Help & Documentation", "도움말과 설명서 열기"), do: func() { _ = application.Get().Browser.OpenURL(helpAddress) }},
		{label: say("Open What’s New in Leafen for Windows", "Windows용 리펜 업데이트 사항 확인"),
			do: func() { _ = application.Get().Browser.OpenURL(whatsNewWindowsAddress) }},
	}
	return []menuEntry{
		{label: say("File", "파일"), sub: file},
		{label: say("Edit", "편집"), sub: edit},
		{label: say("View", "보기"), sub: view},
		{label: say("History", "기록"), sub: history},
		{label: say("Window", "창"), sub: window},
		{label: say("Help", "도움말"), sub: help},
	}
}

// keyboardInput is INPUT for a key (KEYBDINPUT in INPUT's union).
type keyboardInput struct {
	kind    uint32
	_       uint32
	key     uint16
	scan    uint16
	flags   uint32
	time    uint32
	_       uint32
	extra   uintptr
	padding [8]byte
}

var (
	procSendInput          = windows.NewLazySystemDLL("user32.dll").NewProc("SendInput")
	procSetMenuDefaultItem = windows.NewLazySystemDLL("user32.dll").NewProc("SetMenuDefaultItem")
)

// sendChord presses keys together and lets them go, as the person would:
// the page in front answers them its own way (its undo, its paste), as
// Notion's menu has the page do.
func sendChord(keys ...uint16) {
	var inputs []keyboardInput
	for _, key := range keys {
		inputs = append(inputs, keyboardInput{kind: 1, key: key})
	}
	for index := len(keys) - 1; index >= 0; index-- {
		inputs = append(inputs, keyboardInput{kind: 1, key: keys[index], flags: w32.KEYEVENTF_KEYUP})
	}
	procSendInput.Call(uintptr(len(inputs)), uintptr(unsafe.Pointer(&inputs[0])), unsafe.Sizeof(inputs[0]))
}

// toggleDevTools opens a page's developer tools; WebView2 closes them from their own window.
func toggleDevTools(window *application.WebviewWindow) { window.OpenDevTools() }

// openConsole opens the page's developer tools (WebView2 opens them where
// they were last).
func openConsole(window *application.WebviewWindow) { window.OpenDevTools() }
