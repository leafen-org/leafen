//go:build ios

#import <UIKit/UIKit.h>
#import <UniformTypeIdentifiers/UniformTypeIdentifiers.h>

extern void leafenBackupFolderChosen(char *identifier, char *name, char *bookmark);

// The Files picker for one folder; what it picked goes back to Go as the
// folder's name and a security-scoped bookmark, or as nothing when it was
// cancelled or swiped away.
@interface LeafenBackupFolderPicker : NSObject <UIDocumentPickerDelegate, UIAdaptivePresentationControllerDelegate>
@property(nonatomic, copy) NSString *identifier;
@property(nonatomic) BOOL finished;
@end

static NSMutableSet<LeafenBackupFolderPicker *> *leafenBackupFolderPickers;
// The folders open while backups use them, by path: each open adds one use,
// each close takes one away, and the folder's access goes with the last.
static NSMutableDictionary<NSString *, NSURL *> *leafenOpenBackupFolders;
static NSMutableDictionary<NSString *, NSNumber *> *leafenOpenBackupFolderUses;

@implementation LeafenBackupFolderPicker
- (void)finishWithName:(NSString *)name bookmark:(NSString *)bookmark {
	if (self.finished) return;
	self.finished = YES;
	leafenBackupFolderChosen((char *)self.identifier.UTF8String, (char *)name.UTF8String, (char *)bookmark.UTF8String);
	[leafenBackupFolderPickers removeObject:self];
}

- (void)documentPicker:(UIDocumentPickerViewController *)controller didPickDocumentsAtURLs:(NSArray<NSURL *> *)urls {
	NSURL *url = urls.firstObject;
	if (url == nil) {
		[self finishWithName:@"" bookmark:@""];
		return;
	}
	BOOL accessing = [url startAccessingSecurityScopedResource];
	NSError *error = nil;
	NSData *bookmark = [url bookmarkDataWithOptions:0 includingResourceValuesForKeys:nil relativeToURL:nil error:&error];
	if (accessing) [url stopAccessingSecurityScopedResource];
	if (bookmark == nil) {
		[self finishWithName:@"" bookmark:@""];
		return;
	}
	NSString *encoded = [bookmark base64EncodedStringWithOptions:0];
	encoded = [[encoded stringByReplacingOccurrencesOfString:@"+" withString:@"-"] stringByReplacingOccurrencesOfString:@"/" withString:@"_"];
	encoded = [encoded stringByReplacingOccurrencesOfString:@"=" withString:@""];
	[self finishWithName:(url.lastPathComponent ?: @"") bookmark:encoded];
}

- (void)documentPickerWasCancelled:(UIDocumentPickerViewController *)controller {
	[self finishWithName:@"" bookmark:@""];
}

- (void)presentationControllerDidDismiss:(UIPresentationController *)presentationController {
	[self finishWithName:@"" bookmark:@""];
}
@end

static UIViewController *leafenTopViewController(void) {
	UIWindow *window = nil;
	for (UIScene *scene in [UIApplication sharedApplication].connectedScenes) {
		if (![scene isKindOfClass:[UIWindowScene class]]) continue;
		for (UIWindow *candidate in ((UIWindowScene *)scene).windows) {
			if (candidate.isKeyWindow || window == nil) window = candidate;
		}
	}
	UIViewController *top = window.rootViewController;
	while (top.presentedViewController) top = top.presentedViewController;
	return top;
}

void leafenChooseBackupFolder(const char *identifier) {
	NSString *ident = [NSString stringWithUTF8String:identifier];
	dispatch_async(dispatch_get_main_queue(), ^{
		if (leafenBackupFolderPickers == nil) leafenBackupFolderPickers = [NSMutableSet new];
		LeafenBackupFolderPicker *picker = [LeafenBackupFolderPicker new];
		picker.identifier = ident;
		UIViewController *top = leafenTopViewController();
		if (top == nil) {
			leafenBackupFolderChosen((char *)ident.UTF8String, "", "");
			return;
		}
		[leafenBackupFolderPickers addObject:picker];
		UIDocumentPickerViewController *controller =
			[[UIDocumentPickerViewController alloc] initForOpeningContentTypes:@[UTTypeFolder] asCopy:NO];
		controller.delegate = picker;
		controller.allowsMultipleSelection = NO;
		[top presentViewController:controller animated:YES completion:nil];
		controller.presentationController.delegate = picker;
	});
}

// Opens a bookmark for the folder's path while a backup uses it; nil when it
// no longer resolves.
char *leafenOpenBackupFolder(const char *bookmark) {
	NSString *encoded = [[[NSString stringWithUTF8String:bookmark]
		stringByReplacingOccurrencesOfString:@"-" withString:@"+"] stringByReplacingOccurrencesOfString:@"_" withString:@"/"];
	while (encoded.length % 4 != 0) encoded = [encoded stringByAppendingString:@"="];
	NSData *data = [[NSData alloc] initWithBase64EncodedString:encoded options:0];
	if (data == nil) return NULL;
	BOOL stale = NO;
	NSError *error = nil;
	NSURL *url = [NSURL URLByResolvingBookmarkData:data options:0 relativeToURL:nil bookmarkDataIsStale:&stale error:&error];
	if (url == nil) return NULL;
	@synchronized ([LeafenBackupFolderPicker class]) {
		if (leafenOpenBackupFolders == nil) {
			leafenOpenBackupFolders = [NSMutableDictionary new];
			leafenOpenBackupFolderUses = [NSMutableDictionary new];
		}
		NSString *key = url.path;
		NSInteger uses = leafenOpenBackupFolderUses[key].integerValue;
		if (uses == 0) {
			if (![url startAccessingSecurityScopedResource]) return NULL;
			leafenOpenBackupFolders[key] = url;
		}
		leafenOpenBackupFolderUses[key] = @(uses + 1);
		return strdup(key.UTF8String);
	}
}

void leafenCloseBackupFolder(const char *path) {
	NSString *key = [NSString stringWithUTF8String:path];
	@synchronized ([LeafenBackupFolderPicker class]) {
		NSInteger uses = leafenOpenBackupFolderUses[key].integerValue;
		if (uses > 1) {
			leafenOpenBackupFolderUses[key] = @(uses - 1);
			return;
		}
		NSURL *url = leafenOpenBackupFolders[key];
		[leafenOpenBackupFolders removeObjectForKey:key];
		[leafenOpenBackupFolderUses removeObjectForKey:key];
		[url stopAccessingSecurityScopedResource];
	}
}
