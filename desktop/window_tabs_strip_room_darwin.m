//go:build darwin && !ios

#import <AppKit/AppKit.h>
#import <WebKit/WebKit.h>
#import <objc/runtime.h>

// A frame's strip grown over its window while a menu it draws is open
// (window_tabs_strip_room_darwin.go), as Notion's tab bar's menus stand over
// its page: the strip's web view lies over the frame's whole content, in
// front of the pages and see-through where it draws nothing, and takes the
// presses there — but in its holes, the places it leaves to the shown page's
// icon picker over a group's menu, where the presses go through to the page
// under it, as they reach Notion's picker; and, for a tab's menu, everywhere
// but the strip's bar and the menus, as a press outside Notion's tab menu
// goes on to its page.

extern WKWebView *leafenFindWebView(NSView *view);

static void leafenRoomOnMain(void (^block)(void)) {
	if ([NSThread isMainThread]) block();
	else dispatch_sync(dispatch_get_main_queue(), block);
}

// leafenStripOf is a frame's own web view, its strip (Wails' window keeps it).
static WKWebView *leafenStripOf(NSWindow *window) {
	if ([window respondsToSelector:NSSelectorFromString(@"webView")]) {
		id view = [window valueForKey:@"webView"];
		if ([view isKindOfClass:[WKWebView class]]) return view;
	}
	return leafenFindWebView(window.contentView);
}

// leafenTabsGrowStrip lays the frame's strip over its whole content while a
// menu it draws is open, or back along the top.
void leafenTabsGrowStrip(void *native, bool grow, int stripHeight) {
	leafenRoomOnMain(^{
		NSWindow *window = (__bridge NSWindow *)native;
		WKWebView *strip = leafenStripOf(window);
		NSView *content = window.contentView;
		if (!strip || !content) return;
		NSRect bounds = content.bounds;
		if (grow) [strip setValue:@NO forKey:@"drawsBackground"];
		strip.frame = grow ? bounds : NSMakeRect(0, NSHeight(bounds) - stripHeight, NSWidth(bounds), stripHeight);
		strip.autoresizingMask = grow ? (NSViewWidthSizable | NSViewHeightSizable) : (NSViewWidthSizable | NSViewMinYMargin);
	});
}

// ---- The strip's holes ----
//
// A web view hit inside one of its holes (points from its top left) is not
// hit: AppKit goes on to the view under it, the shown page; nor, with solid
// places, outside them all. Web views with neither are hit as ever.

static const char leafenHolesKey = 0;
static const char leafenSolidsKey = 0;
static IMP leafenHitTestBefore;

static NSView *leafenHoledHitTest(NSView *view, SEL command, NSPoint point) {
	NSArray<NSValue *> *holes = objc_getAssociatedObject(view, &leafenHolesKey);
	NSArray<NSValue *> *solids = objc_getAssociatedObject(view, &leafenSolidsKey);
	if ((holes.count > 0 || solids.count > 0) && view.superview) {
		NSPoint local = [view convertPoint:point fromView:view.superview];
		NSPoint fromTop = NSMakePoint(local.x, view.isFlipped ? local.y : NSHeight(view.bounds) - local.y);
		for (NSValue *hole in holes) {
			if (NSPointInRect(fromTop, hole.rectValue)) return nil;
		}
		BOOL solid = solids.count == 0;
		for (NSValue *place in solids) {
			if (NSPointInRect(fromTop, place.rectValue)) solid = YES;
		}
		if (!solid) return nil;
	}
	return ((NSView *(*)(id, SEL, NSPoint))leafenHitTestBefore)(view, command, point);
}

// leafenHoleWebViews puts the holes' hit test on WKWebView, over its own
// or the one it inherits, once.
static void leafenHoleWebViews(void) {
	static dispatch_once_t once;
	dispatch_once(&once, ^{
		Class base = [WKWebView class];
		Method method = class_getInstanceMethod(base, @selector(hitTest:));
		leafenHitTestBefore = method_getImplementation(method);
		if (!class_addMethod(base, @selector(hitTest:), (IMP)leafenHoledHitTest, method_getTypeEncoding(method))) {
			method_setImplementation(method, (IMP)leafenHoledHitTest);
		}
	});
}

static NSArray<NSValue *> *leafenRects(double *values, int count) {
	NSMutableArray<NSValue *> *rects = [NSMutableArray array];
	for (int i = 0; i < count; i++) {
		[rects addObject:[NSValue valueWithRect:NSMakeRect(values[4 * i], values[4 * i + 1], values[4 * i + 2], values[4 * i + 3])]];
	}
	return rects;
}

// leafenTabsStripHoles gives the frame's strip its holes and its solid
// places (x, y, width, height in points from its top left, count of each),
// or none.
void leafenTabsStripHoles(void *native, double *holeValues, int holeCount, double *solidValues, int solidCount) {
	NSArray<NSValue *> *holes = leafenRects(holeValues, holeCount);
	NSArray<NSValue *> *solids = leafenRects(solidValues, solidCount);
	leafenRoomOnMain(^{
		WKWebView *strip = leafenStripOf((__bridge NSWindow *)native);
		if (!strip) return;
		if (holes.count > 0 || solids.count > 0) leafenHoleWebViews();
		objc_setAssociatedObject(strip, &leafenHolesKey, holes.count > 0 ? holes : nil, OBJC_ASSOCIATION_RETAIN_NONATOMIC);
		objc_setAssociatedObject(strip, &leafenSolidsKey, solids.count > 0 ? solids : nil, OBJC_ASSOCIATION_RETAIN_NONATOMIC);
	});
}

// leafenTabsFocusStrip gives the frame's strip the keys.
void leafenTabsFocusStrip(void *native) {
	leafenRoomOnMain(^{
		NSWindow *window = (__bridge NSWindow *)native;
		WKWebView *strip = leafenStripOf(window);
		if (!strip) return;
		NSResponder *first = window.firstResponder;
		if ([first isKindOfClass:[NSView class]] && [(NSView *)first isDescendantOf:strip]) return;
		[window makeFirstResponder:strip];
	});
}

// leafenTabsStripPointer puts where the pointer is over the frame's content,
// in points from its top left, and says whether it is over this window —
// not off it, nor over another window in front of it.
bool leafenTabsStripPointer(void *native, double *x, double *y) {
	__block bool over = false;
	__block NSPoint at = NSZeroPoint;
	leafenRoomOnMain(^{
		NSWindow *window = (__bridge NSWindow *)native;
		NSView *content = window.contentView;
		if (!content) return;
		NSPoint screen = [NSEvent mouseLocation];
		if ([NSWindow windowNumberAtPoint:screen belowWindowWithWindowNumber:0] != window.windowNumber) return;
		NSPoint local = [content convertPoint:[window convertPointFromScreen:screen] fromView:nil];
		if (!NSPointInRect(local, content.bounds)) return;
		at = NSMakePoint(local.x, content.isFlipped ? local.y : NSHeight(content.bounds) - local.y);
		over = true;
	});
	*x = at.x;
	*y = at.y;
	return over;
}
