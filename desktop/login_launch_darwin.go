//go:build darwin && !ios

package main

/*
#cgo CFLAGS: -x objective-c -fobjc-arc
#cgo LDFLAGS: -framework AppKit -framework CoreServices
#import <AppKit/AppKit.h>
#import <CoreServices/CoreServices.h>

static BOOL leafenOpenedAtLogin = NO;
static BOOL leafenStartedBehind = NO;
static BOOL leafenWindowShown = NO;

// leafenHoldsFront says whether the app, started behind its window, is to
// stay out of the front now: until a window of its own (one that can be the
// main window — not the menu bar's icon, a menu or a panel) is on screen.
// Once one has been, the app comes forward as any app does.
static BOOL leafenHoldsFront(NSApplication *app) {
	if (!(leafenOpenedAtLogin || leafenStartedBehind) || leafenWindowShown) return NO;
	for (NSWindow *window in app.windows) {
		if (window.isVisible && window.canBecomeMainWindow) {
			leafenWindowShown = YES;
			return NO;
		}
	}
	return YES;
}

// The app's own NSApplication, made before Wails asks for the shared one,
// so a start behind its window takes no activation before it shows a
// window: Wails activates every app as it finishes launching, which put a
// login start, or a new version the updater started in its place, in front
// of what the person was doing. The Dock icon, the menu bar's Open and every
// other way the person asks for the app show a window first and then
// activate it.
@interface LeafenApplication : NSApplication
@end

@implementation LeafenApplication
- (void)activateIgnoringOtherApps:(BOOL)flag {
	if (leafenHoldsFront(self)) return;
	[super activateIgnoringOtherApps:flag];
}

- (void)activate {
	if (leafenHoldsFront(self)) return;
	if (@available(macOS 14.0, *)) [super activate];
}
@end

// The event the app opens with says whether macOS opened it as a login
// item; it is the current Apple event only while the app finishes
// launching, so it is read then, before Wails' own answer to the launch.
// behind is the start's own word: a new version the updater starts in
// place of one opened at login.
static int leafenWatchLoginLaunch(int behind) {
	leafenStartedBehind = behind != 0;
	[LeafenApplication sharedApplication];
	[[NSNotificationCenter defaultCenter] addObserverForName:NSApplicationDidFinishLaunchingNotification
		object:nil queue:nil usingBlock:^(NSNotification *note) {
			NSAppleEventDescriptor *event = [[NSAppleEventManager sharedAppleEventManager] currentAppleEvent];
			leafenOpenedAtLogin = event != nil && event.eventID == kAEOpenApplication
				&& [event paramDescriptorForKeyword:keyAEPropData].enumCodeValue == keyAELaunchedAsLogInItem;
		}];
	return [NSApp isKindOfClass:[LeafenApplication class]] ? 1 : 0;
}

// An int, as Objective-C's BOOL is a bool on Apple silicon but a signed char
// on Intel.
static int leafenLaunchedAtLogin(void) { return leafenOpenedAtLogin ? 1 : 0; }
*/
import "C"

import (
	"log"
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

// watchLoginLaunch makes the app's NSApplication, which keeps a start behind
// its window out of the front, and listens for how macOS opened it. It runs
// before the app runs, as Wails asks for the shared NSApplication only then.
func watchLoginLaunch() {
	behind := C.int(0)
	if startedBehind() {
		behind = 1
	}
	if C.leafenWatchLoginLaunch(behind) == 0 {
		log.Print("app start: the app's NSApplication was made before Leafen's, so a start behind its window may come forward")
	}
}

// startedBehind says whether the start's arguments ask for it behind its
// window (the updater's "--background").
func startedBehind() bool { return len(os.Args) > 1 && os.Args[1] == "--background" }

func openedAtLogin() bool {
	return C.leafenLaunchedAtLogin() != 0 || startedBehind()
}

// windowsOptions: a Mac start behind its windows is kept out of the front by
// the app's NSApplication (above).
func windowsOptions() application.WindowsOptions { return application.WindowsOptions{} }

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
