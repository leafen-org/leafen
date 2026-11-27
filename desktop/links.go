package main

import (
	"context"
	"net/url"
	"strings"
	"sync"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// Links into the app (docs/app-platform.md 17): a leafen: link another app
// opened, or a Leafen server's address the app was started with. Each waits
// here until the window takes it — the page may still be loading, or
// starting over on another server — and the window hears when one comes.
// The page decides where it leads; Android's page takes its links through
// the app's own messages (LeafenLinks.java).

// openLinkEvent tells the window a link is waiting.
const openLinkEvent = "leafen:open-link"

const (
	maxLinkLength  = 4096
	maxWaitingLink = 8
)

// Links is what the window takes the app's links from.
type Links struct {
	mu      sync.Mutex
	waiting []string
	// window comes forward when a link comes, on a desktop.
	window *application.WebviewWindow
}

var links = &Links{}

// linkWanted says whether a link is one the window may open: a leafen: link
// or a web address, not too long.
func linkWanted(link string) bool {
	if link == "" || len(link) > maxLinkLength {
		return false
	}
	parsed, err := url.Parse(link)
	if err != nil {
		return false
	}
	switch parsed.Scheme {
	case "leafen":
		return true
	case "https", "http":
		return parsed.Host != ""
	}
	return false
}

// opened keeps a link the system handed the app and tells the window.
func (l *Links) opened(link string) {
	link = strings.TrimSpace(link)
	if !linkWanted(link) {
		return
	}
	l.mu.Lock()
	if len(l.waiting) >= maxWaitingLink {
		l.waiting = l.waiting[1:]
	}
	l.waiting = append(l.waiting, link)
	window := l.window
	l.mu.Unlock()
	if !showFront() && window != nil {
		window.Show()
		window.Focus()
	}
	if app := application.Get(); app != nil {
		app.Event.Emit(openLinkEvent)
	}
}

// Take gives the window the links that came, oldest first, each once — on
// a desktop the first window, which comes forward for them, and not a page
// opened in a window of its own (new_window.go); on a Mac the shown tab of
// the window in front (window_tabs.go).
func (l *Links) Take(ctx context.Context) []string {
	var taker application.Window
	if page := frontPage(); page != nil {
		taker = page
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if taker == nil && l.window != nil {
		taker = l.window
	}
	if caller, ok := ctx.Value(application.WindowKey).(application.Window); ok && taker != nil && caller.ID() != taker.ID() {
		return []string{}
	}
	taken := l.waiting
	l.waiting = nil
	if taken == nil {
		return []string{}
	}
	return taken
}

// launchedWithLink keeps the link the system started the app with, or
// handed it while running: on macOS as an Apple event, on Windows as the
// one argument.
func launchedWithLink(event *application.ApplicationEvent) {
	links.opened(event.Context().URL())
}
