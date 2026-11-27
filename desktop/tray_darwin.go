//go:build darwin && !ios

package main

/*
#cgo CFLAGS: -x objective-c -fobjc-arc
#cgo LDFLAGS: -framework AppKit

#import <AppKit/AppKit.h>

static NSStatusBarButton *leafenBareStatusButton(NSView *view, int depth) {
	if ([view isKindOfClass:[NSStatusBarButton class]]) {
		NSStatusBarButton *button = (NSStatusBarButton *)view;
		return button.image == nil && button.title.length == 0 ? button : nil;
	}
	if (depth == 0) return nil;
	for (NSView *child in view.subviews) {
		NSStatusBarButton *found = leafenBareStatusButton(child, depth - 1);
		if (found) return found;
	}
	return nil;
}

// The app's item just made is the one with neither picture nor title yet.
static int leafenSetMenuBarIcon(const void *bytes, int length, double side) {
	NSImage *image = [[NSImage alloc] initWithData:[NSData dataWithBytes:bytes length:length]];
	if (!image) return 0;
	image.size = NSMakeSize(side, side);
	image.template = YES;
	for (NSWindow *window in NSApp.windows) {
		NSStatusBarButton *button = leafenBareStatusButton(window.contentView, 3);
		if (button) {
			button.image = image;
			button.imagePosition = NSImageOnly;
			return 1;
		}
	}
	return 0;
}
*/
import "C"

import (
	"unsafe"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// menuBarIconSide is Notion's menu bar icon's (menuBarIconTemplate, 32 pixels
// at twice the scale), which makes its item 34 points wide with the room
// AppKit leaves around a picture.
const menuBarIconSide = 16

// setMenuBarIcon puts the picture on the menu bar item SystemTray.New has just
// made, on the main thread, at Notion's size: Wails sizes a template to the
// bar's thickness (22 points, an item 40 wide). False when no such item was
// found.
func setMenuBarIcon(icon []byte) bool {
	return C.leafenSetMenuBarIcon(unsafe.Pointer(&icon[0]), C.int(len(icon)), menuBarIconSide) == 1
}

// setTaskbarIcon is Windows' (tray_windows.go): a Mac's menu bar colors the
// template itself.
func setTaskbarIcon(*application.App, *application.SystemTray) {}
