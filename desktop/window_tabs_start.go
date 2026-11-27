//go:build (darwin && !ios) || windows

package main

import (
	"context"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// A tab's page starts in view: its web view, behind the shown tab's, is not
// hidden (wakeTabPage) until its page says its first screen is drawn whole,
// and a little after; then it is hidden as any tab's not shown. A page opened
// behind the shown one — a ⌘-pressed link, thirteen at once — is so ready by
// the time it is chosen, as a tab opened behind in Notion's app is (its tabs
// can start unthrottled: the prewarmed tab, temporarilyDisableBackgroundThrottling;
// its page tells the app its initial-page-render-completed as this one says
// drawn); hidden from the start, it read its first requests and waited until
// shown (2026-11-13).

const (
	// tabStartSettles is how long a page stays in view once its first screen
	// is drawn, for the reads its page's body may still make.
	tabStartSettles = 3 * time.Second
	// tabStartAtMost is the longest a page stays in view as it starts.
	tabStartAtMost = time.Minute
)

// startAwake keeps a tab's new page in view as it starts.
func (t *windowTabs) startAwake(tab *appTab, window *application.WebviewWindow) {
	tab.starting = window
	wakeTabPage(window)
	time.AfterFunc(tabStartAtMost, func() { onMain(func() { t.started(tab, window) }) })
}

// drawn hears a tab's page say its first screen is drawn.
func (t *windowTabs) drawn(tab *appTab) {
	window := tab.starting
	if window == nil || tab.startDrawn {
		return
	}
	tab.startDrawn = true
	time.AfterFunc(tabStartSettles, func() { onMain(func() { t.started(tab, window) }) })
	if ring := tab.ringDrawn; !ring.none() {
		tab.ringDrawn = stripRing{}
		if frame := tab.host; frame != nil && frame.active == tab {
			ringStrip(frame, ring)
		}
	}
}

// started ends a tab's page's start, once.
func (t *windowTabs) started(tab *appTab, window *application.WebviewWindow) {
	if tab.starting != window {
		return
	}
	tab.starting, tab.startDrawn = nil, false
	// Kept in view to follow a step still, it rests when that ends (followRested).
	if tab.window == window && !time.Now().Before(tab.awakeUntil) {
		restTabPage(window)
	}
}

// tabFollowAwake is how long the pages behind the shown tab stay in view once
// a step they follow passed in it.
const tabFollowAwake = 6 * time.Second

// WakeBehind keeps the pages of the tabs behind the shown one in view a
// moment, as a page starting is: a step of the first run passed in the shown
// tab, which those tabs follow (web/src/lib/desktopTabs.ts wakeTabsBehind).
// Hidden, a page's timers and its requests to the app wait for seconds at a
// time, and a tab behind followed each step 1–16 s after the one in front
// (2026-11-14); in view, as soon as it hears. Only the shown tab's page asks.
func (*WindowTabs) WakeBehind(ctx context.Context) {
	caller, _ := ctx.Value(application.WindowKey).(application.Window)
	if caller == nil {
		return
	}
	onMain(func() {
		frame, tab := desktopTabs.frameOf(caller)
		if frame == nil || tab == nil || tab != frame.active || tab.window == nil || tab.window.ID() != caller.ID() {
			return
		}
		desktopTabs.wakeBehind()
	})
}

// wakeBehind keeps every loaded page not shown in view for tabFollowAwake.
func (t *windowTabs) wakeBehind() {
	until := time.Now().Add(tabFollowAwake)
	for _, tab := range t.allTabs() {
		window := tab.window
		if window == nil || tab.host == nil || tab.host.active == tab {
			continue
		}
		tab.awakeUntil = until
		wakeTabPage(window)
		time.AfterFunc(tabFollowAwake, func() { onMain(func() { t.followRested(tab, window) }) })
	}
}

// followRested hides a page kept in view to follow once its time is up,
// unless it is starting still or another step woke it since.
func (t *windowTabs) followRested(tab *appTab, window *application.WebviewWindow) {
	if tab.window != window || tab.starting == window || time.Now().Before(tab.awakeUntil) {
		return
	}
	restTabPage(window)
}
