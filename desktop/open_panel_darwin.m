//go:build darwin && !ios

#import <AppKit/AppKit.h>
#import <WebKit/WebKit.h>
#import <UniformTypeIdentifiers/UniformTypeIdentifiers.h>
#import <objc/runtime.h>

// ---- Choosing files ----
//
// A page's <input type="file" accept="…"> names the files it takes. Wails'
// open panel (v3.0.0-beta.27) let every file be chosen: 가져오기 › Text &
// Markdown offered a .zip or a .csv, which Notion's app dims (2026-11-13).
// The panel here does what Notion's app (Chromium) does: a file whose
// extension the input doesn't take is dimmed, folders stay open, and
// 옵션 보기 shows a 형식: menu — what the input takes, named as Chromium
// names it, then 모든 파일, which lets every file be chosen. The extensions
// are WebKit's own reading of the accept list (text/html becomes html, htm,
// shtml, jhtml; image/* every image extension the Mac knows). An input
// without accept, one whose types the Mac doesn't know, or one that picks
// folders lets everything be chosen, with no menu.

bool leafenMenusKorean(void);

// WebKit's reading of the accept list (WKOpenPanelParametersPrivate).
@interface WKOpenPanelParameters (LeafenWebKit)
- (NSArray<NSString *> *)_acceptedMIMETypes;
- (NSArray<NSString *> *)_acceptedFileExtensions;
- (NSArray<NSString *> *)_allowedFileExtensions;
@end

static NSString *leafenPanelSay(NSString *english, NSString *korean) {
	return leafenMenusKorean() ? korean : english;
}

// What the panel lets be chosen: files with these extensions (lower case,
// no dot) and folders, or everything once 모든 파일 is chosen.
@interface LeafenOpenPanelTypes : NSObject <NSOpenSavePanelDelegate>
@property(nonatomic, copy) NSSet<NSString *> *extensions;
@property(nonatomic) BOOL allFiles;
@property(nonatomic, weak) NSOpenPanel *panel;
@end

@implementation LeafenOpenPanelTypes
- (BOOL)panel:(id)sender shouldEnableURL:(NSURL *)url {
	if (self.allFiles) return YES;
	NSNumber *folder = nil, *package = nil;
	[url getResourceValue:&folder forKey:NSURLIsDirectoryKey error:nil];
	[url getResourceValue:&package forKey:NSURLIsPackageKey error:nil];
	if (folder.boolValue && !package.boolValue) return YES;
	return [self.extensions containsObject:url.pathExtension.lowercaseString];
}

- (void)choseFormat:(NSPopUpButton *)menu {
	self.allFiles = menu.indexOfSelectedItem == menu.numberOfItems - 1;
	[self.panel validateVisibleColumns];
}
@end

// leafenAcceptedExtensions is the accept list's extensions, as WebKit reads it.
static NSArray<NSString *> *leafenAcceptedExtensions(WKOpenPanelParameters *parameters) {
	NSMutableOrderedSet<NSString *> *extensions = [NSMutableOrderedSet orderedSet];
	if ([parameters respondsToSelector:@selector(_allowedFileExtensions)]) {
		for (NSString *extension in parameters._allowedFileExtensions) {
			if (extension.length) [extensions addObject:extension.lowercaseString];
		}
	} else if ([parameters respondsToSelector:@selector(_acceptedFileExtensions)]) {
		for (NSString *extension in parameters._acceptedFileExtensions) {
			NSString *bare = [extension hasPrefix:@"."] ? [extension substringFromIndex:1] : extension;
			if (bare.length) [extensions addObject:bare.lowercaseString];
		}
	}
	return extensions.array;
}

// leafenFormatName names what the input takes as Chromium's panel does
// (FileSelectHelper): image/*, audio/* or video/* by their kind; several
// accepted types, or one type with several extensions, "사용자 지정 파일";
// a single extension by the Mac's name for it, or "MD 파일(.md)" when it
// has none.
static NSString *leafenFormatName(WKOpenPanelParameters *parameters, NSArray<NSString *> *extensions) {
	NSUInteger valid = 0;
	NSString *kind = nil;
	NSArray<NSString *> *types = [parameters respondsToSelector:@selector(_acceptedMIMETypes)] ? parameters._acceptedMIMETypes : @[];
	for (NSString *type in types) {
		NSString *lower = type.lowercaseString;
		if ([lower isEqualToString:@"image/*"]) kind = leafenPanelSay(@"Image Files", @"이미지 파일");
		else if ([lower isEqualToString:@"audio/*"]) kind = leafenPanelSay(@"Audio Files", @"오디오 파일");
		else if ([lower isEqualToString:@"video/*"]) kind = leafenPanelSay(@"Video Files", @"동영상 파일");
		NSArray *known = [UTType typesWithTag:lower tagClass:UTTagClassMIMEType conformingToType:nil];
		BOOL named = NO;
		for (UTType *each in known) named = named || each.tags[UTTagClassFilenameExtension].count > 0;
		if (named || [lower hasSuffix:@"/*"]) valid++;
	}
	if ([parameters respondsToSelector:@selector(_acceptedFileExtensions)]) valid += parameters._acceptedFileExtensions.count;
	if (valid > 1 || (valid == 1 && !kind && extensions.count > 1)) return leafenPanelSay(@"Custom Files", @"사용자 지정 파일");
	if (kind) return kind;
	NSString *extension = extensions.firstObject;
	NSString *described = [UTType typeWithFilenameExtension:extension].localizedDescription;
	if (described.length) return described;
	return [NSString stringWithFormat:leafenPanelSay(@"%@ File (.%@)", @"%@ 파일(.%@)"), extension.uppercaseString, extension];
}

// leafenFormatMenu is the panel's 형식: row: what the input takes, then 모든 파일.
static NSView *leafenFormatMenu(LeafenOpenPanelTypes *types, NSString *name) {
	NSTextField *label = [NSTextField labelWithString:leafenPanelSay(@"Format:", @"형식:")];
	NSPopUpButton *menu = [[NSPopUpButton alloc] initWithFrame:NSZeroRect pullsDown:NO];
	[menu addItemsWithTitles:@[name, leafenPanelSay(@"All Files", @"모든 파일")]];
	[menu selectItemAtIndex:0];
	menu.target = types;
	menu.action = @selector(choseFormat:);
	NSStackView *row = [NSStackView stackViewWithViews:@[label, menu]];
	row.orientation = NSUserInterfaceLayoutOrientationHorizontal;
	row.alignment = NSLayoutAttributeFirstBaseline;
	row.spacing = 8;
	row.edgeInsets = NSEdgeInsetsMake(12, 20, 12, 20);
	NSView *holder = [[NSView alloc] initWithFrame:NSZeroRect];
	row.translatesAutoresizingMaskIntoConstraints = NO;
	[holder addSubview:row];
	[NSLayoutConstraint activateConstraints:@[
		[row.topAnchor constraintEqualToAnchor:holder.topAnchor],
		[row.bottomAnchor constraintEqualToAnchor:holder.bottomAnchor],
		[row.centerXAnchor constraintEqualToAnchor:holder.centerXAnchor],
		[row.leadingAnchor constraintGreaterThanOrEqualToAnchor:holder.leadingAnchor],
	]];
	holder.frame = NSMakeRect(0, 0, MAX(row.fittingSize.width, 360), row.fittingSize.height);
	return holder;
}

// The delegate's answer to a file input's press (WKUIDelegate): the panel
// over the window, with the page's choices.
static void leafenRunOpenPanel(id delegate, SEL command, WKWebView *webView, WKOpenPanelParameters *parameters,
		WKFrameInfo *frame, void (^done)(NSArray<NSURL *> *)) {
	NSOpenPanel *panel = [NSOpenPanel openPanel];
	panel.allowsMultipleSelection = parameters.allowsMultipleSelection;
	panel.canChooseDirectories = parameters.allowsDirectories;
	LeafenOpenPanelTypes *types = nil;
	NSArray<NSString *> *extensions = parameters.allowsDirectories ? @[] : leafenAcceptedExtensions(parameters);
	if (extensions.count) {
		types = [LeafenOpenPanelTypes new];
		types.extensions = [NSSet setWithArray:extensions];
		types.panel = panel;
		panel.delegate = types;
		panel.accessoryView = leafenFormatMenu(types, leafenFormatName(parameters, extensions));
	}
	void (^chosen)(NSInteger) = ^(NSInteger result) {
		// The panel holds its delegate weakly: this keeps it until the panel closes.
		types.panel = nil;
		done(result == NSModalResponseOK ? panel.URLs : nil);
	};
	if (webView.window) {
		[panel beginSheetModalForWindow:webView.window completionHandler:chosen];
	} else {
		chosen([panel runModal]);
	}
}

// leafenAnswerOpenPanels gives a web view delegate's class the answer, in
// place of Wails' own; WebKit reads which answers a delegate has when it
// becomes one.
void leafenAnswerOpenPanels(Class delegate) {
	if (!delegate) return;
	class_replaceMethod(delegate, @selector(webView:runOpenPanelWithParameters:initiatedByFrame:completionHandler:),
		(IMP)leafenRunOpenPanel, "v@:@@@@?");
}
