//go:build darwin && !ios

#import <AppKit/AppKit.h>
#include <stdbool.h>

// ---- The page's copied HTML ----
//
// WebKit puts the HTML a page copies (clipboardData.setData) on the general
// pasteboard only after re-serializing it in a page of its own, which writes
// the computed text look inline on the outer box — `color: rgb(255, 255,
// 255)` under the dark theme (2026-11-16, the test app) — so a copy pasted
// in a light mail or document read white on white. Notion's app (Chromium)
// puts the page's HTML as the page wrote it. leafenKeepCopiedHTML puts the
// page's own HTML back in place of WebKit's, keeping every other entry
// WebKit wrote (the plain text, and its own record of the page's data, which
// a paste in the app reads): only while the pasteboard still holds that copy
// — one item whose plain text is the copy's — so a later copy is never undone.

static NSArray<NSPasteboardType> *leafenStyledTypes(void) {
	// What WebKit derives from the HTML it re-serialized: a reader takes a
	// web archive before the HTML.
	return @[NSPasteboardTypeHTML, @"Apple HTML pasteboard type", @"com.apple.webarchive", @"Apple Web Archive pasteboard type"];
}

bool leafenKeepCopiedHTML(const char *text, const char *html) {
	NSString *plain = text ? [NSString stringWithUTF8String:text] : @"";
	NSString *page = html ? [NSString stringWithUTF8String:html] : nil;
	if (!page.length) return false;
	NSPasteboard *board = [NSPasteboard generalPasteboard];
	NSArray<NSPasteboardItem *> *items = board.pasteboardItems;
	if (items.count != 1) return false;
	NSPasteboardItem *item = items.firstObject;
	if (![item.types containsObject:NSPasteboardTypeHTML]) return false;
	NSString *shown = [item stringForType:NSPasteboardTypeString] ?: @"";
	if (![shown isEqualToString:plain]) return false;
	NSPasteboardItem *kept = [NSPasteboardItem new];
	NSArray<NSPasteboardType> *styled = leafenStyledTypes();
	for (NSPasteboardType type in item.types) {
		if ([styled containsObject:type]) continue;
		NSData *data = [item dataForType:type];
		if (data) [kept setData:data forType:type];
	}
	[kept setString:page forType:NSPasteboardTypeHTML];
	[board clearContents];
	return [board writeObjects:@[kept]];
}
