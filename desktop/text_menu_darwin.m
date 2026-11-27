//go:build darwin && !ios

#import <AppKit/AppKit.h>
#import <WebKit/WebKit.h>
#import <objc/runtime.h>
#include <stdbool.h>

// ---- The text menu ----
//
// The app's own pages draw a right press's menu themselves, as Notion's
// app's page does (web/src/lib/desktopTextMenu.ts); the parts of it only
// the app can do are below, under "The page's menu". What a page doesn't
// draw — a site's page in a pop-up, a frame inside a page — gets the same
// short menu (its desktop client's ElectronContextMenu, read 2026-11-13) in
// place of WebKit's whole one (Look Up, Translate, Font, Speech, Share,
// Services, …), from what WebKit proposes for the press, in Notion's
// sections and words:
//   spelling corrections
//   | 사전에 추가 · 맞춤법 검사 비활성화        (a misspelt word)
//   | 새 탭에서 링크 열기 · 새 창에서 링크 열기 (one of the app's pages)
//     · 브라우저에서 링크 열기 · 링크 복사 (이메일 주소 복사)
//   | 이미지 복사 · 이미지 주소 복사
//   | 잘라내기 ⌘X · 복사 ⌘C · 붙여넣기 ⌘V      (text that can be edited or is chosen)
//   | '〈낱말〉' 검색 · Google로 검색           (chosen text; the first for one word)
//   | 맞춤법 검사 활성화                        (editable text while spelling isn't checked)
// With nothing to offer no menu opens, as in Notion's app. The menu is the
// system's, Notion's is drawn by its page.

extern int leafenNewWindowKind(char *address);

// WebKit's menu items by their tag (WebCore's ContextMenuAction) and, where
// WebKit gives one, their identifier.
enum {
	leafenTagCopyLink = 3,
	leafenTagCopyImage = 6,
	leafenTagCopy = 8,
	leafenTagCut = 13,
	leafenTagPaste = 14,
	leafenTagSpellingGuess = 15,
	leafenTagNoGuessesFound = 16,
	leafenTagIgnoreSpelling = 17,
	leafenTagLearnSpelling = 18,
	leafenTagSearchWeb = 21,
	leafenTagLookUp = 22,
};

static BOOL leafenKoreanMenus;

// leafenTextMenuLanguage says the window's language (app_menu_bar_darwin.go).
void leafenTextMenuLanguage(bool korean) {
	leafenKoreanMenus = korean;
}

// leafenMenusKorean says whether the app's own menus and panels speak Korean
// (the open panel's 형식: menu, open_panel_darwin.m).
bool leafenMenusKorean(void) {
	return leafenKoreanMenus;
}

static NSString *leafenSay(NSString *english, NSString *korean) {
	return leafenKoreanMenus ? korean : english;
}

// What the menu's own items do, for one web view.
@interface LeafenTextMenuTarget : NSObject
@property(nonatomic, weak) WKWebView *webView;
@end

@implementation LeafenTextMenuTarget
- (void)copyText:(NSMenuItem *)item {
	NSString *text = item.representedObject;
	if (![text isKindOfClass:[NSString class]]) return;
	[[NSPasteboard generalPasteboard] clearContents];
	[[NSPasteboard generalPasteboard] setString:text forType:NSPasteboardTypeString];
}
- (void)openOutside:(NSMenuItem *)item {
	NSURL *url = item.representedObject;
	if ([url isKindOfClass:[NSURL class]]) [[NSWorkspace sharedWorkspace] openURL:url];
}
// One of the app's pages: the page opens it in a new tab or window, or
// names its address on the server for the browser or the clipboard
// (web/src/lib/newWindowRequests.ts).
- (void)pageLink:(NSMenuItem *)item {
	NSArray *asked = item.representedObject;
	WKWebView *webView = self.webView;
	if (![asked isKindOfClass:[NSArray class]] || asked.count != 2 || !webView) return;
	NSString *action = asked[0];
	NSData *json = [NSJSONSerialization dataWithJSONObject:asked options:0 error:nil];
	NSString *arguments = json ? [[NSString alloc] initWithData:json encoding:NSUTF8StringEncoding] : nil;
	if (!arguments) return;
	NSString *script = [NSString stringWithFormat:@"window.leafenLinkMenu ? window.leafenLinkMenu(...%@) : ''", arguments];
	[webView evaluateJavaScript:script completionHandler:^(id value, NSError *error) {
		if (![value isKindOfClass:[NSString class]] || [value length] == 0) return;
		if ([action isEqualToString:@"copy"]) {
			[[NSPasteboard generalPasteboard] clearContents];
			[[NSPasteboard generalPasteboard] setString:value forType:NSPasteboardTypeString];
		} else if ([action isEqualToString:@"browser"]) {
			NSURL *url = [NSURL URLWithString:value];
			if (url) [[NSWorkspace sharedWorkspace] openURL:url];
		}
	}];
}
@end

static const char leafenTextMenuTargetKey = 0;

static id leafenRead(id object, NSString *key) {
	if (!object) return nil;
	@try {
		return [object valueForKey:key];
	} @catch (NSException *exception) {
		return nil;
	}
}

static NSMenuItem *leafenItem(NSString *title, id target, SEL action, id represented) {
	NSMenuItem *item = [[NSMenuItem alloc] initWithTitle:title action:action keyEquivalent:@""];
	item.target = target;
	item.representedObject = represented;
	return item;
}

// leafenTake takes WebKit's item for a tag or an identifier out of the
// proposed ones, retitled; nil when WebKit proposed none.
static NSMenuItem *leafenTake(NSMutableArray<NSMenuItem *> *proposed, NSInteger tag, NSString *identifier, NSString *title) {
	for (NSMenuItem *item in proposed) {
		if (item.isSeparatorItem) continue;
		if (item.tag == tag || (identifier && [item.identifier isEqualToString:identifier])) {
			[proposed removeObject:item];
			if (title) item.title = title;
			return item;
		}
	}
	return nil;
}

static void leafenKeys(NSMenuItem *item, NSString *key) {
	if (!item) return;
	item.keyEquivalent = key;
	item.keyEquivalentModifierMask = NSEventModifierFlagCommand;
}

static NSMenu *leafenNotionTextMenu(WKWebView *webView, NSMenu *menu, id element, NSString *selection) {
	NSMutableArray<NSMenuItem *> *proposed = [menu.itemArray mutableCopy];
	id hit = leafenRead(element, @"hitTestResult");
	NSURL *link = leafenRead(hit, @"absoluteLinkURL");
	NSURL *image = leafenRead(hit, @"absoluteImageURL");
	NSString *linkLabel = leafenRead(hit, @"linkLabel");
	NSNumber *editableValue = leafenRead(hit, @"contentEditable");
	NSString *chosen = [selection stringByTrimmingCharactersInSet:[NSCharacterSet whitespaceAndNewlineCharacterSet]];
	BOOL spelling = [[NSUserDefaults standardUserDefaults] boolForKey:@"WebContinuousSpellCheckingEnabled"];

	LeafenTextMenuTarget *target = [LeafenTextMenuTarget new];
	target.webView = webView;
	objc_setAssociatedObject(menu, &leafenTextMenuTargetKey, target, OBJC_ASSOCIATION_RETAIN_NONATOMIC);

	NSMutableArray<NSArray<NSMenuItem *> *> *sections = [NSMutableArray array];
	void (^section)(NSArray<NSMenuItem *> *) = ^(NSArray<NSMenuItem *> *items) {
		if (items.count) [sections addObject:items];
	};

	// Corrections, and the misspelt word's own lines.
	NSMutableArray<NSMenuItem *> *guesses = [NSMutableArray array];
	for (NSMenuItem *item in [proposed copy]) {
		if (!item.isSeparatorItem && item.tag == leafenTagSpellingGuess) {
			[proposed removeObject:item];
			[guesses addObject:item];
		}
	}
	NSMenuItem *learn = leafenTake(proposed, leafenTagLearnSpelling, nil, leafenSay(@"Add to dictionary", @"사전에 추가"));
	BOOL misspelt = guesses.count > 0 || learn || leafenTake(proposed, leafenTagNoGuessesFound, nil, nil);
	section(guesses);
	if (misspelt && spelling) {
		NSMutableArray *lines = [NSMutableArray array];
		if (learn) [lines addObject:learn];
		NSMenuItem *disable = leafenItem(leafenSay(@"Disable spell check", @"맞춤법 검사 비활성화"), webView,
			NSSelectorFromString(@"toggleContinuousSpellChecking:"), nil);
		[lines addObject:disable];
		section(lines);
	}

	// A link: one of the app's pages opens in its tabs or windows, any other
	// address with the computer's browser.
	if (link) {
		NSMutableArray *lines = [NSMutableArray array];
		NSString *address = link.absoluteString;
		BOOL own = leafenNewWindowKind((char *)address.UTF8String) == 2;
		SEL pageLink = @selector(pageLink:);
		if (own) {
			[lines addObject:leafenItem(leafenSay(@"Open link in new tab", @"새 탭에서 링크 열기"), target, pageLink, @[@"tab", address])];
			[lines addObject:leafenItem(leafenSay(@"Open link in new window", @"새 창에서 링크 열기"), target, pageLink, @[@"window", address])];
			[lines addObject:leafenItem(leafenSay(@"Open link in browser", @"브라우저에서 링크 열기"), target, pageLink, @[@"browser", address])];
		} else {
			[lines addObject:leafenItem(leafenSay(@"Open link in browser", @"브라우저에서 링크 열기"), target, @selector(openOutside:), link)];
		}
		if ([link.scheme isEqualToString:@"mailto"]) {
			[lines addObject:leafenItem(leafenSay(@"Copy email address", @"이메일 주소 복사"), target, @selector(copyText:),
				linkLabel.length ? linkLabel : link.resourceSpecifier)];
		} else if (own) {
			[lines addObject:leafenItem(leafenSay(@"Copy link", @"링크 복사"), target, pageLink, @[@"copy", address])];
		} else {
			[lines addObject:leafenItem(leafenSay(@"Copy link", @"링크 복사"), target, @selector(copyText:), address)];
		}
		section(lines);
	}

	// A picture.
	if (image) {
		NSMutableArray *lines = [NSMutableArray array];
		NSMenuItem *copyImage = leafenTake(proposed, leafenTagCopyImage, @"WKMenuItemIdentifierCopyImage", leafenSay(@"Copy image", @"이미지 복사"));
		if (copyImage) [lines addObject:copyImage];
		[lines addObject:leafenItem(leafenSay(@"Copy image address", @"이미지 주소 복사"), target, @selector(copyText:), image.absoluteString)];
		section(lines);
	}

	// Cutting, copying and pasting, as WebKit allows them here.
	NSMenuItem *paste = leafenTake(proposed, leafenTagPaste, @"WKMenuItemIdentifierPaste", leafenSay(@"Paste", @"붙여넣기"));
	BOOL editable = editableValue ? editableValue.boolValue : paste != nil;
	if (editable || chosen.length) {
		NSMutableArray *lines = [NSMutableArray array];
		NSMenuItem *cut = leafenTake(proposed, leafenTagCut, @"WKMenuItemIdentifierCut", leafenSay(@"Cut", @"잘라내기"));
		NSMenuItem *copy = leafenTake(proposed, leafenTagCopy, @"WKMenuItemIdentifierCopy", leafenSay(@"Copy", @"복사"));
		leafenKeys(cut, @"x");
		leafenKeys(copy, @"c");
		leafenKeys(paste, @"v");
		for (NSMenuItem *item in @[cut ?: [NSNull null], copy ?: [NSNull null], paste ?: [NSNull null]]) {
			if ([item isKindOfClass:[NSMenuItem class]]) [lines addObject:item];
		}
		section(lines);
	}

	// The chosen words: the system's look-up for one word, and Google.
	if (chosen.length) {
		NSMutableArray *lines = [NSMutableArray array];
		BOOL oneWord = [chosen rangeOfCharacterFromSet:[NSCharacterSet whitespaceAndNewlineCharacterSet]].location == NSNotFound;
		NSMenuItem *lookUp = oneWord ? leafenTake(proposed, leafenTagLookUp, @"WKMenuItemIdentifierLookUp",
			[NSString stringWithFormat:leafenSay(@"Look Up “%@”", @"'%@' 검색"), chosen]) : nil;
		if (lookUp) [lines addObject:lookUp];
		NSMutableCharacterSet *allowed = [[NSCharacterSet URLQueryAllowedCharacterSet] mutableCopy];
		[allowed removeCharactersInString:@"&=+?#"];
		NSString *query = [chosen stringByAddingPercentEncodingWithAllowedCharacters:allowed];
		NSURL *google = [NSURL URLWithString:[@"https://www.google.com/search?q=" stringByAppendingString:query ?: @""]];
		if (google) [lines addObject:leafenItem(leafenSay(@"Search with Google", @"Google로 검색"), target, @selector(openOutside:), google)];
		section(lines);
	}

	if (editable && !spelling) {
		section(@[leafenItem(leafenSay(@"Enable spell check", @"맞춤법 검사 활성화"), webView,
			NSSelectorFromString(@"toggleContinuousSpellChecking:"), nil)]);
	}

	[menu removeAllItems];
	for (NSArray<NSMenuItem *> *items in sections) {
		if (menu.numberOfItems) [menu addItem:[NSMenuItem separatorItem]];
		for (NSMenuItem *item in items) [menu addItem:item];
	}
	return menu;
}

// The delegate's answer to WebKit asking for a page's menu (WKUIDelegatePrivate):
// what is chosen is read from the page first.
static void leafenContextMenu(id delegate, SEL command, WKWebView *webView, NSMenu *menu, id element, id userInfo,
		void (^done)(NSMenu *)) {
	NSString *chosen = @"(() => { const field = document.activeElement;"
		@" if (field && (field.tagName === 'INPUT' || field.tagName === 'TEXTAREA') && typeof field.selectionStart === 'number')"
		@" return field.value.slice(field.selectionStart, field.selectionEnd);"
		@" return String(getSelection()); })()";
	[webView evaluateJavaScript:chosen completionHandler:^(id value, NSError *error) {
		NSString *selection = [value isKindOfClass:[NSString class]] ? value : @"";
		NSMenu *shown = leafenNotionTextMenu(webView, menu, element, selection);
		done(shown.numberOfItems ? shown : nil);
	}];
}

// leafenAnswerContextMenus gives a web view delegate's class the answer;
// WebKit reads which answers a delegate has when it becomes one.
void leafenAnswerContextMenus(Class delegate) {
	if (!delegate) return;
	class_addMethod(delegate, NSSelectorFromString(@"_webView:getContextMenuFromProposedMenu:forElement:userInfo:completionHandler:"),
		(IMP)leafenContextMenu, "v@:@@@@@?");
}

// ---- The page's menu ----
//
// What the page's own menu asks of the app (text_menu_darwin.go), each on the
// main thread: whether the word pressed is misspelt and its corrections, as
// WebKit's underline has it; adding it to the dictionary; cutting, copying,
// pasting and turning spelling on or off in the asking page's web view, as
// its keys and the menu bar's items do; and the Mac's look-up of a word.

static WKWebView *leafenWebViewOf(void *window) {
	NSWindow *native = (__bridge NSWindow *)window;
	id webView = [native respondsToSelector:NSSelectorFromString(@"webView")] ? leafenRead(native, @"webView") : nil;
	return [webView isKindOfClass:[WKWebView class]] ? webView : nil;
}

// Notion's app offers at most five corrections (Chromium's spelling menu).
static const NSUInteger leafenMostGuesses = 5;

// leafenTextMenuSpelling checks the word at [at, at+length) of its text (UTF-16
// units) as WebKit's underline does — in its text, whose language the Mac
// tells from all of it — and answers as JSON {checking, misspelt, guesses}.
char *leafenTextMenuSpelling(const char *context, long at, long length) {
	NSString *text = context ? [NSString stringWithUTF8String:context] : @"";
	BOOL checking = [[NSUserDefaults standardUserDefaults] boolForKey:@"WebContinuousSpellCheckingEnabled"];
	BOOL misspelt = NO;
	NSArray<NSString *> *guesses = @[];
	if (checking && at >= 0 && length > 0 && (NSUInteger)(at + length) <= text.length) {
		NSSpellChecker *checker = [NSSpellChecker sharedSpellChecker];
		NSOrthography *orthography = nil;
		NSArray<NSTextCheckingResult *> *found = [checker checkString:text range:NSMakeRange(at, length)
			types:NSTextCheckingTypeSpelling options:nil inSpellDocumentWithTag:0 orthography:&orthography wordCount:NULL];
		for (NSTextCheckingResult *result in found) {
			if (result.resultType != NSTextCheckingTypeSpelling) continue;
			misspelt = YES;
			NSString *language = [checker languageForWordRange:result.range inString:text orthography:orthography];
			guesses = [checker guessesForWordRange:result.range inString:text language:language inSpellDocumentWithTag:0] ?: @[];
			if (guesses.count > leafenMostGuesses) guesses = [guesses subarrayWithRange:NSMakeRange(0, leafenMostGuesses)];
			break;
		}
	}
	NSData *json = [NSJSONSerialization dataWithJSONObject:@{@"checking": @(checking), @"misspelt": @(misspelt), @"guesses": guesses}
		options:0 error:nil];
	NSString *answer = json ? [[NSString alloc] initWithData:json encoding:NSUTF8StringEncoding] : @"{}";
	return strdup(answer.UTF8String);
}

void leafenTextMenuLearn(const char *word) {
	NSString *text = word ? [NSString stringWithUTF8String:word] : @"";
	if (text.length) [[NSSpellChecker sharedSpellChecker] learnWord:text];
}

// leafenTextMenuEdit: 0 cuts, 1 copies, 2 pastes, 3 turns spelling on or off,
// 4 chooses all.
void leafenTextMenuEdit(void *window, int action) {
	WKWebView *webView = leafenWebViewOf(window);
	NSArray<NSString *> *commands = @[@"cut:", @"copy:", @"paste:", @"toggleContinuousSpellChecking:", @"selectAll:"];
	if (!webView || action < 0 || action >= (int)commands.count) return;
	SEL command = NSSelectorFromString(commands[action]);
	if ([webView respondsToSelector:command]) [NSApp sendAction:command to:webView from:nil];
}

// leafenTextMenuLookUp shows the Mac's look-up for a word whose text starts
// at x on the baseline y of the page, in its font size (CSS pixels).
void leafenTextMenuLookUp(void *window, const char *word, double x, double y, double size) {
	WKWebView *webView = leafenWebViewOf(window);
	NSString *text = word ? [NSString stringWithUTF8String:word] : @"";
	if (!webView || !text.length) return;
	CGFloat zoom = webView.magnification;
	if (@available(macOS 11.0, *)) zoom *= webView.pageZoom;
	NSPoint point = NSMakePoint(x * zoom, y * zoom);
	if (!webView.isFlipped) point.y = NSHeight(webView.bounds) - point.y;
	NSAttributedString *shown = [[NSAttributedString alloc] initWithString:text
		attributes:@{NSFontAttributeName: [NSFont systemFontOfSize:(size > 0 ? size : 16) * zoom]}];
	[webView showDefinitionForAttributedString:shown atPoint:point];
}
