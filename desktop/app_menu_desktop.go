//go:build (darwin && !ios) || windows

package main

import (
	"context"
	"os/exec"
	"runtime"

	"github.com/wailsapp/wails/v3/pkg/application"

	"leafen.org/leafen/internal/app"
)

// What the menu bar asks the window in front to do
// (web/src/lib/desktopMenuCommands.ts): open Settings, show or hide the
// sidebar, copy the current page's link or its name as a link, open the
// current page's export window (파일 › 인쇄), undo, redo or choose all as ⌘Z,
// ⇧⌘Z and ⌘A do there (web/src/lib/desktopMenuKeys.ts).
const (
	openSettingsEvent  = "leafen:open-settings"
	toggleSidebarEvent = "leafen:toggle-sidebar"
	copyPageLinkEvent  = "leafen:copy-page-link"
	copyPageNameEvent  = "leafen:copy-page-name"
	exportPageEvent    = "leafen:export-page"
	undoEvent          = "leafen:undo"
	redoEvent          = "leafen:redo"
	selectAllEvent     = "leafen:select-all"
)

// helpAddress is where "도움말과 설명서 열기" goes: the project's own pages;
// whatsNewAddress where "MacOS용 리펜 업데이트 사항 확인" goes: its releases.
const (
	helpAddress     = "https://github.com/leafen-org/leafen#readme"
	whatsNewAddress = "https://github.com/leafen-org/leafen/releases"
)

// troubleshootingItem is one of 트러블슈팅's, as Notion's app has them under
// 도움말 and, on Windows, in its tray's menu too (buildTroubleshootingMenu,
// 7.43.0): the app's logs shown in Finder or Explorer, and 콘솔 열기 — the
// shown tab's page in the web inspector, its console in front. Notion's
// Chromium diagnostics, meeting status, reset and installation ID have no
// counterpart in Leafen.
type troubleshootingItem struct {
	label string
	do    func()
}

func troubleshootingItems(korean bool) []troubleshootingItem {
	say := func(english, hangul string) string {
		if korean {
			return hangul
		}
		return english
	}
	logs := troubleshootingItem{say("Show Logs in Finder", "Finder에서 로그 표시"), func() {
		if path, err := startupLogPath(); err == nil {
			_ = exec.Command("open", "-R", path).Start()
		}
	}}
	if runtime.GOOS == "windows" {
		logs = troubleshootingItem{say("Show Logs in Explorer", "탐색기에서 로그 표시"), func() {
			if path, err := startupLogPath(); err == nil {
				_ = exec.Command("explorer.exe", "/select,"+path).Start()
			}
		}}
	}
	console := troubleshootingItem{say("Open Console", "콘솔 열기"), func() {
		tabsAsk(func(_ *windowTabs, _ *tabFrame, tab *appTab) {
			if tab != nil && tab.window != nil {
				openConsole(tab.window)
			}
		})
	}}
	return []troubleshootingItem{logs, console}
}

// updateLine is the app menu's update line now, as Notion's app has it
// (7.42.3's menu source): none where the app doesn't look for updates (a
// store's app, a company's or the person's switch), as Notion's has none
// where it doesn't; else the updater's own line (menuLine): on a Mac
// "restart" once a newer version is ready and "downloading" with its whole
// percent while one is on its way, else "check".
func updateLine() (string, int) {
	if runningUpdater == nil {
		return "", 0
	}
	if enabled, err := runningUpdater.Enabled(); err != nil || !enabled["enabled"] {
		return "", 0
	}
	return runningUpdater.menuLine()
}

// checkForUpdates looks for a newer version now and says what it found, in
// Notion's words for its own: the latest or the list out of reach here, a
// newer version by its system (updateFound).
func (background *Background) checkForUpdates(korean bool) {
	say := func(english, hangul string) string {
		if korean {
			return hangul
		}
		return english
	}
	current := app.ReleaseVersion()
	release, err := runningUpdater.checkNow(context.Background())
	switch {
	case err != nil:
		background.showUpdateResult(say("Leafen failed to make a connection with the update server, either because of a problem with your Internet connection or the update server itself. Please try again later.",
			"인터넷 연결 또는 업데이트 서버 자체에 문제가 있어 업데이트 서버에 연결하지 못했습니다. 나중에 다시 시도하세요."), err.Error())
	case release != nil:
		background.updateFound(korean, current, release)
	default:
		background.showUpdateResult(say("You’re on the latest version of Leafen!", "최신 버전의 리펜을 사용 중입니다!"), current)
	}
}

// front is the page in front: the shown tab of the window in front on a Mac
// or Windows (window_tabs.go), else the app window in front — the first one or a page
// opened in a window of its own (new_window.go) — or else the first one.
func (background *Background) front() application.Window {
	if page := frontPage(); page != nil {
		return page
	}
	if tabsInWindows {
		return nil
	}
	if current := background.app.Window.Current(); current != nil {
		return current
	}
	if background.window != nil {
		return background.window
	}
	return nil
}
