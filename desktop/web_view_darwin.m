//go:build darwin && !ios

#import <AppKit/AppKit.h>
#import <WebKit/WebKit.h>
#import <objc/runtime.h>
#import <objc/message.h>

// WebKit's and AppKit's private calls the app uses here (WKWebViewPrivate,
// WKUIDelegatePrivate, NSTextInputContext): they have no public ones.
@interface WKWebView (LeafenWebKit)
- (NSPrintOperation *)_printOperationWithPrintInfo:(NSPrintInfo *)printInfo forFrame:(id)frame;
- (void)_evaluateJavaScript:(NSString *)javaScriptString asAsyncFunction:(BOOL)asAsyncFunction
	withSourceURL:(NSURL *)sourceURL withArguments:(NSDictionary *)arguments forceUserGesture:(BOOL)forceUserGesture
	inFrame:(WKFrameInfo *)frame inWorld:(WKContentWorld *)world completionHandler:(void (^)(id, NSError *))completionHandler;
- (BOOL)hasMarkedText;
@end
@interface NSTextInputContext (LeafenWebKit)
- (void)handleEvent:(NSEvent *)event completionHandler:(void (^)(BOOL handled))completionHandler;
@end

// ---- Printing ----
//
// A page's window.print() asks the web view's delegate to print the frame it
// was called in, and nothing happens when the delegate has no answer, as
// Wails' has none: File › Print and "PDF로 내보내기" opened no panel
// (2026-11-08). The answer is the system's print panel over the window, for
// that frame alone — the export's hidden frame, an app's PDF, the page — and
// the page waits in print() until the panel closes, as in a browser.

@interface LeafenPrintEnd : NSObject
@property(nonatomic, copy) void (^done)(void);
@end

static NSMutableSet<LeafenPrintEnd *> *leafenPrinting;

@implementation LeafenPrintEnd
- (void)printOperationDidRun:(NSPrintOperation *)operation success:(BOOL)success contextInfo:(void *)info {
	if (self.done) self.done();
	[leafenPrinting removeObject:self];
}
@end

// leafenPrint opens the print panel for a frame of a web view (its main frame
// when frame is nil) and calls done once the panel has closed. A PDF wider
// than tall prints across the page, as Safari does.
static void leafenPrint(WKWebView *webView, id frame, CGSize pdfPage, void (^done)(void)) {
	NSWindow *window = webView.window;
	if (!window || window.attachedSheet) {
		if (done) done();
		return;
	}
	NSPrintInfo *info = [[NSPrintInfo sharedPrintInfo] copy];
	if (pdfPage.width > pdfPage.height && pdfPage.height > 0) info.orientation = NSPaperOrientationLandscape;
	NSPrintOperation *operation = nil;
	if (frame && [webView respondsToSelector:@selector(_printOperationWithPrintInfo:forFrame:)]) {
		operation = [webView _printOperationWithPrintInfo:info forFrame:frame];
	} else if (!frame) {
		operation = [webView printOperationWithPrintInfo:info];
	}
	if (!operation) {
		if (done) done();
		return;
	}
	operation.showsPrintPanel = YES;
	operation.showsProgressPanel = YES;
	// The web view's printing view starts with no size, which the operation refuses.
	operation.view.frame = webView.bounds;
	LeafenPrintEnd *end = [LeafenPrintEnd new];
	end.done = done;
	if (!leafenPrinting) leafenPrinting = [NSMutableSet set];
	[leafenPrinting addObject:end];
	[operation runOperationModalForWindow:window delegate:end
		didRunSelector:@selector(printOperationDidRun:success:contextInfo:) contextInfo:NULL];
}

// The delegate's answer to window.print(): WebKit holds the page until done.
static void leafenPrintFrameUntilDone(id delegate, SEL command, WKWebView *webView, id frame, CGSize pdfPage,
		void (^done)(void)) {
	leafenPrint(webView, frame, pdfPage, done);
}

// The same answer to a WebKit that asks without waiting: the panel opens once
// the page has gone on.
static void leafenPrintFrame(id delegate, SEL command, WKWebView *webView, id frame) {
	dispatch_async(dispatch_get_main_queue(), ^{ leafenPrint(webView, frame, CGSizeZero, nil); });
}

// leafenAnswerPrinting gives a web view delegate's class the answer; WebKit
// reads which answers a delegate has when it becomes one.
void leafenAnswerPrinting(Class delegate) {
	if (!delegate) return;
	class_addMethod(delegate, NSSelectorFromString(@"_webView:printFrame:pdfFirstPageSize:completionHandler:"),
		(IMP)leafenPrintFrameUntilDone, "v@:@@{CGSize=dd}@?");
	class_addMethod(delegate, NSSelectorFromString(@"_webView:printFrame:"), (IMP)leafenPrintFrame, "v@:@@");
}

// ---- Presses after typing ----
//
// While a page's text is being composed with an input method (Korean, say),
// WebKit hands each mouse event to the input method first and passes it on
// when the input method answers, and the answers can come out of order: the
// press after typing reached the page after its release, or its release
// only with the next press, so it made no click (2026-11-08). Here a press
// during composition still goes to the input method first, which ends the
// composition, and every mouse event after it waits for that answer; the
// rest go straight to the page, in the order they came.

@interface LeafenMouseQueue : NSObject
@property(nonatomic) BOOL waiting;
@property(nonatomic) NSUInteger asked;
@property(nonatomic, strong) NSMutableArray<NSArray *> *held;
@end
@implementation LeafenMouseQueue
@end

static const char leafenMouseQueueKey = 0;
static BOOL leafenPassingMouse;
static NSTextInputContext *(*leafenInputContext)(id, SEL);
static NSMutableDictionary<NSString *, NSValue *> *leafenMouseHandlers;

// An answer the input method hasn't given by then is taken as "not handled".
static const int64_t leafenInputMethodWait = 500 * NSEC_PER_MSEC;

static NSTextInputContext *leafenWebViewInputContext(id webView, SEL command) {
	return leafenPassingMouse ? nil : leafenInputContext(webView, command);
}

// leafenPassMouse gives the page a mouse event with no input method between.
static void leafenPassMouse(WKWebView *webView, SEL command, NSEvent *event) {
	IMP handler = leafenMouseHandlers[NSStringFromSelector(command)].pointerValue;
	if (!handler) return;
	leafenPassingMouse = YES;
	((void (*)(id, SEL, NSEvent *))handler)(webView, command, event);
	leafenPassingMouse = NO;
}

static void leafenSendMouse(WKWebView *webView, SEL command, NSEvent *event, LeafenMouseQueue *queue);

static void leafenSendHeld(WKWebView *webView, LeafenMouseQueue *queue) {
	while (!queue.waiting && queue.held.count) {
		NSArray *next = queue.held.firstObject;
		[queue.held removeObjectAtIndex:0];
		leafenSendMouse(webView, NSSelectorFromString(next[0]), next[1], queue);
	}
}

static void leafenSendMouse(WKWebView *webView, SEL command, NSEvent *event, LeafenMouseQueue *queue) {
	NSEventType type = event.type;
	BOOL press = type == NSEventTypeLeftMouseDown || type == NSEventTypeRightMouseDown || type == NSEventTypeOtherMouseDown;
	NSTextInputContext *context = press ? leafenInputContext(webView, @selector(inputContext)) : nil;
	if (!context || ![webView hasMarkedText] || ![context respondsToSelector:@selector(handleEvent:completionHandler:)]) {
		leafenPassMouse(webView, command, event);
		return;
	}
	queue.waiting = YES;
	NSUInteger asked = ++queue.asked;
	void (^answer)(BOOL) = ^(BOOL handled) {
		if (!queue.waiting || queue.asked != asked) return;
		queue.waiting = NO;
		if (!handled) leafenPassMouse(webView, command, event);
		leafenSendHeld(webView, queue);
	};
	[context handleEvent:event completionHandler:^(BOOL handled) {
		if ([NSThread isMainThread]) answer(handled);
		else dispatch_async(dispatch_get_main_queue(), ^{ answer(handled); });
	}];
	dispatch_after(dispatch_time(DISPATCH_TIME_NOW, leafenInputMethodWait), dispatch_get_main_queue(), ^{ answer(NO); });
}

static void leafenWebViewMouse(WKWebView *webView, SEL command, NSEvent *event) {
	LeafenMouseQueue *queue = objc_getAssociatedObject(webView, &leafenMouseQueueKey);
	if (!queue) {
		queue = [LeafenMouseQueue new];
		queue.held = [NSMutableArray array];
		objc_setAssociatedObject(webView, &leafenMouseQueueKey, queue, OBJC_ASSOCIATION_RETAIN_NONATOMIC);
	}
	if (queue.waiting) {
		[queue.held addObject:@[NSStringFromSelector(command), event]];
		return;
	}
	leafenSendMouse(webView, command, event, queue);
}

// leafenOrderMouseEvents puts the app between WebKit's web view and its mouse
// events, for every web view in the app.
static void leafenOrderMouseEvents(void) {
	Class webView = [WKWebView class];
	Method context = class_getInstanceMethod(webView, @selector(inputContext));
	if (!context) return;
	leafenMouseHandlers = [NSMutableDictionary dictionary];
	for (NSString *name in @[@"mouseDown:", @"mouseUp:", @"mouseDragged:", @"rightMouseDown:", @"rightMouseUp:",
			@"rightMouseDragged:", @"otherMouseDown:", @"otherMouseUp:", @"otherMouseDragged:"]) {
		Method method = class_getInstanceMethod(webView, NSSelectorFromString(name));
		if (method) leafenMouseHandlers[name] = [NSValue valueWithPointer:method_setImplementation(method, (IMP)leafenWebViewMouse)];
	}
	leafenInputContext = (NSTextInputContext * (*)(id, SEL))method_setImplementation(context, (IMP)leafenWebViewInputContext);
}

// ---- The app's own scripts in the page ----
//
// Wails runs a script in the page for each event it hands the page — the
// window gaining focus, moving, a message from the app — with WebKit's
// evaluateJavaScript, which runs it as a press of its own and then takes
// away what the person's own press allowed (seen on macOS 26.6). A press
// that one of those events followed allowed nothing: a video's ⤢ was
// refused as not pressed ("Cannot request fullscreen without transient
// activation", 2026-11-09). The app's scripts run as no press at all,
// leaving the person's press as it was.

static void leafenEvaluateWithoutPress(WKWebView *webView, SEL command, NSString *script, void (^done)(id, NSError *)) {
	[webView _evaluateJavaScript:script asAsyncFunction:NO withSourceURL:nil withArguments:nil forceUserGesture:NO
		inFrame:nil inWorld:WKContentWorld.pageWorld completionHandler:done];
}

static void leafenRunScriptsWithoutPress(void) {
	SEL quiet = @selector(_evaluateJavaScript:asAsyncFunction:withSourceURL:withArguments:forceUserGesture:inFrame:inWorld:completionHandler:);
	Method evaluate = class_getInstanceMethod([WKWebView class], @selector(evaluateJavaScript:completionHandler:));
	if (!evaluate || ![WKWebView instancesRespondToSelector:quiet]) return;
	method_setImplementation(evaluate, (IMP)leafenEvaluateWithoutPress);
}

// ---- Spelling ----
//
// WebKit underlines a misspelt word only in an app that turns "check
// spelling while typing" on, and otherwise takes the Mac's own text settings
// — correct spelling, capitalize, replace text, smart quotes and dashes —
// which Notion's app (Chromium) leaves alone: there a misspelt word gets a
// dotted red underline and typing is never changed (seen 2026-11-08; the
// app drew no underline). These are WebKit's defaults for the app, read once
// when its first web view starts; what the person turns on or off in a
// word's menu (Spelling and Grammar, Substitutions) is kept over them.
static void leafenCheckSpelling(void) {
	[[NSUserDefaults standardUserDefaults] registerDefaults:@{
		@"WebContinuousSpellCheckingEnabled": @YES,
		@"WebGrammarCheckingEnabled": @NO,
		@"WebAutomaticSpellingCorrectionEnabled": @NO,
		@"WebAutomaticTextReplacementEnabled": @NO,
		@"WebAutomaticQuoteSubstitutionEnabled": @NO,
		@"WebAutomaticDashSubstitutionEnabled": @NO,
		@"WebAutomaticLinkDetectionEnabled": @NO,
	}];
}

// ---- The web inspector ----
//
// 보기 › 개발자 도구 토글 (⌥⌘I) opens the shown page in WebKit's web
// inspector or closes it, as Notion's app's opens its pages' developer
// tools. WebKit has no public call for either; a build that can't (no
// inspector) does nothing.

static id leafenSend(id object, NSString *name) {
	SEL command = NSSelectorFromString(name);
	if (!object || ![object respondsToSelector:command]) return nil;
	return ((id (*)(id, SEL))objc_msgSend)(object, command);
}

// leafenInspector is the window's page's web inspector, allowed first.
static id leafenInspector(void *window) {
	NSWindow *native = (__bridge NSWindow *)window;
	WKWebView *webView = leafenSend(native, @"webView");
	if (![webView isKindOfClass:[WKWebView class]]) return nil;
	if (@available(macOS 13.3, *)) {
		if (!webView.inspectable) webView.inspectable = YES;
	}
	@try {
		[webView.configuration.preferences setValue:@YES forKey:@"developerExtrasEnabled"];
	} @catch (NSException *exception) {
	}
	return leafenSend(webView, @"_inspector");
}

void leafenToggleInspector(void *window) {
	id inspector = leafenInspector(window);
	if (!inspector) return;
	SEL visible = NSSelectorFromString(@"isVisible");
	BOOL shown = [inspector respondsToSelector:visible] && ((BOOL (*)(id, SEL))objc_msgSend)(inspector, visible);
	leafenSend(inspector, shown ? @"close" : @"show");
}

// leafenShowConsole opens the inspector on its console.
void leafenShowConsole(void *window) {
	id inspector = leafenInspector(window);
	if (!inspector) return;
	if ([inspector respondsToSelector:NSSelectorFromString(@"showConsole")]) {
		leafenSend(inspector, @"showConsole");
	} else {
		leafenSend(inspector, @"show");
	}
}

// ---- Frames drawn as they load ----
//
// Wails has every web view hold back drawing a document until it has loaded
// (WKWebViewConfiguration.suppressesIncrementalRendering), and WebKit holds
// back each frame's document that way too: a video frame put in a page then
// stayed a black box, its player loaded underneath, until the page was laid
// out again (seen 2026-11-15 on a fresh app: a YouTube link put in a page,
// two tries in two; drawn at once in both without the hold). A page draws
// as it loads, as in a browser; the window's background colour covers the
// moment before the first draw.

static void (*leafenSetSuppressesRendering)(id, SEL, BOOL);

static void leafenDrawAsLoaded(id configuration, SEL command, BOOL suppresses) {
	leafenSetSuppressesRendering(configuration, command, NO);
}

static void leafenDrawFramesAsTheyLoad(void) {
	Method set = class_getInstanceMethod([WKWebViewConfiguration class], @selector(setSuppressesIncrementalRendering:));
	if (set) leafenSetSuppressesRendering = (void (*)(id, SEL, BOOL))method_setImplementation(set, (IMP)leafenDrawAsLoaded);
}

void leafenAnswerContextMenus(Class delegate);
void leafenAnswerOpenPanels(Class delegate);

// leafenPrepareWebViews sets these once, before the app's first window:
// Wails' window delegate and the app's pop-ups (new_window_darwin.m) answer
// printing, a page's menu (text_menu_darwin.m) and a file input's open panel
// (open_panel_darwin.m), every web view's mouse events keep their order, the
// app's scripts leave the person's presses alone, spelling is checked as in
// Notion's app, and frames draw as they load.
void leafenPrepareWebViews(void) {
	leafenAnswerPrinting(objc_getClass("WebviewWindowDelegate"));
	leafenAnswerPrinting(objc_getClass("LeafenPopup"));
	leafenAnswerContextMenus(objc_getClass("WebviewWindowDelegate"));
	leafenAnswerContextMenus(objc_getClass("LeafenPopup"));
	leafenAnswerOpenPanels(objc_getClass("WebviewWindowDelegate"));
	leafenAnswerOpenPanels(objc_getClass("LeafenPopup"));
	leafenOrderMouseEvents();
	leafenRunScriptsWithoutPress();
	leafenCheckSpelling();
	leafenDrawFramesAsTheyLoad();
}
