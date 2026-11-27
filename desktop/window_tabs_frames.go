//go:build (darwin && !ios) || windows

package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
)

// stripPage is the frame's own page, its strip of tabs (server.go serves it).
const stripPage = "/desktop-tabs/tab_strip.html"

// tabsFile keeps the windows and their tabs for the app's next start, as
// Notion's app does unless it quits without saving tabs.
const tabsFile = "tabs.json"

// A page's icon kept for next time is at most this long; a larger one comes
// back with the page.
const maxKeptIcon = 64 << 10

// savedTab is a tab as kept, or as closed to open again.
type savedTab struct {
	Path   string `json:"path"`
	Title  string `json:"title,omitempty"`
	Icon   string `json:"icon,omitempty"`
	Pinned bool   `json:"pinned,omitempty"`
	Active bool   `json:"active,omitempty"`
	// SignIn is the sign-in its page showed (window_tabs_kept.go).
	SignIn string `json:"signIn,omitempty"`
	// Group is the id of the group it was in, among its window's.
	Group string `json:"group,omitempty"`
	Index int    `json:"-"`
}

// savedFrame is a window as kept: its tabs and where it was on the screen
// (macOS's own frame, from the bottom left; on Windows its place when not
// maximized, from the top left of the work area, and whether it was).
type savedFrame struct {
	Tabs      []savedTab  `json:"tabs"`
	Frame     *[4]float64 `json:"frame,omitempty"`
	Maximized bool        `json:"maximized,omitempty"`
	// Groups are the groups its tabs were in (window_tabs_groups.go).
	Groups []savedGroup `json:"groups,omitempty"`
	// Section and Shown are what earlier versions kept of the tab space the
	// window showed and the tab it last showed in each, read once to bring
	// the spaces back as groups (spacesAsGroups).
	Section string         `json:"section,omitempty"`
	Shown   map[string]int `json:"shown,omitempty"`
	// At is where on the screen a tab dragged out of its window was let go,
	// and From that window, whose size the new one takes on Windows.
	At   *[2]int                    `json:"-"`
	From *application.WebviewWindow `json:"-"`
}

// tabsKept is the file: the windows, front first, and the pages' colors
// for the strips until a page says them again; Spaces are the tab spaces
// an earlier version kept, read once (spacesAsGroups).
type tabsKept struct {
	Windows []*savedFrame `json:"windows"`
	Spaces  []*savedSpace `json:"spaces,omitempty"`
	Look    *stripLook    `json:"look,omitempty"`
}

// closedTabs is what one close took: tabs of a window still open, with the
// groups they were in, or a whole window.
type closedTabs struct {
	frame  *tabFrame
	groups []savedGroup
	tabs   []savedTab
	window *savedFrame
}

var tabsQuitting atomic.Bool

// firstWindow opens the windows the app had when it last ended, with their
// tabs, or one on the start page, and gives the one in front; each shows
// itself once its strip is in place, the first on a Mac as the app shows it.
func firstWindow(desktop *application.App) *application.WebviewWindow {
	t := desktopTabs
	desktop.OnShutdown(t.ending)
	saved, spaces := loadSavedFrames()
	saved = spacesAsGroups(saved, spaces)
	if len(saved) == 0 {
		saved = []*savedFrame{{Tabs: []savedTab{{Path: "/"}}}}
	}
	t.startUnloading()
	var first *tabFrame
	for index, kept := range saved {
		// The first shows itself once its strip is in place, but on a Mac,
		// whose app shows it as it finishes launching (login_launch_darwin.go).
		frame := t.newFrame(kept, index > 0 || !windowWaitsForLaunch)
		frame.atStart = true
		frame.focused = int64(len(saved) - index)
		if first == nil {
			first = frame
		}
	}
	t.focus = int64(len(saved))
	return first.window
}

func (t *windowTabs) tabsFrom(kept *savedFrame) ([]*appTab, *appTab) {
	var tabs []*appTab
	var active *appTab
	for _, saved := range kept.Tabs {
		tab := t.tabFromSaved(saved)
		tabs = append(tabs, tab)
		if saved.Active || active == nil {
			active = tab
		}
	}
	if len(tabs) == 0 {
		active = t.newTab("/")
		tabs = []*appTab{active}
	}
	return tabs, active
}

// newFrame opens a window with the tabs kept for it, in their groups.
func (t *windowTabs) newFrame(kept *savedFrame, show bool) *tabFrame {
	tabs, active := t.tabsFrom(kept)
	frame := t.openFrame(tabs, active, kept, show)
	t.regroup(frame, tabs, kept.Tabs, kept.Groups)
	return frame
}

// openFrame opens a window holding tabs. Its strip is set in place and its
// shown tab's page made once the strip's page has come (adopt); a window
// shown by the app itself (the first) waits for that too.
func (t *windowTabs) openFrame(tabs []*appTab, active *appTab, kept *savedFrame, show bool) *tabFrame {
	frame := &tabFrame{section: &tabSection{tabs: tabs}, active: active, show: show, saved: kept}
	t.focus++
	frame.focused = t.focus
	frame.window = application.Get().Window.NewWithOptions(frameOptions())
	t.frames = append(t.frames, frame)
	// The strip's page has come: the strip is set in place (adopt), once;
	// the frame enters or leaves full screen (window_tabs_darwin.go,
	// window_tabs_windows.go).
	var once sync.Once
	watchFrame(frame.window, func() {
		once.Do(func() { application.InvokeSync(func() { t.adopt(frame) }) })
	}, func(on bool) {
		onMain(func() {
			frame.fullscreen = on
			t.drawStrip(frame)
			// Closed in full screen while the app keeps running: it hides
			// once it has left full screen (closeAsked).
			if !on && frame.hideWindowed {
				frame.hideWindowed = false
				frame.window.Hide()
			}
		})
	})
	// Its close button and ⌘⇧W close the window and its tabs, kept to open
	// again; the last open one hides or ends the app (closeAsked).
	frame.window.RegisterHook(events.Common.WindowClosing, func(event *application.WindowEvent) {
		if frame.closing.Load() || tabsQuitting.Load() {
			return
		}
		event.Cancel()
		onMain(func() { t.closeAsked(frame) })
	})
	frame.window.OnWindowEvent(events.Common.WindowFocus, func(*application.WindowEvent) {
		onMain(func() { t.frameFocused(frame) })
	})
	return frame
}

// frameFocused is a frame taking the focus: it is the window in front, and
// the keys go back to its shown page, or to the strip's menu left open over
// it (Windows keeps it open behind other windows).
func (t *windowTabs) frameFocused(frame *tabFrame) {
	t.focus++
	frame.focused = t.focus
	if menu := frame.menu; menu != nil && !menu.picking {
		focusStrip(frame)
	} else if tab := frame.active; tab != nil && tab.window != nil && frame.adopted && tab.host == frame {
		focusTabView(frame, tab)
	}
}

// focusFrame brings a frame forward as the app asks for it — shown again
// from the tray or its menu, by itself once its server is up, from a link
// or the Dock, or a tab or a page put in it — with the keys on its shown
// page. Where focusing gives the strip the keys (focusGivesStripKeys), they
// are handed on here rather than left to the window's focus event: the
// hidden window a start behind its windows makes is already the app's
// active window, so showing and focusing it sends no such event, and the
// keys stayed on the strip (the Windows VM 2026-11-28). A window brought
// forward so is one asked for (asked).
func (t *windowTabs) focusFrame(frame *tabFrame) {
	t.asked = true
	frame.window.Focus()
	if focusGivesStripKeys {
		t.frameFocused(frame)
	}
}

// adopt sets a frame's strip in place and shows its tab, once its strip's
// page has come; a frame that shows itself shows then.
func (t *windowTabs) adopt(frame *tabFrame) {
	if frame.adopted || !containsFrame(t.frames, frame) {
		return
	}
	frame.adopted = true
	if t.zoom > 0 {
		setTabZoom(frame.window, t.zoom)
	}
	frame.lights = adoptFrameView(frame.window)
	if kept := frame.saved; kept != nil {
		placeFrame(frame.window, kept)
	}
	for _, tab := range frame.section.tabs {
		if tab == frame.active || tab.window != nil {
			t.tabWindow(frame, tab)
		}
	}
	if frame.active != nil {
		t.selectTab(frame, frame.active)
	}
	if frame.show && !(frame.atStart && openedAtLogin()) {
		frame.window.Show()
		t.focusFrame(frame)
	}
}

// showOnceAdopted has a frame whose strip isn't in place yet show once it is
// (adopt): it was asked for, so a start behind its windows no longer holds
// it back.
func (frame *tabFrame) showOnceAdopted() {
	frame.show, frame.atStart = true, false
	desktopTabs.asked = true
}

// tabWindow is the window a tab's page is in, made (hidden, never shown)
// when the tab first needs its page, its web view put in the frame.
func (t *windowTabs) tabWindow(frame *tabFrame, tab *appTab) *application.WebviewWindow {
	if tab.window != nil {
		return tab.window
	}
	window := application.Get().Window.NewWithOptions(tabWindowOptions(tab.path))
	tab.window = window
	keepTabWindowHidden(window)
	routeNewWindows(window)
	window.RegisterHook(events.Common.WindowClosing, func(event *application.WindowEvent) {
		if !tab.closing.Load() && !tabsQuitting.Load() {
			event.Cancel()
		}
	})
	if t.zoom > 0 {
		setTabZoom(window, t.zoom)
	}
	addTabView(frame, tab)
	t.startAwake(tab, window)
	return window
}

// closeAsked closes a window as its close button, ⌘⇧W or its last tab's ⌘W
// asks: kept to open again. The last window open, while the app keeps
// running in the background, hides — out of full screen first — or, with
// another already hidden, closes, as Notion's app (handleWindowClose hides
// only its one window); else the app ends, as closing its window always did.
func (t *windowTabs) closeAsked(frame *tabFrame) {
	if !containsFrame(t.frames, frame) {
		return
	}
	hidden := false
	for _, other := range t.frames {
		if other == frame {
			continue
		}
		if other.window != nil && windowOpen(other.window) {
			t.closeFrame(frame)
			return
		}
		hidden = true
	}
	if desktopBackground.keepsRunning() {
		if hidden {
			t.closeFrame(frame)
		} else if frame.fullscreen && runtime.GOOS == "darwin" {
			frame.hideWindowed = true
			frame.window.UnFullscreen()
		} else {
			frame.window.Hide()
		}
		return
	}
	t.keepNow()
	go application.Get().Quit()
}

// closeFrame closes a window and its tabs, kept to open again (⌘⇧T).
func (t *windowTabs) closeFrame(frame *tabFrame) {
	t.remember(closedTabs{window: t.savedFrameOf(frame)})
	t.leaveFrame(frame)
}

// leaveFrame closes a window without keeping it: its tabs' pages end with it.
func (t *windowTabs) leaveFrame(frame *tabFrame) {
	for i, each := range t.frames {
		if each == frame {
			t.frames = append(t.frames[:i], t.frames[i+1:]...)
			break
		}
	}
	for _, tab := range frame.section.tabs {
		t.dropTabWindow(tab)
	}
	frame.section.tabs, frame.groups, frame.active, frame.menu = nil, nil, nil, nil
	frame.closing.Store(true)
	frame.window.Close()
	t.keep()
}

func (t *windowTabs) savedFrameOf(frame *tabFrame) *savedFrame {
	kept := &savedFrame{Tabs: savedTabsOf(frame.section.tabs, frame.active), Groups: savedGroupsOf(frame.section.tabs)}
	if frame.adopted {
		kept.Frame, kept.Maximized = frameRect(frame.window), frameMaximized(frame.window)
	} else if frame.saved != nil {
		kept.Frame, kept.Maximized = frame.saved.Frame, frame.saved.Maximized
	}
	return kept
}

// keep writes the windows and tabs down a moment after they change, so an
// app that ends without quitting still opens them again.
var keepTimer struct {
	mu    sync.Mutex
	timer *time.Timer
}

func (t *windowTabs) keep() {
	keepTimer.mu.Lock()
	defer keepTimer.mu.Unlock()
	if keepTimer.timer != nil {
		keepTimer.timer.Stop()
	}
	keepTimer.timer = time.AfterFunc(time.Second, func() { onMain(t.keepNow) })
}

// keepNow writes the windows down at once, front first; a window hidden in
// the background is kept too.
func (t *windowTabs) keepNow() {
	if t.keepNone {
		return
	}
	frames := append([]*tabFrame(nil), t.frames...)
	for i := 1; i < len(frames); i++ {
		for j := i; j > 0 && frames[j].focused > frames[j-1].focused; j-- {
			frames[j], frames[j-1] = frames[j-1], frames[j]
		}
	}
	var kept []*savedFrame
	for _, frame := range frames {
		if len(frame.section.tabs) > 0 {
			kept = append(kept, t.savedFrameOf(frame))
		}
	}
	data, err := json.Marshal(tabsKept{Windows: kept, Look: &t.look})
	if err != nil {
		return
	}
	writeWait.Add(1)
	go func() {
		defer writeWait.Done()
		writeTabsFile(data)
	}()
}

// ending keeps the tabs as the app ends — or none, quitting without them.
func (t *windowTabs) ending() {
	tabsQuitting.Store(true)
	if t.keepNone {
		if path, err := tabsPath(); err == nil {
			_ = os.Remove(path)
		}
		return
	}
	t.keepNow()
	keepTimer.mu.Lock()
	if keepTimer.timer != nil {
		keepTimer.timer.Stop()
	}
	keepTimer.mu.Unlock()
	writeWait.Wait()
}

var writeWait sync.WaitGroup
var writeMu sync.Mutex

func writeTabsFile(data []byte) {
	writeMu.Lock()
	defer writeMu.Unlock()
	path, err := tabsPath()
	if err != nil || tabsQuitting.Load() && desktopTabs.keepNone {
		return
	}
	temporary := path + ".tmp"
	if os.WriteFile(temporary, data, 0o600) == nil {
		_ = os.Rename(temporary, path)
	}
}

func tabsPath() (string, error) {
	local, err := localDataDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(filepath.Dir(local), tabsFile), nil
}

// loadSavedFrames reads the windows kept last time, each with at least one
// tab whose page is one of the app's own or a tab space an earlier version
// showed, and the tab spaces it kept.
func loadSavedFrames() ([]*savedFrame, []*savedSpace) {
	path, err := tabsPath()
	if err != nil {
		return nil, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, nil
	}
	var file tabsKept
	if json.Unmarshal(data, &file) != nil {
		return nil, nil
	}
	if file.Look != nil && len(file.Look.Colors) > 0 {
		desktopTabs.look = *file.Look
	}
	var kept []*savedFrame
	for _, frame := range file.Windows {
		if frame == nil {
			continue
		}
		if frame.Tabs = appPagesOnly(frame.Tabs); len(frame.Tabs) > 0 || frame.Section != "" {
			kept = append(kept, frame)
		}
	}
	var spaces []*savedSpace
	for _, space := range file.Spaces {
		if space != nil && strings.TrimSpace(space.Title) != "" {
			space.Tabs = appPagesOnly(space.Tabs)
			spaces = append(spaces, space)
		}
	}
	return kept, spaces
}

// appPagesOnly are the kept tabs whose pages are the app's own.
func appPagesOnly(tabs []savedTab) []savedTab {
	kept := tabs[:0]
	for _, tab := range tabs {
		if strings.HasPrefix(tab.Path, "/") && !strings.HasPrefix(tab.Path, "//") && len(tab.Path) <= maxLinkLength {
			kept = append(kept, tab)
		}
	}
	return kept
}
