//go:build darwin && !ios

package main

/*
#cgo CFLAGS: -x objective-c -fobjc-arc
#cgo LDFLAGS: -framework AppKit -framework CoreServices
#import <AppKit/AppKit.h>
#import <CoreServices/CoreServices.h>

static BOOL leafenOpenedAtLogin = NO;

// The event the app opens with says whether macOS opened it as a login
// item; it is the current Apple event only while the app finishes
// launching, so it is read then, before anything else runs.
static void leafenWatchLoginLaunch(void) {
	[[NSNotificationCenter defaultCenter] addObserverForName:NSApplicationDidFinishLaunchingNotification
		object:nil queue:nil usingBlock:^(NSNotification *note) {
			NSAppleEventDescriptor *event = [[NSAppleEventManager sharedAppleEventManager] currentAppleEvent];
			leafenOpenedAtLogin = event != nil && event.eventID == kAEOpenApplication
				&& [event paramDescriptorForKeyword:keyAEPropData].enumCodeValue == keyAELaunchedAsLogInItem;
		}];
}

// An int, as Objective-C's BOOL is a bool on Apple silicon but a signed char
// on Intel.
static int leafenLaunchedAtLogin(void) { return leafenOpenedAtLogin ? 1 : 0; }
*/
import "C"

import (
	"os"
	"sync"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
)

// macOS opens a login item (SMAppService) with no arguments, and says so in
// the event it opens it with (keyAELaunchedAsLogInItem), which the app knows
// once it has finished launching — before its window's first page shows. A
// new version the updater starts in its place says so with "--background".
const windowWaitsForLaunch = true

func watchLoginLaunch() { C.leafenWatchLoginLaunch() }

func openedAtLogin() bool {
	return C.leafenLaunchedAtLogin() != 0 || len(os.Args) > 1 && os.Args[1] == "--background"
}

// showUnlessOpenedAtLogin shows the window, created hidden, on its first
// page as it would have shown itself, unless macOS opened the app at login.
func showUnlessOpenedAtLogin(window *application.WebviewWindow) {
	var first sync.Once
	window.OnWindowEvent(events.Mac.WebViewDidFinishNavigation, func(*application.WindowEvent) {
		first.Do(func() {
			if !openedAtLogin() {
				application.InvokeAsync(func() { window.Show() })
			}
		})
	})
}
