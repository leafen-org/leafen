//go:build darwin && !ios

#import <AppKit/AppKit.h>
#import <WebKit/WebKit.h>
#import <objc/runtime.h>

// What new_window.go says an address is (newWindowKind).
enum { LeafenNewWindowRefused, LeafenNewWindowOutside, LeafenNewWindowAppPage, LeafenNewWindowPageMade };

extern int leafenNewWindowKind(char *address);
extern int leafenOpenNewWindow(char *address, int active);

// leafenAsk asks the app's rule what an address is and, when open is set,
// has it open an address outside the app or one of the app's pages — a page
// in a tab behind the shown one, or in front when active is set.
static int leafenAskAs(NSURL *url, BOOL open, BOOL active) {
	char *address = (char *)(url.absoluteString ?: @"").UTF8String;
	return open ? leafenOpenNewWindow(address, active ? 1 : 0) : leafenNewWindowKind(address);
}

static int leafenAsk(NSURL *url, BOOL open) { return leafenAskAs(url, open, NO); }

static WKWebView *leafenNewWindow(WKWebView *opener, WKWebViewConfiguration *configuration,
		WKNavigationAction *action, WKWindowFeatures *features);

// A window the page fills itself: a plain window of the app, as a browser's
// pop-up, with the page's title. Whatever it then goes to outside what the
// page made goes by the rule, and the window closes if it showed nothing yet.
@interface LeafenPopup : NSObject <WKUIDelegate, WKNavigationDelegate, NSWindowDelegate>
@property(nonatomic, strong) NSWindow *window;
@property(nonatomic, strong) WKWebView *webView;
@end

static NSMutableSet<LeafenPopup *> *leafenPopups;

@implementation LeafenPopup
- (WKWebView *)webView:(WKWebView *)webView createWebViewWithConfiguration:(WKWebViewConfiguration *)configuration
		forNavigationAction:(WKNavigationAction *)action windowFeatures:(WKWindowFeatures *)features {
	return leafenNewWindow(webView, configuration, action, features);
}

- (void)webViewDidClose:(WKWebView *)webView {
	[self.window close];
}

- (void)webView:(WKWebView *)webView decidePolicyForNavigationAction:(WKNavigationAction *)action
		decisionHandler:(void (^)(WKNavigationActionPolicy))decisionHandler {
	if (!action.targetFrame.isMainFrame || leafenAsk(action.request.URL, NO) == LeafenNewWindowPageMade) {
		decisionHandler(WKNavigationActionPolicyAllow);
		return;
	}
	decisionHandler(WKNavigationActionPolicyCancel);
	leafenAsk(action.request.URL, YES);
	NSString *shown = webView.URL.absoluteString;
	if (shown.length == 0 || [shown isEqualToString:@"about:blank"]) [self.window close];
}

- (void)windowWillClose:(NSNotification *)notification {
	[self.window unbind:NSTitleBinding];
	self.webView.UIDelegate = nil;
	self.webView.navigationDelegate = nil;
	self.window.delegate = nil;
	// Let go once the window has finished closing.
	LeafenPopup *closed = self;
	dispatch_async(dispatch_get_main_queue(), ^{ [leafenPopups removeObject:closed]; });
}
@end

static WKWebView *leafenNewWindow(WKWebView *opener, WKWebViewConfiguration *configuration,
		WKNavigationAction *action, WKWindowFeatures *features) {
	if (leafenAsk(action.request.URL, YES) != LeafenNewWindowPageMade) return nil;
	CGFloat width = features.width ? MAX(features.width.doubleValue, 320) : 960;
	CGFloat height = features.height ? MAX(features.height.doubleValue, 240) : 720;
	NSWindow *window = [[NSWindow alloc] initWithContentRect:NSMakeRect(0, 0, width, height)
		styleMask:NSWindowStyleMaskTitled | NSWindowStyleMaskClosable | NSWindowStyleMaskMiniaturizable | NSWindowStyleMaskResizable
		backing:NSBackingStoreBuffered defer:NO];
	window.releasedWhenClosed = NO;
	WKWebView *view = [[WKWebView alloc] initWithFrame:window.contentView.bounds configuration:configuration];
	view.autoresizingMask = NSViewWidthSizable | NSViewHeightSizable;
	LeafenPopup *popup = [LeafenPopup new];
	popup.window = window;
	popup.webView = view;
	view.UIDelegate = popup;
	view.navigationDelegate = popup;
	window.delegate = popup;
	window.contentView = view;
	[window bind:NSTitleBinding toObject:view withKeyPath:@"title" options:@{NSNullPlaceholderBindingOption: @""}];
	if (opener.window) {
		NSRect over = opener.window.frame;
		[window setFrameTopLeftPoint:NSMakePoint(NSMinX(over) + 28, NSMaxY(over) - 28)];
	} else {
		[window center];
	}
	if (!leafenPopups) leafenPopups = [NSMutableSet set];
	[leafenPopups addObject:popup];
	[window makeKeyAndOrderFront:nil];
	return view;
}

// The window's delegate (Wails'), answering WebKit's request for a new window.
static WKWebView *leafenWindowCreateWebView(id delegate, SEL command, WKWebView *webView,
		WKWebViewConfiguration *configuration, WKNavigationAction *action, WKWindowFeatures *features) {
	return leafenNewWindow(webView, configuration, action, features);
}

// WKNavigationAction's buttonNumber for the middle button (WebKit gives the
// left 1, the right 2, the middle 4).
static const NSInteger leafenMiddleButton = 4;

// The window's delegate deciding a pressed link in its own page: an address
// it can't show — a site outside the app, mail, a phone number — goes by the
// rule instead of replacing the app or failing, and a ⌘-pressed or
// middle-pressed link to one of the app's pages opens a new tab behind the
// shown one, in front with ⇧, as Notion's links do. Everything else goes on.
static void leafenWindowDecidePolicy(id delegate, SEL command, WKWebView *webView,
		WKNavigationAction *action, void (^decisionHandler)(WKNavigationActionPolicy)) {
	if (action.targetFrame.isMainFrame && action.navigationType == WKNavigationTypeLinkActivated) {
		int kind = leafenAsk(action.request.URL, NO);
		BOOL newTab = (action.modifierFlags & NSEventModifierFlagCommand) != 0 || action.buttonNumber == leafenMiddleButton;
		if (kind == LeafenNewWindowOutside || (kind == LeafenNewWindowAppPage && newTab)) {
			decisionHandler(WKNavigationActionPolicyCancel);
			leafenAskAs(action.request.URL, YES, (action.modifierFlags & NSEventModifierFlagShift) != 0);
			return;
		}
	}
	decisionHandler(WKNavigationActionPolicyAllow);
}

// leafenFindWebView is the web view in a window's content (window_tabs_darwin.m too).
WKWebView *leafenFindWebView(NSView *view) {
	if ([view isKindOfClass:[WKWebView class]]) return (WKWebView *)view;
	for (NSView *child in view.subviews) {
		WKWebView *found = leafenFindWebView(child);
		if (found) return found;
	}
	return nil;
}

// leafenRouteNewWindows gives the web view in a window's content its
// delegate's answers. A method its class has already is kept, and WebKit
// reads which methods a delegate has when the delegate is set, so each is
// set again.
void leafenRouteNewWindows(void *native) {
	NSWindow *window = (__bridge NSWindow *)native;
	dispatch_async(dispatch_get_main_queue(), ^{
		WKWebView *webView = nil;
		if ([window respondsToSelector:NSSelectorFromString(@"webView")]) webView = [window valueForKey:@"webView"];
		if (![webView isKindOfClass:[WKWebView class]]) webView = leafenFindWebView(window.contentView);
		id ui = webView.UIDelegate;
		id navigation = webView.navigationDelegate;
		if (!ui || !navigation) return;
		class_addMethod(object_getClass(ui), @selector(webView:createWebViewWithConfiguration:forNavigationAction:windowFeatures:),
			(IMP)leafenWindowCreateWebView, "@@:@@@@");
		class_addMethod(object_getClass(navigation), @selector(webView:decidePolicyForNavigationAction:decisionHandler:),
			(IMP)leafenWindowDecidePolicy, "v@:@@@?");
		webView.UIDelegate = nil;
		webView.UIDelegate = ui;
		webView.navigationDelegate = nil;
		webView.navigationDelegate = navigation;
	});
}

// leafenWindowOpen says whether a window is ordered in or minimized, read on
// the main thread.
bool leafenWindowOpen(void *native) {
	NSWindow *window = (__bridge NSWindow *)native;
	__block BOOL open = NO;
	void (^read)(void) = ^{ open = window.isVisible || window.isMiniaturized; };
	if ([NSThread isMainThread]) read();
	else dispatch_sync(dispatch_get_main_queue(), read);
	return open;
}
