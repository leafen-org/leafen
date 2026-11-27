//go:build (darwin && !ios) || windows

package main

import (
	_ "embed"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"

	"leafen.org/leafen/internal/app"
)

// Background keeps the app running when its last window closes, as Notion's
// app does (isHideLastWindowOnCloseEnabled, "백그라운드에서 계속 실행", on
// until turned off): the window hides, the command search's key and icon
// stay, and the app serves its workspace to other devices on; the Dock
// (macOS), the menu bar icon or the tray (Windows) brings it back, and its
// menus' quit ends it. Turned off, closing the last window ends the app.
// While this computer serves — its workspace reached from other devices — it
// says so in the menu bar or tray; it opens at login; and, when the person
// allowed it at install, it keeps the computer from sleeping — never the
// display, and on battery only when they say so, as the systems' own
// "prevent sleeping" settings hold on a power adapter only.
type Background struct {
	mu      sync.Mutex
	app     *application.App
	window  *application.WebviewWindow
	local   *localServer
	tray    *application.SystemTray
	status  *application.MenuItem
	trayKey string
	menuKey string
	// The app menu's update line while a download goes on (app_menu_bar_darwin.go).
	updateItem *application.MenuItem
	// The app runs its main thread, where the menu is drawn.
	started  bool
	awake    keepAwake
	settings backgroundSettings
	language string
	serving  bool
	battery  bool
	asserted bool
	wake     chan struct{}
}

// The menu bar's and tray's icon (2026-10-27: the mark's picture on a white
// square stood out among the others): the leaf of the mascot's leaf pin, its
// centre vein cut out — on macOS a template the menu bar colors (32 pixels
// for Notion's 16 points), also the Windows strip's app menu mark
// (window_tabs_strip.go), as every small one-color mark of the app is the
// leaf and only the app's own icon the girl; on Windows dark on a light
// taskbar and light on a dark one.
var (
	//go:embed build/tray/menu-bar.png
	trayMenuBar []byte
	//go:embed build/tray/light-taskbar.png
	trayLightTaskbar []byte
	//go:embed build/tray/dark-taskbar.png
	trayDarkTaskbar []byte
)

// backgroundSettings are the person's, kept beside the app's local data.
type backgroundSettings struct {
	// Closing the last window hides it rather than ending the app.
	KeepRunning bool `json:"keepRunning"`
	OpenAtLogin bool `json:"openAtLogin"`
	// Allowed at install (desktop-agent consent), off until then.
	KeepAwake      bool `json:"keepAwake"`
	AwakeOnBattery bool `json:"awakeOnBattery"`
	// ⇧⌘K (Ctrl+Shift+K) opens the app's search from anywhere
	// (command_search_desktop.go), on until turned off, as Notion's app's
	// command search; the key chosen instead, as Notion writes it
	// ("shift+command+k"), none for the default.
	CommandSearch         bool   `json:"commandSearch"`
	CommandSearchShortcut string `json:"commandSearchShortcut"`
	// The Mac's menu bar icon while command search is on, on until turned
	// off, as Notion's "메뉴 모음에 Notion 표시".
	MenuBarIcon bool `json:"menuBarIcon"`
}

// BackgroundState is what the window is told: the settings, and what the
// app does now.
type BackgroundState struct {
	backgroundSettings
	// Its workspace is reached from other devices.
	Serving   bool   `json:"serving"`
	Awake     bool   `json:"awake"`
	OnBattery bool   `json:"onBattery"`
	LoginItem string `json:"loginItem"`
}

func backgroundFile() (string, error) {
	local, err := localDataDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(filepath.Dir(local), "background.json"), nil
}

func loadBackgroundSettings() backgroundSettings {
	settings := backgroundSettings{KeepRunning: true, OpenAtLogin: true, CommandSearch: true, MenuBarIcon: true}
	if path, err := backgroundFile(); err == nil {
		if data, err := os.ReadFile(path); err == nil {
			_ = json.Unmarshal(data, &settings)
		}
	}
	return settings
}

func (background *Background) save() error {
	path, err := backgroundFile()
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(background.settings, "", "  ")
	if err != nil {
		return err
	}
	temporary := path + ".tmp"
	if err := os.WriteFile(temporary, data, 0o600); err != nil {
		return err
	}
	return os.Rename(temporary, path)
}

// start takes the window and the server once the app has them; hidden says,
// once the app has started, whether the system opened it at login.
func (background *Background) start(desktop *application.App, window *application.WebviewWindow, local *localServer, hidden func() bool) {
	background.mu.Lock()
	background.app, background.window, background.local = desktop, window, local
	background.settings = loadBackgroundSettings()
	background.wake = make(chan struct{}, 1)
	background.mu.Unlock()
	// Closing the window while the app keeps running in the background, or
	// while a page opened in another window is still open (new_window.go),
	// leaves the app running, the window hidden until it is opened again. A
	// window holding tabs (Mac, Windows) decides so itself, as any of its
	// windows closes (window_tabs_frames.go).
	window.RegisterHook(events.Common.WindowClosing, func(event *application.WindowEvent) {
		if tabsInWindows {
			return
		}
		if background.keepsRunning() || anotherWindowOpen(window) {
			event.Cancel()
			window.Hide()
			return
		}
		// Not kept in the background, closing the window ends the app (the
		// app no longer ends by itself with its last window, which ended it
		// even when that window was only hidden).
		go desktop.Quit()
	})
	// The Dock icon pressed with no window of the app's on screen brings its
	// windows back (window_tabs_commands.go); Wails' own answer showed every
	// hidden window, the command search's and the tabs' pages too.
	desktop.Event.RegisterApplicationEventHook(events.Mac.ApplicationShouldHandleReopen, func(event *application.ApplicationEvent) {
		event.Cancel()
		reopenWindows()
	})
	// The tray and the window are the main thread's, which runs once the app
	// has started.
	desktop.Event.OnApplicationEvent(events.Common.ApplicationStarted, func(*application.ApplicationEvent) {
		background.mu.Lock()
		background.started = true
		settings := background.settings
		background.mu.Unlock()
		go func() {
			_ = takeCommandSearchKey(settings.CommandSearch, commandSearchKeyText(settings))
			background.poke()
		}()
		go background.watch(hidden())
	})
}

// keepsRunning says whether the app stays, its window hidden, when its last
// window closes: unless the person turned "백그라운드에서 계속 실행" off.
func (background *Background) keepsRunning() bool {
	background.mu.Lock()
	defer background.mu.Unlock()
	return background.settings.KeepRunning
}

// servesNow reads whether the computer serves other devices now, from its
// server's outside access, for what is shown at once — the "이 컴퓨터"
// line — rather than the last twenty-second step's answer; a change found
// wakes the step for the tray and the rest (2026-11-07: for twenty seconds
// after an address was turned on the line said nothing was served).
func (background *Background) servesNow() bool {
	serving, _ := background.serves()
	background.mu.Lock()
	changed := serving != background.serving
	background.mu.Unlock()
	if changed {
		background.poke()
	}
	return serving
}

// watch follows whether the computer serves, about every twenty seconds and
// whenever something changes, and keeps the tray, the login item and the
// computer's sleep in step. Its first step is at once, a start behind its
// windows too: the menu bar's or tray's icon is there from the start, as
// Notion's (its tray made as the app is ready, at login too), reading as
// serving nothing until the server answers; its Open shows the window,
// which waits for the server on the app's own page as a start in front does,
// rather than leaving the person with neither an icon nor a window until the
// server answers.
func (background *Background) watch(hidden bool) {
	ticker := time.NewTicker(20 * time.Second)
	defer ticker.Stop()
	background.step()
	if hidden {
		// Started behind its window at login: once its server is up, a
		// computer that serves nothing shows the window after all — unless
		// a window was asked forward meanwhile (the icon, the Dock, a link),
		// which stays as the person left it.
		ready, _, _, _ := background.local.current()
		select {
		case <-ready:
		case <-time.After(2 * time.Minute):
		}
		background.step()
		background.mu.Lock()
		serving := background.serving
		background.mu.Unlock()
		if !serving {
			application.InvokeSync(func() {
				if !desktopTabs.asked {
					background.Open()
				}
			})
		}
	}
	for {
		select {
		case <-ticker.C:
		case <-background.wake:
		}
		background.step()
	}
}

func (background *Background) poke() {
	select {
	case background.wake <- struct{}{}:
	default:
	}
}

// serves reads whether the computer serves other devices, from its server's
// outside access; known is false until the server answers (it starts after
// the app) or while its setting can't be read.
func (background *Background) serves() (serving bool, known bool) {
	if background.local == nil {
		return false, false
	}
	background.local.mu.RLock()
	product := background.local.product
	background.local.mu.RUnlock()
	if product == nil {
		return false, false
	}
	serving, err := app.ServesOthers(product)
	return serving, err == nil
}

func (background *Background) step() {
	serving, known := background.serves()
	battery := onBatteryPower()
	background.mu.Lock()
	background.serving, background.battery = serving, battery
	settings, active, language := background.settings, background.serving, background.language
	label := background.statusLabel()
	background.mu.Unlock()
	application.InvokeSync(func() {
		background.showTray(trayWanted{serving: active && settings.KeepRunning, label: label, language: language,
			commandSearch: settings.CommandSearch, menuBarIcon: settings.MenuBarIcon || runtime.GOOS != "darwin",
			openAtLogin: settings.OpenAtLogin, keepRunning: settings.KeepRunning, key: commandSearchKeyText(settings)})
		background.showMenu(language)
	})
	if settings.OpenAtLogin && known {
		background.followServing(active)
	}
	assert := active && settings.KeepAwake && (settings.AwakeOnBattery || !battery)
	background.mu.Lock()
	defer background.mu.Unlock()
	if assert != background.asserted {
		if assert {
			background.asserted = background.awake.hold("Leafen serves this workspace") == nil
		} else {
			background.awake.release()
			background.asserted = false
		}
	}
}

// followServing keeps opening at login as its switch says — "while this
// computer serves its workspace to other devices, Leafen opens when you log
// in": on while it serves, off once it stops (the outside address turned off,
// "나중에" after choosing Tailscale at the first run), on again when it serves
// again. Only on a known answer, so a start before the server is up keeps
// what the last run left. A Mac's item still waiting for approval goes too;
// Windows' startup app the person turned off ("needs-approval" there) is
// theirs to turn on and is left as it is.
func (background *Background) followServing(serving bool) {
	state := loginItemState()
	switch {
	case serving && state == "off":
		_ = setLoginItem(true)
	case !serving && (state == "on" || state == "needs-approval" && runtime.GOOS == "darwin"):
		_ = setLoginItem(false)
	}
}

// statusLabel is the tray's line on what the app is doing, in the window's
// language.
func (background *Background) statusLabel() string {
	if strings.HasPrefix(background.language, "ko") {
		return "이 워크스페이스를 서버로 여는 중"
	}
	return "Serving this workspace"
}

// Open brings the window back: on a Mac the one last in front, with its tabs.
func (background *Background) Open() {
	if showFront() || background.window == nil {
		return
	}
	background.window.Show()
	background.window.Focus()
}

// SetLanguage takes the window's language, for the tray's words.
func (background *Background) SetLanguage(language string) {
	background.mu.Lock()
	background.language = language
	background.mu.Unlock()
	background.poke()
}

// State says what the settings are and what the app does now.
func (background *Background) State() BackgroundState {
	serving := background.servesNow()
	background.mu.Lock()
	defer background.mu.Unlock()
	return BackgroundState{backgroundSettings: background.settings, Serving: serving,
		Awake: background.asserted, OnBattery: background.battery, LoginItem: loginItemState()}
}

// Set changes the settings; opening at login follows at once.
func (background *Background) Set(settings backgroundSettings) (BackgroundState, error) {
	background.mu.Lock()
	before := background.settings
	background.settings = settings
	err := background.save()
	background.mu.Unlock()
	if err != nil {
		return BackgroundState{}, err
	}
	if before.OpenAtLogin && !settings.OpenAtLogin {
		_ = setLoginItem(false)
	}
	if before.CommandSearch != settings.CommandSearch || commandSearchKeyText(before) != commandSearchKeyText(settings) {
		if err := takeCommandSearchKey(settings.CommandSearch, commandSearchKeyText(settings)); err != nil {
			// The key the system refused is not kept; the one before stays.
			background.mu.Lock()
			background.settings.CommandSearchShortcut = before.CommandSearchShortcut
			background.settings.CommandSearch = before.CommandSearch
			_ = background.save()
			background.mu.Unlock()
			return BackgroundState{}, err
		}
	}
	background.step()
	return background.State(), nil
}

// ServiceShutdown lets the computer sleep again as the app ends.
func (background *Background) ServiceShutdown() error {
	background.mu.Lock()
	defer background.mu.Unlock()
	background.awake.release()
	background.asserted = false
	return nil
}
