//go:build darwin && !ios

#import <AppKit/AppKit.h>
#import <Carbon/Carbon.h>
#include <stdbool.h>

// ---- Command search ----
//
// The key for the whole computer, as Notion's app's command search (its
// QuickSearchController, 7.43.0, on by default): a hot key the system gives
// the app while another app is in front, with no permission to ask
// (command_search_darwin.go).

extern void leafenCommandSearchPressed(void);

static const OSType leafenCommandSearchSignature = 'LfCs';
static EventHotKeyRef leafenCommandSearchHotKey;
static EventHandlerRef leafenCommandSearchHandler;

static OSStatus leafenCommandSearchHeard(EventHandlerCallRef next, EventRef event, void *data) {
	EventHotKeyID id;
	if (GetEventParameter(event, kEventParamDirectObject, typeEventHotKeyID, NULL, sizeof(id), NULL, &id) == noErr
			&& id.signature == leafenCommandSearchSignature) {
		leafenCommandSearchPressed();
		return noErr;
	}
	return CallNextEventHandler(next, event);
}

// leafenCommandSearchKey takes the key (a virtual key code and Carbon's
// modifier bits) for the app, or gives it back; false when the system
// refused it (another app holds it). The key it held before is given back
// first, and taken again when the new one is refused.
static UInt32 leafenCommandSearchCode, leafenCommandSearchModifiers;

bool leafenCommandSearchKey(bool on, int code, int modifiers) {
	if (!leafenCommandSearchHandler) {
		EventTypeSpec pressed = {kEventClassKeyboard, kEventHotKeyPressed};
		InstallApplicationEventHandler(NewEventHandlerUPP(leafenCommandSearchHeard), 1, &pressed, NULL, &leafenCommandSearchHandler);
	}
	bool held = leafenCommandSearchHotKey != NULL;
	if (held) {
		UnregisterEventHotKey(leafenCommandSearchHotKey);
		leafenCommandSearchHotKey = NULL;
	}
	if (!on) return true;
	EventHotKeyID id = {leafenCommandSearchSignature, 1};
	if (RegisterEventHotKey((UInt32)code, (UInt32)modifiers, id, GetApplicationEventTarget(), 0, &leafenCommandSearchHotKey) == noErr) {
		leafenCommandSearchCode = (UInt32)code;
		leafenCommandSearchModifiers = (UInt32)modifiers;
		return true;
	}
	leafenCommandSearchHotKey = NULL;
	if (held) {
		RegisterEventHotKey(leafenCommandSearchCode, leafenCommandSearchModifiers, id, GetApplicationEventTarget(), 0, &leafenCommandSearchHotKey);
	}
	return false;
}

// leafenBringAppForward puts the app in front of the one in use.
void leafenBringAppForward(void) {
	[NSApp activateIgnoringOtherApps:YES];
}

// leafenCenterOnPointerScreen puts the window in the middle of the screen
// the pointer is on, as Notion's command search window (centerWindowOnDisplay).
void leafenCenterOnPointerScreen(void *window) {
	NSWindow *nsWindow = (__bridge NSWindow *)window;
	NSPoint pointer = [NSEvent mouseLocation];
	NSScreen *screen = [NSScreen mainScreen];
	for (NSScreen *each in [NSScreen screens]) {
		if (NSMouseInRect(pointer, [each frame], NO)) {
			screen = each;
			break;
		}
	}
	if (!screen) return;
	NSRect area = [screen frame];
	NSRect frame = [nsWindow frame];
	frame.origin.x = round(NSMidX(area) - frame.size.width / 2);
	frame.origin.y = round(NSMidY(area) - frame.size.height / 2);
	[nsWindow setFrame:frame display:NO];
}
