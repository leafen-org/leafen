//go:build darwin && !ios

#import <AppKit/AppKit.h>
#import <WebKit/WebKit.h>
#import <objc/runtime.h>

// A frame's strip and its tabs' web views (window_tabs_darwin.go). A frame's
// own web view is its strip, along the top; each tab's web view comes from
// the tab's own window, which never shows, into the frame's content under the
// strip, and goes back there before that window closes. The app keeps every
// tab's web view while it is in a frame, so a frame closing around it never
// takes it from its window.

extern WKWebView *leafenFindWebView(NSView *view);

static void leafenOnMain(void (^block)(void)) {
	if ([NSThread isMainThread]) block();
	else dispatch_sync(dispatch_get_main_queue(), block);
}

// leafenWebViewOf is a window's own web view (Wails' window keeps it).
static WKWebView *leafenWebViewOf(NSWindow *window) {
	if ([window respondsToSelector:NSSelectorFromString(@"webView")]) {
		id view = [window valueForKey:@"webView"];
		if ([view isKindOfClass:[WKWebView class]]) return view;
	}
	return leafenFindWebView(window.contentView);
}

static NSMutableDictionary<NSValue *, WKWebView *> *leafenTabViews;

static NSValue *leafenKey(NSWindow *window) { return [NSValue valueWithPointer:(__bridge void *)window]; }

// ---- The traffic lights ----
//
// Notion's app puts them in its 40px tab bar: the first 14pt from the left
// (12 before macOS 26), that inset times the zoom above a zoom of 1, each
// centered in the bar, which the zoom makes taller or shorter (its
// getTrafficLightPosition), and its tab bar keeps their span with that inset
// after it for them, as Electron gives the page as its title bar area (the
// lights' container). AppKit lays its title bar out again as the window
// changes, so they are put back each time.

@interface LeafenLights : NSObject
@property(nonatomic, weak) NSWindow *window;
@property(nonatomic) CGFloat height;
@property(nonatomic) CGFloat zoom;
@property(nonatomic) CGFloat spacing;
@property(nonatomic) int room;
@end

@implementation LeafenLights
- (void)place {
	NSWindow *window = self.window;
	if (!window || (window.styleMask & NSWindowStyleMaskFullScreen)) return;
	NSButton *close = [window standardWindowButton:NSWindowCloseButton];
	NSButton *mini = [window standardWindowButton:NSWindowMiniaturizeButton];
	NSButton *zoom = [window standardWindowButton:NSWindowZoomButton];
	NSView *bar = close.superview;
	NSView *container = bar.superview;
	if (!close || !mini || !zoom || !container) return;
	CGFloat size = NSWidth(close.frame);
	if (self.spacing <= 0) self.spacing = NSMinX(mini.frame) - NSMinX(close.frame);
	NSRect frame = container.frame;
	frame.size.height = self.height;
	frame.origin.y = NSHeight(window.frame) - self.height;
	container.frame = frame;
	bar.frame = container.bounds;
	CGFloat y = round((self.height - NSHeight(close.frame)) / 2);
	CGFloat inset = round(self.zoom >= 1 ? size * self.zoom : size);
	NSArray<NSButton *> *buttons = @[ close, mini, zoom ];
	for (NSUInteger i = 0; i < buttons.count; i++) {
		[buttons[i] setFrameOrigin:NSMakePoint(inset + i * self.spacing, y)];
	}
	// The room as at a zoom of 1; the app scales it (window_tabs_darwin.go).
	self.room = (int)round(NSMaxX(zoom.frame) - inset + 2 * size);
}

- (void)changed:(NSNotification *)note {
	dispatch_async(dispatch_get_main_queue(), ^{ [self place]; });
}

- (void)observeValueForKeyPath:(NSString *)path ofObject:(id)object change:(NSDictionary *)change context:(void *)context {
	dispatch_async(dispatch_get_main_queue(), ^{ [self place]; });
}
@end

static const char leafenLightsKey = 0;

int leafenTabsAdoptFrame(void *native, int stripHeight, double zoom) {
	__block int room = 0;
	leafenOnMain(^{
		NSWindow *window = (__bridge NSWindow *)native;
		WKWebView *strip = leafenWebViewOf(window);
		NSView *content = window.contentView;
		if (strip && content) {
			NSRect bounds = content.bounds;
			strip.frame = NSMakeRect(0, NSHeight(bounds) - stripHeight, NSWidth(bounds), stripHeight);
			strip.autoresizingMask = NSViewWidthSizable | NSViewMinYMargin;
		}
		LeafenLights *lights = objc_getAssociatedObject(window, &leafenLightsKey);
		if (!lights) {
			lights = [LeafenLights new];
			lights.window = window;
			objc_setAssociatedObject(window, &leafenLightsKey, lights, OBJC_ASSOCIATION_RETAIN_NONATOMIC);
			for (NSNotificationName name in @[ NSWindowDidResizeNotification, NSWindowDidEndLiveResizeNotification,
					NSWindowDidExitFullScreenNotification, NSWindowDidBecomeKeyNotification, NSWindowDidResignKeyNotification,
					NSWindowDidChangeScreenNotification, NSWindowDidDeminiaturizeNotification ]) {
				[[NSNotificationCenter defaultCenter] addObserver:lights selector:@selector(changed:) name:name object:window];
			}
			[window addObserver:lights forKeyPath:@"title" options:0 context:NULL];
			[[NSNotificationCenter defaultCenter] addObserverForName:NSWindowWillCloseNotification object:window queue:nil
				usingBlock:^(NSNotification *note) {
					[[NSNotificationCenter defaultCenter] removeObserver:lights];
					@try { [window removeObserver:lights forKeyPath:@"title"]; } @catch (NSException *e) {}
				}];
		}
		lights.height = stripHeight;
		lights.zoom = zoom;
		[lights place];
		room = lights.room;
	});
	return room;
}

void leafenTabsPlaceFrame(void *native, bool hasFrame, double x, double y, double width, double height,
		bool hasPoint, int px, int py) {
	leafenOnMain(^{
		NSWindow *window = (__bridge NSWindow *)native;
		NSRect rect = window.frame;
		BOOL moved = NO;
		if (hasFrame && width >= 200 && height >= 200) {
			NSRect kept = NSMakeRect(x, y, width, height);
			for (NSScreen *screen in NSScreen.screens) {
				if (NSIntersectsRect(screen.visibleFrame, kept)) {
					rect = kept;
					moved = YES;
					break;
				}
			}
		}
		if (hasPoint && NSScreen.screens.count > 0) {
			// The page's screen point is from the top left of the main screen:
			// the window opens with its strip under the pointer.
			CGFloat top = NSMaxY(NSScreen.screens.firstObject.frame) - py;
			rect.origin.x = px - 60;
			rect.origin.y = top + 18 - NSHeight(rect);
			moved = YES;
		}
		if (!moved) return;
		NSScreen *screen = window.screen ?: NSScreen.mainScreen;
		for (NSScreen *each in NSScreen.screens) {
			if (NSPointInRect(NSMakePoint(NSMidX(rect), NSMaxY(rect) - 1), each.frame)) screen = each;
		}
		[window setFrame:[window constrainFrameRect:rect toScreen:screen] display:NO];
	});
}

bool leafenTabsFrameRect(void *native, double *x, double *y, double *width, double *height) {
	__block NSRect rect = NSZeroRect;
	__block BOOL full = NO;
	leafenOnMain(^{
		NSWindow *window = (__bridge NSWindow *)native;
		rect = window.frame;
		full = (window.styleMask & NSWindowStyleMaskFullScreen) != 0;
	});
	if (full || NSIsEmptyRect(rect)) return false;
	*x = NSMinX(rect);
	*y = NSMinY(rect);
	*width = NSWidth(rect);
	*height = NSHeight(rect);
	return true;
}

// leafenTabsSetZoom zooms a window's page — a tab's, wherever its web view
// is, or a frame's strip — by WebKit's page zoom, which lays the page out
// again at that size as Notion's zoom does; a pinch's magnification goes.
void leafenTabsSetZoom(void *native, double zoom) {
	leafenOnMain(^{
		NSWindow *window = (__bridge NSWindow *)native;
		WKWebView *view = (leafenTabViews ? leafenTabViews[leafenKey(window)] : nil) ?: leafenWebViewOf(window);
		if (!view) return;
		if (view.magnification != 1) view.magnification = 1;
		if (@available(macOS 11.0, *)) view.pageZoom = zoom;
	});
}

void leafenTabsContentSize(void *native, int *width, int *height) {
	__block NSSize size = NSZeroSize;
	leafenOnMain(^{ size = ((__bridge NSWindow *)native).contentView.bounds.size; });
	*width = (int)round(size.width);
	*height = (int)round(size.height);
}

bool leafenTabsStripPoint(void *native, int px, int py, int stripHeight, int *x) {
	__block BOOL inside = NO;
	__block CGFloat along = 0;
	leafenOnMain(^{
		NSWindow *window = (__bridge NSWindow *)native;
		if (!window.isVisible || window.isMiniaturized || NSScreen.screens.count == 0) return;
		NSPoint point = NSMakePoint(px, NSMaxY(NSScreen.screens.firstObject.frame) - py);
		NSRect rect = window.frame;
		inside = point.x >= NSMinX(rect) && point.x < NSMaxX(rect) && point.y <= NSMaxY(rect) && point.y >= NSMaxY(rect) - stripHeight;
		along = point.x - NSMinX(rect);
	});
	*x = (int)along;
	return inside;
}

// leafenTabsPlace puts a tab's web view in the frame under the strip, behind
// the frame's others when it isn't shown.
static WKWebView *leafenTabsPlace(NSWindow *frame, NSWindow *tab, int stripHeight) {
	WKWebView *view = leafenTabViews[leafenKey(tab)] ?: leafenWebViewOf(tab);
	NSView *content = frame.contentView;
	if (!view || !content) return nil;
	if (!leafenTabViews) leafenTabViews = [NSMutableDictionary dictionary];
	leafenTabViews[leafenKey(tab)] = view;
	if (view.superview != content) {
		[content addSubview:view positioned:NSWindowBelow relativeTo:leafenWebViewOf(frame)];
		view.hidden = YES;
	}
	NSRect bounds = content.bounds;
	view.frame = NSMakeRect(0, 0, NSWidth(bounds), MAX(0, NSHeight(bounds) - stripHeight));
	view.autoresizingMask = NSViewWidthSizable | NSViewHeightSizable;
	return view;
}

// ---- A page made behind starts in view ----
//
// WebKit runs the process of a page not shown at background priority (4,
// against 47 for a shown page's, seen 2026-11-13) and starves it whenever the
// Mac is busy: a tab opened behind read its first requests and then waited,
// its workspace and page unread, until it was shown or a timer of its own
// woke it — thirteen opened behind at once stayed on the loading screen 20
// seconds and more. A page starting behind is in view to WebKit instead: not
// hidden, under the shown tab's page, which covers it, until the app says its
// start is over (window_tabs_start.go); then it is hidden as any tab not shown.

static NSMutableSet<NSValue *> *leafenStartingViews;
static NSMapTable<NSWindow *, WKWebView *> *leafenShownViews;

static NSValue *leafenViewKey(NSView *view) { return [NSValue valueWithPointer:(__bridge void *)view]; }

static BOOL leafenStarting(NSView *view) { return [leafenStartingViews containsObject:leafenViewKey(view)]; }

typedef struct {
	__unsafe_unretained NSView *strip;
	__unsafe_unretained NSView *shown;
} LeafenTabOrder;

// leafenTabRank: the frame's own views keep their places, behind (its
// backdrop) or in front (Wails' drop area) of the pages; of these the strip
// is in front, then the shown tab's page, then the pages starting behind it.
static int leafenTabRank(NSView *view, LeafenTabOrder *order) {
	if ([view isKindOfClass:[NSVisualEffectView class]]) return -1;
	if (![view isKindOfClass:[WKWebView class]]) return 4;
	if (view == order->strip) return 3;
	if (view == order->shown) return 2;
	return leafenStarting(view) ? 1 : 0;
}

static NSComparisonResult leafenTabCompare(__kindof NSView *a, __kindof NSView *b, void *context) {
	int first = leafenTabRank(a, context), second = leafenTabRank(b, context);
	return first < second ? NSOrderedAscending : first > second ? NSOrderedDescending : NSOrderedSame;
}

static void leafenTabsOrder(NSWindow *frame) {
	LeafenTabOrder order = {leafenWebViewOf(frame), [leafenShownViews objectForKey:frame]};
	[frame.contentView sortSubviewsUsingFunction:leafenTabCompare context:&order];
}

void leafenTabsStarting(void *tabNative, bool starting) {
	leafenOnMain(^{
		WKWebView *view = leafenTabViews[leafenKey((__bridge NSWindow *)tabNative)];
		NSWindow *frame = view.window;
		if (!view || !frame || view.superview != frame.contentView) return;
		if (!leafenStartingViews) leafenStartingViews = [NSMutableSet set];
		BOOL shown = [leafenShownViews objectForKey:frame] == view;
		if (starting) {
			[leafenStartingViews addObject:leafenViewKey(view)];
			if (!shown) {
				view.hidden = NO;
				leafenTabsOrder(frame);
			}
		} else {
			[leafenStartingViews removeObject:leafenViewKey(view)];
			if (!shown) view.hidden = YES;
		}
	});
}

void leafenTabsAddView(void *frame, void *tab, int stripHeight) {
	leafenOnMain(^{ leafenTabsPlace((__bridge NSWindow *)frame, (__bridge NSWindow *)tab, stripHeight); });
}

void leafenTabsShowView(void *frameNative, void *tabNative, int stripHeight, bool focus) {
	leafenOnMain(^{
		NSWindow *frame = (__bridge NSWindow *)frameNative;
		WKWebView *view = leafenTabsPlace(frame, (__bridge NSWindow *)tabNative, stripHeight);
		if (!view) return;
		WKWebView *strip = leafenWebViewOf(frame);
		if (!leafenShownViews) leafenShownViews = [NSMapTable weakToWeakObjectsMapTable];
		[leafenShownViews setObject:view forKey:frame];
		view.hidden = NO;
		for (NSView *other in frame.contentView.subviews) {
			if (other != view && other != strip && [other isKindOfClass:[WKWebView class]] && !leafenStarting(other)) other.hidden = YES;
		}
		leafenTabsOrder(frame);
		if (focus) [frame makeFirstResponder:view];
	});
}

void leafenTabsFocusView(void *frameNative, void *tabNative) {
	leafenOnMain(^{
		NSWindow *frame = (__bridge NSWindow *)frameNative;
		WKWebView *view = leafenTabViews[leafenKey((__bridge NSWindow *)tabNative)];
		if (!view || view.window != frame || view.hidden) return;
		NSResponder *first = frame.firstResponder;
		if ([first isKindOfClass:[NSView class]] && [(NSView *)first isDescendantOf:view]) return;
		[frame makeFirstResponder:view];
	});
}

void leafenTabsReleaseView(void *tabNative) {
	leafenOnMain(^{
		NSWindow *tab = (__bridge NSWindow *)tabNative;
		WKWebView *view = leafenTabViews[leafenKey(tab)];
		if (!view) return;
		[leafenStartingViews removeObject:leafenViewKey(view)];
		if (view.window && [leafenShownViews objectForKey:view.window] == view) [leafenShownViews removeObjectForKey:view.window];
		NSView *home = tab.contentView;
		if (home && view.superview != home) {
			NSWindow *frame = view.window;
			BOOL hadKeyboard = frame && [frame.firstResponder isKindOfClass:[NSView class]]
				&& [(NSView *)frame.firstResponder isDescendantOf:view];
			[home addSubview:view];
			view.frame = home.bounds;
			view.autoresizingMask = NSViewWidthSizable | NSViewHeightSizable;
			if (hadKeyboard) [frame makeFirstResponder:nil];
		}
		view.hidden = NO;
		[leafenTabViews removeObjectForKey:leafenKey(tab)];
	});
}

// ---- A tab's own window never shows ----
//
// Wails shows every hidden window when the Dock icon is pressed with none on
// screen; a tab's window is marked, and Wails' window class leaves a marked
// one ordered out. The class is changed rather than the window's own class,
// which WebKit's observing of its window has changed already.

static const char leafenTabWindowKey = 0;
static IMP leafenShowWith[2];
static IMP leafenShowRegardless;

static BOOL leafenIsTabWindow(id window) { return objc_getAssociatedObject(window, &leafenTabWindowKey) != nil; }

static void leafenMakeKeyAndOrderFront(id window, SEL command, id sender) {
	if (!leafenIsTabWindow(window)) ((void (*)(id, SEL, id))leafenShowWith[0])(window, command, sender);
}
static void leafenOrderFront(id window, SEL command, id sender) {
	if (!leafenIsTabWindow(window)) ((void (*)(id, SEL, id))leafenShowWith[1])(window, command, sender);
}
static void leafenOrderFrontRegardless(id window, SEL command) {
	if (!leafenIsTabWindow(window)) ((void (*)(id, SEL))leafenShowRegardless)(window, command);
}

// leafenGuard puts a guard on the class's own method for the selector, or
// adds one over the one it inherits, and gives back the one it guards.
static IMP leafenGuard(Class base, SEL selector, IMP guard, const char *types) {
	Method own = class_getInstanceMethod(base, selector);
	IMP before = method_getImplementation(own);
	if (!class_addMethod(base, selector, guard, types)) method_setImplementation(own, guard);
	return before;
}

void leafenTabsKeepHidden(void *native) {
	leafenOnMain(^{
		NSWindow *window = (__bridge NSWindow *)native;
		static dispatch_once_t once;
		dispatch_once(&once, ^{
			Class base = objc_getClass("WebviewWindow");
			if (!base) return;
			leafenShowWith[0] = leafenGuard(base, @selector(makeKeyAndOrderFront:), (IMP)leafenMakeKeyAndOrderFront, "v@:@");
			leafenShowWith[1] = leafenGuard(base, @selector(orderFront:), (IMP)leafenOrderFront, "v@:@");
			leafenShowRegardless = leafenGuard(base, @selector(orderFrontRegardless), (IMP)leafenOrderFrontRegardless, "v@:");
		});
		objc_setAssociatedObject(window, &leafenTabWindowKey, @YES, OBJC_ASSOCIATION_RETAIN_NONATOMIC);
	});
}

int leafenTabsHistory(void *native) {
	__block int can = 0;
	leafenOnMain(^{
		WKWebView *view = leafenWebViewOf((__bridge NSWindow *)native);
		can = (view.canGoBack ? 1 : 0) | (view.canGoForward ? 2 : 0);
	});
	return can;
}

// leafenTabsHistoryList is the tab's page's history one way as JSON — back
// from the newest entry, or forward from the nearest — each entry's address
// and title; the caller frees it.
char *leafenTabsHistoryList(void *native, bool forward) {
	__block NSString *text = @"[]";
	leafenOnMain(^{
		WKWebView *view = leafenWebViewOf((__bridge NSWindow *)native);
		WKBackForwardList *list = view.backForwardList;
		NSArray<WKBackForwardListItem *> *items = forward ? list.forwardList : list.backList.reverseObjectEnumerator.allObjects;
		NSMutableArray *entries = [NSMutableArray array];
		for (WKBackForwardListItem *item in items) {
			[entries addObject:@{@"url" : item.URL.absoluteString ?: @"", @"title" : item.title ?: @""}];
		}
		NSData *data = [NSJSONSerialization dataWithJSONObject:entries options:0 error:nil];
		if (data) text = [[NSString alloc] initWithData:data encoding:NSUTF8StringEncoding];
	});
	return strdup(text.UTF8String);
}

void leafenTabsGo(void *native, bool forward) {
	leafenOnMain(^{
		WKWebView *view = leafenWebViewOf((__bridge NSWindow *)native);
		if (forward) [view goForward];
		else [view goBack];
	});
}
