//go:build darwin && !ios

package main

import (
	"fmt"
	"strings"

	"github.com/wailsapp/wails/v3/pkg/application"

	"leafen.org/leafen/internal/app"
)

// The Mac's menu bar. What it shares with Windows' app menu — the events it
// sends the window, the troubleshooting items and the update check — is
// app_menu_desktop.go's.

// showMenu puts the menu bar in the window's language, as Notion's app does
// (seen 2026-11-06 in Notion 7.42 in Korean: Notion · 파일 · 편집 · 보기 ·
// 기록 · 창 · 도움말). Only macOS has an application menu bar; Windows opens
// the app's menu from its strip (app_menu_windows.go). It runs on the main
// thread and builds the menu again only when the language or the update line
// changes; while a download goes on, its line takes the new percent where it
// is, so a menu held open shows it too.
func (background *Background) showMenu(language string) {
	if background.app == nil {
		return
	}
	korean := strings.HasPrefix(language, "ko")
	// A page's right-press menu speaks it too (text_menu_darwin.m).
	textMenuLanguage(korean)
	update, percent := updateLine()
	key := language + "|" + update
	if background.menuKey == key {
		if update == "downloading" && background.updateItem != nil {
			background.updateItem.SetLabel(downloadingLabel(korean, percent))
		}
		return
	}
	background.menuKey = key
	background.updateItem = nil
	menu, settings := applicationMenu(background, korean, update, percent)
	background.app.Menu.Set(menu)
	// macOS renames an app-menu item titled as its own "Preferences…" to
	// "Settings…" ("설정…") when the menu bar is set; Notion's app keeps
	// "환경설정…", so the title is given again after.
	settings.SetLabel(settings.Label())
}

// The menu bar as Notion's app has it (7.42.3, seen 2026-11-09 in Korean and
// read from its English source): its items in its order with the tabs of its
// windows (window_tabs.go) — "새로운 탭", "마지막으로 닫은 탭 다시 열기", "탭
// 닫기", "이전·다음 탭 표시", "탭 저장 없이 종료" — and "개발자 도구 토글"
// (⌥⌘I, the shown page's web inspector, seen in Notion's 2026-11-11), but
// for Notion's own Chromium diagnostics under "트러블슈팅". View's reloading,
// inspecting and zoom are the shown tab's, not the window's strip's.
func applicationMenu(background *Background, korean bool, update string, percent int) (*application.Menu, *application.MenuItem) {
	say := func(english, hangul string) string {
		if korean {
			return hangul
		}
		return english
	}
	role := func(menu *application.Menu, kind application.Role, english, hangul string) *application.MenuItem {
		menu.AddRole(kind)
		item := menu.FindByRole(kind)
		if item != nil {
			item.SetLabel(say(english, hangul))
		}
		return item
	}
	// ask tells the window in front to do what an item is for.
	ask := func(event string) func(*application.Context) {
		return func(*application.Context) {
			if window := background.front(); window != nil {
				background.app.Event.Emit(event, window.Name())
			}
		}
	}
	menu := application.NewMenu()

	app := menu.AddSubmenu("Leafen")
	role(app, application.About, "About Leafen", "리펜 소개")
	app.AddSeparator()
	settings := app.Add(say("Preferences…", "환경설정…")).SetAccelerator("CmdOrCtrl+,").OnClick(func(*application.Context) {
		// Settings open in the window in front, which the event names.
		if window := background.front(); window != nil {
			if !showFront() {
				window.Show()
				window.Focus()
			}
			background.app.Event.Emit(openSettingsEvent, window.Name())
		}
	})
	app.AddSeparator()
	switch update {
	case "downloading":
		// Notion's line while its new version downloads, grayed out.
		background.updateItem = app.Add(downloadingLabel(korean, percent)).SetEnabled(false)
		app.AddSeparator()
	case "restart":
		app.Add(say("Restart to Apply Update", "업데이트 적용을 위해 다시 시작")).OnClick(func(*application.Context) {
			go func() { _ = runningUpdater.RestartNow() }()
		})
		app.AddSeparator()
	case "check":
		app.Add(say("Check for Updates…", "업데이트 확인")).OnClick(func(*application.Context) {
			go background.checkForUpdates(korean)
		})
		app.AddSeparator()
	}
	role(app, application.ServicesMenu, "Services", "서비스")
	app.AddSeparator()
	role(app, application.Hide, "Hide Leafen", "리펜 숨기기")
	role(app, application.HideOthers, "Hide Others", "다른 항목 숨기기")
	role(app, application.UnHide, "Show All", "모두 표시하기")
	app.AddSeparator()
	app.Add(say("Quit Without Saving Tabs", "탭 저장 없이 종료")).OnClick(func(*application.Context) { tabsQuitWithoutSaving() })
	role(app, application.Quit, "Quit", "끝내기")

	file := menu.AddSubmenu(say("File", "파일"))
	// ⌘T opens search in the shown tab, its page found opening in a new tab
	// at the end, as Notion's app's does.
	file.Add(say("New Tab", "새로운 탭")).SetAccelerator("CmdOrCtrl+T").OnClick(func(*application.Context) { tabsNew() })
	// A new window on the start page, as the app's first window opens.
	file.Add(say("New Window", "새 창")).SetAccelerator("CmdOrCtrl+Shift+N").OnClick(func(*application.Context) {
		openNewAppWindow(background.front())
	})
	file.Add(say("Reopen Last Closed Tab", "마지막으로 닫은 탭 다시 열기")).SetAccelerator("CmdOrCtrl+Shift+T").
		OnClick(func(*application.Context) { tabsReopen() })
	file.AddSeparator()
	// The window's last tab closes the window; ⌘⇧W, which Notion's app has
	// without an item, closes the window and its tabs (tabKeyBindings).
	file.Add(say("Close Tab", "탭 닫기")).SetAccelerator("CmdOrCtrl+W").OnClick(func(*application.Context) { tabsCloseTab() })
	file.AddSeparator()
	// The shown page's export window, as Notion's app's 인쇄 asks its page
	// ("notion:export-active-page", its 7.43 source; seen 2026-11-16): its
	// PDF is printed from there. No shortcut: ⌘P is the page's search, as
	// in Notion's app.
	file.Add(say("Print…", "인쇄")).OnClick(ask(exportPageEvent))

	edit := menu.AddSubmenu(say("Edit", "편집"))
	// Undo and Redo are the page's own, as ⌘Z and ⇧⌘Z are there, as in
	// Notion's app: not WebKit's undo: and redo:, which knew nothing of the
	// editor's steps — a split line stayed split, and undone typing came back
	// as a new edit that took Redo away (2026-11-14, the test app). The
	// page's own keys answer first; a key it leaves reaches these items.
	edit.Add(say("Undo", "실행 취소")).SetAccelerator("CmdOrCtrl+Z").OnClick(ask(undoEvent))
	edit.Add(say("Redo", "다시 실행")).SetAccelerator("CmdOrCtrl+Shift+Z").OnClick(ask(redoEvent))
	edit.AddSeparator()
	role(edit, application.Cut, "Cut", "잘라내기")
	role(edit, application.Copy, "Copy", "복사")
	// The page's own ⌘L and ⌥⌘L answer first (useAuthenticatedProductShortcuts.ts);
	// these keys reach the menu where the page leaves them.
	edit.Add(say("Copy Link to Current Page", "현재 페이지 링크 복사")).SetAccelerator("CmdOrCtrl+L").OnClick(ask(copyPageLinkEvent))
	edit.Add(say("Copy Name of Current Page", "현재 페이지의 이름 복사")).SetAccelerator("CmdOrCtrl+Alt+L").OnClick(ask(copyPageNameEvent))
	role(edit, application.Paste, "Paste", "붙여넣기")
	role(edit, application.PasteAndMatchStyle, "Paste and Match Style", "서식 유지 붙여넣기")
	// Select All is the page's ⌘A too, as in Notion's app: the caret's line,
	// then its block (seen 2026-11-17), not WebKit's selectAll:, which chose
	// the whole page's text at once; WebKit's is what the page leaves.
	edit.Add(say("Select All", "모두 선택")).SetAccelerator("CmdOrCtrl+A").OnClick(ask(selectAllEvent))
	edit.AddSeparator()
	speech := edit.AddSubmenu(say("Speech", "말하기"))
	role(speech, application.StartSpeaking, "Start Speaking", "말하기 시작")
	role(speech, application.StopSpeaking, "Stop Speaking", "말하기 중지")

	view := menu.AddSubmenu(say("View", "보기"))
	view.Add(say("Reload", "다시 불러오기")).SetAccelerator("CmdOrCtrl+r").OnClick(func(*application.Context) { tabsReload(false) })
	view.Add(say("Force Reload", "강제 새로고침")).SetAccelerator("CmdOrCtrl+Shift+r").OnClick(func(*application.Context) { tabsReload(true) })
	view.Add(say("Toggle Developer Tools", "개발자 도구 토글")).SetAccelerator("CmdOrCtrl+Alt+i").OnClick(func(*application.Context) { tabsInspect() })
	view.AddSeparator()
	// The page's ⌘\ answers first outside its text (AppShell.tsx); in its
	// text the key reaches this item, as in Notion's app.
	view.Add(say("Show/Hide Sidebar", "사이드바 표시/숨기기")).SetAccelerator("CmdOrCtrl+\\").OnClick(ask(toggleSidebarEvent))
	view.AddSeparator()
	view.Add(say("Zoom In", "확대")).SetAccelerator("CmdOrCtrl+plus").OnClick(func(*application.Context) { tabsZoom(1) })
	view.Add(say("Zoom Out", "축소")).SetAccelerator("CmdOrCtrl+-").OnClick(func(*application.Context) { tabsZoom(-1) })
	view.Add(say("Actual Size", "실제 크기")).SetAccelerator("CmdOrCtrl+0").OnClick(func(*application.Context) { tabsZoom(0) })
	role(view, application.ToggleFullscreen, "Toggle Full Screen", "전체 화면 토글")

	history := menu.AddSubmenu(say("History", "기록"))
	history.Add(say("Back", "뒤로")).SetAccelerator("CmdOrCtrl+[").OnClick(func(*application.Context) {
		if window := background.front(); window != nil {
			window.ExecJS("history.back()")
		}
	})
	history.Add(say("Forward", "앞으로")).SetAccelerator("CmdOrCtrl+]").OnClick(func(*application.Context) {
		if window := background.front(); window != nil {
			window.ExecJS("history.forward()")
		}
	})

	// The window menu as the system's own, so macOS adds its tiling and
	// open-window items there as in Notion's, with Notion's tab items between
	// zoom and bringing all to front. ⌃Tab and ⌃⇧Tab are the windows' own keys
	// too, which a page's web view would otherwise take (tabKeyBindings).
	if window := role(menu, application.WindowMenu, "Window", "창"); window != nil {
		items := window.GetSubmenu()
		items.Clear()
		role(items, application.Minimise, "Minimize", "최소화")
		role(items, application.Zoom, "Zoom", "확대/축소")
		items.AddSeparator()
		items.Add(say("Show Previous Tab", "이전 탭 표시")).SetAccelerator("Ctrl+Shift+Tab").OnClick(func(*application.Context) { tabsStep(-1) })
		items.Add(say("Show Next Tab", "다음 탭 표시")).SetAccelerator("Ctrl+Tab").OnClick(func(*application.Context) { tabsStep(1) })
		items.AddSeparator()
		role(items, application.Front, "Bring All to Front", "모두 앞으로 가져오기")
	}

	help := menu.AddSubmenu(say("Help", "도움말"))
	troubleshooting := help.AddSubmenu(say("Troubleshooting", "트러블슈팅"))
	for _, item := range troubleshootingItems(korean) {
		troubleshooting.Add(item.label).OnClick(func(*application.Context) { item.do() })
	}
	help.AddSeparator()
	help.Add(say("Open Help & Documentation", "도움말과 설명서 열기")).OnClick(func(*application.Context) {
		_ = background.app.Browser.OpenURL(helpAddress)
	})
	help.Add(say("Open What’s New in Leafen for macOS", "MacOS용 리펜 업데이트 사항 확인")).OnClick(func(*application.Context) {
		_ = background.app.Browser.OpenURL(whatsNewAddress)
	})
	return menu, settings
}

// downloadingLabel is the update line while a new version downloads, in
// Notion's words ("Downloading Update ({percentage}%)").
func downloadingLabel(korean bool, percent int) string {
	if korean {
		return fmt.Sprintf("업데이트 다운로드 중(%d%%)", percent)
	}
	return fmt.Sprintf("Downloading Update (%d%%)", percent)
}

// redrawUpdateLine draws the app menu's update line again at once when a
// download moves, starts or ends (updater_darwin.go), rather than at the
// background's next look. A download that starts before the app's main
// thread runs is drawn at the background's first look.
func redrawUpdateLine() {
	background := desktopBackground
	background.mu.Lock()
	language, started := background.language, background.started
	background.mu.Unlock()
	if started {
		application.InvokeAsync(func() { background.showMenu(language) })
	}
}

// updateFound says what the menu's "업데이트 확인" found, in Notion's words
// for its own: a newer version fetching in the background; the line counts
// the download meanwhile and turns to "업데이트 적용을 위해 다시 시작" once the
// version is ready (redrawUpdateLine).
func (background *Background) updateFound(korean bool, current string, release *app.DesktopRelease) {
	title := "A new version of Leafen is available and currently being downloaded in the background. Thanks for staying up to date!"
	if korean {
		title = "새 버전의 리펜을 사용할 수 있으며 현재 백그라운드에서 다운로드 중입니다. 최신 상태를 유지해 주셔서 감사합니다!"
	}
	background.showUpdateResult(title, current+" → "+release.Version)
}

// showUpdateResult shows what the menu's "업데이트 확인" found with the app's
// own icon, as Notion's app shows its message boxes, where Wails' info
// dialog would show an "i".
func (background *Background) showUpdateResult(title, detail string) {
	application.InvokeAsync(func() {
		dialog := background.app.Dialog.Info().SetTitle(title).SetMessage(detail)
		if icon := appIcon(); len(icon) > 0 {
			dialog.SetIcon(icon)
		}
		dialog.Show()
	})
}
